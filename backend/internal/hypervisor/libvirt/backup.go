// Backup operations for the libvirt provider: full backups via
// virDomainBackupBegin (push mode) for running domains and direct image
// copies for stopped domains, plus restore and domain-XML export.
package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
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
	// backupPoolPrefix names the transient per-point pools UltraV defines
	// over backup point directories during restore.
	backupPoolPrefix = "ultrav-bk-"
	// backupDiskDownloadSuffix marks the backend-readable copies of the
	// qemu-owned backup images, streamed via libvirtd for restore.
	backupDiskDownloadSuffix = ".download"
)

// BackupVirtualMachine creates a backup of every disk plus the domain
// configuration. Running domains are backed up with virDomainBackupBegin
// (block-level, crash-consistent); stopped domains with a plain image copy.
// Incremental points continue from the newest point's checkpoint; when the
// chain is invalid (or req forces full) a new full is taken.
func (p *Provider) BackupVirtualMachine(ctx context.Context, id string, req types.BackupCreate) (types.Backup, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		defer dom.Free()

		parentID, parentCheckpoint, err := p.backupChainParent(dom, id, req)
		if err != nil {
			return err
		}
		state, _, _ := dom.GetState()
		if state == libvirt.DOMAIN_SHUTOFF {
			// Offline images cannot carry dirty bitmaps: always full.
			return p.backupOffline(ctx, c, dom, id)
		}
		return p.backupOnline(ctx, dom, id, parentID, parentCheckpoint)
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
// consistency) into the backup point directory. parentID/parentCheckpoint
// chain this point to the previous one (both "" for a full).
func (p *Provider) backupOnline(ctx context.Context, dom *libvirt.Domain, id, parentID, parentCheckpoint string) error {
	pt, err := p.backups.Create(id)
	if err != nil {
		return err
	}
	fail := func(err error) error { pt.Abort(); return err }
	if parentID != "" {
		pt.SetParent(parentID)
	}
	// Dirty-bitmap anchor for the NEXT incremental, tied to this point.
	checkpoint := "chk-" + strings.TrimPrefix(pt.ID(), "bk-")
	pt.SetCheckpoint(checkpoint)

	// Capture the domain configuration as it is at backup time.
	domXML, err := dom.GetXMLDesc(0)
	if err != nil {
		return fail(fmt.Errorf("read domain XML: %w", err))
	}

	var parsed domainXML
	if err := xmlUnmarshal([]byte(domXML), &parsed); err != nil {
		return fail(fmt.Errorf("parse domain XML: %w", err))
	}
	var diskDefs, ckptDisks []string
	for _, d := range parsed.Disks {
		if d.Device != "disk" || d.Source.File == "" {
			continue
		}
		target := pt.DiskPath(d.Target.Dev, backup.FormatQcow2)
		diskDefs = append(diskDefs, fmt.Sprintf(
			"  <disk name='%s' type='file'>\n    <target file='%s'/>\n    <driver type='qcow2'/>\n  </disk>",
			d.Target.Dev, xmlEscape(target)))
		ckptDisks = append(ckptDisks, fmt.Sprintf("    <disk name='%s' checkpoint='bitmap'/>", d.Target.Dev))
	}
	if len(diskDefs) == 0 {
		return fail(fmt.Errorf("domain %q has no file-backed disks to back up", id))
	}
	incremental := ""
	if parentCheckpoint != "" {
		// The <incremental> element names the checkpoint to continue from;
		// QEMU copies only the blocks dirtied since that point.
		incremental = fmt.Sprintf("  <incremental>%s</incremental>\n", xmlEscape(parentCheckpoint))
	}
	backupXML := "<domainbackup mode='push'>\n" + incremental +
		"  <disks>\n" + strings.Join(diskDefs, "\n") + "\n  </disks>\n</domainbackup>"
	checkpointXML := fmt.Sprintf(
		"<domaincheckpoint>\n  <name>%s</name>\n  <description>ultrav backup point %s</description>\n  <disks>\n%s\n  </disks>\n</domaincheckpoint>",
		checkpoint, pt.ID(), strings.Join(ckptDisks, "\n"))

	if err := dom.BackupBegin(backupXML, checkpointXML, 0); err != nil {
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
			// The job record vanished: either it never started or the daemon
			// dropped it (e.g. it failed instantly). Confirm by checking the
			// produced images below.
			break
		}
		time.Sleep(backupPollInterval)
	}

	// A vanished job record or a qemu-level failure (ENOSPC mid-copy, for
	// example) is only visible through the produced files: every disk image
	// must exist and be non-empty, otherwise the point is invalid.
	meta := pt.Meta()
	for _, d := range meta.Disks {
		fi, err := os.Stat(filepath.Join(pt.Dir(), d.File))
		if err != nil || fi.Size() == 0 {
			return fail(fmt.Errorf("backup job produced no image for disk %s "+
				"(check host free space and libvirt logs: journalctl -u libvirtd)", d.Name))
		}
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
func (p *Provider) backupOffline(ctx context.Context, c *libvirt.Connect, dom *libvirt.Domain, id string) error {
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
		// Stream through libvirtd rather than copying the file directly: the
		// images are qemu-owned (0600) and may not even be mounted at the
		// same path inside a containerized backend (Regra dos caminhos).
		if err := p.downloadVolumeTo(ctx, c, d.Source.File, target); err != nil {
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

// DeleteBackup removes a backup point from the library and drops its
// dirty-bitmap checkpoint from the domain, if any (otherwise deleting the
// newest point would silently break the incremental chain).
func (p *Provider) DeleteBackup(_ context.Context, id string) error {
	meta, err := p.backups.Get(id)
	if err != nil {
		if err == backup.ErrNotFound {
			return hypervisor.ErrBackupNotFound
		}
		return err
	}
	if err := p.backups.Delete(id); err != nil {
		return err
	}
	if meta.Checkpoint != "" {
		_ = p.withConn(func(c *libvirt.Connect) error {
			dom, err := c.LookupDomainByName(meta.VMID)
			if err != nil {
				return nil // domain is gone; nothing to clean
			}
			defer dom.Free()
			if cp, err := dom.CheckpointLookupByName(meta.Checkpoint, 0); err == nil {
				_ = cp.Delete(0)
				cp.Free()
			}
			return nil
		})
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

		// Resolve the restore chain: the point itself (full) or its full
		// ancestor chain (incremental), oldest first.
		chain, err := p.restoreChain(meta)
		if err != nil {
			dom.Free()
			return err
		}
		for _, bd := range meta.Disks {
			dst, ok := sources[bd.Name]
			if !ok {
				dom.Free()
				return fmt.Errorf("backup disk %s has no matching disk in domain %q", bd.Name, meta.VMID)
			}
			if err := p.restoreDiskChain(ctx, c, chain, bd.Name, dst); err != nil {
				dom.Free()
				return fmt.Errorf("restore disk %s: %w", bd.Name, err)
			}
		}

		// The disks now reflect an older point in time: the dirty-bitmap
		// checkpoints are stale. Drop them so the next backup takes a new
		// full and starts a clean chain.
		deleteUltravCheckpoints(dom)

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

// backupChainParent decides the chain anchor of the requested backup:
//   - forced full → ""/"" (chain reset);
//   - forced incremental → (newest point id, its checkpoint), verified in
//     the domain (ErrBackupInvalidState when missing);
//   - auto → same as incremental, falling back to full silently.
func (p *Provider) backupChainParent(dom *libvirt.Domain, id string, req types.BackupCreate) (string, string, error) {
	forced := ""
	if req.Type != nil {
		forced = string(*req.Type)
	}
	metas, err := p.backups.List(id)
	if err != nil {
		return "", "", err
	}
	if backup.Type(forced) == backup.TypeFull {
		return "", "", nil // explicit full always resets the chain
	}
	forcedIncr := backup.Type(forced) == backup.TypeIncremental
	if len(metas) == 0 || metas[0].Checkpoint == "" {
		if forcedIncr {
			return "", "", fmt.Errorf("%w: no valid backup chain for %q (the previous point has no dirty-bitmap checkpoint — take a full backup first)", hypervisor.ErrBackupInvalidState, id)
		}
		return "", "", nil
	}
	// The chain is only valid if libvirt still has the parent checkpoint
	// (deleted on restore, or by an operator via virsh).
	cp, err := dom.CheckpointLookupByName(metas[0].Checkpoint, 0)
	if err != nil {
		if forcedIncr {
			return "", "", fmt.Errorf("%w: the checkpoint of backup %s no longer exists in the domain — take a full backup first", hypervisor.ErrBackupInvalidState, metas[0].ID)
		}
		return "", "", nil
	}
	cp.Free()
	return metas[0].ID, metas[0].Checkpoint, nil
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

// restoreChain resolves the ancestor chain of a backup point, oldest first
// ([full, incr, ..., point]). Incremental points require an unbroken chain.
func (p *Provider) restoreChain(meta backup.Metadata) ([]backup.Metadata, error) {
	chain := []backup.Metadata{meta}
	cur := meta
	for cur.ParentID != "" {
		parent, err := p.backups.Get(cur.ParentID)
		if err != nil {
			return nil, fmt.Errorf("%w: the chain of backup %s is broken (parent %s is gone)",
				hypervisor.ErrBackupInvalidState, meta.ID, cur.ParentID)
		}
		chain = append([]backup.Metadata{parent}, chain...)
		cur = parent
	}
	return chain, nil
}

// restoreDiskChain materializes a backup chain onto the domain's volume:
// the full image is converted into a temp file, then each incremental is
// replayed on top of it (qemu-img convert -n writes only the clusters the
// incremental image allocated), and finally the temp atomically renames over
// the original volume.
func (p *Provider) restoreDiskChain(ctx context.Context, c *libvirt.Connect, chain []backup.Metadata, disk, dst string) error {
	tmp := dst + backupDiskTempSuffix
	defer os.Remove(tmp)

	// The backup images are owned by qemu (root, 0600) and cannot be opened
	// by the backend directly: stream each chain image from libvirtd into a
	// backend-owned temp copy first.
	type srcImage struct {
		path    string
		format  backup.Format
		cleanup string
	}
	var sources []srcImage
	defer func() {
		for _, s := range sources {
			os.Remove(s.cleanup)
		}
	}()
	for _, m := range chain {
		file, format, err := diskImage(m, disk)
		if err != nil {
			return err
		}
		dir, err := p.backups.PointDir(m.ID)
		if err != nil {
			return err
		}
		image := filepath.Join(dir, file)
		release, err := p.ensurePointPool(c, m.ID, dir)
		if err != nil {
			return err
		}
		local := image + backupDiskDownloadSuffix
		err = p.downloadVolumeTo(ctx, c, image, local)
		release()
		if err != nil {
			return err
		}
		// Incremental images carry a <backingStore> pointing at the VM's
		// original disk; detach it on the local copy (unsafe rebase only
		// rewrites the pointer) so qemu-img never needs the original.
		if m.ParentID != "" {
			if out, err := exec.CommandContext(ctx, "qemu-img", "rebase", "-u", "-b", "", local).CombinedOutput(); err != nil {
				return fmt.Errorf("qemu-img rebase: %s: %w", strings.TrimSpace(string(out)), err)
			}
		}
		sources = append(sources, srcImage{path: local, format: format, cleanup: local})
	}

	// Step 1: the full image (chain head) into the restore temp file.
	cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-O", string(sources[0].format),
		sources[0].path, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img convert (full): %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Step 2: replay each incremental in chronological order (-n: write only
	// the clusters allocated in the incremental image).
	for _, s := range sources[1:] {
		cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-n", "-O", string(s.format),
			s.path, tmp)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("qemu-img convert: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	return os.Rename(tmp, dst)
}

// ensurePointPool exposes one backup point's directory as a transient
// storage pool, so libvirtd can address its images as volumes (needed to
// stream qemu-owned files the backend user cannot open directly). The
// returned release func destroys and undefines the pool.
func (p *Provider) ensurePointPool(c *libvirt.Connect, id, dir string) (release func(), err error) {
	name := backupPoolPrefix + strings.TrimPrefix(id, "bk-")
	if pool, err := c.LookupStoragePoolByName(name); err == nil {
		defer pool.Free()
		return func() {}, nil // leftover from a crashed run; good enough
	}
	poolXML := fmt.Sprintf(
		"<pool type='dir'>\n  <name>%s</name>\n  <target>\n    <path>%s</path>\n  </target>\n</pool>",
		name, xmlEscape(dir))
	pool, err := c.StoragePoolDefineXML(poolXML, 0)
	if err != nil {
		return nil, fmt.Errorf("define backup point pool %s: %w", name, err)
	}
	release = func() {
		_ = pool.Destroy()
		_ = pool.Undefine()
		pool.Free()
	}
	if err := pool.Build(0); err != nil {
		release()
		return nil, fmt.Errorf("build backup point pool %s: %w", name, err)
	}
	if err := pool.Create(0); err != nil {
		release()
		return nil, fmt.Errorf("start backup point pool %s: %w", name, err)
	}
	_ = pool.Refresh(0)
	return release, nil
}

// downloadVolumeTo streams a storage volume (any path on the host, even one
// the backend user cannot open directly, like qemu-owned backup images)
// into a local file via libvirtd.
func (p *Provider) downloadVolumeTo(_ context.Context, c *libvirt.Connect, path, dst string) error {
	vol, err := c.LookupStorageVolByPath(path)
	if err != nil {
		return fmt.Errorf("lookup volume %s: %w", path, err)
	}
	defer vol.Free()
	stream, err := c.NewStream(0)
	if err != nil {
		return err
	}
	defer stream.Free()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()

	errc := make(chan error, 1)
	go func() {
		sink := func(_ *libvirt.Stream, data []byte) (int, error) {
			return out.Write(data)
		}
		errc <- stream.RecvAll(libvirt.StreamSinkFunc(sink))
	}()
	if err := vol.Download(stream, 0, 0, 0); err != nil {
		return fmt.Errorf("download %s: %w", path, err)
	}
	if err := <-errc; err != nil {
		return fmt.Errorf("stream %s: %w", path, err)
	}
	return nil
}

// diskImage finds the image entry of a disk inside a backup point.
func diskImage(m backup.Metadata, disk string) (string, backup.Format, error) {
	for _, d := range m.Disks {
		if d.Name == disk {
			return d.File, d.Format, nil
		}
	}
	return "", "", fmt.Errorf("backup %s has no image for disk %s", m.ID, disk)
}

// deleteUltravCheckpoints removes the dirty-bitmap checkpoints UltraV
// created for this domain (best effort; ignores not-found errors).
func deleteUltravCheckpoints(dom *libvirt.Domain) {
	cps, err := dom.ListAllCheckpoints(0)
	if err != nil {
		return
	}
	for i := range cps {
		if name, err := cps[i].GetName(); err == nil && strings.HasPrefix(name, "chk-") {
			_ = cps[i].Delete(0)
		}
		cps[i].Free()
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
	var parentID *string
	if m.ParentID != "" {
		p := m.ParentID
		parentID = &p
	}
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
		ParentId:     parentID,
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
