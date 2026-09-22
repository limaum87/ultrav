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
	CodeVMNotFound            = "VM_NOT_FOUND"
	CodeVMInvalidState        = "VM_INVALID_STATE"
	CodeVMAlreadyExists       = "VM_ALREADY_EXISTS"
	CodeValidationError       = "VALIDATION_ERROR"
	CodeNotFound              = "NOT_FOUND"
	CodeMethodNotAllowed      = "METHOD_NOT_ALLOWED"
	CodePoolNotFound          = "STORAGE_POOL_NOT_FOUND"
	CodePoolAlreadyExists     = "STORAGE_POOL_ALREADY_EXISTS"
	CodeNetworkNotFound       = "NETWORK_NOT_FOUND"
	CodeNetworkInvalidState   = "NETWORK_INVALID_STATE"
	CodeNetworkAlreadyExists  = "NETWORK_ALREADY_EXISTS"
	CodeIsoNotFound           = "ISO_NOT_FOUND"
	CodeIsoAlreadyExists      = "ISO_ALREADY_EXISTS"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeInvalidCredentials    = "INVALID_CREDENTIALS"
	CodeForbidden             = "FORBIDDEN"
	CodeUserNotFound          = "USER_NOT_FOUND"
	CodeUserAlreadyExists     = "USER_ALREADY_EXISTS"
	CodeLastAdmin             = "LAST_ADMIN"
	CodeInternalError         = "INTERNAL_ERROR"
	CodeConsoleUnavailable    = "CONSOLE_UNAVAILABLE"
	CodeStorageUnavailable    = "STORAGE_UNAVAILABLE"
	CodeKVMUnavailable        = "KVM_UNAVAILABLE"
	CodeNetworkInactive       = "NETWORK_INACTIVE"
	CodeBackupNotFound        = "BACKUP_NOT_FOUND"
	CodeBackupInvalidState    = "BACKUP_INVALID_STATE"
	CodeScheduleNotFound      = "SCHEDULE_NOT_FOUND"
	CodeScheduleAlreadyExists = "SCHEDULE_ALREADY_EXISTS"
)

// errorStatus maps error codes to HTTP status codes.
var errorStatus = map[string]int{
	CodeVMNotFound:            http.StatusNotFound,
	CodeVMInvalidState:        http.StatusConflict,
	CodeVMAlreadyExists:       http.StatusConflict,
	CodeValidationError:       http.StatusBadRequest,
	CodeNotFound:              http.StatusNotFound,
	CodeMethodNotAllowed:      http.StatusMethodNotAllowed,
	CodePoolNotFound:          http.StatusNotFound,
	CodePoolAlreadyExists:     http.StatusConflict,
	CodeNetworkNotFound:       http.StatusNotFound,
	CodeNetworkInvalidState:   http.StatusConflict,
	CodeNetworkAlreadyExists:  http.StatusConflict,
	CodeIsoNotFound:           http.StatusNotFound,
	CodeIsoAlreadyExists:      http.StatusConflict,
	CodeUnauthorized:          http.StatusUnauthorized,
	CodeInvalidCredentials:    http.StatusUnauthorized,
	CodeForbidden:             http.StatusForbidden,
	CodeUserNotFound:          http.StatusNotFound,
	CodeUserAlreadyExists:     http.StatusConflict,
	CodeLastAdmin:             http.StatusConflict,
	CodeInternalError:         http.StatusInternalServerError,
	CodeConsoleUnavailable:    http.StatusConflict,
	CodeStorageUnavailable:    http.StatusConflict,
	CodeKVMUnavailable:        http.StatusConflict,
	CodeNetworkInactive:       http.StatusConflict,
	CodeBackupNotFound:        http.StatusNotFound,
	CodeBackupInvalidState:    http.StatusConflict,
	CodeScheduleNotFound:      http.StatusNotFound,
	CodeScheduleAlreadyExists: http.StatusConflict,
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
	case errors.Is(err, hypervisor.ErrPoolInsufficientSpace):
		s.writeError(w, r, CodeValidationError, err.Error())
	case errors.Is(err, hypervisor.ErrPoolAlreadyExists):
		s.writeError(w, r, CodePoolAlreadyExists, "A storage pool with this name already exists")
	case errors.Is(err, hypervisor.ErrNetworkNotFound):
		s.writeError(w, r, CodeNetworkNotFound, "Network was not found")
	case errors.Is(err, hypervisor.ErrInvalidNetworkState):
		s.writeError(w, r, CodeNetworkInvalidState, err.Error())
	case errors.Is(err, hypervisor.ErrNetworkAlreadyExists):
		s.writeError(w, r, CodeNetworkAlreadyExists, "A network with this name already exists")
	case errors.Is(err, hypervisor.ErrInvalidVMState):
		s.writeError(w, r, CodeVMInvalidState, err.Error())
	case errors.Is(err, hypervisor.ErrVMAlreadyExists):
		s.writeError(w, r, CodeVMAlreadyExists, "A virtual machine with this name already exists")
	case errors.Is(err, hypervisor.ErrIsoNotFound):
		s.writeError(w, r, CodeIsoNotFound, "ISO image was not found")
	case errors.Is(err, hypervisor.ErrNetworkInactive):
		// The message names the network and how to start it.
		var ne *hypervisor.NetworkInactiveError
		if errors.As(err, &ne) {
			s.writeError(w, r, CodeNetworkInactive, ne.Error())
		} else {
			s.writeError(w, r, CodeNetworkInactive, err.Error())
		}
	case errors.Is(err, hypervisor.ErrKVMUnavailable):
		// Host configuration problem, not a transient fault: 409 with the
		// reason and the two possible fixes (mock mode / BIOS + kvm module).
		var ke *hypervisor.KVMUnavailableError
		if errors.As(err, &ke) {
			s.writeError(w, r, CodeKVMUnavailable, ke.Error())
		} else {
			s.writeError(w, r, CodeKVMUnavailable, err.Error())
		}
	case errors.Is(err, hypervisor.ErrStorageUnavailable):
		// Configuration problem, not a transient fault: the message names the
		// unreachable path so the operator can fix the mount.
		var se *hypervisor.StorageUnavailableError
		if errors.As(err, &se) {
			s.writeError(w, r, CodeStorageUnavailable, se.Error())
		} else {
			s.writeError(w, r, CodeStorageUnavailable, err.Error())
		}
	case errors.Is(err, hypervisor.ErrBackupNotFound):
		s.writeError(w, r, CodeBackupNotFound, "Backup was not found")
	case errors.Is(err, hypervisor.ErrBackupInvalidState):
		s.writeError(w, r, CodeBackupInvalidState, err.Error())
	case errors.Is(err, hypervisor.ErrConsoleUnavailable):
		s.writeError(w, r, CodeConsoleUnavailable, "This virtual machine has no graphical console configured (add a VNC <graphics> device to its domain XML)")
	default:
		// Keep the underlying cause in the logs; the response stays generic.
		s.log.Error("provider error", "requestId", RequestID(r), "method", r.Method, "path", r.URL.Path, "err", err.Error())
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
