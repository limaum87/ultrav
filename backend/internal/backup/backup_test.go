package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateCompleteListDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	pt, err := s.Create("erp01")
	if err != nil {
		t.Fatal(err)
	}
	// Point must be invisible before Complete.
	if metas, _ := s.List("erp01"); len(metas) != 0 {
		t.Fatalf("incomplete point visible in listing: %d", len(metas))
	}

	disk := pt.DiskPath("vda", FormatQcow2)
	if filepath.Dir(disk) != pt.Dir() || filepath.Base(disk) != "vda.qcow2" {
		t.Fatalf("unexpected disk path %s", disk)
	}
	if err := os.WriteFile(disk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pt.SetDiskSize("vda", 1)
	pt.SetDomainXML(true)
	if err := pt.Complete(); err != nil {
		t.Fatal(err)
	}

	metas, err := s.List("erp01")
	if err != nil || len(metas) != 1 {
		t.Fatalf("list: %v metas=%d", err, len(metas))
	}
	m := metas[0]
	if m.VMID != "erp01" || m.Type != TypeFull || m.State != StateComplete || !m.HasDomainXML {
		t.Fatalf("unexpected metadata %+v", m)
	}
	if len(m.Disks) != 1 || m.Disks[0].SizeBytes != 1 {
		t.Fatalf("unexpected disks %+v", m.Disks)
	}

	// Get by id resolves across VM directories.
	got, err := s.Get(m.ID)
	if err != nil || got.ID != m.ID {
		t.Fatalf("get: %v %+v", err, got)
	}

	// Unknown ids are not found.
	if _, err := s.Get("bk-does-not-exist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := s.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(m.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListNewestFirstAndVMIsolation(t *testing.T) {
	s, _ := New(t.TempDir())
	for _, vm := range []string{"erp01", "web01"} {
		for i := 0; i < 3; i++ {
			pt, err := s.Create(vm)
			if err != nil {
				t.Fatal(err)
			}
			if err := pt.Complete(); err != nil {
				t.Fatal(err)
			}
		}
	}
	metas, _ := s.List("erp01")
	if len(metas) != 3 {
		t.Fatalf("expected 3, got %d", len(metas))
	}
	for i := 1; i < len(metas); i++ {
		if metas[i-1].CreatedAt.Before(metas[i].CreatedAt) {
			t.Fatalf("listing not sorted newest first")
		}
	}
}

func TestIDValidation(t *testing.T) {
	s, _ := New(t.TempDir())
	if _, err := s.Create("../evil"); err == nil {
		t.Fatal("expected error for unsafe VM name")
	}
	for _, id := range []string{"", "x", "../erp01/bk-x", "bk-../../etc"} {
		if err := s.Delete(id); err == nil {
			t.Fatalf("expected error for invalid id %q", id)
		}
	}
}

func TestIncompletePointNotGettable(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	pt, _ := s.Create("erp01")
	_ = pt.Complete()
	// Simulate a point without metadata.json (crashed write).
	os.Remove(filepath.Join(pt.Dir(), "metadata.json"))
	if _, err := s.Get(pt.ID()); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
