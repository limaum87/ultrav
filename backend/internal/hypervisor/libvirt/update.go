package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
	"github.com/ultrav/ultrav/backend/internal/iso"
)

// UpdateVirtualMachine changes the VM's persistent configuration: vCPU count,
// memory size and the attached ISO (CD-ROM install media). Only the fields
// present in the request are changed.
//
//   - vcpus / memoryBytes: require the domain to be shut off; applied to the
//     persistent config via SetVcpusFlags/SetMemoryFlags.
//   - isoId: a filename attaches (or swaps) the CD-ROM; an empty string
//     detaches it. Applied to the persistent config, so it takes effect on the
//     next boot (the running guest keeps its current media).
//   - networkId: moves the first NIC to another virtual network. Requires the
//     domain to be shut off; applied to the persistent config.
func (p *Provider) UpdateVirtualMachine(_ context.Context, id string, req types.VirtualMachineUpdate) (types.VirtualMachine, error) {
	err := p.withConn(func(c *libvirt.Connect) error {
		dom, err := c.LookupDomainByName(id)
		if err != nil {
			return hypervisor.ErrVMNotFound
		}
		defer dom.Free()

		needsStop := req.Vcpus != nil || req.MemoryBytes != nil || req.NetworkId != nil
		var state libvirt.DomainState
		if needsStop || req.IsoId != nil {
			s, _, err := dom.GetState()
			if err != nil {
				return fmt.Errorf("read domain state: %w", err)
			}
			state = s
		}

		if req.Vcpus != nil || req.MemoryBytes != nil {
			if state != libvirt.DOMAIN_SHUTOFF {
				return fmt.Errorf("%w: vCPU and memory changes require the virtual machine to be stopped", hypervisor.ErrInvalidVMState)
			}
			if req.Vcpus != nil {
				if err := dom.SetVcpusFlags(uint(*req.Vcpus), libvirt.DomainVcpuFlags(libvirt.DOMAIN_AFFECT_CONFIG)); err != nil {
					return fmt.Errorf("set vcpus: %w", err)
				}
			}
			if req.MemoryBytes != nil {
				if err := dom.SetMemoryFlags(uint64(*req.MemoryBytes), libvirt.DomainMemoryModFlags(libvirt.DOMAIN_AFFECT_CONFIG)); err != nil {
					return fmt.Errorf("set memory: %w", err)
				}
			}
		}

		if req.NetworkId != nil {
			if state != libvirt.DOMAIN_SHUTOFF {
				return fmt.Errorf("%w: network changes require the virtual machine to be stopped", hypervisor.ErrInvalidVMState)
			}
			if _, err := c.LookupNetworkByName(*req.NetworkId); err != nil {
				return hypervisor.ErrNetworkNotFound
			}
			if err := setNICNetwork(dom, *req.NetworkId); err != nil {
				return err
			}
		}

		if req.IsoId != nil {
			isoID := *req.IsoId
			// The CD-ROM source in the domain XML must be the absolute path on
			// the host: qemu opens it in the host namespace, so a bare filename
			// makes the domain unbootable (STORAGE_UNAVAILABLE on start).
			path := ""
			if isoID != "" {
				if !iso.ValidID.MatchString(isoID) {
					return fmt.Errorf("%w: invalid ISO filename", os.ErrInvalid)
				}
				path = filepath.Join(p.isoDir(), isoID)
				if _, err := os.Stat(path); err != nil {
					return hypervisor.ErrIsoNotFound
				}
			}
			if err := setCDROM(c, dom, path); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return types.VirtualMachine{}, err
	}
	return p.GetVirtualMachine(context.Background(), id)
}

// setNICNetwork repoints the domain's first network interface at another
// virtual network in the persistent config (takes effect on next boot).
func setNICNetwork(dom *libvirt.Domain, network string) error {
	xmlStr, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return fmt.Errorf("read domain xml: %w", err)
	}
	var dx struct {
		NICs []struct {
			Type string `xml:"type,attr"`
			MAC struct {
				Address string `xml:"address,attr"`
			} `xml:"mac"`
			Source struct {
				Network string `xml:"network,attr"`
				Bridge  string `xml:"bridge,attr"`
			} `xml:"source"`
			Model struct {
				Type string `xml:"type,attr"`
			} `xml:"model"`
		} `xml:"devices>interface"`
	}
	if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
		return fmt.Errorf("parse domain xml: %w", err)
	}
	if len(dx.NICs) == 0 {
		return fmt.Errorf("%w: virtual machine has no network interface", hypervisor.ErrInvalidVMState)
	}
	nic := dx.NICs[0]
	model := nic.Model.Type
	if model == "" {
		model = "virtio"
	}
	deviceXML := fmt.Sprintf(`<interface type='network'>
      <mac address='%s'/>
      <source network='%s'/>
      <model type='%s'/>
    </interface>`, xmlEscape(nic.MAC.Address), xmlEscape(network), xmlEscape(model))
	if err := dom.UpdateDeviceFlags(deviceXML, libvirt.DomainDeviceModifyFlags(libvirt.DOMAIN_AFFECT_CONFIG)); err != nil {
		return fmt.Errorf("update interface: %w", err)
	}
	return nil
}

// setCDROM attaches, swaps or detaches the domain's CD-ROM in the persistent
// config. isoPath == "" detaches. Device changes only affect the next boot.
func setCDROM(c *libvirt.Connect, dom *libvirt.Domain, isoPath string) error {
	// Resolve the existing CD-ROM (dev + bus) so detach/swap reuse it.
	curDev, curBus, err := currentCDROM(dom)
	if err != nil {
		return err
	}

	detach := func() {
		if curDev == "" {
			return
		}
		xml := cdromXML(curDev, curBus, "", false)
		_ = dom.DetachDeviceFlags(xml, libvirt.DomainDeviceModifyFlags(libvirt.DOMAIN_AFFECT_CONFIG))
	}

	switch {
	case isoPath == "":
		detach()
		return setOSBootOrder(c, dom, false)
	case curDev != "":
		detach()
	}

	dev, bus, err := nextCDROMTarget(dom)
	if err != nil {
		return err
	}
	// A domain defined without install media gets <os><boot dev='hd'/></os> from
	// libvirt, and libvirt rejects per-device boot order in that case. Use the
	// per-device form (what provisioning emits) only when there is none.
	osBoot, err := hasOSBootOrder(dom)
	if err != nil {
		return err
	}
	xml := cdromXML(dev, bus, isoPath, !osBoot)
	if err := dom.AttachDeviceFlags(xml, libvirt.DomainDeviceModifyFlags(libvirt.DOMAIN_AFFECT_CONFIG)); err != nil {
		return fmt.Errorf("attach cdrom: %w", asStorageUnavailable(err))
	}
	// Put the install media ahead of the (possibly empty) system disk, otherwise
	// the guest stops at "No bootable device".
	return setOSBootOrder(c, dom, true)
}

var (
	osBlockRe  = regexp.MustCompile(`(?s)<os>.*?</os>`)
	osBootElRe = regexp.MustCompile(`\s*<boot[^>]*/>`)
)

// hasOSBootOrder reports whether the domain uses the domain-level boot order
// (<os><boot dev='...'/></os>) instead of per-device <boot order='N'/>.
func hasOSBootOrder(dom *libvirt.Domain) (bool, error) {
	xmlStr, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return false, fmt.Errorf("read domain xml: %w", err)
	}
	return strings.Contains(xmlStr, "<boot dev="), nil
}

// setOSBootOrder rewrites the domain-level boot order: cdromFirst boots the
// CD-ROM before the hard disk. No-op for domains using per-device boot order.
func setOSBootOrder(c *libvirt.Connect, dom *libvirt.Domain, cdromFirst bool) error {
	xmlStr, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return fmt.Errorf("read domain xml: %w", err)
	}
	if !strings.Contains(xmlStr, "<boot dev=") {
		return nil
	}
	boot := "<boot dev='hd'/>"
	if cdromFirst {
		boot = "<boot dev='cdrom'/><boot dev='hd'/>"
	}
	out := osBlockRe.ReplaceAllStringFunc(xmlStr, func(block string) string {
		body := osBootElRe.ReplaceAllString(block, "")
		return strings.Replace(body, "<os>", "<os>"+boot, 1)
	})
	if out == xmlStr {
		return nil
	}
	nd, err := c.DomainDefineXML(out)
	if err != nil {
		return fmt.Errorf("update boot order: %w", err)
	}
	nd.Free()
	return nil
}

// nextCDROMTarget picks a free target device name and a bus for a new CD-ROM.
//
// The target name cannot be a constant: device names are unique across buses
// (they all surface as sdX in the guest), so a VM created without install media
// already owns sda for its system disk — attaching a CD-ROM as sda fails with
// "target sda already exists". The bus follows the system disk when it is a
// legacy one: a guest with an IDE/SATA controller (e.g. a converted Windows XP
// image) cannot see a CD-ROM on a bus the domain does not have.
func nextCDROMTarget(dom *libvirt.Domain) (dev, bus string, err error) {
	inactive, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return "", "", fmt.Errorf("read domain xml: %w", err)
	}
	var dx domainXML
	if err := xml.Unmarshal([]byte(inactive), &dx); err != nil {
		return "", "", fmt.Errorf("parse domain xml: %w", err)
	}

	used := make(map[string]bool, len(dx.Disks))
	bus = "sata"
	systemBus := ""
	for _, d := range dx.Disks {
		if d.Target.Dev != "" {
			used[d.Target.Dev] = true
		}
		if d.Device != "cdrom" && systemBus == "" {
			systemBus = d.Target.Bus
		}
	}
	if systemBus == "ide" || systemBus == "sata" {
		bus = systemBus
	}

	for c := 'a'; c <= 'z'; c++ {
		candidate := "sd" + string(c)
		if !used[candidate] {
			return candidate, bus, nil
		}
	}
	return "", "", fmt.Errorf("no free target device name for a cdrom")
}

// cdromXML renders the CD-ROM device XML used by attach/detach operations.
// withBoot adds the per-device boot order used by the provisioning path.
func cdromXML(dev, bus, isoPath string, withBoot bool) string {
	source, boot := "", ""
	if isoPath != "" {
		source = fmt.Sprintf("\n      <source file='%s'/>", xmlEscape(isoPath))
		if withBoot {
			boot = "\n      <boot order='1'/>"
		}
	}
	return fmt.Sprintf(`<disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <target dev='%s' bus='%s'/>%s
      <readonly/>%s
    </disk>`, xmlEscape(dev), xmlEscape(bus), source, boot)
}

// currentCDROM returns the target device and bus of the domain's existing
// CD-ROM (from the persistent config), or "" when it has none.
func currentCDROM(dom *libvirt.Domain) (dev, bus string, err error) {
	xmlStr, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return "", "", fmt.Errorf("read domain xml: %w", err)
	}
	var dx domainXML
	if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
		return "", "", fmt.Errorf("parse domain xml: %w", err)
	}
	for _, d := range dx.Disks {
		if d.Device == "cdrom" {
			return d.Target.Dev, d.Target.Bus, nil
		}
	}
	return "", "", nil
}
