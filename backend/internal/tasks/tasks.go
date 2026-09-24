// Package tasks implements the in-memory async job system (Job System v1).
// Long-running provider operations (VM creation, backups, restores) run in
// background goroutines and publish their lifecycle here so clients can poll
// GET /tasks/{id} instead of holding a synchronous HTTP request open.
package tasks

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// maxHistory bounds memory usage: only the newest maxHistory tasks are kept.
const maxHistory = 500

// Type identifies which kind of operation a task runs.
type Type string

const (
	TypeVMCreate      Type = "vm-create"
	TypeBackupCreate  Type = "backup-create"
	TypeBackupRestore Type = "backup-restore"
)

func (t Type) valid() bool {
	switch t {
	case TypeVMCreate, TypeBackupCreate, TypeBackupRestore:
		return true
	}
	return false
}

// internal task state (the API view is types.Task).
type task struct {
	id           string
	typ          Type
	status       types.TaskStatus
	resourceID   string
	resourceName string
	message      string
	err          string
	progress     int
	warnings     []string
	createdAt    time.Time
	startedAt    time.Time
	finishedAt   time.Time
	cancel       context.CancelFunc
}

// Reporter lets a running operation publish progress, messages and warnings.
// Methods are safe to call from the worker goroutine only (the goroutine
// that received the run function), which is the only caller by design.
type Reporter struct {
	mgr *Manager
	t   *task
}

// lock/unlock delegate to the manager mutex: task fields are shared between
// the worker goroutine and API readers, so a single lock protects them all.
func (r *Reporter) lock()   { r.mgr.mu.Lock() }
func (r *Reporter) unlock() { r.mgr.mu.Unlock() }

// SetProgress records completion percentage (0–100). Values are clamped.
func (r *Reporter) SetProgress(p int) {
	r.lock()
	defer r.unlock()
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	r.t.progress = p
}

// SetMessage describes the current step (e.g. "allocating disk volume").
func (r *Reporter) SetMessage(msg string) {
	r.lock()
	defer r.unlock()
	r.t.message = msg
}

// AddWarning appends a non-fatal note shown to the user when the task ends.
func (r *Reporter) AddWarning(w string) {
	r.lock()
	defer r.unlock()
	r.t.warnings = append(r.t.warnings, w)
}

// SetResourceID records the identifier of the produced resource (VM name,
// backup point id) as soon as it is known.
func (r *Reporter) SetResourceID(id string) {
	r.lock()
	defer r.unlock()
	r.t.resourceID = id
}

// Manager keeps task history and runs task workers.
type Manager struct {
	mu    sync.Mutex
	seq   int
	order []*task
	byID  map[string]*task
	log   *slog.Logger
}

// NewManager creates an empty task manager.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{byID: map[string]*task{}, log: log}
}

// Start registers a task and immediately runs fn in a background goroutine.
// The returned task is in status queued (the goroutine flips it to running
// right away). fn receives a Reporter and a context that is cancelled when
// the task is cancelled via Cancel.
func (m *Manager) Start(typ Type, resourceID, name string, fn func(ctx context.Context, rep *Reporter) error) (types.Task, error) {
	if !typ.valid() {
		return types.Task{}, fmt.Errorf("invalid task type %q", string(typ))
	}
	if fn == nil {
		return types.Task{}, fmt.Errorf("task run function is required")
	}

	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("task-%06d", m.seq)
	ctx, cancel := context.WithCancel(context.Background())
	t := &task{
		id:         id,
		typ:        typ,
		status:     types.TaskStatusQueued,
		resourceID: resourceID,
		createdAt:  time.Now().UTC(),
		cancel:     cancel,
	}
	rep := &Reporter{mgr: m, t: t}
	m.order = append(m.order, t)
	m.byID[id] = t
	if name != "" {
		t.resourceName = name
	}
	// Trim history (drop oldest finished tasks beyond the cap).
	if len(m.order) > maxHistory {
		for _, old := range m.order {
			if len(m.order) <= maxHistory {
				break
			}
			switch old.status {
			case types.TaskStatusSucceeded, types.TaskStatusFailed, types.TaskStatusCancelled:
				m.dropLocked(old)
			}
		}
	}
	m.mu.Unlock()

	go m.run(ctx, t, rep, fn)
	view, _ := m.Get(id)
	return view, nil
}

// run executes the worker and records the terminal state.
func (m *Manager) run(ctx context.Context, t *task, rep *Reporter, fn func(ctx context.Context, rep *Reporter) error) {
	m.mu.Lock()
	t.status = types.TaskStatusRunning
	t.startedAt = time.Now().UTC()
	m.mu.Unlock()

	err := fn(ctx, rep)

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	t.finishedAt = now
	t.cancel = nil
	switch {
	case ctx.Err() != nil:
		t.status = types.TaskStatusCancelled
		t.message = "cancelled"
	case err != nil:
		t.status = types.TaskStatusFailed
		t.err = err.Error()
	default:
		t.status = types.TaskStatusSucceeded
		if t.progress < 100 {
			t.progress = 100
		}
	}
	m.log.Info("task finished", "id", t.id, "type", string(t.typ), "status", string(t.status), "err", t.err)
}

// Get returns a snapshot of the task.
func (m *Manager) Get(id string) (types.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return types.Task{}, false
	}
	return m.viewLocked(t), true
}

// List returns task snapshots newest first, optionally filtered by status
// (empty = all), limited to limit results (limit <= 0 = no limit).
func (m *Manager) List(status types.TaskStatus, limit int) []types.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	ordered := make([]*task, len(m.order))
	copy(ordered, m.order)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].createdAt.After(ordered[j].createdAt) })
	out := make([]types.Task, 0, len(ordered))
	for _, t := range ordered {
		if status != "" && t.status != status {
			continue
		}
		out = append(out, m.viewLocked(t))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Cancel requests cancellation. Returns the updated task and whether the
// request was accepted (false when the task is unknown or already terminal).
func (m *Manager) Cancel(id string) (types.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return types.Task{}, false
	}
	switch t.status {
	case types.TaskStatusQueued, types.TaskStatusRunning:
		t.status = types.TaskStatusCancelling
		if t.cancel != nil {
			t.cancel()
		}
		return m.viewLocked(t), true
	default:
		return m.viewLocked(t), false
	}
}

// dropLocked removes a task from history (caller holds m.mu).
func (m *Manager) dropLocked(t *task) {
	delete(m.byID, t.id)
	for i, o := range m.order {
		if o == t {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

// viewLocked builds the API view (caller holds m.mu).
func (m *Manager) viewLocked(t *task) types.Task {
	cancellable := false
	switch t.status {
	case types.TaskStatusQueued, types.TaskStatusRunning:
		cancellable = true
	}
	out := types.Task{
		Id:          t.id,
		Type:        types.TaskType(t.typ),
		Status:      t.status,
		ResourceId:  strPtr(t.resourceID),
		Message:     strPtr(t.message),
		Error:       strPtr(t.err),
		Progress:    t.progress,
		Cancellable: cancellable,
		CreatedAt:   t.createdAt,
	}
	if t.resourceName != "" {
		out.ResourceName = strPtr(t.resourceName)
	}
	if len(t.warnings) > 0 {
		w := append([]string(nil), t.warnings...)
		out.Warnings = &w
	}
	if !t.startedAt.IsZero() {
		out.StartedAt = &t.startedAt
	}
	if !t.finishedAt.IsZero() {
		out.FinishedAt = &t.finishedAt
	}
	return out
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
