package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/backup"
	"github.com/ultrav/ultrav/backend/internal/tasks"
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

// handleCreateVMBackup runs a backup of the VM as a background task
// (Job System v1). The body is optional: {"type": "full"|"incremental"}
// forces the point type; absent = auto. The VM must exist (404 otherwise);
// failures during the copy are reported in the task's error.
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
	vmID := r.PathValue("id")
	if _, err := s.provider.GetVirtualMachine(r.Context(), vmID); err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	task, err := s.tasks.Start(tasks.TypeBackupCreate, vmID, vmID, func(ctx context.Context, rep *tasks.Reporter) error {
		rep.SetMessage("backing up " + vmID)
		rep.SetProgress(10)
		b, err := s.provider.BackupVirtualMachine(ctx, vmID, req)
		if err != nil {
			return err
		}
		rep.SetResourceID(b.Id)
		rep.SetProgress(100)
		rep.SetMessage("backup of " + vmID + " completed")
		return nil
	})
	if err != nil {
		s.log.Error("failed to start backup-create task", "err", err)
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	writeJSON(w, http.StatusAccepted, task)
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

// handleRestoreBackup restores a backup into its (stopped) VM as a
// background task (Job System v1); poll GET /tasks/{id} for the result.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.tasks.Start(tasks.TypeBackupRestore, id, id, func(ctx context.Context, rep *tasks.Reporter) error {
		rep.SetMessage("restoring backup " + id)
		rep.SetProgress(10)
		vm, err := s.provider.RestoreBackup(ctx, id)
		if err != nil {
			return err
		}
		rep.SetResourceID(vm.Id)
		rep.SetProgress(100)
		rep.SetMessage("restore of " + id + " completed")
		return nil
	})
	if err != nil {
		s.log.Error("failed to start backup-restore task", "err", err)
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	writeJSON(w, http.StatusAccepted, task)
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
