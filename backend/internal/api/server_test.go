package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/hypervisor/mock"
)

func testServer(t *testing.T) *Server {
	return NewServer(mock.New(), testIsoStore(t), testLogger())
}

func get(t *testing.T, s *Server, path string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res, body
}

func post(t *testing.T, s *Server, path string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res, body
}

func TestHealthAndReadiness(t *testing.T) {
	s := testServer(t)

	res, body := get(t, s, "/api/v1/health")
	if res.StatusCode != 200 || body["status"] != "ok" {
		t.Errorf("health: %d %v", res.StatusCode, body)
	}

	res, body = get(t, s, "/api/v1/ready")
	if res.StatusCode != 200 || body["status"] != "ready" || body["hypervisor"] != "ready" {
		t.Errorf("ready: %d %v", res.StatusCode, body)
	}
}

func TestHostEndpoint(t *testing.T) {
	s := testServer(t)
	res, body := get(t, s, "/api/v1/host")
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if body["hostname"] != "kvm01" {
		t.Errorf("hostname = %v", body["hostname"])
	}
	if body["qemuVersion"] == nil || body["libvirtVersion"] == nil {
		t.Errorf("missing versions: %v", body)
	}
	if _, ok := body["storageTotalBytes"]; !ok {
		t.Errorf("missing storageTotalBytes")
	}
}

func TestListVMsShape(t *testing.T) {
	s := testServer(t)
	res, body := get(t, s, "/api/v1/vms")
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items missing: %v", body)
	}
	if body["total"] != float64(len(items)) {
		t.Errorf("total mismatch: %v vs %d", body["total"], len(items))
	}
}

func TestVMOperationsThroughHTTP(t *testing.T) {
	s := testServer(t)

	// 404 with stable error envelope
	res, body := get(t, s, "/api/v1/vms/nope")
	if res.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "VM_NOT_FOUND" || errObj["requestId"] == nil {
		t.Errorf("bad error envelope: %v", body)
	}

	// start a stopped VM
	res, body = post(t, s, "/api/v1/vms/monitoring01/start")
	if res.StatusCode != 200 || body["state"] != "running" {
		t.Fatalf("start: %d %v", res.StatusCode, body)
	}

	// starting again -> 409
	res, body = post(t, s, "/api/v1/vms/monitoring01/start")
	if res.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", res.StatusCode)
	}
	if body["error"].(map[string]any)["code"] != "VM_INVALID_STATE" {
		t.Errorf("bad error code: %v", body)
	}

	// force stop
	res, body = post(t, s, "/api/v1/vms/monitoring01/stop")
	if res.StatusCode != 200 || body["state"] != "stopped" {
		t.Fatalf("force stop: %d %v", res.StatusCode, body)
	}

	// force stop again -> 409
	res, _ = post(t, s, "/api/v1/vms/monitoring01/stop")
	if res.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", res.StatusCode)
	}
}

func postJSON(t *testing.T, s *Server, path string, payload string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res, body
}

func TestCreateVM(t *testing.T) {
	s := testServer(t)

	valid := `{"name":"app01","vcpus":2,"memoryBytes":4294967296,"disk":{"poolId":"default","sizeBytes":42949672960,"format":"qcow2"},"networkId":"default","start":false}`

	// invalid body -> 400
	res, body := postJSON(t, s, "/api/v1/vms", `{"name":"bad name!","vcpus":2,"memoryBytes":4294967296,"disk":{"poolId":"d","sizeBytes":1}}`)
	if res.StatusCode != 400 || body["error"].(map[string]any)["code"] != "VALIDATION_ERROR" {
		t.Fatalf("expected 400 VALIDATION_ERROR, got %d %v", res.StatusCode, body)
	}

	// create -> 201, stopped, visible in list
	res, body = postJSON(t, s, "/api/v1/vms", valid)
	if res.StatusCode != 201 || body["id"] != "app01" || body["state"] != "stopped" {
		t.Fatalf("create: %d %v", res.StatusCode, body)
	}
	_, body = get(t, s, "/api/v1/vms/app01")
	if body["id"] != "app01" {
		t.Fatalf("created VM not listed")
	}

	// duplicate -> 409 VM_ALREADY_EXISTS
	res, body = postJSON(t, s, "/api/v1/vms", valid)
	if res.StatusCode != 409 || body["error"].(map[string]any)["code"] != "VM_ALREADY_EXISTS" {
		t.Fatalf("expected 409 VM_ALREADY_EXISTS, got %d %v", res.StatusCode, body)
	}
}

func TestVMIDValidation(t *testing.T) {
	s := testServer(t)
	res, body := get(t, s, "/api/v1/vms/..%2Fetc%2Fpasswd")
	if res.StatusCode != 400 {
		t.Fatalf("expected 400 for traversal id, got %d", res.StatusCode)
	}
	if body["error"].(map[string]any)["code"] != "VALIDATION_ERROR" {
		t.Errorf("bad code: %v", body)
	}
}

func TestRequestIDAndSecurityHeaders(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/host", nil)
	req.Header.Set("X-Request-Id", "req_my-correlation-id")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "req_my-correlation-id" {
		t.Errorf("X-Request-Id = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
}

func TestOpenAPIAndDocs(t *testing.T) {
	s := testServer(t)

	res, body := get(t, s, "/openapi.json")
	if res.StatusCode != 200 {
		t.Fatalf("openapi.json: %d", res.StatusCode)
	}
	info, _ := body["info"].(map[string]any)
	if info == nil || info["title"] != "UltraV API" {
		t.Errorf("bad openapi.json: %v", body["info"])
	}
	paths, _ := body["paths"].(map[string]any)
	for _, p := range []string{"/host", "/capabilities", "/vms", "/vms/{id}", "/vms/{id}/start", "/vms/{id}/shutdown", "/vms/{id}/reboot", "/vms/{id}/stop", "/health", "/ready"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("path %s missing from served openapi.json", p)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "swagger-ui") {
		t.Errorf("docs page wrong: %d", rec.Code)
	}
}

func TestUnknownRoute(t *testing.T) {
	s := testServer(t)
	res, body := get(t, s, "/api/v1/unknown")
	if res.StatusCode != 404 || body["error"].(map[string]any)["code"] != "NOT_FOUND" {
		t.Errorf("unknown route: %d %v", res.StatusCode, body)
	}

	// No generic execute endpoint may ever exist.
	res, _ = get(t, s, "/api/v1/execute")
	if res.StatusCode != 404 {
		t.Errorf("execute endpoint must not exist, got %d", res.StatusCode)
	}
}

func TestStoragePoolsAndNetworks(t *testing.T) {
	s := testServer(t)

	// pools list
	res, body := get(t, s, "/api/v1/storage/pools")
	if res.StatusCode != 200 {
		t.Fatalf("pools: %d", res.StatusCode)
	}
	if body["total"] != float64(3) {
		t.Errorf("expected 3 pools, got %v", body["total"])
	}

	// pool detail + 404
	res, body = get(t, s, "/api/v1/storage/pools/default")
	if res.StatusCode != 200 || body["type"] != "dir" {
		t.Errorf("pool default: %d %v", res.StatusCode, body["type"])
	}
	res, _ = get(t, s, "/api/v1/storage/pools/nope")
	if res.StatusCode != 404 {
		t.Errorf("pool 404: %d", res.StatusCode)
	}

	// refresh active pool ok, inactive 409
	res, _ = post(t, s, "/api/v1/storage/pools/default/refresh")
	if res.StatusCode != 200 {
		t.Errorf("refresh default: %d", res.StatusCode)
	}
	res, body = post(t, s, "/api/v1/storage/pools/iso/refresh")
	if res.StatusCode != 409 || body["error"].(map[string]any)["code"] != "NETWORK_INVALID_STATE" {
		t.Errorf("refresh inactive pool: %d %v", res.StatusCode, body)
	}

	// networks list + detail
	res, body = get(t, s, "/api/v1/networks")
	if res.StatusCode != 200 || body["total"] != float64(2) {
		t.Fatalf("networks: %d %v", res.StatusCode, body["total"])
	}
	res, body = get(t, s, "/api/v1/networks/default")
	if res.StatusCode != 200 || body["state"] != "active" {
		t.Errorf("network default: %d %v", res.StatusCode, body["state"])
	}

	// network start/stop transitions with 409 on invalid state
	res, _ = post(t, s, "/api/v1/networks/default/start")
	if res.StatusCode != 409 {
		t.Errorf("start active network: %d", res.StatusCode)
	}
	res, body = post(t, s, "/api/v1/networks/mgmt/start")
	if res.StatusCode != 200 || body["state"] != "active" {
		t.Errorf("start mgmt: %d %v", res.StatusCode, body["state"])
	}
	res, body = post(t, s, "/api/v1/networks/mgmt/stop")
	if res.StatusCode != 200 || body["state"] != "inactive" {
		t.Errorf("stop mgmt: %d %v", res.StatusCode, body["state"])
	}
	res, _ = post(t, s, "/api/v1/networks/nope/stop")
	if res.StatusCode != 404 {
		t.Errorf("stop unknown network: %d", res.StatusCode)
	}
}

func TestIsoLibrary(t *testing.T) {
	s := testServer(t)

	upload := func(name string) (*http.Response, map[string]any) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write([]byte("fake iso content"))
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/isos", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		res := rec.Result()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res, body
	}

	// invalid filename -> 400
	res, body := upload("notes.txt")
	if res.StatusCode != 400 || body["error"].(map[string]any)["code"] != "VALIDATION_ERROR" {
		t.Fatalf("expected 400, got %d %v", res.StatusCode, body)
	}

	// upload -> 201
	res, body = upload("ubuntu-24.04.iso")
	if res.StatusCode != 201 || body["id"] != "ubuntu-24.04.iso" || body["sizeBytes"] != float64(16) {
		t.Fatalf("upload: %d %v", res.StatusCode, body)
	}

	// duplicate -> 409
	res, body = upload("ubuntu-24.04.iso")
	if res.StatusCode != 409 || body["error"].(map[string]any)["code"] != "ISO_ALREADY_EXISTS" {
		t.Fatalf("expected 409, got %d %v", res.StatusCode, body)
	}

	// list contains it
	_, body = get(t, s, "/api/v1/storage/isos")
	if body["total"] != float64(1) {
		t.Fatalf("list: %v", body)
	}

	// delete -> 204, then 404
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage/isos/ubuntu-24.04.iso", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/storage/isos/ubuntu-24.04.iso", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
