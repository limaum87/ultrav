// Package backup implements the on-disk backup library shared by the
// hypervisor providers. Layout:
//
//	<dir>/<vm-name>/<backup-id>/
//	  ├── metadata.json   (point metadata, written last with state=complete)
//	  ├── domain.xml      (saved domain configuration)
//	  └── <target>.qcow2  (one file per disk)
//
// A backup point is only visible to the API once metadata.json exists with
// state "complete": the metadata is written last, so a crashed backup never
// leaves a half-written point in the listing.
package backup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// IDPattern matches a backup point identifier (also the contract's path
// parameter pattern, keeping the filesystem names and the API aligned).
var IDPattern = regexp.MustCompile(`^bk-[a-zA-Z0-9._-]{1,80}$`)

// Errors returned by the store. Handlers map them to public error codes.
var (
	// ErrNotFound is returned when the backup point does not exist.
	ErrNotFound = errors.New("backup was not found")
	// ErrInvalidID is returned for identifiers that could escape the
	// backup directory (defensive: the API already validates the pattern).
	ErrInvalidID = errors.New("invalid backup identifier")
)

// Format is the on-disk format of a backed-up disk image.
type Format string

const (
	// FormatQcow2 is the qcow2 image format (the default for backups).
	FormatQcow2 Format = "qcow2"
	// FormatRaw is a raw image copy.
	FormatRaw Format = "raw"
)

// State is the lifecycle state of a backup point.
type State string

// StateComplete means the copy finished and the point is usable.
const StateComplete State = "complete"

// Type is the backup kind. Slice 1 implements full only; incremental will
// join this enum when checkpoint chains land.
type Type string

// TypeFull copies every disk in full.
const TypeFull Type = "full"

// Disk describes one disk image inside a backup point.
type Disk struct {
	// Name is the disk target inside the domain (e.g. "vda").
	Name string `json:"name"`
	// File is the image file name inside the backup point directory.
	File string `json:"file"`
	// Format is the image format (qcow2, raw).
	Format Format `json:"format"`
	// SizeBytes is the size of the backup image on the backup storage.
	SizeBytes int64 `json:"sizeBytes"`
}

// Metadata is the persisted descriptor of a backup point (metadata.json).
type Metadata struct {
	ID           string    `json:"id"`
	VMID         string    `json:"vmId"`
	VMName       string    `json:"vmName"`
	Type         Type      `json:"type"`
	State        State     `json:"state"`
	CreatedAt    time.Time `json:"createdAt"`
	Disks        []Disk    `json:"disks"`
	HasDomainXML bool      `json:"hasDomainXml"`
}

// Store is the backup library rooted at a directory.
type Store struct {
	dir string
}

// New opens (creating if needed) the backup library at dir.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the root directory of the library.
func (s *Store) Dir() string { return s.dir }

// Point is a handle to one backup point: while it is being written (provider
// fills in disks and calls Complete) and afterwards (read, delete).
type Point struct {
	store *Store
	meta  Metadata
	dir   string
}

// Create starts a new backup point for the VM, allocating its directory and
// generating the identifier. The point becomes visible to List/Get only
// after Complete succeeds.
func (s *Store) Create(vmID string) (*Point, error) {
	if !idSafe(vmID) {
		return nil, fmt.Errorf("%w: unsafe VM name %q", ErrInvalidID, vmID)
	}
	pt := &Point{
		store: s,
		dir:   filepath.Join(s.dir, vmID, newID()),
		meta: Metadata{
			VMID:      vmID,
			VMName:    vmID,
			Type:      TypeFull,
			CreatedAt: time.Now().UTC(),
		},
	}
	pt.meta.ID = filepath.Base(pt.dir)
	if err := os.MkdirAll(pt.dir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup point directory: %w", err)
	}
	return pt, nil
}

// ID returns the backup point identifier.
func (p *Point) ID() string { return p.meta.ID }

// Meta returns the current (in-memory) metadata.
func (p *Point) Meta() Metadata { return p.meta }

// Dir returns the directory holding this point's files.
func (p *Point) Dir() string { return p.dir }

// DiskPath returns the path where the named disk image must be written.
func (p *Point) DiskPath(name string, format Format) string {
	file := name + "." + string(format)
	p.meta.Disks = append(p.meta.Disks, Disk{Name: name, File: file, Format: format})
	return filepath.Join(p.dir, file)
}

// SetDiskSize records the on-disk size of a previously allocated disk.
func (p *Point) SetDiskSize(name string, size int64) {
	for i := range p.meta.Disks {
		if p.meta.Disks[i].Name == name {
			p.meta.Disks[i].SizeBytes = size
		}
	}
}

// SetDomainXML records (or removes, with false) the presence of domain.xml.
func (p *Point) SetDomainXML(has bool) { p.meta.HasDomainXML = has }

// DomainXMLPath returns the path of the saved domain configuration.
func (p *Point) DomainXMLPath() string { return filepath.Join(p.dir, "domain.xml") }

// Complete persists the metadata with state=complete, making the point
// visible. Call once all files are in place.
func (p *Point) Complete() error {
	p.meta.State = StateComplete
	return p.save()
}

// Abort removes a failed backup point's directory (best effort).
func (p *Point) Abort() { _ = os.RemoveAll(p.dir) }

// save writes metadata.json.
func (p *Point) save() error {
	b, err := json.MarshalIndent(p.meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.dir, "metadata.json"), b, 0o644)
}

// pointFromDir loads metadata.json from a backup point directory.
func pointFromDir(dir string) (Metadata, error) {
	b, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return Metadata{}, err
	}
	var m Metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return Metadata{}, fmt.Errorf("corrupt metadata.json in %s: %w", dir, err)
	}
	return m, nil
}

// List returns the completed backup points of a VM, newest first. VMs with
// no backups return an empty slice.
func (s *Store) List(vmID string) ([]Metadata, error) {
	if !idSafe(vmID) {
		return nil, fmt.Errorf("%w: unsafe VM name %q", ErrInvalidID, vmID)
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, vmID))
	if os.IsNotExist(err) {
		return []Metadata{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Metadata
	for _, e := range entries {
		if !e.IsDir() || !IDPattern.MatchString(e.Name()) {
			continue
		}
		m, err := pointFromDir(filepath.Join(s.dir, vmID, e.Name()))
		if err != nil || m.State != StateComplete {
			continue // invisible or corrupt points are skipped, not fatal
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Get returns the metadata of one backup point (searched across all VMs,
// since the API addresses points by their globally unique id).
func (s *Store) Get(id string) (Metadata, error) {
	dir, err := s.pointDir(id)
	if err != nil {
		return Metadata{}, err
	}
	m, err := pointFromDir(dir)
	if err != nil || m.State != StateComplete {
		return Metadata{}, ErrNotFound
	}
	return m, nil
}

// PointDir returns the directory of a backup point (for the provider to read
// its images). Incomplete or missing points return ErrNotFound.
func (s *Store) PointDir(id string) (string, error) {
	dir, err := s.pointDir(id)
	if err != nil {
		return "", err
	}
	if _, err := pointFromDir(dir); err != nil {
		return "", ErrNotFound
	}
	return dir, nil
}

// Delete removes a backup point's directory.
func (s *Store) Delete(id string) error {
	if !IDPattern.MatchString(id) {
		return ErrInvalidID
	}
	dir, err := s.pointDir(id)
	if err != nil {
		return err
	}
	if _, err := pointFromDir(dir); err != nil {
		return ErrNotFound
	}
	return os.RemoveAll(dir)
}

// pointDir resolves and validates the directory of a backup id.
func (s *Store) pointDir(id string) (string, error) {
	if !IDPattern.MatchString(id) {
		return "", ErrInvalidID
	}
	matches, err := filepath.Glob(filepath.Join(s.dir, "*", id))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", ErrNotFound
	}
	return matches[0], nil
}

// newID generates a time-ordered, collision-resistant identifier.
func newID() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err)) // unreachable in practice
	}
	return fmt.Sprintf("bk-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(b[:]))
}

// idSafe rejects names that could escape the library directory.
func idSafe(name string) bool {
	return name == filepath.Base(name) && name != "." && name != ".." && name != ""
}
