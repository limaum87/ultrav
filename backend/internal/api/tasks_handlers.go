package api

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// taskIDPattern guards the /tasks/{id} path parameter.
var taskIDRE = regexp.MustCompile(`^task-[a-zA-Z0-9._-]{1,64}$`)

// handleListTasks lists the in-memory task history (newest first).
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	status := types.TaskStatus(r.URL.Query().Get("status"))
	switch status {
	case "", types.TaskStatusQueued, types.TaskStatusRunning, types.TaskStatusSucceeded,
		types.TaskStatusFailed, types.TaskStatusCancelling, types.TaskStatusCancelled:
	default:
		s.writeError(w, r, CodeValidationError, "invalid status filter")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			s.writeError(w, r, CodeValidationError, "limit must be between 1 and 500")
			return
		}
		limit = n
	}
	items := s.tasks.List(status, limit)
	writeJSON(w, http.StatusOK, types.TaskList{Items: items, Total: len(items)})
}

// handleGetTask returns one task's snapshot.
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		s.writeError(w, r, CodeTaskNotFound, "Task was not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleCancelTask requests cancellation of a queued/running task.
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Cancel(r.PathValue("id"))
	if !ok {
		if _, exists := s.tasks.Get(r.PathValue("id")); !exists {
			s.writeError(w, r, CodeTaskNotFound, "Task was not found")
			return
		}
		s.writeError(w, r, CodeTaskNotCancellable, "Task already reached a terminal state")
		return
	}
	writeJSON(w, http.StatusAccepted, t)
}

// requireValidTaskID guards the /tasks/{id} routes.
func (s *Server) requireValidTaskID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !taskIDRE.MatchString(r.PathValue("id")) {
			s.writeError(w, r, CodeValidationError, "Invalid task identifier")
			return
		}
		next(w, r)
	}
}
