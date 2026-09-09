import { useCallback, useState } from 'react';
import { api, unwrap, ApiError, type StoragePool } from '../api/client';
import { formatBytes, usePolling } from '../lib/hooks';
import { ActionMenu, EmptyState, MetricCard, TableSkeleton, type MenuItem } from '../components/ui';
import { IsoLibrary } from '../components/IsoLibrary';
import { CreatePoolModal } from '../components/CreatePoolModal';
import { useToast } from '../components/Toast';

export default function Storage() {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const [poolModalOpen, setPoolModalOpen] = useState(false);
  const { data, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/storage/pools')),
  );

  const refreshPool = useCallback(
    async (pool: StoragePool) => {
      setBusy(pool.id);
      try {
        await unwrap(api.POST('/storage/pools/{id}/refresh', { params: { path: { id: pool.id } } }));
        toast.push('success', `${pool.name}: refresh requested`);
        await refresh();
      } catch (e) {
        toast.push('error', e instanceof ApiError ? `${pool.name}: ${e.code} — ${e.message}` : String(e));
      } finally {
        setBusy(null);
      }
    },
    [refresh, toast],
  );

  if (error) return <div className="page page-wide error alert">Failed to reach API: {error}</div>;

  const pools = data?.items ?? [];
  const active = pools.filter((p) => p.state === 'active').length;
  const totalCap = pools.reduce((s, p) => s + p.capacityBytes, 0);
  const totalAlloc = pools.reduce((s, p) => s + p.allocationBytes, 0);

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Storage</h1>
          <p className="subtitle">{loading ? 'loading…' : `${data?.total ?? 0} storage pools on this host`}</p>
        </div>
        <button className="btn btn-primary" onClick={() => setPoolModalOpen(true)}>
          + Add Pool
        </button>
      </header>

      <section className="metric-grid">
        <MetricCard label="Pools" value={data?.total ?? 0} hint={`${active} active`} loading={loading} />
        <MetricCard label="Capacity" value={formatBytes(totalCap, 1)} loading={loading} />
        <MetricCard
          label="Allocated"
          value={formatBytes(totalAlloc, 1)}
          used={totalCap ? (totalAlloc / totalCap) * 100 : null}
          loading={loading}
        />
      </section>

      <div className="card table-card">
        {loading ? (
          <TableSkeleton rows={3} cols={6} />
        ) : pools.length === 0 ? (
          <EmptyState title="No storage pools" message="Add a pool to store VM disk images." />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Status</th>
                <th>Capacity</th>
                <th>Allocated</th>
                <th>Path</th>
                <th className="th-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {pools.map((pool) => {
                const pct = pool.capacityBytes ? (pool.allocationBytes / pool.capacityBytes) * 100 : 0;
                const menu: MenuItem[] = [
                  {
                    label: 'Refresh',
                    onSelect: () => void refreshPool(pool),
                    disabled: busy === pool.id || pool.state !== 'active',
                    title: 'Re-sync pool usage with the backing storage',
                  },
                ];
                return (
                  <tr key={pool.id} className={busy === pool.id ? 'row-busy' : undefined}>
                    <td className="vm-link">{pool.name}</td>
                    <td className="cell-dim">{pool.type}</td>
                    <td>
                      <span className={`state${pool.state === 'active' ? ' state-running' : ''}`}>
                        <span className="state-dot" />
                        {pool.state}
                      </span>
                    </td>
                    <td>{formatBytes(pool.capacityBytes, 0)}</td>
                    <td>
                      <div className="cell-progress" title={`${pool.name} allocation`}>
                        <span className="cell-progress-pct">{Math.round(pct)}%</span>
                        <div className="metric-bar">
                          <div
                            className={`metric-bar-fill${pct > 85 ? ' high' : ''}`}
                            style={{ width: `${Math.min(100, pct)}%` }}
                          />
                        </div>
                      </div>
                      <div className="cell-sub">{formatBytes(pool.allocationBytes, 0)}</div>
                    </td>
                    <td className="mono">{pool.targetPath ?? '—'}</td>
                    <td className="td-actions">
                      <ActionMenu items={menu} label={`Actions for ${pool.name}`} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      <IsoLibrary />

      <CreatePoolModal
        open={poolModalOpen}
        onClose={() => setPoolModalOpen(false)}
        onCreated={() => void refresh()}
      />
    </div>
  );
}
