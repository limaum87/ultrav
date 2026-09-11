package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/auth"
)

// minPasswordLength is the minimum accepted password size.
const minPasswordLength = 8

// requireAdmin rejects non-admin authenticated users.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFrom(r.Context())
		if !ok {
			s.writeError(w, r, CodeUnauthorized, "Authentication required")
			return
		}
		if user.Role != "admin" {
			s.writeError(w, r, CodeForbidden, "Admin role required")
			return
		}
		next(w, r)
	}
}

func toTypesUser(u *auth.User) types.User {
	return types.User{Id: int(u.ID), Username: u.Username, Role: types.UserRole(u.Role), CreatedAt: u.CreatedAt}
}

// handleListUsers implements GET /users.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.authn.ListUsers()
	if err != nil {
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	items := make([]types.User, 0, len(users))
	for i := range users {
		items = append(items, toTypesUser(&users[i]))
	}
	writeJSON(w, http.StatusOK, types.UserList{Total: len(items), Items: items})
}

// handleCreateUser implements POST /users.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req types.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < minPasswordLength {
		s.writeError(w, r, CodeValidationError,
			"username is required and password must have at least "+strconv.Itoa(minPasswordLength)+" characters")
		return
	}
	if req.Role != "admin" && req.Role != "viewer" {
		s.writeError(w, r, CodeValidationError, "role must be admin or viewer")
		return
	}
	u, err := s.authn.CreateUser(req.Username, req.Password, string(req.Role))
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			s.writeError(w, r, CodeUserAlreadyExists, "A user with this username already exists")
			return
		}
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	writeJSON(w, http.StatusCreated, toTypesUser(u))
}

// parseUserID extracts and validates the {id} path parameter.
func parseUserID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// handleDeleteUser implements DELETE /users/{id}.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	caller, _ := UserFrom(r.Context())
	id, ok := parseUserID(r)
	if !ok {
		s.writeError(w, r, CodeValidationError, "invalid user id")
		return
	}
	err := s.authn.DeleteUser(int64(caller.ID), id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, auth.ErrLastAdmin):
		s.writeError(w, r, CodeLastAdmin, "Cannot delete yourself or the last admin user")
	case errors.Is(err, sql.ErrNoRows):
		s.writeError(w, r, CodeUserNotFound, "User was not found")
	default:
		s.writeError(w, r, CodeInternalError, "")
	}
}

// handleResetUserPassword implements PUT /users/{id}/password.
func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(r)
	if !ok {
		s.writeError(w, r, CodeValidationError, "invalid user id")
		return
	}
	var req types.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if len(req.Password) < minPasswordLength {
		s.writeError(w, r, CodeValidationError,
			"password must have at least "+strconv.Itoa(minPasswordLength)+" characters")
		return
	}
	if err := s.authn.UpdatePassword(id, req.Password); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, r, CodeUserNotFound, "User was not found")
			return
		}
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleChangeOwnPassword implements PUT /auth/password.
func (s *Server) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	caller, ok := UserFrom(r.Context())
	if !ok {
		s.writeError(w, r, CodeUnauthorized, "Authentication required")
		return
	}
	var req types.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if req.CurrentPassword == "" || len(req.NewPassword) < minPasswordLength {
		s.writeError(w, r, CodeValidationError,
			"currentPassword is required and newPassword must have at least "+strconv.Itoa(minPasswordLength)+" characters")
		return
	}
	if err := s.authn.ChangeOwnPassword(int64(caller.ID), req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.writeError(w, r, CodeInvalidCredentials, "Current password is incorrect")
			return
		}
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
