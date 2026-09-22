package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/hypervisor/mock"
)

// testBackupServer builds a server whose mock provider persists backups in a
// temporary directory.
func testBackupServer(t *testing.T) *Server {
	return NewServer(mock.New(t.TempDir()), testIsoStore(t), nil, nil, testLogger())
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

func TestBackupLifecycle(t *testing.T) {
	s := testBackupServer(t)

	// Unknown VM: 404 with the public code.
	res, body := post(t, s, "/api/v1/vms/nope/backups")
	if res.StatusCode != http.StatusNotFound || body["error"].(map[string]any)["code"] != "VM_NOT_FOUND" {
		t.Fatalf("backup of unknown VM: %d %v", res.StatusCode, body)
	}

	// Full backup of a stopped VM.
	res, body = post(t, s, "/api/v1/vms/monitoring01/backups")
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create backup: %d %v", res.StatusCode, body)
	}
	backup := body
	id, _ := backup["id"].(string)
	if !strings.HasPrefix(id, "bk-") {
		t.Fatalf("unexpected backup id %q", id)
	}
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

	// Restore while stopped: OK.
	res, _ = post(t, s, "/api/v1/backups/"+id+"/restore")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("restore stopped VM: %d", res.StatusCode)
	}

	// Start the VM; restore must now conflict with BACKUP_INVALID_STATE.
	if res, _ = post(t, s, "/api/v1/vms/monitoring01/start"); res.StatusCode != http.StatusOK {
		t.Fatalf("start: %d", res.StatusCode)
	}
	res, body = post(t, s, "/api/v1/backups/"+id+"/restore")
	if res.StatusCode != http.StatusConflict || body["error"].(map[string]any)["code"] != "BACKUP_INVALID_STATE" {
		t.Fatalf("restore running VM: %d %v", res.StatusCode, body)
	}

	// Delete, then the point is gone (404 BACKUP_NOT_FOUND).
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/backups/"+id, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	res, body = post(t, s, "/api/v1/backups/"+id+"/restore")
	if res.StatusCode != http.StatusNotFound || body["error"].(map[string]any)["code"] != "BACKUP_NOT_FOUND" {
		t.Fatalf("restore after delete: %d %v", res.StatusCode, body)
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
