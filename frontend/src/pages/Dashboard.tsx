import { api, unwrap, type Host, type VirtualMachine } from '../api/client';
import { formatBytes, formatUptime, usePolling } from '../lib/hooks';
import { EmptyState, MetricCard, StateBadge, TableSkeleton } from '../components/ui';
import { Cpu, MemoryStick, HardDrive, Monitor } from 'lucide-react';

export default function Dashboard() {
  const { data, error, loading } = usePolling(async () => {
    const host = await unwrap(api.GET('/host'));
    const vms = await unwrap(api.GET('/vms'));
    return { host, vms } satisfies { host: Host; vms: { items: VirtualMachine[]; total: number } };
  });

  if (error) return <div className="page page-wide error alert">Failed to reach API: {error}</div>;

  const host = data?.host;
  const vms = data?.vms;
  const running = vms ? vms.items.filter((v) => v.state === 'running').length : 0;
  const cpuPct = host?.cpu.usagePercent ?? 0;
  const memPct = host ? (host.memoryUsedBytes / host.memoryTotalBytes) * 100 : 0;
  const storPct =
    host?.storageTotalBytes && host.storageUsedBytes != null
      ? (host.storageUsedBytes / host.storageTotalBytes) * 100
      : null;

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Dashboard</h1>
          <p className="subtitle">
            {host ? (
              <>{host.hostname} · {host.operatingSystem} · kernel {host.kernel}</>
            ) : (
              'connecting…'
            )}
          </p>
        </div>
        {host && <div className="uptime-chip">up {formatUptime(host.uptimeSeconds)}</div>}
      </header>

      <section className="metric-grid">
        <MetricCard
          label="CPU Usage"
          value={`${cpuPct.toFixed(1)}%`}
          used={cpuPct}
          hint={host ? `${host.cpu.model} · ${host.cpu.threads} threads` : undefined}
          loading={loading}
          icon={<Cpu size={24} className="ic ic-electric" strokeWidth={1.75} aria-hidden />}
          barTone="blue"
        />
        <MetricCard
          label="Memory Usage"
          value={host ? `${formatBytes(host.memoryUsedBytes, 1)} / ${formatBytes(host.memoryTotalBytes, 0)}` : '—'}
          used={host ? memPct : null}
          hint={host ? `${Math.round(memPct)}% used` : undefined}
          loading={loading}
          icon={<MemoryStick size={24} className="ic ic-violet" strokeWidth={1.75} aria-hidden />}
          barTone="violet"
        />
        <MetricCard
          label="Storage Usage"
          value={
            host
              ? `${formatBytes(host.storageUsedBytes ?? 0, 1)} / ${formatBytes(host.storageTotalBytes ?? 0, 1)}`
              : '—'
          }
          used={storPct}
          hint={storPct != null ? `${Math.round(storPct)}% used` : undefined}
          loading={loading}
          icon={<HardDrive size={24} className="ic ic-purple" strokeWidth={1.75} aria-hidden />}
          barTone="purple"
        />
        <MetricCard
          label="Virtual Machines"
          value={vms?.total ?? 0}
          hint={vms ? `${running} running · ${vms.total - running} stopped` : undefined}
          loading={loading}
          icon={<Monitor size={24} className="ic ic-blue" strokeWidth={1.75} aria-hidden />}
        />
      </section>

      <div className="card table-card">
        <div className="card-title card-title-pad">Virtual Machines</div>
        {loading ? (
          <TableSkeleton rows={4} cols={5} />
        ) : !vms || vms.items.length === 0 ? (
          <EmptyState title="No virtual machines" message="Create your first VM from the Virtual Machines page." />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Status</th>
                <th>vCPU</th>
                <th>Memory</th>
                <th>IP Address</th>
                <th>Uptime</th>
              </tr>
            </thead>
            <tbody>
              {vms.items.map((vm) => (
                <tr key={vm.id}>
                  <td>
                    <a className="vm-link" href={`/vms/${vm.id}`}>
                      {vm.name}
                    </a>
                  </td>
                  <td><StateBadge state={vm.state} /></td>
                  <td>{vm.vcpus}</td>
                  <td>{formatBytes(vm.memoryBytes, 0)}</td>
                  <td className="mono">{vm.ipAddress ?? '—'}</td>
                  <td>{vm.state === 'running' ? formatUptime(vm.uptimeSeconds) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
