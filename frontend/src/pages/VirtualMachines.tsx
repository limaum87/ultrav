import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, unwrap, ApiError, type VirtualMachine } from '../api/client';
import { formatBytes, formatUptime, usePolling } from '../lib/hooks';
import { StateBadge, VmActions } from '../components/ui';
import { CreateVMWizard } from '../components/CreateVMWizard';

export default function VirtualMachines() {
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [wizardOpen, setWizardOpen] = useState(false);
  const { data, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/vms')),
  );

  const runAction = useCallback(
    async (vm: VirtualMachine, action: 'start' | 'shutdown' | 'reboot' | 'stop') => {
      setBusy(vm.id);
      setActionError(null);
      try {
        await unwrap(api.POST(`/vms/{id}/${action}`, { params: { path: { id: vm.id } } }));
        await refresh();
      } catch (e) {
        setActionError(
          e instanceof ApiError ? `${e.code}: ${e.message}` : String(e),
        );
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
          <h1>Virtual Machines</h1>
          <p className="subtitle">{data.total} machines on this host</p>
        </div>
        <button className="btn btn-primary" onClick={() => setWizardOpen(true)}>
          + New VM
        </button>
      </header>

      <CreateVMWizard
        open={wizardOpen}
        onClose={() => setWizardOpen(false)}
        onCreated={() => void refresh()}
      />

      {actionError && <div className="alert error">{actionError}</div>}

      <div className="card table-card">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>vCPU</th>
              <th>Memory</th>
              <th>IP</th>
              <th>Uptime</th>
              <th className="th-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {data.items.map((vm) => (
              <tr key={vm.id}>
                <td>
                  <Link className="vm-link" to={`/vms/${vm.id}`}>
                    {vm.name}
                  </Link>
                </td>
                <td>
                  <StateBadge state={vm.state} />
                </td>
                <td>{vm.vcpus}</td>
                <td>{formatBytes(vm.memoryBytes, 0)}</td>
                <td>{vm.ipAddress ?? '—'}</td>
                <td>{formatUptime(vm.uptimeSeconds)}</td>
                <td>
                  <VmActions vm={vm} busy={busy === vm.id} onAction={(a) => void runAction(vm, a)} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
