package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/scheduler"
)

// schedulerIDPattern guards the /backup-schedules/{id} path parameter.
var schedulerIDPattern = regexp.MustCompile(`^sch-[a-zA-Z0-9._-]{1,60}$`)

// --- backup schedule endpoints (nil scheduler = feature not wired) ---

func (s *Server) requireScheduler(w http.ResponseWriter, r *http.Request) bool {
	if s.sched == nil {
		s.writeError(w, r, CodeInternalError, "")
		return false
	}
	return true
}

// handleListBackupSchedules lists the backup schedules.
func (s *Server) handleListBackupSchedules(w http.ResponseWriter, r *http.Request) {
	if !s.requireScheduler(w, r) {
		return
	}
	list := s.sched.List()
	writeJSON(w, http.StatusOK, types.BackupScheduleList{Items: list, Total: len(list)})
}

// handleCreateBackupSchedule validates and creates a schedule.
func (s *Server) handleCreateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.requireScheduler(w, r) {
		return
	}
	var req types.BackupScheduleCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	sch, err := s.sched.Create(req)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrInvalid):
			s.writeError(w, r, CodeValidationError, err.Error())
		case errors.Is(err, scheduler.ErrNameTaken):
			s.writeError(w, r, CodeScheduleAlreadyExists, "A backup schedule with this name already exists")
		default:
			s.writeError(w, r, CodeInternalError, "")
		}
		return
	}
	writeJSON(w, http.StatusCreated, sch)
}

// requireValidScheduleID guards the /backup-schedules/{id} path parameter.
func (s *Server) requireValidScheduleID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !schedulerIDPattern.MatchString(r.PathValue("id")) {
			s.writeError(w, r, CodeValidationError, "Invalid backup schedule identifier")
			return
		}
		next(w, r)
	}
}

// handleUpdateBackupSchedule applies a partial schedule update.
func (s *Server) handleUpdateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.requireScheduler(w, r) {
		return
	}
	var req types.BackupScheduleUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	sch, err := s.sched.Update(r.PathValue("id"), req)
	if err != nil {
		switch {
		case errors.Is(err, scheduler.ErrNotFound):
			s.writeError(w, r, CodeScheduleNotFound, "Backup schedule was not found")
		case errors.Is(err, os.ErrInvalid):
			s.writeError(w, r, CodeValidationError, err.Error())
		default:
			s.writeError(w, r, CodeInternalError, "")
		}
		return
	}
	writeJSON(w, http.StatusOK, sch)
}

// handleDeleteBackupSchedule removes a schedule definition.
func (s *Server) handleDeleteBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.requireScheduler(w, r) {
		return
	}
	if err := s.sched.Delete(r.PathValue("id")); err != nil {
		if errors.Is(err, scheduler.ErrNotFound) {
			s.writeError(w, r, CodeScheduleNotFound, "Backup schedule was not found")
		} else {
			s.writeError(w, r, CodeInternalError, "")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
