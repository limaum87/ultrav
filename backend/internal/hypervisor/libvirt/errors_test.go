package libvirt

import (
	"errors"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

func TestAsActionableError(t *testing.T) {
	tests := []struct {
		name  string
		err   string
		match error
		check func(error) bool
	}{
		{
			name:  "inactive network",
			err:   "virError(Code=55, Domain=19, Message='Requested operation is not valid: network 'default' is not active')",
			match: hypervisor.ErrNetworkInactive,
		},
		{
			name:  "kvm missing",
			err:   "internal error: qemu unexpectedly closed the monitor: 2025-01-01T00:00:00.000000Z qemu-system-x86_64: failed to initialize kvm: No such file or directory",
			match: hypervisor.ErrKVMUnavailable,
		},
		{
			name:  "kvm permission denied",
			err:   "internal error: process exited while connecting to monitor: qemu-system-x86_64: /dev/kvm: Permission denied",
			match: hypervisor.ErrKVMUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := asActionableError(errors.New(tc.err))
			if !errors.Is(got, tc.match) {
				t.Fatalf("asActionableError() = %v, want %v", got, tc.match)
			}
			if got.Error() == tc.err {
				t.Fatal("expected the message to be rewritten with an actionable hint")
			}
		})
	}

	t.Run("carries network name", func(t *testing.T) {
		got := asActionableError(errors.New("Requested operation is not valid: network 'default' is not active"))
		var ne *hypervisor.NetworkInactiveError
		if !errors.As(got, &ne) || ne.Network != "default" {
			t.Fatalf("got %v, want NetworkInactiveError{default}", got)
		}
	})

	t.Run("storage passthrough", func(t *testing.T) {
		got := asActionableError(errors.New("Cannot access storage file '/var/lib/libvirt/images/vm.qcow2' (as uid:65534): Permission denied"))
		if !errors.Is(got, hypervisor.ErrStorageUnavailable) {
			t.Fatalf("got %v, want ErrStorageUnavailable", got)
		}
	})

	t.Run("unknown error untouched", func(t *testing.T) {
		orig := errors.New("some opaque libvirt failure")
		if got := asActionableError(orig); got != orig {
			t.Fatalf("got %v, want the original error untouched", got)
		}
	})

	t.Run("nil is nil", func(t *testing.T) {
		if got := asActionableError(nil); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}
