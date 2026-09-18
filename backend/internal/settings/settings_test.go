package settings

import (
	"path/filepath"
	"testing"
)

func TestIsoDirOverridePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.IsoDir(); got != "" {
		t.Fatalf("expected empty override, got %q", got)
	}
	if err := s.SetIsoDir("/var/lib/libvirt/isos"); err != nil {
		t.Fatal(err)
	}
	// reopen: the override must survive a restart
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.IsoDir(); got != "/var/lib/libvirt/isos" {
		t.Fatalf("override not persisted, got %q", got)
	}
	if err := s2.SetIsoDir(""); err != nil {
		t.Fatal(err)
	}
	if s3, _ := Open(path); s3.IsoDir() != "" {
		t.Fatalf("expected override cleared, got %q", s3.IsoDir())
	}
}
