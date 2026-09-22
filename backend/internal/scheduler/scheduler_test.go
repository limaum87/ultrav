package scheduler

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
	"github.com/ultrav/ultrav/backend/internal/hypervisor/mock"
)

func testService(t *testing.T) (*Service, *Store, hypervisor.Provider) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "schedules.json"))
	if err != nil {
		t.Fatal(err)
	}
	prov := mock.New(t.TempDir())
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := New(st, prov, log)
	return svc, st, prov
}

func boolp(b bool) *bool    { return &b }
func intp(i int) *int       { return &i }
func strp(s string) *string { return &s }

func TestScheduleCRUDAndValidation(t *testing.T) {
	svc, _, _ := testService(t)

	// Invalid: bad time.
	_, err := svc.Create(types.BackupScheduleCreate{
		Name: "bad", Time: "25:00", VmIds: []string{"*"},
	})
	if err == nil {
		t.Fatal("expected validation error for bad time")
	}

	// Invalid: empty targets.
	_, err = svc.Create(types.BackupScheduleCreate{Name: "bad", Time: "03:00", VmIds: nil})
	if err == nil {
		t.Fatal("expected validation error for empty vmIds")
	}

	sch, err := svc.Create(types.BackupScheduleCreate{
		Name: "nightly", Time: "03:00", VmIds: []string{"*"},
		RetentionKeepLast: intp(2), Enabled: boolp(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sch.Id == "" || sch.Type != "auto" || !sch.Enabled || sch.RetentionKeepLast != 2 {
		t.Fatalf("unexpected schedule %+v", sch)
	}

	// Duplicate name: 409.
	if _, err := svc.Create(types.BackupScheduleCreate{
		Name: "nightly", Time: "04:00", VmIds: []string{"*"},
	}); err != ErrNameTaken {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}

	// Persistence: a fresh store over the same file sees the schedule.
	st3, err := Open(svc.store.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st3.Get(sch.Id)
	if err != nil || got.Name != "nightly" {
		t.Fatalf("persisted schedule not found: %v %+v", err, got)
	}

	// Update: toggle enabled.
	upd, err := svc.Update(sch.Id, types.BackupScheduleUpdate{Enabled: boolp(false)})
	if err != nil || upd.Enabled {
		t.Fatalf("update: %v %+v", err, upd)
	}

	// Delete.
	if err := svc.Delete(sch.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(sch.Id); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDueAndTick(t *testing.T) {
	svc, st, _ := testService(t)
	now := time.Date(2025, 9, 1, 3, 5, 0, 0, time.Local)

	sch, err := svc.Create(types.BackupScheduleCreate{
		Name: "nightly", Time: "03:00", VmIds: []string{"monitoring01"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Backdate creation so "never fired + created before today's run time"
	// holds (a schedule created at 18:00 for 03:00 fires tomorrow).
	st.list[0].CreatedAt = now.Add(-24 * time.Hour)
	if !svc.due(st.list[0], now) {
		t.Fatal("schedule should be due at 03:05 for a 03:00 run")
	}
	// A fresh schedule whose time already passed today is NOT due (fires
	// tomorrow, not immediately).
	fresh := st.list[0]
	fresh.CreatedAt = now.Add(time.Minute)
	if svc.due(fresh, now) {
		t.Fatal("fresh schedule must not fire immediately for a past time")
	}
	svc.tick(now)
	// tick runs the schedule in a goroutine; wait for lastRun to be recorded.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := st.Get(sch.Id)
		if got.LastRun != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("schedule never ran")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Not due again on the same day.
	if svc.due(st.list[0], now.Add(time.Minute)) {
		t.Fatal("schedule must not fire twice on the same day")
	}
	// Disabled schedules never fire.
	got, _ := st.Get(sch.Id)
	got.Enabled = false
	_ = st.Replace(got)
	svc.tick(now.Add(24 * time.Hour))
	got, _ = st.Get(sch.Id)
	if got.LastRun.Day() != now.Day() {
		t.Fatal("disabled schedule fired")
	}
}

func TestPrune(t *testing.T) {
	svc, _, prov := testService(t)
	ctx := context.Background()

	// Four full points on the (stopped) monitoring01.
	var ids []string
	for i := 0; i < 4; i++ {
		b, err := prov.BackupVirtualMachine(ctx, "monitoring01", types.BackupCreate{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, b.Id)
		time.Sleep(2 * time.Millisecond) // distinct createdAt
	}

	if pruned := svc.prune("monitoring01", 2); pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}
	backups, _ := prov.ListVMBackups(ctx, "monitoring01")
	if len(backups) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(backups))
	}
	// The kept ones must be the newest.
	kept := map[string]bool{}
	for _, b := range backups {
		kept[b.Id] = true
	}
	if !kept[ids[2]] || !kept[ids[3]] {
		t.Fatalf("retention kept the wrong points: %v", kept)
	}
	// Under the target: nothing to prune.
	if pruned := svc.prune("monitoring01", 2); pruned != 0 {
		t.Fatalf("expected 0 pruned, got %d", pruned)
	}
}
