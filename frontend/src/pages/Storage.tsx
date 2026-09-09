import { useCallback, useState } from 'react';
import { api, unwrap, ApiError, type StoragePool } from '../api/client';
import { formatBytes, usePolling } from '../lib/hooks';
import { IsoLibrary } from '../components/IsoLibrary';
import { CreatePoolModal } from '../components/CreatePoolModal';

export default function Storage() {
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [poolModalOpen, setPoolModalOpen] = useState(false);
  const { data, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/storage/pools')),
  );

  const refreshPool = useCallback(
    async (pool: StoragePool) => {
      setBusy(pool.id);
      setActionError(null);
      try {
        await unwrap(api.POST('/storage/pools/{id}/refresh', { params: { path: { id: pool.id } } }));
        await refresh();
      } catch (e) {
        setActionError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
      } finally {
        setBusy(null);
      }
    },
    [refresh],
  );

  if (loading) return <div className="page loading">Loading…</div>;
  if (error || !data) return <div className="page error">Failed to reach API: {error}</div>;

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Storage</h1>
          <p className="subtitle">{data.total} storage pools on this host</p>
        </div>
        <button className="btn btn-primary" onClick={() => setPoolModalOpen(true)}>
          + Add Pool
        </button>
      </header>

      {actionError && <div className="alert error">{actionError}</div>}

      <section className="grid-2" style={{ marginBottom: 16 }}>
        {data.items
          .filter((p) => p.state === 'active' && p.capacityBytes > 0)
          .map((pool) => {
            const pct = (pool.allocationBytes / pool.capacityBytes) * 100;
            return (
              <div className="card" key={pool.id}>
                <Gauge
                  label={pool.name}
                  value={formatBytes(pool.allocationBytes)}
                  max={formatBytes(pool.capacityBytes)}
                  used={pct}
                />
                <div className="card-sub">
                  {pool.type} · {pool.targetPath}
                </div>
              </div>
            );
          })}
      </section>

      <div className="card table-card">
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
            {data.items.map((pool) => (
              <tr key={pool.id}>
                <td className="vm-link">{pool.name}</td>
                <td>{pool.type}</td>
                <td>
                  <span className={`state state-${pool.state === 'active' ? 'running' : ''}`}>
                    <span className="state-dot" />
                    {pool.state}
                  </span>
                </td>
                <td>{formatBytes(pool.capacityBytes, 0)}</td>
                <td>{formatBytes(pool.allocationBytes, 0)}</td>
                <td className="mono">{pool.targetPath ?? '—'}</td>
                <td>
                  <div className="actions">
                    <button
                      className="btn"
                      disabled={busy === pool.id || pool.state !== 'active'}
                      onClick={() => void refreshPool(pool)}
                      title="Re-sync pool usage with the backing storage"
                    >
                      Refresh
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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

function Gauge({ label, value, max, used }: { label: string; value: string; max: string; used: number }) {
  const pct = Math.min(100, Math.max(0, used));
  return (
    <div className="gauge">
      <div className="gauge-head">
        <span className="gauge-label">{label}</span>
        <span className="gauge-value">{value}</span>
      </div>
      <div className="gauge-bar">
        <div className={`gauge-fill${pct > 85 ? ' high' : ''}`} style={{ width: `${pct}%` }} />
      </div>
      <div className="gauge-sub">of {max}</div>
    </div>
  );
}
