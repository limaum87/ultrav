// Package settings persists a handful of runtime settings (currently the ISO
// library directory) as a small JSON file next to the main database, so
// choices made through the API survive backend restarts.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// data is the on-disk shape of the settings file.
type data struct {
	// IsoDir overrides the ULTRAV_ISO_DIR environment variable when non-empty.
	IsoDir string `json:"isoDir,omitempty"`
}

// Store is the file-backed settings store.
type Store struct {
	mu   sync.Mutex
	path string
	d    data
}

// Open loads (or creates) the settings file at path.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil // defaults
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.d); err != nil {
		return nil, err
	}
	return s, nil
}

// IsoDir returns the persisted ISO library directory ("" = not overridden).
func (s *Store) IsoDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.IsoDir
}

// SetIsoDir persists the ISO library directory override (empty string clears it).
func (s *Store) SetIsoDir(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.IsoDir = dir
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
