package api

import (
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

// handleCreateVMBackup runs a full backup of the VM (synchronous).
func (s *Server) handleCreateVMBackup(w http.ResponseWriter, r *http.Request) {
	backup, err := s.provider.BackupVirtualMachine(r.Context(), r.PathValue("id"))
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
