package libvirt

import (
	"fmt"
	"regexp"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// libvirt reports most start-time failures as opaque virErrors whose code has
// changed across releases; the human message is the only stable identifier.
// These regexes match the well-known ones so the API can answer with an
// actionable 4xx instead of a generic 500.

// networkInactiveRe matches e.g.:
//
//	Requested operation is not valid: network 'default' is not active
var networkInactiveRe = regexp.MustCompile(`network '([^']*)' is not active`)

// kvmRe matches the KVM-unavailable family, e.g.:
//
//	internal error: qemu unexpectedly closed the monitor: ... failed to initialize kvm: No such file or directory
//	Request operation failed: ... /dev/kvm: Permission denied
//	internal error: process exited while connecting to monitor: ... KVM is not available
var kvmRe = regexp.MustCompile(`failed to initialize kvm[^\n]*|/dev/kvm[^\n]*|KVM is not available`)

// asActionableError converts a libvirt error whose message identifies a known,
// operator-fixable cause into a typed hypervisor error the API layer renders
// as a clear 4xx. Errors it does not recognize are returned untouched and end
// up as a generic 500 with the cause in the logs.
func asActionableError(err error) error {
	if err == nil {
		return nil
	}
	if m := networkInactiveRe.FindStringSubmatch(err.Error()); m != nil {
		return &hypervisor.NetworkInactiveError{Network: m[1]}
	}
	if m := kvmRe.FindStringSubmatch(err.Error()); m != nil {
		return &hypervisor.KVMUnavailableError{Reason: fmt.Sprintf("%s", m[0])}
	}
	return asStorageUnavailable(err)
}
