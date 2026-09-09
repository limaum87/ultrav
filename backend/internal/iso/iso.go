// Package iso implements the ISO image library: a dedicated directory on
// the host where installation media is stored (uploaded via the browser)
// and served to the VM creation wizard.
package iso

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// ValidID matches safe ISO filenames (basename, no traversal).
var ValidID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._ -]{0,127}\.iso$`)

// ErrNotFound is returned when the requested ISO does not exist.
var ErrNotFound = errors.New("ISO image was not found")

// ErrAlreadyExists is returned when uploading over an existing filename.
var ErrAlreadyExists = errors.New("an ISO image with this filename already exists")

// Store is the filesystem-backed ISO library.
type Store struct {
	dir string
}

// New creates the store, ensuring the directory exists.
func New(dir string) (*Store, error) {
	if dir == "" {
		dir = "/var/lib/libvirt/images/isos"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the backing directory (used by the libvirt provider to
// resolve attachment paths).
func (s *Store) Dir() string { return s.dir }

// List returns every .iso file in the library, sorted by name.
func (s *Store) List() ([]types.Iso, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]types.Iso, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, toModel(e.Name(), info))
	}
	// sorted by ReadDir already (lexicographic)
	return out, nil
}

// Create streams a new ISO into the library (temp file + atomic rename).
// name must pass ValidID.
func (s *Store) Create(name string, r io.Reader) (types.Iso, error) {
	if !ValidID.MatchString(name) {
		return types.Iso{}, os.ErrInvalid
	}
	dst := s.path(name)
	if _, err := os.Stat(dst); err == nil {
		return types.Iso{}, ErrAlreadyExists
	}
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return types.Iso{}, err
	}
	defer os.Remove(tmp.Name()) // no-op after successful rename

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return types.Iso{}, err
	}
	if err := tmp.Close(); err != nil {
		return types.Iso{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return types.Iso{}, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return types.Iso{}, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return types.Iso{}, err
	}
	return toModel(name, info), nil
}

// Delete removes an ISO from the library.
func (s *Store) Delete(id string) error {
	if !ValidID.MatchString(id) {
		return os.ErrInvalid
	}
	if err := os.Remove(s.path(id)); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// Path resolves an ISO id to its absolute path.
func (s *Store) Path(id string) (string, error) {
	if !ValidID.MatchString(id) {
		return "", os.ErrInvalid
	}
	p := s.path(id)
	if _, err := os.Stat(p); err != nil {
		return "", ErrNotFound
	}
	return p, nil
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id) }

func toModel(name string, info os.FileInfo) types.Iso {
	uploaded := info.ModTime().Round(time.Second).UTC()
	return types.Iso{
		Id:         name,
		FileName:   name,
		SizeBytes:  info.Size(),
		UploadedAt: &uploaded,
	}
}
