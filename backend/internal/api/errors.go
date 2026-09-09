package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// Error codes are stable, public identifiers (see docs/api/openapi.yaml).
const (
	CodeVMNotFound          = "VM_NOT_FOUND"
	CodeVMInvalidState      = "VM_INVALID_STATE"
	CodeValidationError     = "VALIDATION_ERROR"
	CodeNotFound            = "NOT_FOUND"
	CodeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	CodePoolNotFound        = "STORAGE_POOL_NOT_FOUND"
	CodeNetworkNotFound     = "NETWORK_NOT_FOUND"
	CodeNetworkInvalidState = "NETWORK_INVALID_STATE"
	CodeInternalError       = "INTERNAL_ERROR"
)

// errorStatus maps error codes to HTTP status codes.
var errorStatus = map[string]int{
	CodeVMNotFound:          http.StatusNotFound,
	CodeVMInvalidState:      http.StatusConflict,
	CodeValidationError:     http.StatusBadRequest,
	CodeNotFound:            http.StatusNotFound,
	CodeMethodNotAllowed:    http.StatusMethodNotAllowed,
	CodePoolNotFound:        http.StatusNotFound,
	CodeNetworkNotFound:     http.StatusNotFound,
	CodeNetworkInvalidState: http.StatusConflict,
	CodeInternalError:       http.StatusInternalServerError,
}

// writeError writes the standard error envelope. Internal details are logged,
// never sent to the client.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, code, message string) {
	status := errorStatus[code]
	reqID := RequestID(r)
	if code == CodeInternalError {
		s.log.Error("internal error", "requestId", reqID, "method", r.Method, "path", r.URL.Path)
		message = "An internal error occurred"
	} else {
		s.log.Warn("request error", "requestId", reqID, "code", code, "message", message, "method", r.Method, "path", r.URL.Path)
	}
	writeJSON(w, status, types.Error{
		Error: struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestId string `json:"requestId"`
		}{Code: code, Message: message, RequestId: reqID},
	})
}

// writeProviderError maps a hypervisor.Provider error to the public error model.
func (s *Server) writeProviderError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, hypervisor.ErrVMNotFound):
		s.writeError(w, r, CodeVMNotFound, "Virtual machine was not found")
	case errors.Is(err, hypervisor.ErrPoolNotFound):
		s.writeError(w, r, CodePoolNotFound, "Storage pool was not found")
	case errors.Is(err, hypervisor.ErrNetworkNotFound):
		s.writeError(w, r, CodeNetworkNotFound, "Network was not found")
	case errors.Is(err, hypervisor.ErrInvalidNetworkState):
		s.writeError(w, r, CodeNetworkInvalidState, err.Error())
	case errors.Is(err, hypervisor.ErrInvalidVMState):
		s.writeError(w, r, CodeVMInvalidState, err.Error())
	default:
		s.writeError(w, r, CodeInternalError, "")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}
