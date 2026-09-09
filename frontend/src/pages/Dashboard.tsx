import { api, unwrap, type Host, type VirtualMachine } from '../api/client';
import { formatBytes, formatUptime, usePolling } from '../lib/hooks';
import { Gauge, StateBadge } from '../components/ui';

export default function Dashboard() {
  const { data, error, loading } = usePolling(async () => {
    const host = await unwrap(api.GET('/host'));
    const vms = await unwrap(api.GET('/vms'));
    return { host, vms } satisfies { host: Host; vms: { items: VirtualMachine[]; total: number } };
  });

  if (loading) return <div className="page loading">Loading…</div>;
  if (error || !data) return <div className="page error">Failed to reach API: {error}</div>;

  const { host, vms } = data;
  const running = vms.items.filter((v) => v.state === 'running').length;
  const stopped = vms.total - running;
  const cpuPct = host.cpu.usagePercent ?? 0;
  const memPct = (host.memoryUsedBytes / host.memoryTotalBytes) * 100;
  const storPct = host.storageTotalBytes
    ? ((host.storageUsedBytes ?? 0) / host.storageTotalBytes) * 100
    : 0;

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Dashboard</h1>
          <p className="subtitle">
            {host.hostname} · {host.operatingSystem} · kernel {host.kernel}
          </p>
        </div>
        <div className="uptime-chip">up {formatUptime(host.uptimeSeconds)}</div>
      </header>

      <section className="grid-3">
        <div className="card">
          <Gauge
            label="CPU"
            value={`${cpuPct.toFixed(1)}%`}
            max={`${host.cpu.threads} threads`}
            used={cpuPct}
          />
          <div className="card-sub">{host.cpu.model}</div>
        </div>
        <div className="card">
          <Gauge
            label="Memory"
            value={formatBytes(host.memoryUsedBytes)}
            max={formatBytes(host.memoryTotalBytes)}
            used={memPct}
          />
        </div>
        <div className="card">
          <Gauge
            label="Storage"
            value={formatBytes(host.storageUsedBytes ?? 0)}
            max={formatBytes(host.storageTotalBytes ?? 0)}
            used={storPct}
          />
        </div>
      </section>

      <section className="card vms-summary">
        <div className="summary-row">
          <div className="summary-block">
            <span className="summary-num">{vms.total}</span>
            <span className="summary-label">Virtual Machines</span>
          </div>
          <div className="summary-block">
            <span className="summary-num ok">{running}</span>
            <span className="summary-label">Running</span>
          </div>
          <div className="summary-block">
            <span className="summary-num">{stopped}</span>
            <span className="summary-label">Stopped</span>
          </div>
          <div className="summary-vms">
            {vms.items.map((vm) => (
              <div key={vm.id} className="summary-vm">
                <StateBadge state={vm.state} />
                <span className="summary-vm-name">{vm.name}</span>
              </div>
            ))}
          </div>
        </div>
      </section>
    </div>
  );
}
