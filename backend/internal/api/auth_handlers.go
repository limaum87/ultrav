package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/auth"
)

// publicPaths bypass authentication (health/readiness probes and login).
var publicPaths = map[string]bool{
	"/api/v1/health":     true,
	"/api/v1/ready":      true,
	"/api/v1/auth/login": true,
	"/openapi.json":      true,
	"/docs":              true,
}

// withAuth rejects requests without a valid bearer token. The WebSocket
// console endpoint also accepts `?token=` because browsers cannot set
// headers on a WebSocket handshake.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authn == nil || publicPaths[r.URL.Path] { // auth disabled (tests/dev)
			next.ServeHTTP(w, r)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token") // WebSocket handshake
		}
		user, err := s.authn.Verify(token)
		if err != nil {
			s.writeError(w, r, CodeUnauthorized, "Authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// ctxKeyUser stores the authenticated user on the request context.
type ctxKeyUser struct{}

func withUser(ctx context.Context, u *auth.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser{}, u)
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (*auth.User, bool) {
	u, ok := ctx.Value(ctxKeyUser{}).(*auth.User)
	return u, ok
}

// handleLogin implements POST /auth/login.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authn == nil {
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	var req types.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if req.Username == "" || req.Password == "" {
		s.writeError(w, r, CodeValidationError, "username and password are required")
		return
	}
	token, user, err := s.authn.Login(req.Username, req.Password)
	if err != nil {
		s.writeError(w, r, CodeInvalidCredentials, "Invalid username or password")
		return
	}
	writeJSON(w, http.StatusOK, types.LoginResponse{
		Token:            token,
		TokenType:        "Bearer",
		ExpiresInSeconds: int(auth.TokenTTL.Seconds()),
		User: types.User{
			Id:        int(user.ID),
			Username:  user.Username,
			Role:      types.UserRole(user.Role),
			CreatedAt: user.CreatedAt,
		},
	})
}

// handleGetCurrentUser implements GET /auth/me.
func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		s.writeError(w, r, CodeUnauthorized, "Authentication required")
		return
	}
	writeJSON(w, http.StatusOK, types.User{
		Id:        int(user.ID),
		Username:  user.Username,
		Role:      types.UserRole(user.Role),
		CreatedAt: user.CreatedAt,
	})
}
