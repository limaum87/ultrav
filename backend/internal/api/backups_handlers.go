package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/backup"
)

// --- backup endpoints ---

// handleListVMBackups lists the backup points of a VM (newest first).
func (s *Server) handleListVMBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := s.provider.ListVMBackups(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, types.BackupList{Items: backups, Total: len(backups)})
}

// handleCreateVMBackup runs a backup of the VM (synchronous). The body is
// optional: {"type": "full"|"incremental"} forces the point type; absent = auto.
func (s *Server) handleCreateVMBackup(w http.ResponseWriter, r *http.Request) {
	var req types.BackupCreate
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			s.writeError(w, r, CodeValidationError, "Invalid JSON body")
			return
		}
	}
	if req.Type != nil && *req.Type != types.BackupCreateTypeFull && *req.Type != types.BackupCreateTypeIncremental {
		s.writeError(w, r, CodeValidationError, "type must be 'full' or 'incremental'")
		return
	}
	backup, err := s.provider.BackupVirtualMachine(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, backup)
}

// requireValidBackupID guards the /backups/{id} path parameter.
func (s *Server) requireValidBackupID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !backup.IDPattern.MatchString(r.PathValue("id")) {
			s.writeError(w, r, CodeValidationError, "Invalid backup identifier")
			return
		}
		next(w, r)
	}
}

// handleDeleteBackup permanently removes a backup point.
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if err := s.provider.DeleteBackup(r.Context(), r.PathValue("id")); err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRestoreBackup restores a backup into its (stopped) VM.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	vm, err := s.provider.RestoreBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

// handleExportVMConfig returns the domain XML (application/xml).
func (s *Server) handleExportVMConfig(w http.ResponseWriter, r *http.Request) {
	xml, err := s.provider.ExportVMConfig(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xml)
}
