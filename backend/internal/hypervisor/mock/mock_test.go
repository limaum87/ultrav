package mock

import (
	"context"
	"testing"
	"time"

	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

func TestListVirtualMachines(t *testing.T) {
	p := New()
	vms, err := p.ListVirtualMachines(context.Background())
	if err != nil {
		t.Fatalf("ListVirtualMachines: %v", err)
	}
	if len(vms) != 4 {
		t.Fatalf("expected 4 VMs, got %d", len(vms))
	}
	// Stable sort order by id.
	want := []string{"database01", "erp01", "monitoring01", "web01"}
	for i, id := range want {
		if vms[i].Id != id {
			t.Errorf("vm[%d].Id = %q, want %q", i, vms[i].Id, id)
		}
	}
}

func TestInitialState(t *testing.T) {
	p := New()
	ctx := context.Background()

	erp, err := p.GetVirtualMachine(ctx, "erp01")
	if err != nil {
		t.Fatalf("erp01: %v", err)
	}
	if erp.State != "running" || erp.IpAddress == nil || erp.UptimeSeconds == nil {
		t.Errorf("erp01 should be running with IP and uptime, got %+v", erp)
	}

	mon, err := p.GetVirtualMachine(ctx, "monitoring01")
	if err != nil {
		t.Fatalf("monitoring01: %v", err)
	}
	if mon.State != "stopped" || mon.IpAddress != nil || mon.UptimeSeconds != nil {
		t.Errorf("monitoring01 should be stopped without IP/uptime, got %+v", mon)
	}
	if len(mon.Disks) != 1 || mon.Disks[0].Format != "qcow2" {
		t.Errorf("monitoring01 disk mismatch: %+v", mon.Disks)
	}
}

func TestPowerTransitions(t *testing.T) {
	ctx := context.Background()
	p := New()

	// start a stopped VM
	vm, err := p.StartVirtualMachine(ctx, "monitoring01")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if vm.State != "running" || vm.IpAddress == nil || vm.BootTime == nil {
		t.Fatalf("monitoring01 should be running after start, got %+v", vm)
	}
	bootTime := *vm.BootTime

	// start again -> invalid state
	if _, err := p.StartVirtualMachine(ctx, "monitoring01"); err == nil {
		t.Fatal("starting a running VM should fail")
	}

	// reboot resets uptime
	time.Sleep(10 * time.Millisecond)
	vm, err = p.RebootVirtualMachine(ctx, "monitoring01")
	if err != nil {
		t.Fatalf("reboot: %v", err)
	}
	if !vm.BootTime.After(bootTime) {
		t.Error("reboot should reset boot time")
	}

	// reboot a stopped VM -> invalid state
	if _, err := p.ForceStopVirtualMachine(ctx, "monitoring01"); err != nil {
		t.Fatalf("force stop: %v", err)
	}
	if _, err := p.RebootVirtualMachine(ctx, "monitoring01"); err == nil {
		t.Fatal("rebooting a stopped VM should fail")
	}
	if _, err := p.StartVirtualMachine(ctx, "monitoring01"); err != nil {
		t.Fatalf("restart after stop: %v", err)
	}
}

func TestShutdownIsGraceful(t *testing.T) {
	ctx := context.Background()
	p := New()

	vm, err := p.ShutdownVirtualMachine(ctx, "web01")
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if vm.State != "shutting-down" {
		t.Fatalf("expected shutting-down right after shutdown, got %q", vm.State)
	}

	// still shutting down shortly after
	time.Sleep(100 * time.Millisecond)
	vm, _ = p.GetVirtualMachine(ctx, "web01")
	if vm.State != "shutting-down" {
		t.Fatalf("expected to still be shutting-down, got %q", vm.State)
	}

	// eventually stopped
	time.Sleep(4 * time.Second)
	vm, _ = p.GetVirtualMachine(ctx, "web01")
	if vm.State != "stopped" {
		t.Fatalf("expected stopped after grace period, got %q", vm.State)
	}
	if vm.IpAddress != nil {
		t.Error("stopped VM must not have an IP")
	}
}

func TestHostAndCapabilities(t *testing.T) {
	ctx := context.Background()
	p := New()

	host, err := p.GetHost(ctx)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if host.Hostname != "kvm01" {
		t.Errorf("hostname = %q, want kvm01", host.Hostname)
	}
	if host.MemoryTotalBytes != 64*1024*1024*1024 {
		t.Errorf("unexpected memory total: %d", host.MemoryTotalBytes)
	}
	if host.StorageTotalBytes == nil || *host.StorageTotalBytes != 2*1024*1024*1024*1024 {
		t.Errorf("unexpected storage total: %v", host.StorageTotalBytes)
	}
	if host.MemoryUsedBytes <= 0 || host.MemoryUsedBytes > host.MemoryTotalBytes {
		t.Errorf("memory used out of range: %d", host.MemoryUsedBytes)
	}

	caps, err := p.GetCapabilities(ctx)
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if !caps.Virtualization.Kvm || !caps.Storage.Qcow2 {
		t.Error("unexpected capabilities")
	}
	if caps.Features.Backup || caps.Features.Migration || caps.Features.CloudInit {
		t.Error("future features must be false in Phase 1")
	}
}

func TestReadyAndErrors(t *testing.T) {
	p := New()
	ctx := context.Background()

	if err := p.Ready(ctx); err != nil {
		t.Errorf("Ready: %v", err)
	}
	if _, err := p.GetVirtualMachine(ctx, "does-not-exist"); err != hypervisor.ErrVMNotFound {
		t.Errorf("expected ErrVMNotFound, got %v", err)
	}
	// ID validation is the API layer's job, but the mock must also be safe.
	if _, err := p.GetVirtualMachine(ctx, "../../etc/passwd"); err != hypervisor.ErrVMNotFound {
		t.Errorf("expected ErrVMNotFound for traversal id, got %v", err)
	}
}
