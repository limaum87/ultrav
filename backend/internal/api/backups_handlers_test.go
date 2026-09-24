package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ultrav/ultrav/backend/internal/hypervisor/mock"
)

// testBackupServer builds a server whose mock provider persists backups in a
// temporary directory.
func testBackupServer(t *testing.T) *Server {
	return NewServer(mock.New(t.TempDir()), testIsoStore(t), nil, nil, nil, testLogger())
}

// do issues an arbitrary request and decodes the JSON body (if any).
func do(t *testing.T, s *Server, method, path string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil) //nolint:bodyclose
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res, body
}

// postBackupTask posts a backup/restore request, asserts 202 and waits for
// the task to reach a terminal state, which it returns.
func postBackupTask(t *testing.T, s *Server, path, body string) map[string]any {
	t.Helper()
	var res *http.Response
	var task map[string]any
	if body == "" {
		res, task = do(t, s, http.MethodPost, path)
	} else {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		res = rec.Result()
		task = map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&task)
	}
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for %s, got %d: %v", path, res.StatusCode, task)
	}
	id, _ := task["id"].(string)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, task = get(t, s, "/api/v1/tasks/"+id)
		switch task["status"] {
		case "succeeded", "failed", "cancelled":
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("backup task %s did not finish in time", id)
	return nil
}

// backupPoint fetches a single backup point from the VM listing.
func backupPoint(t *testing.T, s *Server, vmID, id string) map[string]any {
	t.Helper()
	_, body := get(t, s, "/api/v1/vms/"+vmID+"/backups")
	items, _ := body["items"].([]any)
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["id"] == id {
			return m
		}
	}
	t.Fatalf("backup point %s not found in listing", id)
	return nil
}

func TestBackupLifecycle(t *testing.T) {
	s := testBackupServer(t)

	// Unknown VM: 404 with the public code (sync pre-check).
	res, body := post(t, s, "/api/v1/vms/nope/backups")
	if res.StatusCode != http.StatusNotFound || body["error"].(map[string]any)["code"] != "VM_NOT_FOUND" {
		t.Fatalf("backup of unknown VM: %d %v", res.StatusCode, body)
	}

	// Full backup of a stopped VM: 202 + task that succeeds.
	task := postBackupTask(t, s, "/api/v1/vms/monitoring01/backups", "")
	if task["status"] != "succeeded" || task["type"] != "backup-create" {
		t.Fatalf("create backup: %v", task)
	}
	id, _ := task["resourceId"].(string)
	if !strings.HasPrefix(id, "bk-") {
		t.Fatalf("unexpected backup id %q", id)
	}
	backup := backupPoint(t, s, "monitoring01", id)
	if backup["type"] != "full" || backup["vmId"] != "monitoring01" {
		t.Fatalf("unexpected backup body %v", backup)
	}

	// Listing shows the point.
	_, body = get(t, s, "/api/v1/vms/monitoring01/backups")
	if body["total"].(float64) != 1 {
		t.Fatalf("expected 1 backup, got %v", body["total"])
	}

	// Config export.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vms/monitoring01/config-export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<domain") {
		t.Fatalf("config-export: %d %s", rec.Code, rec.Body.String())
	}

	// Restore while stopped: task succeeds.
	task = postBackupTask(t, s, "/api/v1/backups/"+id+"/restore", "")
	if task["status"] != "succeeded" || task["type"] != "backup-restore" {
		t.Fatalf("restore stopped VM: %v", task)
	}

	// Start the VM; restoring now fails inside the task (BACKUP_INVALID_STATE).
	if res, _ = post(t, s, "/api/v1/vms/monitoring01/start"); res.StatusCode != http.StatusOK {
		t.Fatalf("start: %d", res.StatusCode)
	}
	task = postBackupTask(t, s, "/api/v1/backups/"+id+"/restore", "")
	if task["status"] != "failed" {
		t.Fatalf("restore running VM: %v", task)
	}

	// Delete, then the point is gone (404 BACKUP_NOT_FOUND).
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/backups/"+id, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	task = postBackupTask(t, s, "/api/v1/backups/"+id+"/restore", "")
	if task["status"] != "failed" || !strings.Contains(task["error"].(string), "not found") {
		t.Fatalf("restore after delete: %v", task)
	}
}

func TestBackupInvalidIdentifiers(t *testing.T) {
	s := testBackupServer(t)
	res, body := post(t, s, "/api/v1/backups/not-a-backup-id/restore")
	if res.StatusCode != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "VALIDATION_ERROR" {
		t.Fatalf("invalid id: %d %v", res.StatusCode, body)
	}
	res, _ = do(t, s, http.MethodDelete, "/api/v1/backups/not-a-backup-id")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid id delete: %d", res.StatusCode)
	}
}

func TestBackupIncrementalLifecycle(t *testing.T) {
	s := testBackupServer(t)

	// Auto on a stopped VM: offline backups are always full.
	task := postBackupTask(t, s, "/api/v1/vms/monitoring01/backups", "")
	if task["status"] != "succeeded" {
		t.Fatalf("offline auto backup: %v", task)
	}
	fullID := task["resourceId"].(string)
	if pt := backupPoint(t, s, "monitoring01", fullID); pt["type"] != "full" {
		t.Fatalf("offline auto backup type: %v", pt["type"])
	}

	// Forced incremental without a running chain: the task fails.
	task = postBackupTask(t, s, "/api/v1/vms/monitoring01/backups", `{"type":"incremental"}`)
	if task["status"] != "failed" {
		t.Fatalf("forced incremental offline: %v", task)
	}

	// Start the VM. The first online backup is still auto=full (the offline
	// point has no dirty-bitmap anchor), but it now anchors a checkpoint.
	if res, _ := post(t, s, "/api/v1/vms/monitoring01/start"); res.StatusCode != http.StatusOK {
		t.Fatalf("start: %d", res.StatusCode)
	}
	task = postBackupTask(t, s, "/api/v1/vms/monitoring01/backups", "")
	if task["status"] != "succeeded" {
		t.Fatalf("first online auto backup: %v", task)
	}
	onlineFullID := task["resourceId"].(string)
	if pt := backupPoint(t, s, "monitoring01", onlineFullID); pt["type"] != "full" {
		t.Fatalf("first online auto backup type: %v", pt["type"])
	}

	// The next auto backup is incremental, chained to the online full.
	task = postBackupTask(t, s, "/api/v1/vms/monitoring01/backups", "")
	if task["status"] != "succeeded" {
		t.Fatalf("auto incremental: %v", task)
	}
	incrID := task["resourceId"].(string)
	if pt := backupPoint(t, s, "monitoring01", incrID); pt["type"] != "incremental" || pt["parentId"] != onlineFullID {
		t.Fatalf("expected incremental chained to %s, got %v", onlineFullID, pt)
	}

	// Force another full: resets the chain.
	task = postBackupTask(t, s, "/api/v1/vms/monitoring01/backups", `{"type":"full"}`)
	if task["status"] != "succeeded" {
		t.Fatalf("forced full: %v", task)
	}

	// Restore of the incremental point requires its chain — delete the full
	// ancestor first and the restore must 409.
	if res, _ := do(t, s, http.MethodDelete, "/api/v1/backups/"+fullID); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete full: %d", res.StatusCode)
	}
	task = postBackupTask(t, s, "/api/v1/backups/"+incrID+"/restore", "")
	if task["status"] != "failed" {
		t.Fatalf("restore broken chain: %v", task)
	}
}
