package libvirt

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// parsedDomain is the full subset of the domain XML the tests assert on.
type parsedDomain struct {
	XMLName xml.Name `xml:"domain"`
	Name    string   `xml:"name"`
	Memory  struct {
		Value int64  `xml:",chardata"`
		Unit  string `xml:"unit,attr"`
	} `xml:"memory"`
	VCPU      int      `xml:"vcpu"`
	IOThreads *int     `xml:"iothreads"`
	CPU       *struct {
		Mode       string `xml:"mode,attr"`
		Check      string `xml:"check,attr"`
		Migratable string `xml:"migratable,attr"`
	} `xml:"cpu"`
	Features struct {
		ACPI   *struct{} `xml:"acpi"`
		APIC   *struct{} `xml:"apic"`
		Hyperv *struct {
			Mode        string `xml:"mode,attr"`
			Relaxed     *struct{} `xml:"relaxed"`
			Vapic       *struct{} `xml:"vapic"`
			Spinlocks   *struct {
				State   string `xml:"state,attr"`
				Retries int    `xml:"retries,attr"`
			} `xml:"spinlocks"`
			Vpindex     *struct{} `xml:"vpindex"`
			Synic       *struct{} `xml:"synic"`
			Stimer      *struct{} `xml:"stimer"`
			Runtime     *struct{} `xml:"runtime"`
			Frequencies *struct{} `xml:"frequencies"`
			Reset       *struct{} `xml:"reset"`
			Tlbflush    *struct{} `xml:"tlbflush"`
			Ipi         *struct{} `xml:"ipi"`
		} `xml:"hyperv"`
	} `xml:"features"`
	Clock struct {
		Offset string `xml:"offset,attr"`
		Timers []struct {
			Name       string `xml:"name,attr"`
			Tickpolicy string `xml:"tickpolicy,attr"`
			Present    string `xml:"present,attr"`
		} `xml:"timer"`
	} `xml:"clock"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Driver struct {
				Name         string `xml:"name,attr"`
				Type         string `xml:"type,attr"`
				Cache        string `xml:"cache,attr"`
				IO           string `xml:"io,attr"`
				Discard      string `xml:"discard,attr"`
				DetectZeroes string `xml:"detect_zeroes,attr"`
			} `xml:"driver"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
			Target struct {
				Dev string `xml:"dev,attr"`
				Bus string `xml:"bus,attr"`
			} `xml:"target"`
			Readonly *struct{} `xml:"readonly"`
			Boot     *struct {
				Order int `xml:"order,attr"`
			} `xml:"boot"`
		} `xml:"disk"`
		Controller *struct {
			Type   string `xml:"type,attr"`
			Model  string `xml:"model,attr"`
			Driver *struct {
				Queues   int `xml:"queues,attr"`
				IOThread int `xml:"iothread,attr"`
			} `xml:"driver"`
		} `xml:"controller"`
		Interface struct {
			Type   string `xml:"type,attr"`
			Source struct {
				Network string `xml:"network,attr"`
			} `xml:"source"`
			Model struct {
				Type string `xml:"type,attr"`
			} `xml:"model"`
			Driver *struct {
				Name   string `xml:"name,attr"`
				Queues int    `xml:"queues,attr"`
			} `xml:"driver"`
		} `xml:"interface"`
		Channel struct {
			Type   string `xml:"type,attr"`
			Target struct {
				Type string `xml:"type,attr"`
				Name string `xml:"name,attr"`
			} `xml:"target"`
		} `xml:"channel"`
		RNG *struct {
			Model   string `xml:"model,attr"`
			Backend struct {
				Model string `xml:"model,attr"`
				Text  string `xml:",chardata"`
			} `xml:"backend"`
		} `xml:"rng"`
		Memballoon struct {
			Model string `xml:"model,attr"`
		} `xml:"memballoon"`
		Graphics struct {
			Type   string `xml:"type,attr"`
			Listen struct {
				Type string `xml:"type,attr"`
			} `xml:"listen"`
		} `xml:"graphics"`
		Video struct {
			Model struct {
				Type string `xml:"type,attr"`
			} `xml:"model"`
		} `xml:"video"`
		Console *struct{} `xml:"console"`
	} `xml:"devices"`
}

func build(t *testing.T, spec domainSpec, caps hostFeatures) (parsedDomain, *appliedProfile) {
	t.Helper()
	out, profile, err := buildDomainXML(spec, caps)
	if err != nil {
		t.Fatalf("buildDomainXML: %v", err)
	}
	var d parsedDomain
	if err := xml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("round-trip parse: %v\nXML:\n%s", err, out)
	}
	return d, profile
}

func baseSpec(osType types.VirtualMachineCreateOsType) domainSpec {
	return domainSpec{
		Name:        "app01",
		MemoryBytes: 4 << 30,
		Vcpus:       4,
		OSType:      osType,
		DiskPath:    "/var/lib/libvirt/images/app01.qcow2",
		DiskFormat:  "qcow2",
		NetworkId:   "default",
		ISOPath:     "/var/lib/libvirt/images/isos/ubuntu-24.04.iso",
	}
}

func modernCaps() hostFeatures {
	return hostFeatures{QEMUVersion: qemuVersionHint(8, 0, 0), LibvirtVersion: qemuVersionHint(9, 0, 0)}
}

func TestBuildDomainXML_CommonProfile(t *testing.T) {
	d, profile := build(t, baseSpec(types.VirtualMachineCreateOsTypeLinux), modernCaps())

	if d.Features.ACPI == nil || d.Features.APIC == nil {
		t.Error("acpi/apic must be present")
	}
	if d.CPU == nil || d.CPU.Mode != "host-passthrough" || d.CPU.Check != "none" || d.CPU.Migratable != "on" {
		t.Errorf("cpu = %+v, want host-passthrough check=none migratable=on", d.CPU)
	}
	if d.IOThreads == nil || *d.IOThreads != 1 {
		t.Errorf("iothreads = %v, want 1", d.IOThreads)
	}
	c := d.Devices.Controller
	if c == nil || c.Type != "scsi" || c.Model != "virtio-scsi" || c.Driver == nil ||
		c.Driver.Queues != 4 || c.Driver.IOThread != 1 {
		t.Errorf("scsi controller = %+v, want virtio-scsi queues=4 iothread=1", c)
	}

	var disk *struct {
		Cache        string
		IO           string
		Discard      string
		DetectZeroes string
		Bus          string
		Dev          string
		Fmt          string
		Boot         int
	}
	for _, dk := range d.Devices.Disks {
		if dk.Device != "disk" {
			continue
		}
		disk = &struct {
			Cache        string
			IO           string
			Discard      string
			DetectZeroes string
			Bus          string
			Dev          string
			Fmt          string
			Boot         int
		}{dk.Driver.Cache, dk.Driver.IO, dk.Driver.Discard, dk.Driver.DetectZeroes,
			dk.Target.Bus, dk.Target.Dev, dk.Driver.Type, dk.Boot.Order}
	}
	if disk == nil {
		t.Fatal("system disk not found")
	}
	if disk.Cache != "none" || disk.IO != "native" || disk.Discard != "unmap" || disk.DetectZeroes != "unmap" {
		t.Errorf("disk driver opts = %+v, want cache=none io=native discard=unmap detect_zeroes=unmap", disk)
	}
	if disk.Bus != "scsi" || disk.Fmt != "qcow2" {
		t.Errorf("disk target = %s/%s, want scsi/qcow2", disk.Bus, disk.Fmt)
	}
	if disk.Boot != 2 {
		t.Errorf("disk boot order = %d, want 2 (CD-ROM first)", disk.Boot)
	}

	nic := d.Devices.Interface
	if nic.Model.Type != "virtio" || nic.Driver == nil || nic.Driver.Name != "vhost" || nic.Driver.Queues != 4 {
		t.Errorf("NIC = %+v, want virtio vhost queues=4", nic)
	}
	if d.Devices.Channel.Target.Name != "org.qemu.guest_agent.0" || d.Devices.Channel.Target.Type != "virtio" {
		t.Errorf("guest agent channel = %+v", d.Devices.Channel)
	}
	if d.Devices.Memballoon.Model != "none" {
		t.Errorf("memballoon = %s, want none", d.Devices.Memballoon.Model)
	}
	if d.Devices.RNG == nil || d.Devices.RNG.Backend.Model != "random" || !strings.Contains(d.Devices.RNG.Backend.Text, "/dev/urandom") {
		t.Errorf("rng = %+v", d.Devices.RNG)
	}
	if d.Devices.Graphics.Type != "vnc" || d.Devices.Graphics.Listen.Type != "socket" {
		t.Errorf("graphics = %+v, want vnc on socket", d.Devices.Graphics)
	}
	if d.Devices.Console == nil {
		t.Error("console missing")
	}

	// timers
	wantTimers := map[string][2]string{
		"rtc":         {"catchup", ""},
		"pit":         {"delay", ""},
		"hpet":        {"", "no"},
		"hypervclock": {"", ""},
	}
	for _, tm := range d.Clock.Timers {
		want, ok := wantTimers[tm.Name]
		if !ok {
			t.Errorf("unexpected timer %s", tm.Name)
			continue
		}
		delete(wantTimers, tm.Name)
		if want[0] != "" && tm.Tickpolicy != want[0] {
			t.Errorf("timer %s tickpolicy = %s, want %s", tm.Name, tm.Tickpolicy, want[0])
		}
		if want[1] != "" && tm.Present != want[1] {
			t.Errorf("timer %s present = %s, want %s", tm.Name, tm.Present, want[1])
		}
	}
	if _, ok := wantTimers["hypervclock"]; ok {
		// hypervclock only in the windows profile; absence is expected for linux
	} 
	for name := range wantTimers {
		if name != "hypervclock" {
			t.Errorf("missing timer %s", name)
		}
	}

	if profile == nil || profile.CpuMode != "host-passthrough" || profile.DiskBus != "scsi" ||
		profile.Cache != "none" || profile.IOThreads != 1 {
		t.Errorf("appliedProfile = %+v", profile)
	}
	if profile.OSType != types.VirtualMachineCreateOsTypeLinux {
		t.Errorf("profile osType = %s", profile.OSType)
	}
}

func TestBuildDomainXML_WindowsProfile_FullCapabilities(t *testing.T) {
	spec := baseSpec(types.VirtualMachineCreateOsTypeWindows)
	spec.VirtioISOPath = "/var/lib/libvirt/images/isos/virtio-win-0.1.266.iso"
	d, profile := build(t, spec, modernCaps())

	if d.Clock.Offset != "localtime" {
		t.Errorf("clock offset = %s, want localtime", d.Clock.Offset)
	}
	found := false
	for _, tm := range d.Clock.Timers {
		if tm.Name == "hypervclock" && tm.Present == "yes" {
			found = true
		}
	}
	if !found {
		t.Error("hypervclock timer (present=yes) missing")
	}

	hv := d.Features.Hyperv
	if hv == nil || hv.Mode != "custom" {
		t.Fatalf("hyperv = %+v, want mode=custom", hv)
	}
	for name, present := range map[string]bool{
		"relaxed": hv.Relaxed != nil, "vapic": hv.Vapic != nil,
		"vpindex": hv.Vpindex != nil, "synic": hv.Synic != nil,
		"stimer": hv.Stimer != nil, "runtime": hv.Runtime != nil,
		"frequencies": hv.Frequencies != nil, "reset": hv.Reset != nil,
		"tlbflush": hv.Tlbflush != nil, "ipi": hv.Ipi != nil,
	} {
		if !present {
			t.Errorf("enlightenment %s missing on a fully capable host", name)
		}
	}
	if hv.Spinlocks == nil || hv.Spinlocks.State != "on" || hv.Spinlocks.Retries != 8191 {
		t.Errorf("spinlocks = %+v, want on retries=8191", hv.Spinlocks)
	}

	// Two CD-ROMs: install media (boot 1) then VirtIO drivers; system disk
	// must get a distinct target dev.
	cdroms := 0
	devs := map[string]bool{}
	for _, dk := range d.Devices.Disks {
		if devs[dk.Target.Dev] {
			t.Errorf("duplicate target dev %s", dk.Target.Dev)
		}
		devs[dk.Target.Dev] = true
		if dk.Device != "cdrom" {
			continue
		}
		cdroms++
		if dk.Target.Bus != "sata" {
			t.Errorf("cdrom bus = %s, want sata", dk.Target.Bus)
		}
		if strings.HasSuffix(dk.Source.File, "ubuntu-24.04.iso") && (dk.Boot == nil || dk.Boot.Order != 1) {
			t.Errorf("install ISO boot = %+v, want order 1", dk.Boot)
		}
	}
	if cdroms != 2 {
		t.Errorf("cdroms = %d, want 2 (install + virtio drivers)", cdroms)
	}

	if profile == nil || len(profile.HypervEnlightenments) != 11 {
		t.Errorf("profile enlightenments = %v, want all 11", profile.HypervEnlightenments)
	}
	if len(profile.SkippedEnlightenments) != 0 {
		t.Errorf("skipped = %v, want none on a fully capable host", profile.SkippedEnlightenments)
	}
}

func TestBuildDomainXML_WindowsProfile_NoDriversISO(t *testing.T) {
	d, _ := build(t, baseSpec(types.VirtualMachineCreateOsTypeWindows), modernCaps())

	cdroms := 0
	for _, dk := range d.Devices.Disks {
		if dk.Device == "cdrom" {
			cdroms++
		}
	}
	if cdroms != 1 {
		t.Errorf("cdroms = %d, want 1 (install media only)", cdroms)
	}
}

func TestBuildDomainXML_WindowsProfile_OldQEMU(t *testing.T) {
	// Simulates the QEMU 4.2 test host: no frequencies/reset/tlbflush/ipi.
	caps := hostFeatures{QEMUVersion: qemuVersionHint(4, 2, 0), LibvirtVersion: qemuVersionHint(5, 5, 0)}
	d, profile := build(t, baseSpec(types.VirtualMachineCreateOsTypeWindows), caps)

	hv := d.Features.Hyperv
	if hv == nil {
		t.Fatal("hyperv block missing")
	}
	if hv.Relaxed == nil || hv.Vapic == nil || hv.Spinlocks == nil || hv.Vpindex == nil ||
		hv.Synic == nil || hv.Stimer == nil || hv.Runtime == nil {
		t.Error("base enlightenments must be present on QEMU 4.2")
	}
	if hv.Frequencies != nil || hv.Reset != nil || hv.Tlbflush != nil || hv.Ipi != nil {
		t.Error("frequencies/reset/tlbflush/ipi must be omitted on QEMU 4.2")
	}

	got := map[string]bool{}
	for _, e := range profile.HypervEnlightenments {
		got[e] = true
	}
	for _, e := range []string{"frequencies", "reset", "tlbflush", "ipi"} {
		if got[e] {
			t.Errorf("profile reports %s as applied, but the host does not support it", e)
		}
	}
	skipped := map[string]bool{}
	for _, e := range profile.SkippedEnlightenments {
		skipped[e] = true
	}
	for _, e := range []string{"frequencies", "reset", "tlbflush", "ipi"} {
		if !skipped[e] {
			t.Errorf("expected %s in skipped enlightenments, got %v", e, profile.SkippedEnlightenments)
		}
	}
}

func TestBuildDomainXML_OtherProfile(t *testing.T) {
	d, profile := build(t, baseSpec(types.VirtualMachineCreateOsTypeOther), modernCaps())

	if d.CPU == nil || d.CPU.Mode != "host-model" {
		t.Errorf("cpu mode = %+v, want host-model", d.CPU)
	}
	if d.IOThreads != nil {
		t.Errorf("iothreads = %v, want none for the compatibility profile", d.IOThreads)
	}
	if d.Devices.Controller != nil {
		t.Errorf("scsi controller = %+v, want none (SATA disk)", d.Devices.Controller)
	}
	var sysDisk *struct {
		Bus   string
		Cache string
	}
	for i, dk := range d.Devices.Disks {
		if dk.Device == "disk" {
			sysDisk = &struct{ Bus, Cache string }{dk.Target.Bus, dk.Driver.Cache}
			_ = i
		}
	}
	if sysDisk == nil || sysDisk.Bus != "sata" {
		t.Errorf("system disk = %+v, want sata", sysDisk)
	}
	if sysDisk != nil && sysDisk.Cache != "" {
		t.Errorf("disk cache = %s, want host default for the compatibility profile", sysDisk.Cache)
	}
	if d.Devices.Interface.Model.Type != "e1000e" {
		t.Errorf("NIC model = %s, want e1000e", d.Devices.Interface.Model.Type)
	}
	if d.Devices.Interface.Driver != nil {
		t.Errorf("NIC driver = %+v, want none (no vhost queues on e1000e)", d.Devices.Interface.Driver)
	}
	if d.Features.Hyperv != nil {
		t.Error("hyperv features must be absent for the other profile")
	}
	if profile == nil || profile.CpuMode != "host-model" || profile.DiskBus != "sata" || profile.IOThreads != 0 {
		t.Errorf("appliedProfile = %+v", profile)
	}
}

func TestBuildDomainXML_QueueCap(t *testing.T) {
	spec := baseSpec(types.VirtualMachineCreateOsTypeLinux)
	spec.Vcpus = 64
	d, _ := build(t, spec, modernCaps())
	if d.Devices.Controller.Driver.Queues != 8 {
		t.Errorf("queues = %d, want capped at 8", d.Devices.Controller.Driver.Queues)
	}
}

func TestBuildDomainXML_Validation(t *testing.T) {
	cases := []struct {
		name string
		spec domainSpec
	}{
		{"no name", domainSpec{Vcpus: 1, MemoryBytes: 1, DiskPath: "x", DiskFormat: "qcow2", NetworkId: "n"}},
		{"no vcpus", domainSpec{Name: "a", MemoryBytes: 1, DiskPath: "x", DiskFormat: "qcow2", NetworkId: "n"}},
		{"no disk", domainSpec{Name: "a", Vcpus: 1, MemoryBytes: 1, NetworkId: "n"}},
		{"no network", domainSpec{Name: "a", Vcpus: 1, MemoryBytes: 1, DiskPath: "x", DiskFormat: "qcow2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := buildDomainXML(tc.spec, hostFeatures{}); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestBuildDomainXML_XMLEscaping(t *testing.T) {
	// Hostile values must not break out of their attributes/elements.
	spec := baseSpec(types.VirtualMachineCreateOsTypeLinux)
	spec.Name = `x"><script alert(1)</script`
	spec.NetworkId = `default</source><interface type='bridge'/>`
	spec.DiskPath = `/tmp/a"b'c<d>.qcow2`
	out, _, err := buildDomainXML(spec, hostFeatures{})
	if err != nil {
		t.Fatal(err)
	}
	var d parsedDomain
	if err := xml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("hostile values broke the XML: %v", err)
	}
	if d.Name != spec.Name {
		t.Errorf("name round-trip = %q, want %q", d.Name, spec.Name)
	}
	if d.Devices.Interface.Source.Network != spec.NetworkId {
		t.Errorf("network round-trip = %q, want %q", d.Devices.Interface.Source.Network, spec.NetworkId)
	}
	var diskSrc string
	for _, dk := range d.Devices.Disks {
		if dk.Device == "disk" {
			diskSrc = dk.Source.File
		}
	}
	if diskSrc != spec.DiskPath {
		t.Errorf("disk path round-trip = %q, want %q", diskSrc, spec.DiskPath)
	}
}

func TestSelectHypervFeatures(t *testing.T) {
	all, none := selectHypervFeatures(hostFeatures{QEMUVersion: qemuVersionHint(9, 0, 0), LibvirtVersion: qemuVersionHint(9, 0, 0)})
	if len(all) != 11 || len(none) != 0 {
		t.Errorf("modern host: supported=%v skipped=%v", all, none)
	}
	base, skipped := selectHypervFeatures(hostFeatures{QEMUVersion: qemuVersionHint(4, 2, 0), LibvirtVersion: qemuVersionHint(5, 5, 0)})
	if len(skipped) != 4 {
		t.Errorf("QEMU 4.2: skipped = %v, want 4", skipped)
	}
	if len(base) != 7 {
		t.Errorf("QEMU 4.2: supported = %v, want 7", base)
	}
}

func TestDetectProfileFromDomainXML(t *testing.T) {
	// windows domain → windows + profile
	osType, profile := detectProfileFromDomainXML(`<domain><features><hyperv mode='custom'><relaxed state='on'/></hyperv></features><cpu mode='host-passthrough'/><iothreads>1</iothreads><devices><disk device='disk'><driver cache='none'/><target bus='scsi'/></disk></devices></domain>`)
	if osType == nil || *osType != types.VirtualMachineOsTypeWindows {
		t.Errorf("osType = %v, want windows", osType)
	}
	if profile == nil || profile.CpuMode == nil || *profile.CpuMode != types.HostPassthrough ||
		profile.DiskBus == nil || *profile.DiskBus != types.Scsi {
		t.Errorf("profile = %+v", profile)
	}

	// pre-profile domain (virtio-blk, no hyperv) → nils, no regression
	osType, profile = detectProfileFromDomainXML(`<domain><features><acpi/></features><devices><disk device='disk'><driver type='qcow2'/><target dev='vda' bus='virtio'/></disk></devices></domain>`)
	if osType != nil || profile != nil {
		t.Errorf("pre-profile domain: got %v / %+v, want nil/nil", osType, profile)
	}
}

func TestOsTypeFromDomainXML(t *testing.T) {
	if got := osTypeFromDomainXML(`<domain><features><hyperv/></features></domain>`); got != types.VirtualMachineOsTypeWindows {
		t.Errorf("got %q, want windows", got)
	}
	if got := osTypeFromDomainXML(`<domain><features><acpi/></features></domain>`); got != types.VirtualMachineOsTypeLinux {
		t.Errorf("got %q, want linux", got)
	}
}
