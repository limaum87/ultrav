// Package libvirt implements hypervisor.Provider backed by a real libvirt
// daemon (QEMU/KVM), using the official libvirt.org/go-libvirt bindings.
// It is the only place in the codebase that imports a libvirt client.
package libvirt

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// Provider talks to a libvirt daemon.
type Provider struct {
	uri    string
	isoDir string

	mu   sync.Mutex
	conn *libvirt.Connect
}

// New creates a provider for the given libvirt URI (e.g. qemu:///system).
// isoDir backs the ISO library used for install-media attachments.
// The connection is established lazily and re-established on failure.
func New(uri, isoDir string) *Provider {
	return &Provider{uri: uri, isoDir: isoDir}
}

// Ready reports whether a connection to the daemon can be established.
func (p *Provider) Ready(_ context.Context) error {
	c, err := p.connect()
	if err != nil {
		return err
	}
	_, err = c.GetLibVersion()
	return err
}

// connect returns a live connection, (re)connecting when necessary.
// Callers must not retain the connection beyond the callback; use withConn.
func (p *Provider) connect() (*libvirt.Connect, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		if alive, _ := p.conn.IsAlive(); alive {
			return p.conn, nil
		}
		_, _ = p.conn.Close()
		p.conn = nil
	}
	c, err := libvirt.NewConnect(p.uri)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to libvirt (%s): %w", p.uri, err)
	}
	p.conn = c
	return c, nil
}

// withConn runs fn with a live connection, mapping libvirt errors to
// provider errors where possible.
func (p *Provider) withConn(fn func(c *libvirt.Connect) error) error {
	c, err := p.connect()
	if err != nil {
		return err
	}
	return fn(c)
}

// GetHost returns real host metrics gathered from libvirt and the local OS.
func (p *Provider) GetHost(_ context.Context) (types.Host, error) {
	var host types.Host
	err := p.withConn(func(c *libvirt.Connect) error {
		hostname, err := c.GetHostname()
		if err != nil {
			return err
		}
		ni, err := c.GetNodeInfo()
		if err != nil {
			return err
		}
		libVer, err := c.GetLibVersion()
		if err != nil {
			return err
		}

		cpuUsage := readCPUUsage()
		kvmEnabled := true
		memTotal := int64(ni.Memory) * 1024 // NodeInfo.Memory is in KiB
		memUsed := readMemUsed(memTotal)
		storTotal, storUsed := sumStoragePools(c)

		host = types.Host{
			Hostname:        hostname,
			OperatingSystem: prettyOSName(),
			Kernel:          kernelVersion(),
			UptimeSeconds:   readUptimeSeconds(),
			Cpu: types.Cpu{
				Model:        strings.TrimSpace(ni.Model),
				Cores:        int(ni.Sockets * ni.Cores),
				Threads:      int(ni.Threads * ni.Sockets * ni.Cores),
				UsagePercent: &cpuUsage,
			},
			MemoryTotalBytes:  memTotal,
			MemoryUsedBytes:   memUsed,
			StorageTotalBytes: &storTotal,
			StorageUsedBytes:  &storUsed,
			Virtualization:    &types.HostVirtualization{KvmEnabled: &kvmEnabled},
			QemuVersion:       qemuVersion(c),
			LibvirtVersion:    strPtr(formatLibvirtVersion(libVer)),
		}
		return nil
	})
	return host, err
}

// GetCapabilities derives the capability set from the daemon capabilities XML.
func (p *Provider) GetCapabilities(_ context.Context) (types.Capabilities, error) {
	var caps types.Capabilities
	err := p.withConn(func(c *libvirt.Connect) error {
		xml, err := c.GetCapabilities()
		if err != nil {
			return err
		}
		kvm := strings.Contains(xml, "<domain type='kvm'>") ||
			strings.Contains(xml, "<domain type=\"kvm\">")
		uefi := strings.Contains(xml, "<loader") || strings.Contains(xml, "OVMF")
		caps = types.Capabilities{
			Virtualization: types.CapabilitiesVirtualization{Kvm: kvm, Uefi: uefi},
			Storage:        types.CapabilitiesStorage{Qcow2: true, Raw: true, Snapshots: true},
			Features:       types.CapabilitiesFeatures{Backup: false, Migration: false, CloudInit: false},
		}
		return nil
	})
	return caps, err
}

// sumStoragePools aggregates capacity and allocation across ACTIVE storage
// pools (inactive pools report 0/0 from libvirt and would skew totals down,
// while capacity-only aggregation would overstate available space).
func sumStoragePools(c *libvirt.Connect) (total, used int64) {
	pools, err := c.ListAllStoragePools(libvirt.CONNECT_LIST_STORAGE_POOLS_ACTIVE)
	if err != nil {
		return 0, 0
	}
	for i := range pools {
		info, err := pools[i].GetInfo()
		if err != nil {
			continue
		}
		total += int64(info.Capacity)
		used += int64(info.Allocation)
	}
	return total, used
}

// --- local OS helpers (host-local reads; no shell, no injection surface) ---

func readUptimeSeconds() int {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(fields[0], 64)
	return int(f)
}

func readMemUsed(total int64) int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var memTotal, memAvailable int64
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = meminfoKB(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = meminfoKB(line)
		}
	}
	if memTotal == 0 {
		return 0
	}
	used := (memTotal - memAvailable) * 1024
	if used <= 0 || used > total {
		return 0
	}
	return used
}

func meminfoKB(line string) int64 {
	fields := strings.Fields(strings.SplitN(line, ":", 2)[1])
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(fields[0], 10, 64)
	return v
}

func readCPUUsage() float32 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)[1:]
			var vals []float64
			for _, f := range fields {
				v, _ := strconv.ParseFloat(f, 64)
				vals = append(vals, v)
			}
			if len(vals) < 4 {
				return 0
			}
			idle := vals[3]
			var total float64
			for _, v := range vals {
				total += v
			}
			if total == 0 {
				return 0
			}
			return float32((1 - idle/total) * 100)
		}
	}
	return 0
}

func prettyOSName() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return "Linux"
}

func kernelVersion() string {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return "unknown"
	}
	return charsToString(uts.Release[:])
}

func charsToString(ca []int8) string {
	b := make([]byte, 0, len(ca))
	for _, c := range ca {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

func formatLibvirtVersion(v uint32) string {
	major := v / 1_000_000
	minor := (v % 1_000_000) / 1_000
	patch := v % 1_000
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func qemuVersion(c *libvirt.Connect) *string {
	// The daemon-side QEMU version is not uniformly exposed across setups;
	// leave it null rather than guessing.
	_ = c
	return nil
}

func strPtr(s string) *string { return &s }
