// Package hypervisor defines the single boundary between the domain/API layer
// and the virtualization stack (libvirt/KVM/QEMU). Handlers must never talk to
// libvirt directly; the domain never depends on concrete providers.
package hypervisor

import (
	"context"
	"errors"

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

	StartVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	ShutdownVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	RebootVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)
	ForceStopVirtualMachine(ctx context.Context, id string) (types.VirtualMachine, error)

	ListStoragePools(ctx context.Context) ([]types.StoragePool, error)
	GetStoragePool(ctx context.Context, id string) (types.StoragePool, error)
	RefreshStoragePool(ctx context.Context, id string) (types.StoragePool, error)

	ListNetworks(ctx context.Context) ([]types.Network, error)
	GetNetwork(ctx context.Context, id string) (types.Network, error)
	StartNetwork(ctx context.Context, id string) (types.Network, error)
	StopNetwork(ctx context.Context, id string) (types.Network, error)
}

var (
	// ErrVMNotFound is returned when the requested VM does not exist.
	ErrVMNotFound = errVMNotFound{}
	// ErrInvalidVMState is returned when an operation is not valid for the
	// current VM state (e.g. starting an already running VM).
	ErrInvalidVMState = errInvalidVMState{}
	// ErrPoolNotFound is returned when the requested storage pool does not exist.
	ErrPoolNotFound = errors.New("storage pool was not found")
	// ErrNetworkNotFound is returned when the requested network does not exist.
	ErrNetworkNotFound = errors.New("network was not found")
	// ErrInvalidNetworkState is returned when a network operation is not valid
	// for the current network state (e.g. starting an active network).
	ErrInvalidNetworkState = errors.New("operation is not valid for the current network state")
)

type errVMNotFound struct{}

func (errVMNotFound) Error() string { return "virtual machine was not found" }

type errInvalidVMState struct{}

func (errInvalidVMState) Error() string { return "operation is not valid for the current virtual machine state" }
