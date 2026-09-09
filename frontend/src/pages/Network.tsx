import { useCallback, useState } from 'react';
import { api, unwrap, ApiError, type Network } from '../api/client';
import { usePolling } from '../lib/hooks';

export default function NetworkPage() {
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const { data, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/networks')),
  );

  const runAction = useCallback(
    async (net: Network, action: 'start' | 'stop') => {
      setBusy(net.id);
      setActionError(null);
      try {
        await unwrap(api.POST(`/networks/{id}/${action}`, { params: { path: { id: net.id } } }));
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
          <h1>Network</h1>
          <p className="subtitle">{data.total} virtual networks on this host</p>
        </div>
      </header>

      {actionError && <div className="alert error">{actionError}</div>}

      <div className="card table-card">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Bridge</th>
              <th>Gateway</th>
              <th>DHCP</th>
              <th>Autostart</th>
              <th className="th-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {data.items.map((net) => (
              <tr key={net.id}>
                <td>
                  {net.name}
                  {net.domainName && <div className="net-domain">{net.domainName}</div>}
                </td>
                <td>
                  <span className={`state state-${net.state === 'active' ? 'running' : ''}`}>
                    <span className="state-dot" />
                    {net.state}
                  </span>
                </td>
                <td className="mono">{net.bridge ?? '—'}</td>
                <td className="mono">
                  {net.ipAddress ? `${net.ipAddress}/${net.ipPrefix ?? ''}` : '—'}
                </td>
                <td>{net.dhcpEnabled ? 'yes' : 'no'}</td>
                <td>{net.autostart ? 'yes' : 'no'}</td>
                <td>
                  <div className="actions">
                    <button
                      className="btn"
                      disabled={busy === net.id || net.state === 'active'}
                      onClick={() => void runAction(net, 'start')}
                    >
                      Start
                    </button>
                    <button
                      className="btn"
                      disabled={busy === net.id || net.state !== 'active'}
                      onClick={() => void runAction(net, 'stop')}
                    >
                      Stop
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
