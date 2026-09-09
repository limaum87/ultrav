// Package mock implements an in-memory hypervisor.Provider that simulates a
// small KVM host ("kvm01") so the whole application can be developed, tested
// and demonstrated without a real KVM/QEMU server.
package mock

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

const (
	hostName        = "kvm01"
	hostCPUModel    = "AMD EPYC 7402P 24-Core Processor"
	hostCPUCores    = 24
	hostCPUThreads  = 48
	hostMemoryBytes = int64(64) * 1024 * 1024 * 1024 // 64 GB
	osName          = "Ubuntu 24.04 LTS"
	kernel          = "6.8.0-45-generic"
)

// Addressable values (pointers are exposed in generated types).
var (
	hostDiskBytes  = int64(2) * 1024 * 1024 * 1024 * 1024 // 2 TB
	qemuVersion    = "8.2.0"
	libvirtVersion = "10.0.0"
)

const (
	// shutdownGrace is how long a graceful shutdown takes to complete.
	shutdownGrace = 3 * time.Second

	// baseMemoryUse is the host memory used with no VMs running.
	baseMemoryUse = int64(8) * 1024 * 1024 * 1024 // 8 GB
	// baseCPUUse is the host CPU usage with no VMs running.
	baseCPUUse = 6.0
)

// vmSpec is the static definition of a simulated VM.
type vmSpec struct {
	id      string
	vcpus   int
	memory  int64
	os      string
	diskGB  int
	ip      string // assigned while running
	mac     string
}

var specs = []vmSpec{
	{id: "erp01", vcpus: 4, memory: 8 * 1024 * 1024 * 1024, os: "Ubuntu 24.04 LTS", diskGB: 120, ip: "10.0.0.11", mac: "52:54:00:00:00:11"},
	{id: "web01", vcpus: 2, memory: 4 * 1024 * 1024 * 1024, os: "Debian 12", diskGB: 40, ip: "10.0.0.12", mac: "52:54:00:00:00:12"},
	{id: "database01", vcpus: 8, memory: 32 * 1024 * 1024 * 1024, os: "Ubuntu 22.04 LTS", diskGB: 500, ip: "10.0.0.13", mac: "52:54:00:00:00:13"},
	{id: "monitoring01", vcpus: 2, memory: 4 * 1024 * 1024 * 1024, os: "Debian 12", diskGB: 60, ip: "10.0.0.14", mac: "52:54:00:00:00:14"},
}

// vmState is the mutable runtime state of one simulated VM.
type vmState struct {
	spec      vmSpec
	state     types.VMState
	bootTime  time.Time // zero when not running
	ipAddress string    // empty when not running
	// disks overrides the disk list derived from spec (used by created VMs);
	// nil means "derive from spec".
	disks []types.Disk
}

// Provider is an in-memory simulated KVM host.
type Provider struct {
	mu    sync.Mutex
	vms   map[string]*vmState
	rng   *rand.Rand
	start time.Time
}

// New creates the mock provider with the simulated fleet. VMs erp01, web01
// and database01 start running (with staggered boot times); monitoring01
// starts stopped.
func New() *Provider {
	now := time.Now()
	p := &Provider{
		vms: make(map[string]*vmState, len(specs)),
		rng: rand.New(rand.NewSource(now.UnixNano())),
		start: now,
	}
	boots := map[string]time.Duration{
		"erp01":       96 * time.Hour,
		"web01":       240 * time.Hour,
		"database01":  720 * time.Hour,
		"monitoring01": 0,
	}
	for _, s := range specs {
		st := &vmState{spec: s, state: types.VMStateStopped}
		if d, ok := boots[s.id]; ok && d > 0 {
			st.state = types.VMStateRunning
			st.bootTime = now.Add(-d)
			st.ipAddress = s.ip
		}
		p.vms[s.id] = st
	}
	return p
}

// Ready always succeeds: the mock is available as soon as it is constructed.
func (p *Provider) Ready(_ context.Context) error { return nil }

// GetHost returns the simulated host kvm01 with live-feeling metrics derived
// from the number of running VMs plus a small deterministic jitter.
func (p *Provider) GetHost(_ context.Context) (types.Host, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	runningMem := int64(0)
	runningCount := 0
	for _, vm := range p.vms {
		if vm.state == types.VMStateRunning || vm.state == types.VMStateStarting || vm.state == types.VMStateShuttingDown {
			runningCount++
			runningMem += vm.spec.memory / 2 // guests use ~50% of assigned RAM
		}
	}
	memUsed := baseMemoryUse + runningMem
	cpuUse := baseCPUUse + float64(runningCount)*4.5 + p.jitter(3.0)
	var usage float32 = float32(cpuUse)
	kvmEnabled := true

	return types.Host{
		Hostname:        hostName,
		OperatingSystem: osName,
		Kernel:          kernel,
		UptimeSeconds:   int(time.Since(p.start).Seconds()) + 47*3600,
		Cpu: types.Cpu{
			Model:        hostCPUModel,
			Cores:        hostCPUCores,
			Threads:      hostCPUThreads,
			UsagePercent: &usage,
		},
		MemoryTotalBytes:  hostMemoryBytes,
		MemoryUsedBytes:   memUsed,
		Virtualization:    &types.HostVirtualization{KvmEnabled: &kvmEnabled},
		StorageTotalBytes: &hostDiskBytes,
		StorageUsedBytes:  p.storageUsedLocked(),
		QemuVersion:       &qemuVersion,
		LibvirtVersion:    &libvirtVersion,
	}, nil
}

// GetCapabilities returns the fixed capability set of the simulated host.
func (p *Provider) GetCapabilities(_ context.Context) (types.Capabilities, error) {
	return types.Capabilities{
		Virtualization: types.CapabilitiesVirtualization{Kvm: true, Uefi: true},
		Storage:        types.CapabilitiesStorage{Qcow2: true, Raw: true, Snapshots: true},
		Features:       types.CapabilitiesFeatures{Backup: false, Migration: false, CloudInit: false},
	}, nil
}

// ListVirtualMachines returns every VM, sorted by id for stable output.
func (p *Provider) ListVirtualMachines(_ context.Context) ([]types.VirtualMachine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ids := make([]string, 0, len(p.vms))
	for id := range p.vms {
		ids = append(ids, id)
	}
	sortStrings(ids)

	out := make([]types.VirtualMachine, 0, len(ids))
	for _, id := range ids {
		out = append(out, p.toModel(p.vms[id]))
	}
	return out, nil
}

// GetVirtualMachine returns a single VM by id.
func (p *Provider) GetVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	vm, ok := p.vms[id]
	if !ok {
		return types.VirtualMachine{}, hypervisor.ErrVMNotFound
	}
	return p.toModel(vm), nil
}

// CreateVirtualMachine defines a new simulated VM in the stopped state
// (or running when start is true), deriving an IP/MAC from the fleet size.
func (p *Provider) CreateVirtualMachine(_ context.Context, req types.VirtualMachineCreate) (types.VirtualMachine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.vms[req.Name]; ok {
		return types.VirtualMachine{}, hypervisor.ErrVMAlreadyExists
	}
	n := len(p.vms) + 1
	mac := fmt.Sprintf("52:54:00:00:%02x:%02x", n/256, n%256)
	ip := fmt.Sprintf("10.0.0.%d", 20+n)
	format := types.DiskFormatQcow2
	if req.Disk.Format != nil {
		format = types.DiskFormat(*req.Disk.Format)
	}
	bus := types.DiskBusVirtio

	st := &vmState{
		spec: vmSpec{
			id:     req.Name,
			vcpus:  req.Vcpus,
			memory: req.MemoryBytes,
			os:     "—",
			diskGB: int(req.Disk.SizeBytes / (1024 * 1024 * 1024)),
			ip:     ip,
			mac:    mac,
		},
		state: types.VMStateStopped,
	}
	st.disks = []types.Disk{{
		Name:      "vda",
		Format:    format,
		SizeBytes: req.Disk.SizeBytes,
		Bus:       &bus,
	}}
	if req.Start != nil && *req.Start {
		st.state = types.VMStateRunning
		st.bootTime = time.Now()
		st.ipAddress = ip
	}
	p.vms[req.Name] = st
	return p.toModel(st), nil
}

// StartVirtualMachine powers on a stopped VM.
func (p *Provider) StartVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	vm, err := p.getLocked(id)
	if err != nil {
		return types.VirtualMachine{}, err
	}
	switch vm.state {
	case types.VMStateRunning, types.VMStateStarting, types.VMStateShuttingDown:
		return types.VirtualMachine{}, fmt.Errorf("%w: cannot start %s VM", hypervisor.ErrInvalidVMState, vm.state)
	case types.VMStatePaused:
		vm.state = types.VMStateRunning
	default:
		vm.state = types.VMStateRunning
		vm.bootTime = time.Now()
		vm.ipAddress = vm.spec.ip
	}
	return p.toModel(vm), nil
}

// ShutdownVirtualMachine begins a graceful ACPI shutdown: the VM enters
// shutting-down and transitions to stopped after a short grace period,
// mimicking a guest OS shutting down cleanly.
func (p *Provider) ShutdownVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	p.mu.Lock()
	vm, err := p.getLocked(id)
	if err != nil {
		p.mu.Unlock()
		return types.VirtualMachine{}, err
	}
	if vm.state != types.VMStateRunning {
		err = fmt.Errorf("%w: cannot shutdown %s VM", hypervisor.ErrInvalidVMState, vm.state)
		p.mu.Unlock()
		return types.VirtualMachine{}, err
	}
	vm.state = types.VMStateShuttingDown
	p.mu.Unlock()

	go func() {
		time.Sleep(shutdownGrace)
		p.mu.Lock()
		defer p.mu.Unlock()
		if vm.state == types.VMStateShuttingDown {
			p.powerOffLocked(vm)
		}
	}()
	return p.toModel(vm), nil
}

// RebootVirtualMachine restarts a running VM (uptime resets).
func (p *Provider) RebootVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	vm, err := p.getLocked(id)
	if err != nil {
		return types.VirtualMachine{}, err
	}
	if vm.state != types.VMStateRunning {
		return types.VirtualMachine{}, fmt.Errorf("%w: cannot reboot %s VM", hypervisor.ErrInvalidVMState, vm.state)
	}
	vm.bootTime = time.Now()
	return p.toModel(vm), nil
}

// ForceStopVirtualMachine immediately powers a VM off (equivalent to pulling
// the power cable).
func (p *Provider) ForceStopVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	vm, err := p.getLocked(id)
	if err != nil {
		return types.VirtualMachine{}, err
	}
	if vm.state == types.VMStateStopped {
		return types.VirtualMachine{}, fmt.Errorf("%w: VM is already stopped", hypervisor.ErrInvalidVMState)
	}
	p.powerOffLocked(vm)
	return p.toModel(vm), nil
}

// --- internal helpers (p.mu must be held) ---

func (p *Provider) getLocked(id string) (*vmState, error) {
	vm, ok := p.vms[id]
	if !ok {
		return nil, hypervisor.ErrVMNotFound
	}
	return vm, nil
}

func (p *Provider) powerOffLocked(vm *vmState) {
	vm.state = types.VMStateStopped
	vm.bootTime = time.Time{}
	vm.ipAddress = ""
}

func (p *Provider) storageUsedLocked() *int64 {
	var used int64
	for _, vm := range p.vms {
		used += int64(vm.spec.diskGB) * 1024 * 1024 * 1024
	}
	return &used
}

func (p *Provider) toModel(vm *vmState) types.VirtualMachine {
	m := types.VirtualMachine{
		Id:      vm.spec.id,
		Name:    vm.spec.id,
		State:   vm.state,
		Vcpus:   vm.spec.vcpus,
		MemoryBytes: vm.spec.memory,
		Os:      &vm.spec.os,
		Disks:   vm.disks,
	}
	if vm.disks == nil {
		m.Disks = []types.Disk{{
			Name:      "vda",
			Format:    types.DiskFormatQcow2,
			SizeBytes: int64(vm.spec.diskGB) * 1024 * 1024 * 1024,
			Bus:       ptr(types.DiskBusVirtio),
		}}
	}
	m.NetworkInterfaces = []types.NetworkInterface{{
		Name:       "ens3",
		Model:      types.NetworkInterfaceModelVirtio,
		MacAddress: &vm.spec.mac,
		Network:    ptr("default"),
	}}
	if vm.state == types.VMStateRunning || vm.state == types.VMStateShuttingDown {
		uptime := int(time.Since(vm.bootTime).Seconds())
		m.UptimeSeconds = &uptime
		m.BootTime = &vm.bootTime
	}
	if vm.state == types.VMStateRunning && vm.ipAddress != "" {
		m.IpAddress = &vm.ipAddress
		m.NetworkInterfaces[0].IpAddress = &vm.ipAddress
	}
	return m
}

// jitter returns a small value in [-f, f) based on a slow sine of the current
// time plus noise, so host metrics feel alive without background goroutines.
func (p *Provider) jitter(f float64) float64 {
	t := float64(time.Now().UnixNano()%1_000_000_000) / 1e9
	wave := math.Sin(t*math.Pi*2) * f * 0.5
	return wave + p.rng.Float64()*f
}

func ptr[T any](v T) *T { return &v }

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
