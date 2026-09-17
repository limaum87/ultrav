package libvirt

import (
	"encoding/xml"
	"fmt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// This file turns a create request into libvirt domain XML. The assembly is a
// pure function (buildDomainXML) so profiles can be unit-tested without a
// libvirt connection. The Provider only resolves host facts (QEMU/libvirt
// versions) and passes them in as hostFeatures.
//
// Three performance profiles, selected by the requested osType:
//
//   - linux (default): virtio-scsi disk on an iothread-ed controller with
//     cache='none' / io='native' / discard='unmap', multi-queue virtio NIC,
//     cpu host-passthrough (best performance; limits live migration between
//     hosts with different CPUs — a multi-host phase concern), guest agent
//     channel, memballoon disabled, catchup timers, virtio RNG.
//   - windows: the linux profile plus Hyper-V enlightenments (only the ones
//     the host supports), clock offset='localtime' with hypervclock, and an
//     optional second SATA CD-ROM with the VirtIO drivers ISO.
//   - other: maximum compatibility — SATA disk, e1000e NIC, cpu
//     host-model, no enlightenments (exotic guests, converted VHDX without
//     virtio drivers).

// queueCount is the multi-queue count for the SCSI controller and the vhost
// NIC: one queue per vCPU, capped at 8.
func queueCount(vcpus int) int {
	if vcpus < 1 {
		return 1
	}
	if vcpus > 8 {
		return 8
	}
	return vcpus
}

// domainSpec is everything buildDomainXML needs to render a domain.
type domainSpec struct {
	Name        string
	MemoryBytes int64
	Vcpus       int
	OSType      types.VirtualMachineCreateOsType // linux (default), windows, other

	DiskPath   string
	DiskFormat string // qcow2 | raw

	NetworkId string

	// ISOPath is the install media attached as first SATA CD-ROM (boot order
	// 1); empty means boot from disk.
	ISOPath string
	// VirtioISOPath is the VirtIO drivers ISO attached as second SATA CD-ROM
	// (windows installs only); empty means not attached.
	VirtioISOPath string

	// NICModel overrides the profile's network adapter model. Empty means
	// "use the profile default" (virtio, or e1000e for the other profile).
	NICModel types.VirtualMachineCreateNicModel
}

// hostFeatures carries the host facts the profiles depend on. A zero value
// means "very old host": only enlightenments supported since forever are
// emitted, which is the safe default.
type hostFeatures struct {
	// QEMUVersion and LibvirtVersion use libvirt's packed encoding
	// (major*1_000_000 + minor*1_000 + micro), as returned by
	// virConnectGetVersion / virConnectGetLibVersion.
	QEMUVersion    uint32
	LibvirtVersion uint32
}

// hypervFeature is one Hyper-V enlightenment with the minimum QEMU and
// libvirt versions that support it. Versions are deliberately conservative:
// when in doubt the feature is left out and reported (the cost of a missing
// enlightenment is small; the cost of an unknown element is a domain that
// refuses to define).
type hypervFeature struct {
	name        string
	minQEMU     uint32
	minLibvirt  uint32
	needsConfig func(spec hostFeatures) bool
}

func ver(major, minor, micro uint32) uint32 { return major*1_000_000 + minor*1_000 + micro }

// spinlocks uses a custom element (retries attribute), handled separately.
var hypervFeatures = []hypervFeature{
	// Available since the earliest supported QEMU/libvirt combos.
	{name: "relaxed"},
	{name: "vapic"},
	{name: "spinlocks", needsConfig: func(hostFeatures) bool { return true }},
	{name: "vpindex"},
	{name: "synic", minQEMU: ver(3, 0, 0), minLibvirt: ver(1, 2, 0)},
	{name: "stimer", minQEMU: ver(3, 0, 0), minLibvirt: ver(3, 2, 0)},
	{name: "runtime", minQEMU: ver(3, 0, 0), minLibvirt: ver(3, 2, 0)},
	{name: "frequencies", minQEMU: ver(4, 2, 0), minLibvirt: ver(5, 10, 0)},
	{name: "reset", minQEMU: ver(4, 2, 0), minLibvirt: ver(5, 10, 0)},
	{name: "tlbflush", minQEMU: ver(5, 0, 0), minLibvirt: ver(6, 2, 0)},
	{name: "ipi", minQEMU: ver(5, 0, 0), minLibvirt: ver(6, 2, 0)},
}

// selectHypervFeatures returns the enlightenment names supported by the host
// and the list of those that had to be left out (for structured logging and
// for performanceProfile.hypervEnlightenments).
func selectHypervFeatures(caps hostFeatures) (supported, skipped []string) {
	for _, f := range hypervFeatures {
		if caps.QEMUVersion >= f.minQEMU && caps.LibvirtVersion >= f.minLibvirt {
			supported = append(supported, f.name)
		} else {
			skipped = append(skipped, f.name)
		}
	}
	return supported, skipped
}

// --- domain XML structs (only the subset UltraV generates) ---

type memXML struct {
	Value string `xml:",chardata"`
	Unit  string `xml:"unit,attr"`
}

type domainCPU struct {
	Mode       string `xml:"mode,attr"`
	Check      string `xml:"check,attr,omitempty"`
	Migratable string `xml:"migratable,attr,omitempty"`
}

type domainBoot struct {
	Dev string `xml:"dev,attr"`
}

type domainOSType struct {
	Arch    string `xml:"arch,attr"`
	Machine string `xml:"machine,attr"`
	HVM     string `xml:",chardata"`
}

type domainOS struct {
	Type domainOSType `xml:"type"`
	Boot []domainBoot `xml:"boot"`
}

type hypervSpinlocks struct {
	State   string `xml:"state,attr"`
	Retries int    `xml:"retries,attr"`
}

type hypervFeatureXML struct {
	State string `xml:"state,attr"`
}

type hypervXML struct {
	Mode       string             `xml:"mode,attr"`
	Relaxed    *hypervFeatureXML  `xml:"relaxed"`
	Vapic      *hypervFeatureXML  `xml:"vapic"`
	Spinlocks  *hypervSpinlocks   `xml:"spinlocks"`
	Vpindex    *hypervFeatureXML  `xml:"vpindex"`
	Synic      *hypervFeatureXML  `xml:"synic"`
	Stimer     *hypervFeatureXML  `xml:"stimer"`
	Runtime    *hypervFeatureXML  `xml:"runtime"`
	Frequencies *hypervFeatureXML `xml:"frequencies"`
	Reset      *hypervFeatureXML  `xml:"reset"`
	Tlbflush   *hypervFeatureXML  `xml:"tlbflush"`
	Ipi        *hypervFeatureXML  `xml:"ipi"`
}

type domainFeatures struct {
	ACPI   *struct{}  `xml:"acpi"`
	APIC   *struct{}  `xml:"apic"`
	Hyperv *hypervXML `xml:"hyperv,omitempty"`
}

type clockTimer struct {
	Name       string `xml:"name,attr"`
	Tickpolicy string `xml:"tickpolicy,attr,omitempty"`
	Present    string `xml:"present,attr,omitempty"`
}

type domainClock struct {
	Offset string       `xml:"offset,attr"`
	Timers []clockTimer `xml:"timer"`
}

type diskDriver struct {
	Name         string `xml:"name,attr"`
	Type         string `xml:"type,attr"`
	Cache        string `xml:"cache,attr,omitempty"`
	IO           string `xml:"io,attr,omitempty"`
	Discard      string `xml:"discard,attr,omitempty"`
	DetectZeroes string `xml:"detect_zeroes,attr,omitempty"`
	Queues       int    `xml:"queues,attr,omitempty"`
	IOThread     int    `xml:"iothread,attr,omitempty"`
}

type diskSource struct {
	File string `xml:"file,attr"`
}

type diskTarget struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

type diskBoot struct {
	Order int `xml:"order,attr"`
}

type diskXML struct {
	Device   string      `xml:"device,attr"`
	Driver   diskDriver  `xml:"driver"`
	Source   diskSource  `xml:"source"`
	Target   diskTarget  `xml:"target"`
	Readonly *struct{}   `xml:"readonly"`
	Boot     *diskBoot   `xml:"boot,omitempty"`
}

type controllerDriver struct {
	Queues   int `xml:"queues,attr"`
	IOThread int `xml:"iothread,attr"`
}

type controllerXML struct {
	Type   string            `xml:"type,attr"`
	Index  string            `xml:"index,attr"`
	Model  string            `xml:"model,attr,omitempty"`
	Driver *controllerDriver `xml:"driver,omitempty"`
}

type interfaceSource struct {
	Network string `xml:"network,attr"`
}

type interfaceModel struct {
	Type string `xml:"type,attr"`
}

type interfaceDriver struct {
	Name   string `xml:"name,attr,omitempty"`
	Queues int    `xml:"queues,attr,omitempty"`
}

type interfaceXML struct {
	Type    string           `xml:"type,attr"`
	Source  interfaceSource  `xml:"source"`
	Model   interfaceModel   `xml:"model"`
	Driver  *interfaceDriver `xml:"driver,omitempty"`
}

type channelTarget struct {
	Type string `xml:"type,attr"`
	Name string `xml:"name,attr"`
	State string `xml:"state,attr,omitempty"`
}

type channelXML struct {
	Type   string        `xml:"type,attr"`
	Target channelTarget `xml:"target"`
}

type rngBackend struct {
	Model string `xml:"model,attr"`
	Text  string `xml:",chardata"`
}

type rngXML struct {
	Model   string     `xml:"model,attr"`
	Backend rngBackend `xml:"backend"`
}

type memballoonXML struct {
	Model string `xml:"model,attr"`
}

type graphicsListen struct {
	Type string `xml:"type,attr"`
}

type vncGraphicsXML struct {
	Type   string         `xml:"type,attr"`
	Listen graphicsListen `xml:"listen"`
}

type videoModel struct {
	Type string `xml:"type,attr"`
}

type videoXML struct {
	Model videoModel `xml:"model"`
}

type devicesXML struct {
	Disks      []diskXML       `xml:"disk"`
	Controller *controllerXML  `xml:"controller,omitempty"`
	Interface  interfaceXML    `xml:"interface"`
	Channel    channelXML      `xml:"channel"`
	RNG        *rngXML         `xml:"rng,omitempty"`
	Memballoon memballoonXML   `xml:"memballoon"`
	Graphics   vncGraphicsXML  `xml:"graphics"`
	Video      videoXML        `xml:"video"`
	Console    *struct{}       `xml:"console"`
}

type domainXMLModel struct {
	XMLName     xml.Name        `xml:"domain"`
	Type        string          `xml:"type,attr"`
	Name        string          `xml:"name"`
	Memory      memXML          `xml:"memory"`
	VCPU        int             `xml:"vcpu"`
	OS          domainOS        `xml:"os"`
	Features    domainFeatures  `xml:"features"`
	CPU         *domainCPU      `xml:"cpu,omitempty"`
	Clock       domainClock     `xml:"clock"`
	IOThreads   *int            `xml:"iothreads,omitempty"`
	OnPoweroff  string          `xml:"on_poweroff"`
	OnReboot    string          `xml:"on_reboot"`
	OnCrash     string          `xml:"on_crash"`
	Devices     devicesXML      `xml:"devices"`
}

// appliedProfile records what buildDomainXML decided, so the Provider can
// report performanceProfile back to the API and log skipped enlightenments.
type appliedProfile struct {
	CpuMode               string
	DiskBus               string
	Cache                 string
	NICModel              string
	IOThreads             int
	HypervEnlightenments  []string
	SkippedEnlightenments []string
	OSType                types.VirtualMachineCreateOsType
}

func (a appliedProfile) toAPI() *types.PerformanceProfile {
	io := a.IOThreads
	var enlight []string
	if len(a.HypervEnlightenments) > 0 {
		enlight = a.HypervEnlightenments
	}
	var cpuMode types.PerformanceProfileCpuMode
	switch a.CpuMode {
	case "host-passthrough":
		cpuMode = types.HostPassthrough
	case "host-model":
		cpuMode = types.HostModel
	default:
		cpuMode = types.Custom
	}
	var diskBus types.PerformanceProfileDiskBus
	if a.DiskBus == "sata" {
		diskBus = types.Sata
	} else {
		diskBus = types.Scsi
	}
	var cache *types.PerformanceProfileCache
	if a.Cache != "" {
		c := types.PerformanceProfileCache(a.Cache)
		cache = &c
	}
	osType := types.VirtualMachineOsType(a.OSType)
	_ = osType
	var nic *types.PerformanceProfileNicModel
	if a.NICModel != "" {
		n := types.PerformanceProfileNicModel(a.NICModel)
		nic = &n
	}
	return &types.PerformanceProfile{
		CpuMode:              &cpuMode,
		DiskBus:              &diskBus,
		Cache:                cache,
		NicModel:             nic,
		IoThreads:            &io,
		HypervEnlightenments: &enlight,
	}
}

// buildDomainXML renders the libvirt domain XML for the given spec. It is
// pure: all dynamic values flow through encoding/xml marshaling, so no
// user-provided string is ever concatenated into the document.
func buildDomainXML(spec domainSpec, caps hostFeatures) (string, *appliedProfile, error) {
	if spec.Name == "" {
		return "", nil, fmt.Errorf("domain name is required")
	}
	if spec.Vcpus < 1 {
		return "", nil, fmt.Errorf("vcpus must be >= 1")
	}
	if spec.MemoryBytes < 1 {
		return "", nil, fmt.Errorf("memory must be > 0")
	}
	if spec.DiskPath == "" || spec.DiskFormat == "" {
		return "", nil, fmt.Errorf("disk path and format are required")
	}
	if spec.NetworkId == "" {
		return "", nil, fmt.Errorf("network id is required")
	}

	osType := spec.OSType
	if osType == "" {
		osType = types.VirtualMachineCreateOsTypeLinux
	}

	d := domainXMLModel{
		Type: "kvm",
		Name: spec.Name,
		Memory: memXML{Value: fmt.Sprintf("%d", spec.MemoryBytes), Unit: "bytes"},
		VCPU: spec.Vcpus,
		OS: domainOS{
			Type: domainOSType{Arch: "x86_64", Machine: "q35", HVM: "hvm"},
		},
		Clock: domainClock{
			Offset: "utc",
			Timers: []clockTimer{
				{Name: "rtc", Tickpolicy: "catchup"},
				{Name: "pit", Tickpolicy: "delay"},
				{Name: "hpet", Present: "no"},
			},
		},
		OnPoweroff: "destroy",
		OnReboot:   "restart",
		OnCrash:    "destroy",
	}

	queue := queueCount(spec.Vcpus)
	profile := appliedProfile{
		OSType:    osType,
		CpuMode:   "host-passthrough",
		DiskBus:   "scsi",
		Cache:     "none",
		NICModel:  "virtio",
		IOThreads: 1,
	}

	d.Features = domainFeatures{ACPI: &struct{}{}, APIC: &struct{}{}}
	d.CPU = &domainCPU{Mode: "host-passthrough", Check: "none", Migratable: "on"}
	d.IOThreads = &profile.IOThreads
	d.Devices.Controller = &controllerXML{
		Type:  "scsi",
		Index: "0",
		Model: "virtio-scsi",
		Driver: &controllerDriver{
			Queues:   queue,
			IOThread: 1,
		},
	}

	switch osType {
	case types.VirtualMachineCreateOsTypeWindows:
		profile.OSType = types.VirtualMachineCreateOsTypeWindows
		d.Clock.Offset = "localtime"
		d.Clock.Timers = append(d.Clock.Timers, clockTimer{Name: "hypervclock", Present: "yes"})
		supported, skipped := selectHypervFeatures(caps)
		profile.HypervEnlightenments = supported
		profile.SkippedEnlightenments = skipped
		hv := &hypervXML{Mode: "custom"}
		for _, name := range supported {
			switch name {
			case "relaxed":
				hv.Relaxed = &hypervFeatureXML{State: "on"}
			case "vapic":
				hv.Vapic = &hypervFeatureXML{State: "on"}
			case "spinlocks":
				hv.Spinlocks = &hypervSpinlocks{State: "on", Retries: 8191}
			case "vpindex":
				hv.Vpindex = &hypervFeatureXML{State: "on"}
			case "synic":
				hv.Synic = &hypervFeatureXML{State: "on"}
			case "stimer":
				hv.Stimer = &hypervFeatureXML{State: "on"}
			case "runtime":
				hv.Runtime = &hypervFeatureXML{State: "on"}
			case "frequencies":
				hv.Frequencies = &hypervFeatureXML{State: "on"}
			case "reset":
				hv.Reset = &hypervFeatureXML{State: "on"}
			case "tlbflush":
				hv.Tlbflush = &hypervFeatureXML{State: "on"}
			case "ipi":
				hv.Ipi = &hypervFeatureXML{State: "on"}
			}
		}
		d.Features.Hyperv = hv

	case types.VirtualMachineCreateOsTypeOther:
		// Maximum compatibility: SATA disk, e1000e NIC, host-model CPU,
		// no iothreads, no enlightenments.
		profile.CpuMode = "host-model"
		profile.DiskBus = "sata"
		profile.Cache = ""
		profile.NICModel = "e1000e"
		profile.IOThreads = 0
		d.CPU = &domainCPU{Mode: "host-model"}
		d.IOThreads = nil
		d.Devices.Controller = nil

	case types.VirtualMachineCreateOsTypeLinux:
		// the defaults set above are the linux profile
	default:
		return "", nil, fmt.Errorf("unknown osType %q", osType)
	}

	// An explicit nicModel overrides the profile: a Windows guest that must
	// have network before NetKVM is installed needs an emulated adapter.
	if spec.NICModel != "" {
		switch spec.NICModel {
		case types.VirtualMachineCreateNicModelVirtio,
			types.VirtualMachineCreateNicModelE1000e,
			types.VirtualMachineCreateNicModelRtl8139:
			profile.NICModel = string(spec.NICModel)
		default:
			return "", nil, fmt.Errorf("unknown nicModel %q", spec.NICModel)
		}
	}

	// Disks. Device names must be unique across buses (they all surface as
	// sdX in the guest), so they are assigned in order of attachment.
	nextDev := 'a'
	nextDevName := func() string {
		name := "sd" + string(nextDev)
		nextDev++
		return name
	}

	if spec.ISOPath != "" {
		// Install media: first CD-ROM, boot order 1.
		d.Devices.Disks = append(d.Devices.Disks, diskXML{
			Device: "cdrom",
			Driver: diskDriver{Name: "qemu", Type: "raw"},
			Source: diskSource{File: spec.ISOPath},
			Target: diskTarget{Dev: nextDevName(), Bus: "sata"},
			Readonly: &struct{}{},
			Boot:  &diskBoot{Order: 1},
		})
	}
	if spec.VirtioISOPath != "" {
		d.Devices.Disks = append(d.Devices.Disks, diskXML{
			Device: "cdrom",
			Driver: diskDriver{Name: "qemu", Type: "raw"},
			Source: diskSource{File: spec.VirtioISOPath},
			Target: diskTarget{Dev: nextDevName(), Bus: "sata"},
			Readonly: &struct{}{},
		})
	}

	disk := diskXML{
		Device: "disk",
		Driver: diskDriver{Name: "qemu", Type: spec.DiskFormat},
		Source: diskSource{File: spec.DiskPath},
		Target: diskTarget{Bus: profile.DiskBus},
	}
	if profile.Cache != "" {
		disk.Driver.Cache = profile.Cache
		disk.Driver.IO = "native"
		disk.Driver.Discard = "unmap"
		disk.Driver.DetectZeroes = "unmap"
	}
	if spec.ISOPath != "" {
		// CD-ROM boots first; the system disk is the second boot device.
		disk.Boot = &diskBoot{Order: 2}
	}
	d.Devices.Disks = append(d.Devices.Disks, disk)
	// SCSI disks need a controller; libvirt assigns the LUN automatically,
	// but the target dev name must still be unique.
	d.Devices.Disks[len(d.Devices.Disks)-1].Target.Dev = nextDevName()

	nic := interfaceXML{
		Type:   "network",
		Source: interfaceSource{Network: spec.NetworkId},
		Model:  interfaceModel{Type: profile.NICModel},
	}
	// vhost multi-queue is a virtio-net feature; emulated models reject it.
	if profile.NICModel == "virtio" {
		nic.Driver = &interfaceDriver{Name: "vhost", Queues: queue}
	}
	d.Devices.Interface = nic

	// QEMU guest agent channel: the API resolves guest IPs through it.
	d.Devices.Channel = channelXML{
		Type:   "unix",
		Target: channelTarget{Type: "virtio", Name: "org.qemu.guest_agent.0"},
	}
	d.Devices.RNG = &rngXML{
		Model:   "virtio",
		Backend: rngBackend{Model: "random", Text: "/dev/urandom"},
	}
	// DC / database workloads dislike memory ballooning; keep it off.
	d.Devices.Memballoon = memballoonXML{Model: "none"}

	// VNC on an auto-generated unix socket: the web console dials it
	// directly and it is never exposed on the network.
	d.Devices.Graphics = vncGraphicsXML{Type: "vnc", Listen: graphicsListen{Type: "socket"}}
	d.Devices.Video = videoXML{Model: videoModel{Type: "vga"}}
	d.Devices.Console = &struct{}{}

	out, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("marshal domain XML: %w", err)
	}
	return xml.Header[:len(xml.Header)-1] + "\n" + string(out), &profile, nil
}

// detectProfileFromDomainXML extracts the applied profile (and OS family)
// from an existing domain's XML, for the read model. Returns nils for
// domains that predate the profiles.
func detectProfileFromDomainXML(xmlStr string) (osType *types.VirtualMachineOsType, profile *types.PerformanceProfile) {
	var dx struct {
		Features struct {
			Hyperv *struct{} `xml:"hyperv"`
		} `xml:"features"`
		CPU struct {
			Mode string `xml:"mode,attr"`
		} `xml:"cpu"`
		IOThreads *int `xml:"iothreads"`
		Clock     struct {
			Offset string `xml:"offset,attr"`
		} `xml:"clock"`
		Devices struct {
			Disks []struct {
				Device string `xml:"device,attr"`
				Driver struct {
					Cache string `xml:"cache,attr"`
				} `xml:"driver"`
				Target struct {
					Bus string `xml:"bus,attr"`
				} `xml:"target"`
			} `xml:"disk"`
			Interface struct {
				Model struct {
					Type string `xml:"type,attr"`
				} `xml:"model"`
			} `xml:"interface"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
		return nil, nil
	}

	if dx.Features.Hyperv == nil {
		return nil, nil // pre-profile domain
	}

	os := types.VirtualMachineOsTypeWindows
	cpuMode := types.PerformanceProfileCpuMode(dx.CPU.Mode)

	diskBus := types.Scsi
	var cache *types.PerformanceProfileCache
	ioThreads := 0
	for _, dsk := range dx.Devices.Disks {
		if dsk.Device != "disk" {
			continue
		}
		if dsk.Target.Bus == "sata" {
			diskBus = types.Sata
		}
		if dsk.Driver.Cache != "" {
			c := types.PerformanceProfileCache(dsk.Driver.Cache)
			cache = &c
		}
	}
	if dx.IOThreads != nil {
		ioThreads = *dx.IOThreads
	}
	var nic *types.PerformanceProfileNicModel
	switch dx.Devices.Interface.Model.Type {
	case "virtio", "e1000e", "rtl8139":
		n := types.PerformanceProfileNicModel(dx.Devices.Interface.Model.Type)
		nic = &n
	}
	return &os, &types.PerformanceProfile{
		CpuMode:   &cpuMode,
		DiskBus:   &diskBus,
		Cache:     cache,
		NicModel:  nic,
		IoThreads: &ioThreads,
	}
}

// osTypeFromDomainXML derives the OS family of any domain (pre-profile
// domains included) from its XML, for the read model.
func osTypeFromDomainXML(xmlStr string) types.VirtualMachineOsType {
	var dx struct {
		Features struct {
			Hyperv *struct{} `xml:"hyperv"`
		} `xml:"features"`
	}
	if err := xml.Unmarshal([]byte(xmlStr), &dx); err != nil {
		return ""
	}
	if dx.Features.Hyperv != nil {
		return types.VirtualMachineOsTypeWindows
	}
	// linux profile is the default; "other" cannot be distinguished from
	// pre-profile linux domains, so report linux.
	return types.VirtualMachineOsTypeLinux
}

// qemuVersionHint is used by tests to build hostFeatures quickly.
func qemuVersionHint(major, minor, micro uint32) uint32 { return ver(major, minor, micro) }
