package mock

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/backup"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// backupImageHeaderBytes is the fixed content size of a simulated disk image
// in the backup library. The mock writes a small placeholder file (the
// metadata reports the full disk size, like a real sparse-aware backup).
const backupImageHeaderBytes = 64 * 1024

// backupSimulatedCopyTime models the elapsed copy work so callers observe a
// realistic (short) latency in demonstrations.
const backupSimulatedCopyTime = 200 * time.Millisecond

// BackupVirtualMachine creates a full backup of the simulated VM: one
// placeholder image per disk plus a synthesized domain XML, all recorded in
// the shared backup library (real files on disk).
func (p *Provider) BackupVirtualMachine(_ context.Context, id string) (types.Backup, error) {
	p.mu.Lock()
	vm, ok := p.vms[id]
	if !ok {
		p.mu.Unlock()
		return types.Backup{}, hypervisor.ErrVMNotFound
	}
	if vm.state == types.VMStateStarting || vm.state == types.VMStateShuttingDown {
		p.mu.Unlock()
		return types.Backup{}, hypervisor.ErrInvalidVMState
	}
	disks := vmDisks(vm)
	p.mu.Unlock()

	if p.backups == nil {
		return types.Backup{}, fmt.Errorf("mock provider has no backup library configured")
	}
	pt, err := p.backups.Create(id)
	if err != nil {
		return types.Backup{}, err
	}
	defer func() {
		if err != nil {
			pt.Abort()
		}
	}()

	time.Sleep(backupSimulatedCopyTime)
	for _, d := range disks {
		format := backup.FormatQcow2
		if d.Format == types.DiskFormatRaw {
			format = backup.FormatRaw
		}
		path := pt.DiskPath(d.Name, format)
		if err := writeSimulatedImage(path); err != nil {
			return types.Backup{}, err
		}
		// Record the on-disk size of the placeholder image (the real provider
		// records the actual size of the copied image).
		pt.SetDiskSize(d.Name, backupImageHeaderBytes)
	}
	if err := os.WriteFile(pt.DomainXMLPath(), mockDomainXML(vm), 0o644); err != nil {
		return types.Backup{}, err
	}
	pt.SetDomainXML(true)
	if err := pt.Complete(); err != nil {
		return types.Backup{}, err
	}
	return metaToBackup(pt.Meta()), nil
}

// vmDisks returns the disk list of a simulated VM (same derivation as
// toModel without the usedBytes jitter).
func vmDisks(vm *vmState) []types.Disk {
	if len(vm.disks) > 0 {
		return vm.disks
	}
	return []types.Disk{{
		Name:      "vda",
		Format:    types.DiskFormatQcow2,
		SizeBytes: int64(vm.spec.diskGB) * 1024 * 1024 * 1024,
	}}
}

// ListVMBackups returns the VM's completed backup points, newest first.
func (p *Provider) ListVMBackups(_ context.Context, id string) ([]types.Backup, error) {
	p.mu.Lock()
	_, ok := p.vms[id]
	p.mu.Unlock()
	if !ok {
		return nil, hypervisor.ErrVMNotFound
	}
	if p.backups == nil {
		return []types.Backup{}, nil
	}
	metas, err := p.backups.List(id)
	if err != nil {
		return nil, err
	}
	out := make([]types.Backup, 0, len(metas))
	for _, m := range metas {
		out = append(out, metaToBackup(m))
	}
	return out, nil
}

// DeleteBackup removes a backup point from the library.
func (p *Provider) DeleteBackup(_ context.Context, id string) error {
	if p.backups == nil {
		return hypervisor.ErrBackupNotFound
	}
	if err := p.backups.Delete(id); err != nil {
		if err == backup.ErrNotFound {
			return hypervisor.ErrBackupNotFound
		}
		return err
	}
	return nil
}

// RestoreBackup "restores" the simulated disks: the VM must exist and be
// stopped; the copy itself is instantaneous in the simulation.
func (p *Provider) RestoreBackup(_ context.Context, id string) (types.VirtualMachine, error) {
	if p.backups == nil {
		return types.VirtualMachine{}, hypervisor.ErrBackupNotFound
	}
	meta, err := p.backups.Get(id)
	if err != nil {
		if err == backup.ErrNotFound {
			return types.VirtualMachine{}, hypervisor.ErrBackupNotFound
		}
		return types.VirtualMachine{}, err
	}

	p.mu.Lock()
	vm, ok := p.vms[meta.VMID]
	if !ok {
		p.mu.Unlock()
		return types.VirtualMachine{}, fmt.Errorf("%w: the VM %q of this backup no longer exists",
			hypervisor.ErrBackupInvalidState, meta.VMID)
	}
	if vm.state != types.VMStateStopped {
		p.mu.Unlock()
		return types.VirtualMachine{}, fmt.Errorf("%w: restore requires the VM to be stopped",
			hypervisor.ErrBackupInvalidState)
	}
	p.mu.Unlock()

	return p.GetVirtualMachine(context.Background(), meta.VMID)
}

// ExportVMConfig returns the synthesized domain XML of the simulated VM.
func (p *Provider) ExportVMConfig(_ context.Context, id string) ([]byte, error) {
	p.mu.Lock()
	vm, ok := p.vms[id]
	p.mu.Unlock()
	if !ok {
		return nil, hypervisor.ErrVMNotFound
	}
	return mockDomainXML(vm), nil
}

// writeSimulatedImage writes a small placeholder disk image.
func writeSimulatedImage(path string) error {
	img := make([]byte, backupImageHeaderBytes)
	return os.WriteFile(path, img, 0o644)
}

// mockDomainXML synthesizes a libvirt-like domain XML for the simulated VM.
func mockDomainXML(vm *vmState) []byte {
	disks := vmDisks(vm)
	var diskXML string
	for _, d := range disks {
		bus := "virtio"
		if d.Bus != nil {
			bus = string(*d.Bus)
		}
		diskXML += fmt.Sprintf("    <disk type='file' device='disk'>\n      <target dev='%s' bus='%s'/>\n    </disk>\n",
			d.Name, bus)
	}
	return []byte(fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  <memory unit='KiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64' machine='pc-q35-8.2'>hvm</type>
  </os>
  <devices>
%s  </devices>
</domain>
`, vm.spec.id, vm.spec.memory/1024, vm.spec.vcpus, diskXML))
}

// metaToBackup converts library metadata to the API model.
func metaToBackup(m backup.Metadata) types.Backup {
	state := types.Complete
	hasXML := m.HasDomainXML
	disks := make([]types.BackupDisk, 0, len(m.Disks))
	var total int64
	for _, d := range m.Disks {
		format := types.BackupDiskFormat(d.Format)
		disks = append(disks, types.BackupDisk{
			Name:      d.Name,
			File:      d.File,
			Format:    &format,
			SizeBytes: d.SizeBytes,
		})
		total += d.SizeBytes
	}
	return types.Backup{
		Id:           m.ID,
		VmId:         m.VMID,
		VmName:       m.VMName,
		Type:         types.BackupType(m.Type),
		State:        &state,
		CreatedAt:    m.CreatedAt,
		SizeBytes:    total,
		Disks:        disks,
		HasDomainXml: &hasXML,
	}
}
