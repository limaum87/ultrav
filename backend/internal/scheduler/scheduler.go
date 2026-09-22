// Package scheduler implements recurring backup jobs and retention: a
// schedule fires daily at a fixed local time, backs up its target VMs and
// then prunes old points, keeping the newest N full chains.
//
// Schedules are persisted in a JSON file (survive restarts; lastRun prevents
// re-firing the same occurrence). The run loop is expected to be started
// once from main with the process lifetime context.
package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// Schedule is one recurring backup job definition.
type Schedule struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Time  string   `json:"time"`  // daily run time, HH:MM (server local)
	VMIDs []string `json:"vmIds"` // target VM names; ["*"] = all
	Type  string   `json:"type"`  // auto | full | incremental
	// RetentionKeepLast is the number of full chains to keep after each run
	// (0 = keep everything).
	RetentionKeepLast int        `json:"retentionKeepLast"`
	Enabled           bool       `json:"enabled"`
	LastRun           *time.Time `json:"lastRun,omitempty"`
	// CreatedAt anchors the "never run" case: a newly created schedule whose
	// time already passed today fires tomorrow, not immediately.
	CreatedAt time.Time `json:"createdAt"`
}

// Errors surfaced to the API layer.
var (
	ErrNotFound = errors.New("backup schedule was not found")
	// ErrNameTaken is returned when creating a schedule with a used name.
	ErrNameTaken = errors.New("a backup schedule with this name already exists")
	// ErrRunning is returned when the service is mid-run and cannot accept
	// mutation of the schedule set (readers are fine).
	ErrRunning = errors.New("a scheduled backup run is in progress")
)

var (
	namePattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	timePattern  = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	vmIDPattern  = regexp.MustCompile(`^[a-zA-Z0-9*][a-zA-Z0-9._*-]{0,63}$`)
	validTypes   = map[string]bool{"": true, "auto": true, "full": true, "incremental": true}
	pollInterval = 30 * time.Second
)

// Store persists schedules in a JSON file.
type Store struct {
	mu   sync.Mutex
	path string
	list []Schedule
}

// Open loads (creating if needed) the schedule store at path.
func Open(path string) (*Store, error) {
	st := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &st.list); err != nil {
		return nil, fmt.Errorf("corrupt schedule store %s: %w", path, err)
	}
	return st, nil
}

// List returns the schedules in creation order.
func (st *Store) List() []Schedule {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]Schedule(nil), st.list...)
}

// Get returns one schedule.
func (st *Store) Get(id string) (Schedule, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.list {
		if s.ID == id {
			return s, nil
		}
	}
	return Schedule{}, ErrNotFound
}

// Append stores a new schedule.
func (st *Store) Append(s Schedule) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.list = append(st.list, s)
	return st.save()
}

// Replace updates an existing schedule (matched by id).
func (st *Store) Replace(s Schedule) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.list {
		if st.list[i].ID == s.ID {
			st.list[i] = s
			return st.save()
		}
	}
	return ErrNotFound
}

// Remove deletes a schedule.
func (st *Store) Remove(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.list {
		if st.list[i].ID == id {
			st.list = append(st.list[:i], st.list[i+1:]...)
			return st.save()
		}
	}
	return ErrNotFound
}

// save writes the file (caller holds the lock).
func (st *Store) save() error {
	b, err := json.MarshalIndent(st.list, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Service binds a schedule store to a hypervisor provider and runs them.
type Service struct {
	store    *Store
	provider hypervisor.Provider
	log      *slog.Logger

	runMu sync.Mutex // one scheduled run at a time
}

// New builds the service from a schedule store and provider.
func New(store *Store, provider hypervisor.Provider, log *slog.Logger) *Service {
	return &Service{store: store, provider: provider, log: log}
}

// List returns all schedules as the API model.
func (s *Service) List() []types.BackupSchedule {
	list := s.store.List()
	out := make([]types.BackupSchedule, 0, len(list))
	for _, sch := range list {
		out = append(out, scheduleToModel(sch))
	}
	return out
}

// Get returns one schedule as the API model.
func (s *Service) Get(id string) (types.BackupSchedule, error) {
	sch, err := s.store.Get(id)
	if err != nil {
		return types.BackupSchedule{}, ErrNotFound
	}
	return scheduleToModel(sch), nil
}

// Create validates and stores a new schedule.
func (s *Service) Create(req types.BackupScheduleCreate) (types.BackupSchedule, error) {
	if msg := validateCreate(&req); msg != "" {
		return types.BackupSchedule{}, fmt.Errorf("%w: %s", os.ErrInvalid, msg)
	}
	for _, sch := range s.store.List() {
		if sch.Name == req.Name {
			return types.BackupSchedule{}, ErrNameTaken
		}
	}
	now := time.Now()
	sch := Schedule{
		ID:                newID(now),
		Name:              req.Name,
		Time:              req.Time,
		VMIDs:             append([]string(nil), req.VmIds...),
		Type:              scheduleType(createTypeString(req.Type)),
		RetentionKeepLast: retentionOrDefault(req.RetentionKeepLast),
		Enabled:           req.Enabled == nil || *req.Enabled,
		CreatedAt:         now,
	}
	if err := s.store.Append(sch); err != nil {
		return types.BackupSchedule{}, err
	}
	return scheduleToModel(sch), nil
}

// Update applies a partial update.
func (s *Service) Update(id string, req types.BackupScheduleUpdate) (types.BackupSchedule, error) {
	sch, err := s.store.Get(id)
	if err != nil {
		return types.BackupSchedule{}, ErrNotFound
	}
	if req.Time != nil {
		if !timePattern.MatchString(*req.Time) {
			return types.BackupSchedule{}, fmt.Errorf("%w: time must be HH:MM (00:00–23:59)", os.ErrInvalid)
		}
		sch.Time = *req.Time
	}
	if req.VmIds != nil {
		if len(*req.VmIds) == 0 {
			return types.BackupSchedule{}, fmt.Errorf("%w: vmIds must not be empty", os.ErrInvalid)
		}
		for _, id := range *req.VmIds {
			if !vmIDPattern.MatchString(id) {
				return types.BackupSchedule{}, fmt.Errorf("%w: invalid vmId %q", os.ErrInvalid, id)
			}
		}
		sch.VMIDs = append([]string(nil), *req.VmIds...)
	}
	if req.Type != nil {
		t := scheduleType(string(*req.Type))
		if !validTypes[t] {
			return types.BackupSchedule{}, fmt.Errorf("%w: type must be auto, full or incremental", os.ErrInvalid)
		}
		sch.Type = t
	}
	if req.RetentionKeepLast != nil {
		if *req.RetentionKeepLast < 0 {
			return types.BackupSchedule{}, fmt.Errorf("%w: retentionKeepLast must be >= 0", os.ErrInvalid)
		}
		sch.RetentionKeepLast = *req.RetentionKeepLast
	}
	if req.Enabled != nil {
		sch.Enabled = *req.Enabled
	}
	if err := s.store.Replace(sch); err != nil {
		return types.BackupSchedule{}, err
	}
	return scheduleToModel(sch), nil
}

// Delete removes a schedule definition (points are untouched).
func (s *Service) Delete(id string) error {
	if err := s.store.Remove(id); err != nil {
		return ErrNotFound
	}
	return nil
}

// RunLoop blocks running the scheduler until ctx is canceled: every poll
// interval it fires the enabled schedules whose time has come.
func (s *Service) RunLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

// tick fires every schedule due at now.
func (s *Service) tick(now time.Time) {
	for _, sch := range s.store.List() {
		if !sch.Enabled || !s.due(sch, now) {
			continue
		}
		if !s.runMu.TryLock() {
			s.log.Warn("scheduled backup skipped: another run in progress", "schedule", sch.Name)
			continue
		}
		go func(sch Schedule) {
			defer s.runMu.Unlock()
			s.log.Info("scheduled backup starting", "schedule", sch.Name, "vms", sch.VMIDs, "type", sch.Type)
			s.runSchedule(sch, now)
		}(sch)
	}
}

// due reports whether sch should fire at now: now is past today's run time
// and the last recorded run happened before it.
func (s *Service) due(sch Schedule, now time.Time) bool {
	h, m := 0, 0
	if _, err := fmt.Sscanf(sch.Time, "%d:%d", &h, &m); err != nil {
		return false // invalid time stored; skip (validated at the API)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if now.Before(today) {
		return false
	}
	if sch.LastRun != nil {
		return sch.LastRun.Before(today)
	}
	// Never fired: only due when it was created before today's run time.
	return !sch.CreatedAt.After(today)
}

// runSchedule backs up the targets and applies retention. Errors on
// individual VMs are logged, not fatal: one broken VM must not block the
// rest of the fleet.
func (s *Service) runSchedule(sch Schedule, now time.Time) {
	ids, err := s.targets(sch.VMIDs)
	if err != nil {
		s.log.Error("scheduled backup: resolve targets", "schedule", sch.Name, "err", err.Error())
		return
	}
	var btype *types.BackupCreateType
	if sch.Type == "full" {
		t := types.BackupCreateTypeFull
		btype = &t
	} else if sch.Type == "incremental" {
		t := types.BackupCreateTypeIncremental
		btype = &t
	}
	for _, id := range ids {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		_, err := s.provider.BackupVirtualMachine(ctx, id, types.BackupCreate{Type: btype})
		cancel()
		if err != nil {
			s.log.Error("scheduled backup failed", "schedule", sch.Name, "vm", id, "err", err.Error())
			continue
		}
		s.log.Info("scheduled backup done", "schedule", sch.Name, "vm", id)
		if sch.RetentionKeepLast > 0 {
			if pruned := s.prune(id, sch.RetentionKeepLast); pruned > 0 {
				s.log.Info("retention pruned old backups", "vm", id, "pruned", pruned, "keepLast", sch.RetentionKeepLast)
			}
		}
	}
	_ = s.markRun(sch.ID, now)
}

// targets resolves the schedule's VM list ("*" = every VM).
func (s *Service) targets(vmIDs []string) ([]string, error) {
	for _, id := range vmIDs {
		if id == "*" {
			vms, err := s.provider.ListVirtualMachines(context.Background())
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(vms))
			for _, vm := range vms {
				out = append(out, vm.Id)
			}
			return out, nil
		}
	}
	return append([]string(nil), vmIDs...), nil
}

// prune deletes the points older than the retentionKeepLast-th newest full
// (a full plus the incrementals on top of it form one chain).
func (s *Service) prune(vmID string, keepLast int) int {
	metas, err := s.provider.ListVMBackups(context.Background(), vmID)
	if err != nil {
		s.log.Warn("retention: list backups", "vm", vmID, "err", err.Error())
		return 0
	}
	// Newest first (providers sort by createdAt desc).
	sort.Slice(metas, func(i, j int) bool { return metas[i].CreatedAt.After(metas[j].CreatedAt) })
	fulls := 0
	var cutoff time.Time
	found := false
	for _, m := range metas {
		if m.Type == types.BackupTypeFull {
			fulls++
			if fulls == keepLast {
				cutoff = m.CreatedAt
				found = true
				break
			}
		}
	}
	if !found {
		return 0 // under the retention target: fewer fulls than keepLast
	}
	pruned := 0
	for _, m := range metas {
		if m.CreatedAt.Before(cutoff) {
			if err := s.provider.DeleteBackup(context.Background(), m.Id); err != nil {
				s.log.Warn("retention: delete", "vm", vmID, "backup", m.Id, "err", err.Error())
				continue
			}
			pruned++
		}
	}
	return pruned
}

// markRun records the schedule's last run time.
func (s *Service) markRun(id string, now time.Time) error {
	sch, err := s.store.Get(id)
	if err != nil {
		return err
	}
	sch.LastRun = &now
	return s.store.Replace(sch)
}

// validateCreate enforces create-time constraints.
func validateCreate(req *types.BackupScheduleCreate) string {
	if !namePattern.MatchString(req.Name) {
		return "Invalid schedule name"
	}
	if !timePattern.MatchString(req.Time) {
		return "time must be HH:MM (00:00–23:59)"
	}
	if len(req.VmIds) == 0 {
		return "vmIds must not be empty"
	}
	for _, id := range req.VmIds {
		if !vmIDPattern.MatchString(id) {
			return "vmIds must be VM names or '*'"
		}
	}
	if req.Type != nil && !validTypes[scheduleType(string(*req.Type))] {
		return "type must be auto, full or incremental"
	}
	if req.RetentionKeepLast != nil && *req.RetentionKeepLast < 0 {
		return "retentionKeepLast must be >= 0"
	}
	return ""
}

// scheduleType normalizes the requested type ("" = auto).
func scheduleType(t string) string {
	if t == "" {
		return "auto"
	}
	return t
}

// createTypeString renders an optional create type as a plain string.
func createTypeString(t *types.BackupScheduleCreateType) string {
	if t == nil {
		return ""
	}
	return string(*t)
}

// retentionOrDefault maps nil retention to 0 (keep everything).
func retentionOrDefault(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// scheduleToModel converts to the API model.
func scheduleToModel(sch Schedule) types.BackupSchedule {
	t := types.BackupScheduleType(sch.Type)
	out := types.BackupSchedule{
		Id:                sch.ID,
		Name:              sch.Name,
		Time:              sch.Time,
		VmIds:             sch.VMIDs,
		Type:              t,
		RetentionKeepLast: sch.RetentionKeepLast,
		Enabled:           sch.Enabled,
	}
	if sch.LastRun != nil {
		out.LastRun = sch.LastRun
	}
	return out
}

// newID generates a schedule identifier.
func newID(now time.Time) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err)) // unreachable in practice
	}
	return "sch-" + hex.EncodeToString(b[:])
}
