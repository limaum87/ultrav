package tasks

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

func testMgr() *Manager { return NewManager(slog.Default()) }

func waitStatus(t *testing.T, m *Manager, id string, want types.TaskStatus) types.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := m.Get(id)
		if ok && task.Status == want {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s in time", id, want)
	return types.Task{}
}

func TestStartSucceeds(t *testing.T) {
	m := testMgr()
	task, err := m.Start(TypeVMCreate, "erp01", "erp01", func(ctx context.Context, rep *Reporter) error {
		rep.SetProgress(50)
		rep.SetMessage("halfway")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Id == "" || task.Type != types.TaskTypeVmCreate || task.Cancellable != true {
		t.Fatalf("unexpected task view: %+v", task)
	}
	done := waitStatus(t, m, task.Id, types.TaskStatusSucceeded)
	if done.Progress != 100 || done.Error != nil {
		t.Fatalf("unexpected finished task: %+v", done)
	}
	if done.FinishedAt == nil {
		t.Fatal("finishedAt not set")
	}
}

func TestStartFails(t *testing.T) {
	m := testMgr()
	task, _ := m.Start(TypeBackupCreate, "erp01", "erp01", func(ctx context.Context, rep *Reporter) error {
		return errors.New("disk full")
	})
	done := waitStatus(t, m, task.Id, types.TaskStatusFailed)
	if done.Error == nil || *done.Error != "disk full" {
		t.Fatalf("expected error preserved, got %+v", done)
	}
}

func TestCancelRunningTask(t *testing.T) {
	m := testMgr()
	started := make(chan struct{})
	task, _ := m.Start(TypeVMCreate, "erp01", "erp01", func(ctx context.Context, rep *Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	updated, ok := m.Cancel(task.Id)
	if !ok || updated.Status != types.TaskStatusCancelling {
		t.Fatalf("cancel not accepted: %+v ok=%v", updated, ok)
	}
	waitStatus(t, m, task.Id, types.TaskStatusCancelled)

	// Terminal tasks cannot be cancelled again.
	if _, ok := m.Cancel(task.Id); ok {
		t.Fatal("cancel of terminal task should not be accepted")
	}
}

func TestCancelUnknownTask(t *testing.T) {
	m := testMgr()
	if _, ok := m.Cancel("task-999999"); ok {
		t.Fatal("unknown task cancel should fail")
	}
}

func TestListNewestFirstAndFilter(t *testing.T) {
	m := testMgr()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, _ := m.Start(TypeVMCreate, "x", "x", func(ctx context.Context, rep *Reporter) error { return nil })
			waitStatus(t, m, task.Id, types.TaskStatusSucceeded)
		}()
	}
	wg.Wait()
	all := m.List("", 0)
	if len(all) != 3 || all[0].CreatedAt.Before(all[2].CreatedAt) {
		t.Fatalf("expected 3 tasks newest first, got %+v", all)
	}
	failed := m.List(types.TaskStatusFailed, 0)
	if len(failed) != 0 {
		t.Fatalf("expected no failed tasks, got %d", len(failed))
	}
}

func TestListLimit(t *testing.T) {
	m := testMgr()
	for i := 0; i < 5; i++ {
		task, _ := m.Start(TypeVMCreate, "x", "x", func(ctx context.Context, rep *Reporter) error { return nil })
		waitStatus(t, m, task.Id, types.TaskStatusSucceeded)
	}
	if got := m.List("", 2); len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
}

func TestInvalidTypeRejected(t *testing.T) {
	m := testMgr()
	if _, err := m.Start(Type("bogus"), "x", "x", func(ctx context.Context, rep *Reporter) error { return nil }); err == nil {
		t.Fatal("invalid type should be rejected")
	}
}

func TestWarningsCollected(t *testing.T) {
	m := testMgr()
	task, _ := m.Start(TypeVMCreate, "win01", "win01", func(ctx context.Context, rep *Reporter) error {
		rep.AddWarning("no virtio drivers")
		return nil
	})
	done := waitStatus(t, m, task.Id, types.TaskStatusSucceeded)
	if done.Warnings == nil || len(*done.Warnings) != 1 {
		t.Fatalf("expected warnings, got %+v", done)
	}
}
