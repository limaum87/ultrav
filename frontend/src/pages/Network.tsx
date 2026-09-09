import { useCallback, useState } from 'react';
import { api, unwrap, ApiError, type Network } from '../api/client';
import { usePolling } from '../lib/hooks';
import { ActionMenu, EmptyState, MetricCard, TableSkeleton, type MenuItem } from '../components/ui';
import { CreateNetworkModal } from '../components/CreateNetworkModal';
import { useToast } from '../components/Toast';

export default function NetworkPage() {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const { data, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/networks')),
  );

  const runAction = useCallback(
    async (net: Network, action: 'start' | 'stop') => {
      setBusy(net.id);
      try {
        await unwrap(api.POST(`/networks/{id}/${action}`, { params: { path: { id: net.id } } }));
        toast.push('success', `${net.name}: ${action} requested`);
        await refresh();
      } catch (e) {
        toast.push('error', e instanceof ApiError ? `${net.name}: ${e.code} — ${e.message}` : String(e));
      } finally {
        setBusy(null);
      }
    },
    [refresh, toast],
  );

  if (error) return <div className="page page-wide error alert">Failed to reach API: {error}</div>;

  const nets = data?.items ?? [];
  const active = nets.filter((n) => n.state === 'active').length;

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Network</h1>
          <p className="subtitle">{loading ? 'loading…' : `${data?.total ?? 0} virtual networks on this host`}</p>
        </div>
        <button className="btn btn-primary" onClick={() => setModalOpen(true)}>
          + Add Network
        </button>
      </header>

      <CreateNetworkModal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        onCreated={() => void refresh()}
      />

      <section className="metric-grid">
        <MetricCard label="Networks" value={data?.total ?? 0} loading={loading} />
        <MetricCard label="Active" value={active} tone="ok" loading={loading} />
        <MetricCard label="Autostart" value={nets.filter((n) => n.autostart).length} loading={loading} />
      </section>

      <div className="card table-card">
        {loading ? (
          <TableSkeleton rows={3} cols={7} />
        ) : nets.length === 0 ? (
          <EmptyState title="No virtual networks" message="Add a network to attach VMs to." />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Mode</th>
                <th>Status</th>
                <th>Bridge</th>
                <th>Gateway</th>
                <th>DHCP</th>
                <th>Autostart</th>
                <th className="th-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {nets.map((net) => {
                const menu: MenuItem[] = [
                  {
                    label: 'Start',
                    onSelect: () => void runAction(net, 'start'),
                    disabled: busy === net.id || net.state === 'active',
                  },
                  {
                    label: 'Stop',
                    onSelect: () => void runAction(net, 'stop'),
                    disabled: busy === net.id || net.state !== 'active',
                  },
                ];
                return (
                  <tr key={net.id} className={busy === net.id ? 'row-busy' : undefined}>
                    <td>
                      {net.name}
                      {net.domainName && <div className="net-domain">{net.domainName}</div>}
                    </td>
                    <td>
                      <span className={`mode-badge mode-${net.mode ?? 'nat'}`}>{net.mode ?? 'nat'}</span>
                    </td>
                    <td>
                      <span className={`state${net.state === 'active' ? ' state-running' : ''}`}>
                        <span className="state-dot" />
                        {net.state}
                      </span>
                    </td>
                    <td className="mono">{net.bridge ?? '—'}</td>
                    <td className="mono">
                      {net.ipAddress
                        ? `${net.ipAddress}/${net.ipPrefix ?? ''}`
                        : net.mode === 'bridge'
                          ? 'from LAN DHCP'
                          : '—'}
                    </td>
                    <td>{net.dhcpEnabled ? 'yes' : 'no'}</td>
                    <td>{net.autostart ? 'yes' : 'no'}</td>
                    <td className="td-actions">
                      <ActionMenu items={menu} label={`Actions for ${net.name}`} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
