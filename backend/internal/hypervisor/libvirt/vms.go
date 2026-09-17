package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
	"github.com/ultrav/ultrav/backend/internal/iso"
)

// domainXML is the subset of the libvirt domain XML we consume.
type domainXML struct {
	Name   string `xml:"name"`
	OS     string `xml:"os>type"`
	Memory struct {
		Value int64  `xml:",chardata"`
		Unit  string `xml:"unit,attr"`
	} `xml:"memory"`
	CurrentMemory struct {
		Value int64  `xml:",chardata"`
		Unit  string `xml:"unit,attr"`
	} `xml:"currentMemory"`
	VCPU  int          `xml:"vcpu"`
	Disks []domainDisk `xml:"devices>disk"`
	NICs  []domainNIC  `xml:"devices>interface"`
}

type domainDisk struct {
	Device string `xml:"device,attr"`
	Driver struct {
		Type string `xml:"type,attr"`
	} `xml:"driver"`
	Target struct {
		Dev string `xml:"dev,attr"`
		Bus string `xml:"bus,attr"`
	} `xml:"target"`
	Source struct {
		File string `xml:"file,attr"`
	} `xml:"source"`
}

type domainNIC struct {
	Type string `xml:"type,attr"`
	MAC  struct {
		Address string `xml:"address,attr"`
	} `xml:"mac"`
	Source struct {
		Network string `xml:"network,attr"`
		Bridge  string `xml:"bridge,attr"`
	} `xml:"source"`
	Target struct {
		Dev string `xml:"dev,attr"`
	} `xml:"target"`
}

func stateToModel(state libvirt.DomainState) types.VMState {
	switch state {
	case libvirt.DOMAIN_RUNNING, libvirt.DOMAIN_BLOCKED:
		return types.VMStateRunning
	case libvirt.DOMAIN_PAUSED:
		return types.VMStatePaused
	case libvirt.DOMAIN_SHUTDOWN, libvirt.DOMAIN_SHUTOFF, libvirt.DOMAIN_CRASHED:
		return types.VMStateStopped
	case libvirt.DOMAIN_PMSUSPENDED:
		return types.VMStatePaused
	default:
		return types.VMStateError
	}
}

// ListVirtualMachines returns every domain (active + inactive), sorted by id
// as returned by libvirt (ListAllDomains order is unspecified; we sort here).
func (p *Provider) ListVirtualMachines(_ context.Context) ([]types.VirtualMachine, error) {
	var out []types.VirtualMachine
	err := p.withConn(func(c *libvirt.Connect) error {
		doms, err := c.ListAllDomains(
			libvirt.CONNECT_LIST_DOMAINS_ACTIVE | libvirt.CONNECT_LIST_DOMAINS_INACTIVE)
		if err != nil {
			return err
		}
		out = make([]types.VirtualMachine, 0, len(doms))
		for i := range doms {
			vm, err := p.domainToModel(&doms[i])
			if err != nil {
				continue // skip unreadable domains rather than failing the list
			}
			out = append(out, vm)
		}
		sortVMs(out)
		// Drop sampling state for domains that are no longer running.
		running := make(map[string]bool, len(out))
		for i := range out {
			if out[i].State == types.VMStateRunning {
				running[out[i].Id] = true
			}
		}
		p.forgetSamples(running)
		return nil
	})
	return out, err
}

// CreateVirtualMachine allocates a disk volume in the requested storage
// pool and defines the domain (stopped, or started when req.Start is true).
// The requested osType selects a performance profile (see domainxml.go); the
// applied profile is reported back in the returned VM.
func (p *Provider) CreateVirtualMachine(_ context.Context, req types.VirtualMachineCreate) (types.VirtualMachine, error) {
	format := "qcow2"
	if req.Disk.Format != nil {
		format = string(*req.Disk.Format)
	}
	if req.NetworkId == nil || *req.NetworkId == "" {
		def := "default"
		req.NetworkId = &def
	}
	osType := types.VirtualMachineCreateOsTypeLinux
	if req.OsType != nil && *req.OsType != "" {
		osType = *req.OsType
	}
	var warnings []string
	var poolSpaceErr error

	err := p.withConn(func(c *libvirt.Connect) error {
		// Refuse to redefine an existing domain.
		if dom, err := c.LookupDomainByName(req.Name); err == nil {
			dom.Free()
			return hypervisor.ErrVMAlreadyExists
		}

		pool, err := c.LookupStoragePoolByName(req.Disk.PoolId)
		if err != nil {
			return hypervisor.ErrPoolNotFound
		}
		if active, err := pool.IsActive(); err == nil {
			// Recompute capacity/allocation so the free-space check below never
			// sees stale numbers (libvirt caches them until an explicit refresh).
			_ = pool.Refresh(0)
			if active && req.Disk.SizeBytes > 0 {
				if info, err := pool.GetInfo(); err == nil && req.Disk.SizeBytes > int64(info.Available) {
					poolSpaceErr = fmt.Errorf("%w: disk size (%d bytes) exceeds the free space in pool %q (%d bytes available)",
						hypervisor.ErrPoolInsufficientSpace, req.Disk.SizeBytes, req.Disk.PoolId, info.Available)
					return poolSpaceErr
				}
			}
		}

		volXML := fmt.Sprintf(`<volume type='file'>
  <name>%s.%s</name>
  <capacity unit='bytes'>%d</capacity>
  <target>
    <format type='%s'/>
  </target>
</volume>`, req.Name, format, req.Disk.SizeBytes, format)
		vol, err := pool.StorageVolCreateXML(volXML, 0)
		if err != nil {
			return fmt.Errorf("create volume: %w", err)
		}
		defer vol.Free()
		volPath, err := vol.GetPath()
		if err != nil {
			return fmt.Errorf("resolve volume path: %w", err)
		}

		// Validate the network reference so a bad networkId fails fast.
		net, err := c.LookupNetworkByName(*req.NetworkId)
		if err != nil {
			return hypervisor.ErrNetworkNotFound
		}
		defer net.Free()

		// Optional install media: resolve the ISO in the library.
		var isoPath string
		if req.IsoId != nil && *req.IsoId != "" {
			if !iso.ValidID.MatchString(*req.IsoId) {
				return fmt.Errorf("%w: invalid ISO filename", os.ErrInvalid)
			}
			path := filepath.Join(p.isoDir, *req.IsoId)
			if _, err := os.Stat(path); err != nil {
				return hypervisor.ErrIsoNotFound
			}
			isoPath = path
		}

		// Optional VirtIO drivers ISO (windows installs): same validation as
		// the install media. Only attached for windows; ignored otherwise.
		var virtioISOPath string
		if osType == types.VirtualMachineCreateOsTypeWindows && req.VirtioDriversIsoId != nil && *req.VirtioDriversIsoId != "" {
			if !iso.ValidID.MatchString(*req.VirtioDriversIsoId) {
				return fmt.Errorf("%w: invalid VirtIO drivers ISO filename", os.ErrInvalid)
			}
			path := filepath.Join(p.isoDir, *req.VirtioDriversIsoId)
			if _, err := os.Stat(path); err != nil {
				return hypervisor.ErrIsoNotFound
			}
			virtioISOPath = path
		}
		// os.Stat above only proves the files exist in *this* mount namespace.
		// qemu opens them on the host, so flag a namespace mismatch before the
		// VM is created rather than letting it surface as a failed start.
		for _, mediaPath := range []string{isoPath, virtioISOPath} {
			if w := isoVisibilityWarning(mediaPath); w != "" {
				slog.Warn("createVirtualMachine: ISO may be unreachable by the hypervisor",
					"vm", req.Name, "iso", mediaPath)
				warnings = append(warnings, w)
			}
		}

		if osType == types.VirtualMachineCreateOsTypeWindows && virtioISOPath == "" {
			warnings = append(warnings,
				"Windows without a VirtIO drivers ISO: the installer will not see the "+
					"virtio-scsi disk until the vioscsi driver is loaded from another source "+
					"(attach the virtio-win ISO in the library to avoid this).")
		}

		// Host versions drive the Hyper-V enlightenment table (see
		// domainxml.go). Best-effort: a failed probe means "very old host",
		// which errs on the conservative side.
		var caps hostFeatures
		if v, err := c.GetVersion(); err == nil {
			caps.QEMUVersion = v
		} else {
			slog.Warn("createVirtualMachine: could not probe QEMU version; assuming old host", "error", err.Error())
		}
		if v, err := c.GetLibVersion(); err == nil {
			caps.LibvirtVersion = v
		} else {
			slog.Warn("createVirtualMachine: could not probe libvirt version; assuming old host", "error", err.Error())
		}

		spec := domainSpec{
			Name:          req.Name,
			MemoryBytes:   req.MemoryBytes,
			Vcpus:         req.Vcpus,
			OSType:        osType,
			DiskPath:      volPath,
			DiskFormat:    format,
			NetworkId:     *req.NetworkId,
			ISOPath:       isoPath,
			VirtioISOPath: virtioISOPath,
		}
		domXML, profile, err := buildDomainXML(spec, caps)
		if err != nil {
			_ = vol.Delete(0)
			return fmt.Errorf("build domain XML: %w", err)
		}
		if len(profile.SkippedEnlightenments) > 0 {
			slog.Info("createVirtualMachine: hyper-v enlightenments skipped (unsupported by host)",
				"vm", req.Name,
				"qemuVersion", caps.QEMUVersion,
				"libvirtVersion", caps.LibvirtVersion,
				"skipped", profile.SkippedEnlightenments)
		}
		dom, err := c.DomainDefineXML(domXML)
		if err != nil {
			// Best-effort cleanup of the volume we just allocated.
			_ = vol.Delete(0)
			return fmt.Errorf("define domain: %w", err)
		}
		defer dom.Free()
		if req.Start != nil && *req.Start {
			if err := dom.Create(); err != nil {
				return fmt.Errorf("start domain: %w", asStorageUnavailable(err))
			}
		}
		return nil
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	vm, err := p.GetVirtualMachine(context.Background(), req.Name)
	if err != nil {
		return types.VirtualMachine{}, err
	}
	if len(warnings) > 0 {
		vm.Warnings = &warnings
	}
	return vm, nil
}

// xmlEscape escapes a string for embedding in XML attribute values.
func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (p *Provider) GetVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	var vm types.VirtualMachine
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		vm, err = p.domainToModel(dom)
		return err
	})
	return vm, err
}

func (p *Provider) StartVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		state, _, err := dom.GetState()
		if err != nil {
			return err
		}
		switch state {
		case libvirt.DOMAIN_RUNNING, libvirt.DOMAIN_BLOCKED:
			return fmt.Errorf("%w: virtual machine is already running", hypervisor.ErrInvalidVMState)
		case libvirt.DOMAIN_PAUSED, libvirt.DOMAIN_PMSUSPENDED:
			return dom.Resume()
		default:
			// A boot is where a path the backend can reach but the hypervisor
			// cannot finally bites; say so instead of returning a raw 500.
			return asStorageUnavailable(dom.Create())
		}
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(context.Background(), id)
}

func (p *Provider) ShutdownVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		state, _, err := dom.GetState()
		if err != nil {
			return err
		}
		if state != libvirt.DOMAIN_RUNNING && state != libvirt.DOMAIN_BLOCKED && state != libvirt.DOMAIN_PAUSED {
			return fmt.Errorf("%w: cannot gracefully shut down a stopped virtual machine", hypervisor.ErrInvalidVMState)
		}
		return dom.ShutdownFlags(0)
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(context.Background(), id)
}

func (p *Provider) RebootVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		state, _, err := dom.GetState()
		if err != nil {
			return err
		}
		if state != libvirt.DOMAIN_RUNNING && state != libvirt.DOMAIN_BLOCKED {
			return fmt.Errorf("%w: cannot reboot a stopped virtual machine", hypervisor.ErrInvalidVMState)
		}
		return dom.Reboot(libvirt.DOMAIN_REBOOT_DEFAULT)
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(context.Background(), id)
}

func (p *Provider) ForceStopVirtualMachine(_ context.Context, id string) (types.VirtualMachine, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		state, _, err := dom.GetState()
		if err != nil {
			return err
		}
		if state == libvirt.DOMAIN_SHUTOFF {
			return fmt.Errorf("%w: virtual machine is already stopped", hypervisor.ErrInvalidVMState)
		}
		return dom.DestroyFlags(0)
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(context.Background(), id)
}

// DeleteVirtualMachine undefines the domain; when deleteDisks is true the
// backing disk volumes are deleted from their pool as well. The VM must be
// stopped. CD-ROM / ISO media is never touched.
func (p *Provider) DeleteVirtualMachine(_ context.Context, id string, deleteDisks bool) error {
	return p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		defer dom.Free()

		state, _, err := dom.GetState()
		if err != nil {
			return err
		}
		if state != libvirt.DOMAIN_SHUTOFF && state != libvirt.DOMAIN_SHUTDOWN && state != libvirt.DOMAIN_CRASHED {
			return fmt.Errorf("%w: cannot delete a virtual machine that is not stopped", hypervisor.ErrInvalidVMState)
		}

		// Collect backing file paths before undefining (the XML is gone after).
		var diskPaths []string
		if deleteDisks {
			xmlStr, err := dom.GetXMLDesc(0)
			if err != nil {
				return err
			}
			var dx domainXML
			if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
				return err
			}
			for _, d := range dx.Disks {
				if d.Device == "disk" && d.Source.File != "" {
					diskPaths = append(diskPaths, d.Source.File)
				}
			}
		}

		if err := dom.UndefineFlags(libvirt.DOMAIN_UNDEFINE_MANAGED_SAVE | libvirt.DOMAIN_UNDEFINE_SNAPSHOTS_METADATA | libvirt.DOMAIN_UNDEFINE_NVRAM); err != nil {
			// Older libvirt may reject the flag set; fall back to plain undefine.
			if err2 := dom.Undefine(); err2 != nil {
				return fmt.Errorf("undefine domain: %w", err2)
			}
			slog.Warn("deleteVirtualMachine: full undefine flag set unsupported; used plain undefine", "vm", id, "error", err.Error())
		}

		for _, path := range diskPaths {
			vol, err := c.LookupStorageVolByPath(path)
			if err != nil {
				slog.Warn("deleteVirtualMachine: disk volume not found in any pool; skipped",
					"vm", id, "path", path)
				continue
			}
			if err := vol.Delete(0); err != nil {
				vol.Free()
				return fmt.Errorf("delete volume %s: %w", path, err)
			}
			vol.Free()
		}
		return nil
	})
}

// domainToModel converts one libvirt domain into the API model. Disk allocation
// and memory usage are resolved best-effort; IPs via the QEMU guest agent when
// available; live utilization via the provider's sampling cache.
func (p *Provider) domainToModel(dom *libvirt.Domain) (types.VirtualMachine, error) {
	name, err := dom.GetName()
	if err != nil {
		return types.VirtualMachine{}, err
	}
	xmlStr, err := dom.GetXMLDesc(0)
	if err != nil {
		return types.VirtualMachine{}, err
	}
	var dx domainXML
	if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
		return types.VirtualMachine{}, err
	}
	state, _, err := dom.GetState()
	if err != nil {
		return types.VirtualMachine{}, err
	}

	memBytes := normalizeBytes(dx.Memory.Value, dx.Memory.Unit)
	if memBytes == 0 {
		memBytes = normalizeBytes(dx.CurrentMemory.Value, dx.CurrentMemory.Unit)
	}

	vm := types.VirtualMachine{
		Id:          name,
		Name:        name,
		State:       stateToModel(state),
		Vcpus:       dx.VCPU,
		MemoryBytes: memBytes,
		Os:          osNameFromXML(&dx),
	}
	// OS family and applied profile, detected from the domain XML (nil for
	// domains that predate the profiles).
	if ot := osTypeFromDomainXML(xmlStr); ot != "" {
		vm.OsType = &ot
	}
	if _, profile := detectProfileFromDomainXML(xmlStr); profile != nil {
		vm.PerformanceProfile = profile
	}

	for _, d := range dx.Disks {
		if d.Device == "cdrom" && d.Source.File != "" && vm.IsoId == nil {
			iso := filepath.Base(d.Source.File)
			vm.IsoId = &iso
		}
		if d.Device != "disk" {
			continue
		}
		disk := types.Disk{
			Name:      d.Target.Dev,
			Format:    diskFormat(d.Driver.Type),
			SizeBytes: 0,
			Bus:       diskBus(d.Target.Bus),
		}
		// Capacity/allocation live on the backing volume, not in the domain
		// XML. GetBlockInfo returns both without touching the storage pool.
		if d.Source.File != "" {
			if info, err := dom.GetBlockInfo(d.Source.File, 0); err == nil && info != nil {
				disk.SizeBytes = int64(info.Capacity)
				alloc := int64(info.Allocation)
				disk.UsedBytes = &alloc
			}
		}
		vm.Disks = append(vm.Disks, disk)
	}
	if vm.Disks == nil {
		vm.Disks = []types.Disk{}
	}

	for _, n := range dx.NICs {
		nic := types.NetworkInterface{
			Name:       n.Target.Dev,
			Model:      nicModel(n.Type),
			MacAddress: strPtr(n.MAC.Address),
		}
		switch {
		case n.Source.Network != "":
			nic.Network = strPtr(n.Source.Network)
		case n.Source.Bridge != "":
			nic.Network = strPtr(n.Source.Bridge)
		}
		vm.NetworkInterfaces = append(vm.NetworkInterfaces, nic)
	}
	if vm.NetworkInterfaces == nil {
		vm.NetworkInterfaces = []types.NetworkInterface{}
	}

	// Best-effort IP discovery via the QEMU guest agent. Silently skipped
	// when the agent is not installed / not responding.
	if vm.State == types.VMStateRunning {
		vm.IpAddress = agentIPAddress(dom, vm.NetworkInterfaces)
		vm.Metrics = p.sampleMetrics(dom, &dx, name, memBytes)
	}
	return vm, nil
}

// agentIPAddress tries the qemu-guest-agent interface addresses and returns
// the first IPv4 address of the first NIC.
func agentIPAddress(dom *libvirt.Domain, nics []types.NetworkInterface) *string {
	addrs, err := dom.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT)
	if err != nil {
		return nil
	}
	if len(addrs) == 0 {
		return nil
	}
	for _, a := range addrs {
		for _, ip := range a.Addrs {
			if ip.Type == libvirt.IP_ADDR_TYPE_IPV4 && !strings.HasPrefix(ip.Addr, "127.") {
				found := ip.Addr
				return &found
			}
		}
	}
	_ = nics
	return nil
}

// unit-normalizing helper: libvirt XML may use KiB/MiB/GB...
func normalizeBytes(value int64, unit string) int64 {
	switch strings.ToLower(unit) {
	case "b", "bytes", "":
		return value
	case "kib", "k":
		return value * 1024
	case "mib", "m":
		return value * 1024 * 1024
	case "gib", "g":
		return value * 1024 * 1024 * 1024
	case "tib", "t":
		return value * 1024 * 1024 * 1024 * 1024
	case "kb":
		return value * 1000
	case "mb":
		return value * 1000 * 1000
	case "gb":
		return value * 1000 * 1000 * 1000
	default:
		return value
	}
}

func diskFormat(t string) types.DiskFormat {
	if t == "raw" {
		return types.DiskFormatRaw
	}
	return types.DiskFormatQcow2
}

func diskBus(b string) *types.DiskBus {
	switch b {
	case "virtio":
		v := types.DiskBusVirtio
		return &v
	case "sata":
		v := types.DiskBusSata
		return &v
	case "scsi":
		v := types.DiskBusScsi
		return &v
	}
	return nil
}

func nicModel(ifaceType string) types.NetworkInterfaceModel {
	if ifaceType == "virtio" {
		return types.NetworkInterfaceModelVirtio
	}
	if ifaceType == "e1000" {
		return types.NetworkInterfaceModelE1000
	}
	return types.NetworkInterfaceModelRtl8139
}

func osNameFromXML(dx *domainXML) *string {
	os := strings.TrimSpace(dx.OS)
	if os == "" {
		return nil
	}
	return &os
}

func sortVMs(vms []types.VirtualMachine) {
	for i := 1; i < len(vms); i++ {
		for j := i; j > 0 && vms[j].Id < vms[j-1].Id; j-- {
			vms[j], vms[j-1] = vms[j-1], vms[j]
		}
	}
}
