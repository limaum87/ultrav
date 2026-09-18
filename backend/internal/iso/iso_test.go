package iso

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDirSwitchesLibrary(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	s, err := New(dir1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "ubuntu.iso"), make([]byte, 8), 0o644); err != nil {
		t.Fatal(err)
	}

	// after SetDir, existing ISOs of the new directory are listed (re-use case)
	if err := s.SetDir(dir2); err != nil {
		t.Fatal(err)
	}
	if s.Dir() != dir2 {
		t.Fatalf("Dir() = %q, want %q", s.Dir(), dir2)
	}
	isos, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(isos) != 1 || isos[0].Id != "ubuntu.iso" {
		t.Fatalf("unexpected list: %+v", isos)
	}
	// and the new dir was created if missing
	dir3 := filepath.Join(dir2, "nested", "lib")
	if err := s.SetDir(dir3); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir3); err != nil || !fi.IsDir() {
		t.Fatalf("SetDir did not create directory: %v", err)
	}
	if _, err := s.Create("x.iso", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
}
