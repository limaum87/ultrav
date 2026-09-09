import { useCallback, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api, unwrap, ApiError } from '../api/client';
import { formatBytes, formatUptime, usePolling } from '../lib/hooks';
import { KV, StateBadge, VmActions } from '../components/ui';

export default function VMDetails() {
  const { id = '' } = useParams();
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const { data: vm, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/vms/{id}', { params: { path: { id } } })),
  );

  const runAction = useCallback(
    async (action: 'start' | 'shutdown' | 'reboot' | 'stop') => {
      setBusy(true);
      setActionError(null);
      try {
        await unwrap(api.POST(`/vms/{id}/${action}`, { params: { path: { id } } }));
        await refresh();
      } catch (e) {
        setActionError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
      } finally {
        setBusy(false);
      }
    },
    [id, refresh],
  );

  if (loading) return <div className="page loading">Loading…</div>;
  if (error || !vm)
    return (
      <div className="page error">
        Failed to load VM: {error}
        <div>
          <Link to="/vms">← Back to Virtual Machines</Link>
        </div>
      </div>
    );

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <Link to="/vms" className="back-link">
            ← Virtual Machines
          </Link>
          <h1 className="vm-title">
            {vm.name} <StateBadge state={vm.state} />
          </h1>
        </div>
        <VmActions vm={vm} busy={busy} onAction={(a) => void runAction(a)} size="md" />
      </header>

      {actionError && <div className="alert error">{actionError}</div>}

      <section className="grid-2">
        <div className="card">
          <h2 className="card-title">Overview</h2>
          <KV k="Status" v={<StateBadge state={vm.state} />} />
          <KV k="IP Address" v={vm.ipAddress ?? '—'} />
          <KV k="Uptime" v={formatUptime(vm.uptimeSeconds)} />
          <KV k="Guest OS" v={vm.os ?? '—'} />
          <KV k="Boot Time" v={vm.bootTime ? new Date(vm.bootTime).toLocaleString() : '—'} />
        </div>

        <div className="card">
          <h2 className="card-title">Hardware</h2>
          <KV k="vCPU" v={vm.vcpus} />
          <KV k="Memory" v={formatBytes(vm.memoryBytes, 0)} />
          <h3 className="card-subtitle">Disks</h3>
          {vm.disks.map((d) => (
            <KV
              key={d.name}
              k={`${d.name} (${d.bus ?? '—'})`}
              v={`${formatBytes(d.sizeBytes, 0)} · ${d.format}`}
            />
          ))}
          <h3 className="card-subtitle">Network Interfaces</h3>
          {vm.networkInterfaces.map((n) => (
            <KV
              key={n.name}
              k={`${n.name} (${n.model})`}
              v={n.ipAddress ? `${n.ipAddress} · ${n.macAddress ?? ''}` : (n.macAddress ?? '—')}
            />
          ))}
        </div>
      </section>
    </div>
  );
}
