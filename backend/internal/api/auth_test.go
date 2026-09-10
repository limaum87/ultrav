package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/auth"
	"github.com/ultrav/ultrav/backend/internal/hypervisor/mock"
)

func authTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	svc, err := auth.Open(filepath.Join(t.TempDir(), "test.db"), "test-secret", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateUser("admin", "s3cret", "admin"); err != nil {
		t.Fatal(err)
	}
	return NewServer(mock.New(), testIsoStore(t), svc, testLogger()), "admin"
}

func TestAuthLoginFlow(t *testing.T) {
	s, username := authTestServer(t)

	// protected endpoint without token -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/v1/host", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	// health stays public
	req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health should be public, got %d", rec.Code)
	}

	// bad credentials -> 401
	body, _ := json.Marshal(map[string]string{"username": username, "password": "wrong"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad credentials, got %d", rec.Code)
	}

	// good credentials -> token
	body, _ = json.Marshal(map[string]string{"username": username, "password": "s3cret"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Token string `json:"token"`
		User  struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}
	if login.Token == "" || login.User.Username != "admin" || login.User.Role != "admin" {
		t.Fatalf("unexpected login payload: %+v", login)
	}

	// token grants access
	for _, tc := range []struct{ name, path string }{
		{"host", "/api/v1/host"},
		{"me", "/api/v1/auth/me"},
	} {
		req = httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+login.Token)
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s with token: expected 200, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// TestAuthBootstrap ensures an admin user exists on an empty DB (created
// with a generated password).
func TestAuthBootstrap(t *testing.T) {
	svc, err := auth.Open(filepath.Join(t.TempDir(), "u.db"), "", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.BootstrapAdmin("", "", testLogger()); err != nil {
		t.Fatal(err)
	}
	// wrong password must fail cleanly (user exists, hash valid)
	if _, _, err := svc.Login("admin", "definitely-wrong"); err != auth.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}
