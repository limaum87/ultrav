package libvirt

import (
	"time"

	libvirt "libvirt.org/go/libvirt"

	"github.com/ultrav/ultrav/backend/internal/api/types"
)

// vmSample is the previous raw reading for one domain. It is kept only so the
// next poll can turn libvirt's cumulative counters (CPU time, interface bytes)
// into rates; gauges are read fresh on every call.
type vmSample struct {
	at      time.Time
	cpuTime uint64 // cumulative CPU time (ns) across all vCPUs
	rx, tx  int64  // cumulative bytes across all interfaces
}

// sampleMetrics builds a utilization snapshot for a running domain. CPU% and
// network throughput are derived from the delta against the previous sample,
// so those fields stay null until a second sample exists. Memory usage is a
// live gauge and is reported whenever the guest exposes it.
func (p *Provider) sampleMetrics(dom *libvirt.Domain, dx *domainXML, name string, memTotal int64) *types.VmMetrics {
	info, err := dom.GetInfo()
	if err != nil {
		return nil
	}
	now := time.Now()

	cur := &vmSample{at: now, cpuTime: info.CpuTime}
	for _, n := range dx.NICs {
		if n.Target.Dev == "" {
			continue
		}
		st, err := dom.InterfaceStats(n.Target.Dev)
		if err != nil || st == nil {
			continue // interface disappeared / no tap device yet
		}
		cur.rx += st.RxBytes
		cur.tx += st.TxBytes
	}

	prev := p.swapSample(name, cur)

	m := &types.VmMetrics{
		SampledAt:       now,
		MemoryUsedBytes: memoryUsedBytes(dom, memTotal),
	}
	if prev == nil {
		return m
	}
	elapsed := now.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return m
	}

	if ncpus := uint(info.NrVirtCpu); ncpus > 0 && cur.cpuTime >= prev.cpuTime {
		deltaNs := float64(cur.cpuTime - prev.cpuTime)
		pct := float32(deltaNs / (elapsed * 1e9) * 100 / float64(ncpus))
		pct = clampPercent(pct)
		m.CpuPercent = &pct
	}

	rx := ratePerSecond(cur.rx, prev.rx, elapsed)
	tx := ratePerSecond(cur.tx, prev.tx, elapsed)
	m.NetworkRxBytesPerSecond = &rx
	m.NetworkTxBytesPerSecond = &tx
	return m
}

// memoryUsedBytes reports guest memory in use. libvirt exposes an explicit
// "unused" figure when the balloon driver is present; otherwise we fall back to
// the host-side RSS of the QEMU process. Returns nil when neither is available.
//
// virDomainMemoryStats reports its values in kibibytes, so they are converted to
// bytes before being combined with the byte-denominated allocation.
func memoryUsedBytes(dom *libvirt.Domain, memTotal int64) *int64 {
	stats, err := dom.MemoryStats(uint32(libvirt.DOMAIN_MEMORY_STAT_NR), 0)
	if err != nil {
		return nil
	}
	var unused, rss int64
	var haveUnused, haveRSS bool
	for _, s := range stats {
		switch libvirt.DomainMemoryStatTags(s.Tag) {
		case libvirt.DOMAIN_MEMORY_STAT_UNUSED:
			unused, haveUnused = int64(s.Val)*1024, true
		case libvirt.DOMAIN_MEMORY_STAT_RSS:
			rss, haveRSS = int64(s.Val)*1024, true
		}
	}
	if haveUnused && memTotal > 0 {
		used := memTotal - unused
		if used < 0 {
			used = 0
		}
		return &used
	}
	if haveRSS && rss > 0 {
		return &rss
	}
	return nil
}

// swapSample stores the current reading and returns the previous one (nil on
// the first call for this domain).
func (p *Provider) swapSample(name string, cur *vmSample) *vmSample {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	if p.samples == nil {
		p.samples = make(map[string]*vmSample)
	}
	prev := p.samples[name]
	p.samples[name] = cur
	return prev
}

// forgetSamples drops cached readings for domains that are no longer running,
// so the cache cannot grow without bound.
func (p *Provider) forgetSamples(running map[string]bool) {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	for name := range p.samples {
		if !running[name] {
			delete(p.samples, name)
		}
	}
}

// ratePerSecond converts two cumulative counters into a non-negative bytes/s
// rate, guarding against counter resets (which read as a negative delta).
func ratePerSecond(cur, prev int64, elapsed float64) int64 {
	delta := cur - prev
	if delta < 0 {
		return 0
	}
	return int64(float64(delta) / elapsed)
}

func clampPercent(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
