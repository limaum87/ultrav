package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"

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
			if isoID != "" {
				if !iso.ValidID.MatchString(isoID) {
					return fmt.Errorf("%w: invalid ISO filename", os.ErrInvalid)
				}
				path := filepath.Join(p.isoDir(), isoID)
				if _, err := os.Stat(path); err != nil {
					return hypervisor.ErrIsoNotFound
				}
			}
			if err := setCDROM(dom, isoID); err != nil {
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
func setCDROM(dom *libvirt.Domain, isoPath string) error {
	// Resolve the current CD-ROM target (e.g. sda) so detach/swap reuses it.
	curDev, err := currentCDROMDev(dom)
	if err != nil {
		return err
	}

	detach := func() {
		if curDev == "" {
			return
		}
		xml := cdromXML(curDev, "")
		_ = dom.DetachDeviceFlags(xml, libvirt.DomainDeviceModifyFlags(libvirt.DOMAIN_AFFECT_CONFIG))
	}

	switch {
	case isoPath == "":
		detach()
		return nil
	case curDev != "":
		detach()
	}

	xml := cdromXML(cdromTarget, isoPath)
	if err := dom.AttachDeviceFlags(xml, libvirt.DomainDeviceModifyFlags(libvirt.DOMAIN_AFFECT_CONFIG)); err != nil {
		return fmt.Errorf("attach cdrom: %w", asStorageUnavailable(err))
	}
	return nil
}

// cdromTarget is the fixed target device used for newly attached CD-ROMs,
// matching the sata CD-ROM created at VM provisioning time.
const cdromTarget = "sda"

// cdromXML renders the CD-ROM device XML used by attach/detach operations.
func cdromXML(dev, isoPath string) string {
	source := ""
	if isoPath != "" {
		source = fmt.Sprintf("\n      <source file='%s'/>", xmlEscape(isoPath))
	}
	return fmt.Sprintf(`<disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <target dev='%s' bus='sata'/>%s
      <readonly/>
    </disk>`, xmlEscape(dev), source)
}

// currentCDROMDev returns the target device of the domain's existing CD-ROM
// (from the persistent config), or "" when it has none.
func currentCDROMDev(dom *libvirt.Domain) (string, error) {
	xmlStr, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return "", fmt.Errorf("read domain xml: %w", err)
	}
	var dx domainXML
	if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
		return "", fmt.Errorf("parse domain xml: %w", err)
	}
	for _, d := range dx.Disks {
		if d.Device == "cdrom" {
			return d.Target.Dev, nil
		}
	}
	return "", nil
}
