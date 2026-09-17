package libvirt

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

func TestInspectPathIn(t *testing.T) {
	const (
		// Backend in a container, ISO library on a Docker named volume: the
		// bug this guard exists for.
		namedVolume = `1861 1802 259:2 /var/lib/docker/volumes/deploy_ultrav-isos/_data /var/lib/ultrav/isos rw,relatime - ext4 /dev/nvme0n1p2 rw`
		// The fix: same absolute path on both sides.
		identityBind = `1862 1802 259:2 /var/lib/libvirt/isos /var/lib/libvirt/isos rw,relatime - ext4 /dev/nvme0n1p2 rw`
		// Bind mount whose target differs from its source on the host.
		movedBind = `1863 1802 259:2 /var/lib/libvirt/isos /var/lib/ultrav/isos rw,relatime - ext4 /dev/nvme0n1p2 rw`
		// The container's own root: nothing under it exists on the host.
		overlayRoot = `1801 1800 0:95 / / rw,relatime - overlay overlay rw,lowerdir=/var/lib/docker/overlay2/l/ABC`
		// Backend running natively on the host.
		hostRoot = `29 1 259:2 / / rw,relatime - ext4 /dev/nvme0n1p2 rw`
	)

	tests := []struct {
		name      string
		mountinfo string
		path      string
		want      pathVisibility
		wantHost  string
	}{
		{
			name:      "named volume is elsewhere on the host",
			mountinfo: overlayRoot + "\n" + namedVolume,
			path:      "/var/lib/ultrav/isos/virtio-win-0.1.302.iso",
			want:      pathVisibilityElsewhere,
			wantHost:  "/var/lib/docker/volumes/deploy_ultrav-isos/_data/virtio-win-0.1.302.iso",
		},
		{
			name:      "identity bind mount is the same path",
			mountinfo: overlayRoot + "\n" + identityBind,
			path:      "/var/lib/libvirt/isos/virtio-win-0.1.302.iso",
			want:      pathVisibilitySame,
		},
		{
			name:      "bind mount with a different target is elsewhere",
			mountinfo: overlayRoot + "\n" + movedBind,
			path:      "/var/lib/ultrav/isos/win10.iso",
			want:      pathVisibilityElsewhere,
			wantHost:  "/var/lib/libvirt/isos/win10.iso",
		},
		{
			name:      "container-private filesystem",
			mountinfo: overlayRoot,
			path:      "/var/lib/ultrav/isos/win10.iso",
			want:      pathVisibilityPrivate,
		},
		{
			name:      "native backend sees the host paths",
			mountinfo: hostRoot,
			path:      "/var/lib/libvirt/isos/win10.iso",
			want:      pathVisibilitySame,
		},
		{
			name:      "no mount information at all",
			mountinfo: "",
			path:      "/var/lib/libvirt/isos/win10.iso",
			want:      pathVisibilityUnknown,
		},
		{
			name:      "most specific mount wins",
			mountinfo: hostRoot + "\n" + namedVolume,
			path:      "/var/lib/ultrav/isos/win10.iso",
			want:      pathVisibilityElsewhere,
			wantHost:  "/var/lib/docker/volumes/deploy_ultrav-isos/_data/win10.iso",
		},
		{
			name:      "sibling directory is not inside the mount",
			mountinfo: hostRoot + "\n" + namedVolume,
			path:      "/var/lib/ultrav/isos-backup/win10.iso",
			want:      pathVisibilitySame,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hostPath := inspectPathIn(tt.path, parseMountinfo(strings.NewReader(tt.mountinfo)))
			if got != tt.want {
				t.Errorf("visibility = %v, want %v", got, tt.want)
			}
			if hostPath != tt.wantHost {
				t.Errorf("hostPath = %q, want %q", hostPath, tt.wantHost)
			}
		})
	}
}

// Filenames in the ISO library may contain spaces, which the kernel escapes.
func TestParseMountinfoUnescapes(t *testing.T) {
	line := `1861 1802 259:2 /srv/my\040isos /var/lib/ultrav/isos rw,relatime - ext4 /dev/nvme0n1p2 rw`
	mounts := parseMountinfo(strings.NewReader(line))
	if len(mounts) != 1 {
		t.Fatalf("parsed %d mounts, want 1", len(mounts))
	}
	if mounts[0].root != "/srv/my isos" {
		t.Errorf("root = %q, want %q", mounts[0].root, "/srv/my isos")
	}
}

// Optional fields (an arbitrary number of them) sit between the mount options
// and the "-" separator, so the separator must be searched for, not assumed.
func TestParseMountinfoOptionalFields(t *testing.T) {
	line := `1861 1802 259:2 /src /dst rw,relatime shared:1 master:2 - xfs /dev/sda1 rw`
	mounts := parseMountinfo(strings.NewReader(line))
	if len(mounts) != 1 {
		t.Fatalf("parsed %d mounts, want 1", len(mounts))
	}
	if mounts[0].fsType != "xfs" {
		t.Errorf("fsType = %q, want xfs", mounts[0].fsType)
	}
}

func TestAsStorageUnavailable(t *testing.T) {
	path := "/var/lib/ultrav/isos/virtio-win-0.1.302.iso"
	libvirtErr := fmt.Errorf("Cannot access storage file '%s': No such file or directory", path)

	err := asStorageUnavailable(libvirtErr)
	if !errors.Is(err, hypervisor.ErrStorageUnavailable) {
		t.Fatalf("errors.Is(err, ErrStorageUnavailable) = false, err = %v", err)
	}
	var se *hypervisor.StorageUnavailableError
	if !errors.As(err, &se) {
		t.Fatalf("errors.As did not yield *StorageUnavailableError, err = %v", err)
	}
	if se.Path != path {
		t.Errorf("Path = %q, want %q", se.Path, path)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("message %q does not name the path", err.Error())
	}
}

func TestAsStorageUnavailableBackingFile(t *testing.T) {
	err := asStorageUnavailable(errors.New("Cannot access backing file '/pool/base.qcow2' of storage file '/pool/vm.qcow2'"))
	var se *hypervisor.StorageUnavailableError
	if !errors.As(err, &se) {
		t.Fatalf("errors.As did not yield *StorageUnavailableError, err = %v", err)
	}
	if se.Path != "/pool/base.qcow2" {
		t.Errorf("Path = %q, want /pool/base.qcow2", se.Path)
	}
}

func TestAsStorageUnavailablePassesOtherErrorsThrough(t *testing.T) {
	orig := errors.New("Domain not found: no domain with matching name 'web01'")
	if got := asStorageUnavailable(orig); got != orig {
		t.Errorf("error was rewritten: %v", got)
	}
	if got := asStorageUnavailable(nil); got != nil {
		t.Errorf("nil became %v", got)
	}
}

func TestIsoVisibilityWarning(t *testing.T) {
	if w := isoVisibilityWarning(""); w != "" {
		t.Errorf("empty path produced a warning: %q", w)
	}
}
