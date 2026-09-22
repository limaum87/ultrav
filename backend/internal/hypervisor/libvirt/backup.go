// Backup operations for the libvirt provider: full backups via
// virDomainBackupBegin (push mode) for running domains and direct image
// copies for stopped domains, plus restore and domain-XML export.
package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/backup"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

const (
	// backupPollInterval is how often the backup job progress is checked.
	backupPollInterval = 500 * time.Millisecond
	// backupDiskTempSuffix is used for the temp file during restore.
	backupDiskTempSuffix = ".restore-tmp"
)

// BackupVirtualMachine creates a full backup of every disk plus the domain
// configuration. Running domains are backed up with virDomainBackupBegin
// (block-level, crash-consistent); stopped domains with a plain image copy.
func (p *Provider) BackupVirtualMachine(ctx context.Context, id string) (types.Backup, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		defer dom.Free()

		state, _, _ := dom.GetState()
		if state == libvirt.DOMAIN_SHUTOFF {
			return p.backupOffline(ctx, dom, id)
		}
		return p.backupOnline(ctx, dom, id)
	})
	if err != nil {
		return types.Backup{}, err
	}
	metas, err := p.backups.List(id)
	if err != nil || len(metas) == 0 {
		return types.Backup{}, err
	}
	return metaToBackup(metas[0]), nil
}

// backupOnline performs a push-mode virDomainBackupBegin on a running
// domain. The hypervisor copies each disk (with the internal snapshot
// consistency) into the backup point directory.
func (p *Provider) backupOnline(ctx context.Context, dom *libvirt.Domain, id string) error {
	pt, err := p.backups.Create(id)
	if err != nil {
		return err
	}
	fail := func(err error) error { pt.Abort(); return err }

	// Capture the domain configuration as it is at backup time.
	domXML, err := dom.GetXMLDesc(0)
	if err != nil {
		return fail(fmt.Errorf("read domain XML: %w", err))
	}

	var parsed domainXML
	if err := xmlUnmarshal([]byte(domXML), &parsed); err != nil {
		return fail(fmt.Errorf("parse domain XML: %w", err))
	}
	var diskDefs []string
	for _, d := range parsed.Disks {
		if d.Device != "disk" || d.Source.File == "" {
			continue
		}
		target := pt.DiskPath(d.Target.Dev, backup.FormatQcow2)
		diskDefs = append(diskDefs, fmt.Sprintf(
			"  <disk name='%s' type='file'>\n    <target file='%s'/>\n    <driver type='qcow2'/>\n  </disk>",
			d.Target.Dev, xmlEscape(target)))
	}
	if len(diskDefs) == 0 {
		return fail(fmt.Errorf("domain %q has no file-backed disks to back up", id))
	}
	backupXML := "<domainbackup mode='push'>\n" + strings.Join(diskDefs, "\n") + "\n</domainbackup>"

	if err := dom.BackupBegin(backupXML, "", 0); err != nil {
		return fail(fmt.Errorf("begin backup job: %w", err))
	}
	slog.Info("backup job started", "vm", id, "point", pt.ID(), "disks", len(diskDefs))

	// Poll until the job completes; honor cancellation by aborting.
	for {
		if err := ctx.Err(); err != nil {
			_ = dom.AbortJob()
			return fail(fmt.Errorf("backup canceled: %w", err))
		}
		info, err := dom.GetJobInfo()
		if err != nil {
			return fail(fmt.Errorf("query backup job: %w", err))
		}
		if info.Type == libvirt.DOMAIN_JOB_COMPLETED {
			break
		}
		if info.Type == libvirt.DOMAIN_JOB_NONE {
			// Older daemons drop the job record as soon as it ends.
			break
		}
		time.Sleep(backupPollInterval)
	}

	if err := os.WriteFile(pt.DomainXMLPath(), []byte(domXML), 0o644); err != nil {
		return fail(err)
	}
	pt.SetDomainXML(true)
	recordDiskSizes(pt, pt.Dir())
	return pt.Complete()
}

// backupOffline copies the disk images directly (the domain is stopped, so a
// file copy is consistent by definition).
func (p *Provider) backupOffline(ctx context.Context, dom *libvirt.Domain, id string) error {
	pt, err := p.backups.Create(id)
	if err != nil {
		return err
	}
	fail := func(err error) error { pt.Abort(); return err }

	domXML, err := dom.GetXMLDesc(0)
	if err != nil {
		return fail(fmt.Errorf("read domain XML: %w", err))
	}
	var parsed domainXML
	if err := xmlUnmarshal([]byte(domXML), &parsed); err != nil {
		return fail(fmt.Errorf("parse domain XML: %w", err))
	}
	for _, d := range parsed.Disks {
		if d.Device != "disk" || d.Source.File == "" {
			continue
		}
		target := pt.DiskPath(d.Target.Dev, backup.FormatQcow2)
		if err := copyFile(ctx, d.Source.File, target); err != nil {
			return fail(fmt.Errorf("copy disk %s: %w", d.Target.Dev, err))
		}
	}
	if err := os.WriteFile(pt.DomainXMLPath(), []byte(domXML), 0o644); err != nil {
		return fail(err)
	}
	pt.SetDomainXML(true)
	recordDiskSizes(pt, pt.Dir())
	return pt.Complete()
}

// ListVMBackups returns the VM's completed backup points, newest first.
func (p *Provider) ListVMBackups(_ context.Context, id string) ([]types.Backup, error) {
	var out []types.Backup
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		dom.Free()
		metas, err := p.backups.List(id)
		if err != nil {
			return err
		}
		out = make([]types.Backup, 0, len(metas))
		for _, m := range metas {
			out = append(out, metaToBackup(m))
		}
		return nil
	})
	return out, err
}

// DeleteBackup removes a backup point from the library.
func (p *Provider) DeleteBackup(_ context.Context, id string) error {
	if err := p.backups.Delete(id); err != nil {
		if err == backup.ErrNotFound {
			return hypervisor.ErrBackupNotFound
		}
		return err
	}
	return nil
}

// RestoreBackup overwrites the disks of the backup's VM with the point's
// images and redefines the domain from the saved XML (when present).
// The VM must exist and be stopped.
func (p *Provider) RestoreBackup(ctx context.Context, id string) (types.VirtualMachine, error) {
	var meta backup.Metadata
	err := p.withConn(func(c *libvirt.Connect) error {
		m, err := p.backups.Get(id)
		if err != nil {
			if err == backup.ErrNotFound {
				return hypervisor.ErrBackupNotFound
			}
			return err
		}
		meta = m
		dir, err := p.backups.PointDir(id)
		if err != nil {
			return err
		}

		dom, err := c.LookupDomainByName(meta.VMID)
		if err != nil {
			return fmt.Errorf("%w: the VM %q of this backup no longer exists",
				hypervisor.ErrBackupInvalidState, meta.VMID)
		}
		state, _, _ := dom.GetState()
		if state != libvirt.DOMAIN_SHUTOFF {
			dom.Free()
			return fmt.Errorf("%w: restore requires the VM to be stopped",
				hypervisor.ErrBackupInvalidState)
		}

		// Map disk targets to their current volume paths.
		curXML, err := dom.GetXMLDesc(0)
		if err != nil {
			dom.Free()
			return fmt.Errorf("read domain XML: %w", err)
		}
		var parsed domainXML
		if err := xmlUnmarshal([]byte(curXML), &parsed); err != nil {
			dom.Free()
			return fmt.Errorf("parse domain XML: %w", err)
		}
		sources := make(map[string]string, len(parsed.Disks))
		for _, d := range parsed.Disks {
			if d.Device == "disk" && d.Source.File != "" {
				sources[d.Target.Dev] = d.Source.File
			}
		}

		for _, bd := range meta.Disks {
			dst, ok := sources[bd.Name]
			if !ok {
				dom.Free()
				return fmt.Errorf("backup disk %s has no matching disk in domain %q", bd.Name, meta.VMID)
			}
			if err := restoreDisk(ctx, filepath.Join(dir, bd.File), dst, string(bd.Format)); err != nil {
				dom.Free()
				return fmt.Errorf("restore disk %s: %w", bd.Name, err)
			}
		}

		// Redefine the domain from the saved configuration (best effort: the
		// disks are already restored; a failed redefine is logged, not fatal).
		if meta.HasDomainXML {
			saved, err := os.ReadFile(filepath.Join(dir, "domain.xml"))
			if err == nil {
				if err := dom.Undefine(); err != nil {
					slog.Warn("restoreBackup: could not undefine current domain", "vm", meta.VMID, "error", err.Error())
				} else if _, err := c.DomainDefineXML(string(saved)); err != nil {
					slog.Error("restoreBackup: could not redefine domain from saved XML", "vm", meta.VMID, "error", err.Error())
				}
			} else {
				slog.Warn("restoreBackup: saved domain XML unreadable", "backup", id, "error", err.Error())
			}
		}
		dom.Free()

		// Refresh pool info so freed/replaced volumes report fresh numbers.
		refreshDomainPools(c, parsed)
		return nil
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(ctx, meta.VMID)
}

// ExportVMConfig returns the live domain XML of the VM.
func (p *Provider) ExportVMConfig(_ context.Context, id string) ([]byte, error) {
	var out []byte
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		defer dom.Free()
		xml, err := dom.GetXMLDesc(0)
		if err != nil {
			return err
		}
		out = []byte(xml)
		return nil
	})
	return out, err
}

// --- helpers ---

// recordDiskSizes fills in each disk's on-disk size after the copy.
func recordDiskSizes(pt *backup.Point, dir string) {
	meta := pt.Meta()
	for _, d := range meta.Disks {
		if fi, err := os.Stat(filepath.Join(dir, d.File)); err == nil {
			pt.SetDiskSize(d.Name, fi.Size())
		}
	}
}

// restoreDisk copies a backup image over the domain's volume: convert into a
// temp file in the same directory, then atomically rename over the original.
func restoreDisk(ctx context.Context, src, dst, format string) error {
	tmp := dst + backupDiskTempSuffix
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-O", format, src, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img convert: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return os.Rename(tmp, dst)
}

// copyFile copies src to dst honoring context cancellation, reserving the
// destination size up front (sparse-friendly).
func copyFile(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := out.Truncate(fi.Size()); err != nil {
		return err
	}
	buf := make([]byte, 4*1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

// refreshDomainPools refreshes the storage pools backing a domain's disks.
func refreshDomainPools(c *libvirt.Connect, parsed domainXML) {
	seen := map[string]bool{}
	for _, d := range parsed.Disks {
		dir := filepath.Dir(d.Source.File)
		if dir == "." || seen[dir] {
			continue
		}
		seen[dir] = true
		if pool, err := c.LookupStoragePoolByTargetPath(dir); err == nil {
			_ = pool.Refresh(0)
			pool.Free()
		}
	}
}

// metaToBackup converts library metadata to the API model.
func metaToBackup(m backup.Metadata) types.Backup {
	state := types.BackupState(m.State)
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

// xmlUnmarshal decodes the domain XML subset into the shared structs.
func xmlUnmarshal(b []byte, v any) error {
	return xml.Unmarshal(b, v)
}
