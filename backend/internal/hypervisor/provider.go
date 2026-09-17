// Package hypervisor defines the single boundary between the domain/API layer
// and the virtualization stack (libvirt/KVM/QEMU). Handlers must never talk to
// libvirt directly; the domain never depends on concrete providers.
package hypervisor

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// Provider is the interface every hypervisor backend must implement.
// Operations are semantically specific by design (agent-friendly, scopeable).
type Provider interface {
	// Ready reports whether the provider is connected and able to serve.
	Ready(ctx context.Context) error

	GetHost(ctx context.Context) (types.Host, error)
	GetCapabilities(ctx context.Context) (types.Capabilities, error)

	ListVirtualMachines(ctx context.Context) ([]types.VirtualMachine, error)
	GetVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	// CreateVirtualMachine defines a new VM (volume + domain). Returns
	// ErrVMAlreadyExists if the name is taken, ErrPoolNotFound for an unknown
	// storage pool, ErrNetworkNotFound for an unknown network.
	CreateVirtualMachine(ctx context.Context, req types.VirtualMachineCreate) (types.VirtualMachine, error)

	StartVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	// OpenVMConsole returns a live stream (VNC/RFB) to the VM's graphical
	// console. The caller owns the connection and must Close it. Returns
	// ErrVMInvalidState when the VM is not running.
	OpenVMConsole(ctx context.Context, id string) (net.Conn, error)
	ShutdownVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	RebootVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	ForceStopVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	// UpdateVirtualMachine changes VM settings (vcpus, memoryBytes, isoId).
	// Only provided fields are changed. vCPU/memory changes require the VM to
	// be stopped (ErrInvalidVMState otherwise); the ISO can be swapped in any
	// state and applies on the next boot. Returns ErrVMNotFound,
	// ErrInvalidVMState or ErrIsoNotFound.
	UpdateVirtualMachine(ctx context.Context, id string, req types.VirtualMachineUpdate) (types.VirtualMachine, error)
	// DeleteVirtualMachine removes a VM. When deleteDisks is true the disk
	// volumes are also deleted from their storage pool; otherwise only the
	// domain definition is removed. Requires the VM to be stopped (returns
	// ErrInvalidVMState otherwise). Returns ErrVMNotFound for an unknown VM.
	DeleteVirtualMachine(ctx context.Context, id string, deleteDisks bool) error

	ListStoragePools(ctx context.Context) ([]types.StoragePool, error)
	GetStoragePool(ctx context.Context, id string) (types.StoragePool, error)
	CreateStoragePool(ctx context.Context, req types.StoragePoolCreate) (types.StoragePool, error)
	RefreshStoragePool(ctx context.Context, id string) (types.StoragePool, error)

	ListNetworks(ctx context.Context) ([]types.Network, error)
	GetNetwork(ctx context.Context, id string) (types.Network, error)
	// CreateNetwork defines and starts a virtual network (NAT, bridge to a
	// host bridge, or isolated). Returns ErrNetworkAlreadyExists if the name
	// is taken.
	CreateNetwork(ctx context.Context, req types.NetworkCreate) (types.Network, error)
	StartNetwork(ctx context.Context, id string) (types.Network, error)
	StopNetwork(ctx context.Context, id string) (types.Network, error)
	// UpdateNetwork applies a partial update to a network's persistent
	// definition. Structural changes require the network to be inactive;
	// autostart can change at any time. Name is immutable.
	UpdateNetwork(ctx context.Context, id string, req types.NetworkUpdate) (types.Network, error)
	// DeleteNetwork undefines a network; it must be inactive.
	DeleteNetwork(ctx context.Context, id string) error
	// ListHostBridges returns Linux bridges configured on the host (e.g. br0),
	// which bridge-mode virtual networks attach to.
	ListHostBridges(ctx context.Context) ([]types.HostBridge, error)
}

var (
	// ErrVMNotFound is returned when the requested VM does not exist.
	ErrVMNotFound = errVMNotFound{}
	// ErrVMAlreadyExists is returned when creating a VM whose name is taken.
	ErrVMAlreadyExists = errors.New("a virtual machine with this name already exists")
	// ErrConsoleUnavailable is returned when the VM has no graphical console
	// (no <graphics> device in its domain XML).
	ErrConsoleUnavailable = errors.New("this virtual machine has no graphical console configured")
	// ErrInvalidVMState is returned when an operation is not valid for the
	// current VM state (e.g. starting an already running VM).
	ErrInvalidVMState = errInvalidVMState{}
	// ErrPoolAlreadyExists is returned when creating a storage pool whose name
	// is taken.
	ErrPoolAlreadyExists = errors.New("a storage pool with this name already exists")
	// ErrPoolNotFound is returned when the requested storage pool does not exist.
	ErrPoolNotFound = errors.New("storage pool was not found")
	// ErrPoolInsufficientSpace is returned when creating a VM whose disk does
	// not fit in the storage pool's available space.
	ErrPoolInsufficientSpace = errors.New("insufficient space in storage pool")
	// ErrNetworkNotFound is returned when the requested network does not exist.
	ErrNetworkNotFound = errors.New("network was not found")
	// ErrNetworkAlreadyExists is returned when creating a network whose name
	// is taken.
	ErrNetworkAlreadyExists = errors.New("a network with this name already exists")
	// ErrIsoNotFound is returned when the referenced ISO image does not exist.
	ErrIsoNotFound = errors.New("ISO image was not found")
	// ErrInvalidNetworkState is returned when a network operation is not valid
	// for the current network state (e.g. starting an active network).
	ErrInvalidNetworkState = errors.New("operation is not valid for the current network state")
	// ErrStorageUnavailable is returned when the hypervisor itself cannot open
	// a file a domain references (install ISO, disk volume). Match it with
	// errors.Is; the concrete *StorageUnavailableError carries the path.
	ErrStorageUnavailable = errors.New("the hypervisor cannot access a file this virtual machine references")
)

// StorageUnavailableError is the concrete form of ErrStorageUnavailable. It
// exists because the path is the whole diagnostic: the file is reachable from
// the backend (validation passed) but not from the hypervisor, which is what
// actually opens it. The usual cause is a containerized backend whose storage
// directory is not mounted at an identical path on the host.
type StorageUnavailableError struct {
	// Path is the file as written in the domain XML.
	Path string
	// Hint explains why the hypervisor cannot see it, when that can be
	// determined. May be empty.
	Hint string
}

func (e *StorageUnavailableError) Error() string {
	msg := fmt.Sprintf("the hypervisor cannot access %q", e.Path)
	if e.Hint != "" {
		msg += " — " + e.Hint
	}
	return msg
}

// Is makes errors.Is(err, ErrStorageUnavailable) match any instance.
func (e *StorageUnavailableError) Is(target error) bool { return target == ErrStorageUnavailable }

type errVMNotFound struct{}

func (errVMNotFound) Error() string { return "virtual machine was not found" }

type errInvalidVMState struct{}

func (errInvalidVMState) Error() string {
	return "operation is not valid for the current virtual machine state"
}
