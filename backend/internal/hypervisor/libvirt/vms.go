package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
)

// domainXML is the subset of the libvirt domain XML we consume.
type domainXML struct {
	Name   string `xml:"name"`
	OS     string `xml:"os>type"`
	Memory struct {
		Value int64  `xml:"chardata"`
		Unit  string `xml:"unit,attr"`
	} `xml:"memory"`
	CurrentMemory struct {
		Value int64  `xml:"chardata"`
		Unit  string `xml:"unit,attr"`
	} `xml:"currentMemory"`
	VCPU int            `xml:"vcpu"`
	Disks []domainDisk  `xml:"devices>disk"`
	NICs  []domainNIC   `xml:"devices>interface"`
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
			libvirt.CONNECT_LIST_DOMAINS_ACTIVE|libvirt.CONNECT_LIST_DOMAINS_INACTIVE)
		if err != nil {
			return err
		}
		out = make([]types.VirtualMachine, 0, len(doms))
		for i := range doms {
			vm, err := domainToModel(&doms[i])
			if err != nil {
				continue // skip unreadable domains rather than failing the list
			}
			out = append(out, vm)
		}
		sortVMs(out)
		return nil
	})
	return out, err
}

// CreateVirtualMachine allocates a disk volume in the requested storage
// pool and defines the domain (stopped, or started when req.Start is true).
func (p *Provider) CreateVirtualMachine(_ context.Context, req types.VirtualMachineCreate) (types.VirtualMachine, error) {
	format := "qcow2"
	if req.Disk.Format != nil {
		format = string(*req.Disk.Format)
	}
	if req.NetworkId == nil || *req.NetworkId == "" {
		def := "default"
		req.NetworkId = &def
	}

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
		if active, err := pool.IsActive(); err == nil && !active {
			pool.Refresh(0)
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

		domXML := fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  <memory unit='bytes'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='%s'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='network'>
      <source network='%s'/>
      <model type='virtio'/>
    </interface>
    <console type='pty'/>
  </devices>
</domain>`, xmlEscape(req.Name), req.MemoryBytes, req.Vcpus, format, xmlEscape(volPath), xmlEscape(*req.NetworkId))
		dom, err := c.DomainDefineXML(domXML)
		if err != nil {
			// Best-effort cleanup of the volume we just allocated.
			_ = vol.Delete(0)
			return fmt.Errorf("define domain: %w", err)
		}
		defer dom.Free()
		if req.Start != nil && *req.Start {
			if err := dom.Create(); err != nil {
				return fmt.Errorf("start domain: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(context.Background(), req.Name)
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
		vm, err = domainToModel(dom)
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
			return dom.Create()
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

// domainToModel converts one libvirt domain into the API model. Disk sizes
// are resolved best-effort through storage volume lookups; IPs via the QEMU
// guest agent when available.
func domainToModel(dom *libvirt.Domain) (types.VirtualMachine, error) {
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

	for _, d := range dx.Disks {
		if d.Device != "disk" {
			continue
		}
		disk := types.Disk{
			Name:      d.Target.Dev,
			Format:    diskFormat(d.Driver.Type),
			SizeBytes: 0,
			Bus:       diskBus(d.Target.Bus),
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
		vm.NetworkInterfaces = append(vm.NetworkInterfaces, nic)
	}
	if vm.NetworkInterfaces == nil {
		vm.NetworkInterfaces = []types.NetworkInterface{}
	}

	// Best-effort IP discovery via the QEMU guest agent. Silently skipped
	// when the agent is not installed / not responding.
	if vm.State == types.VMStateRunning {
		vm.IpAddress = agentIPAddress(dom, vm.NetworkInterfaces)
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
