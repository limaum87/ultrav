package libvirt

// Guards against the one failure mode a containerized backend cannot see
// coming: a path that exists for this process but not for the hypervisor.
//
// Every path we write into the domain XML is opened by qemu, on the host.
// os.Stat only proves the file exists in *this* mount namespace, so a backend
// whose ISO library is a Docker named volume passes every validation and then
// produces a domain that cannot start:
//
//	error: Cannot access storage file '/var/lib/ultrav/isos/virtio-win.iso'
//
// Two mitigations live here: a best-effort namespace check used to warn at
// create time, and the translation of that libvirt error into an actionable
// one at start time.

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// cannotAccessRe matches libvirt's error for a file qemu could not open. The
// wording has been stable across libvirt releases, and the error code it
// arrives with has not (it is reported as a plain system error), so the
// message is what we match on.
var cannotAccessRe = regexp.MustCompile(`Cannot access (?:storage|backing) file '([^']*)'`)

// asStorageUnavailable converts libvirt's "Cannot access storage file" into
// hypervisor.ErrStorageUnavailable, which the API renders as an actionable
// message instead of a generic 500. Any other error is returned untouched.
func asStorageUnavailable(err error) error {
	if err == nil {
		return nil
	}
	m := cannotAccessRe.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	path := m[1]
	return &hypervisor.StorageUnavailableError{Path: path, Hint: visibilityHint(path)}
}

// visibilityHint explains, when we can tell, why the hypervisor cannot see a
// path this process can. Empty when there is nothing useful to add.
func visibilityHint(path string) string {
	switch v, hostPath := inspectPath(path); v {
	case pathVisibilityElsewhere:
		return fmt.Sprintf("this backend runs in a container: the path is %s on the host, "+
			"so it is not where the domain XML points", hostPath)
	case pathVisibilityPrivate:
		return "the file is on a container-private filesystem, invisible to the host"
	default:
		return ""
	}
}

// isoVisibilityWarning returns a warning for the create response when the
// hypervisor host most likely cannot see an ISO we are about to reference.
// It never fails the creation: the check reads mount tables, not the host's
// filesystem, so it cannot account for a symlink on the host side that makes
// the path resolve after all. Empty when there is nothing to warn about.
func isoVisibilityWarning(path string) string {
	if path == "" {
		return ""
	}
	switch v, hostPath := inspectPath(path); v {
	case pathVisibilityElsewhere:
		return fmt.Sprintf("The ISO %s may not be visible to the hypervisor: this backend runs "+
			"in a container and the host sees that directory as %s. qemu opens the path written "+
			"in the domain XML, so unless the host resolves it (a symlink, for instance), the VM "+
			"will fail to start with \"Cannot access storage file\". Mount the ISO library at an "+
			"identical path inside and outside the container (see ULTRAV_ISO_DIR).",
			path, filepath.Dir(hostPath))
	case pathVisibilityPrivate:
		return fmt.Sprintf("The ISO %s is on a container-private filesystem and the hypervisor "+
			"cannot open it: the VM will fail to start. Mount the ISO library from the host at "+
			"an identical path (see ULTRAV_ISO_DIR).", path)
	default:
		return ""
	}
}

// logIsoDirVisibility reports, once at startup, an ISO library the hypervisor
// will not be able to read. Cheaper to notice here than on the first failed
// VM start, days later.
func logIsoDirVisibility(isoDir string) {
	switch v, hostPath := inspectPath(isoDir); v {
	case pathVisibilityElsewhere:
		slog.Warn("ISO library is not at the same path on the hypervisor host: "+
			"VMs referencing an ISO will fail to start. Mount it at an identical path "+
			"inside and outside the container (see ULTRAV_ISO_DIR).",
			"isoDir", isoDir, "hostPath", hostPath)
	case pathVisibilityPrivate:
		slog.Warn("ISO library is on a container-private filesystem, invisible to the "+
			"hypervisor: VMs referencing an ISO will fail to start. Mount it from the host "+
			"at an identical path (see ULTRAV_ISO_DIR).",
			"isoDir", isoDir)
	}
}

// pathVisibility describes how a path in this mount namespace relates to the
// host's, which is the namespace qemu resolves domain XML paths in.
type pathVisibility int

const (
	// pathVisibilityUnknown: not enough information to decide. Assume fine.
	pathVisibilityUnknown pathVisibility = iota
	// pathVisibilitySame: the host resolves the very same absolute path.
	pathVisibilitySame
	// pathVisibilityElsewhere: the host sees the file under another path.
	pathVisibilityElsewhere
	// pathVisibilityPrivate: container-private filesystem; the host sees nothing.
	pathVisibilityPrivate
)

// inspectPath works out where the host sees path, from /proc/self/mountinfo.
// For a bind mount the mount's root field is the source path on the host, so
// the host path is that root plus whatever path adds below the mount point.
// Returns the host path for pathVisibilityElsewhere only.
func inspectPath(path string) (pathVisibility, string) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return pathVisibilityUnknown, ""
	}
	defer f.Close()
	return inspectPathIn(path, parseMountinfo(f))
}

func inspectPathIn(path string, mounts []mountEntry) (pathVisibility, string) {
	m, ok := mountFor(path, mounts)
	if !ok {
		return pathVisibilityUnknown, ""
	}
	// The container's own layers: nothing under them exists on the host.
	switch m.fsType {
	case "overlay", "tmpfs", "ramfs":
		return pathVisibilityPrivate, ""
	}
	rel := strings.TrimPrefix(path, m.mountPoint)
	hostPath := filepath.Join(m.root, rel)
	if hostPath == filepath.Clean(path) {
		return pathVisibilitySame, ""
	}
	return pathVisibilityElsewhere, hostPath
}

// mountEntry is the part of a /proc/self/mountinfo line we need.
type mountEntry struct {
	root       string // path of this mount within its filesystem
	mountPoint string // where it is mounted in this namespace
	fsType     string
}

// mountFor returns the most specific mount containing path.
func mountFor(path string, mounts []mountEntry) (mountEntry, bool) {
	path = filepath.Clean(path)
	var best mountEntry
	found := false
	for _, m := range mounts {
		if m.mountPoint != "/" && m.mountPoint != path && !strings.HasPrefix(path, m.mountPoint+"/") {
			continue
		}
		if !found || len(m.mountPoint) > len(best.mountPoint) {
			best, found = m, true
		}
	}
	return best, found
}

// parseMountinfo reads /proc/self/mountinfo. Layout (proc(5)):
//
//	id parent maj:min root mountPoint options [optional...] - fsType source superOpts
func parseMountinfo(r io.Reader) []mountEntry {
	var out []mountEntry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 7 {
			continue
		}
		sep := -1
		for i := 5; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep == -1 || sep+1 >= len(fields) {
			continue
		}
		out = append(out, mountEntry{
			root:       unescapeMount(fields[3]),
			mountPoint: unescapeMount(fields[4]),
			fsType:     fields[sep+1],
		})
	}
	return out
}

// unescapeMount decodes the octal escapes the kernel writes for characters
// that would otherwise break the field layout (space, tab, newline, backslash)
// — ISO filenames are allowed to contain spaces.
var mountEscapes = strings.NewReplacer(
	`\040`, " ",
	`\011`, "\t",
	`\012`, "\n",
	`\134`, `\`,
)

func unescapeMount(s string) string { return mountEscapes.Replace(s) }
