import { useCallback, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api, unwrap, ApiError, type VirtualMachine } from '../api/client';
import { formatBytes, formatUptime, usePolling } from '../lib/hooks';
import {
  ActionMenu,
  ConfirmDialog,
  EmptyState,
  KV,
  MetricCard,
  ProgressMetric,
  StateBadge,
  TableSkeleton,
  type MenuItem,
} from '../components/ui';
import { InfoCard, Tabs } from '../components/Tabs';
import { useToast } from '../components/Toast';

type PowerAction = 'start' | 'shutdown' | 'reboot' | 'stop';

const TABS = [
  { id: 'overview', label: 'Overview' },
  // TODO(backend): console requires a websocket/noVNC proxy endpoint.
  { id: 'console', label: 'Console', disabled: true },
  { id: 'hardware', label: 'Hardware' },
  { id: 'disks', label: 'Disks' },
  { id: 'network', label: 'Network' },
  // TODO(backend): snapshots and backups are roadmap items (phases 3-5).
  { id: 'snapshots', label: 'Snapshots', disabled: true },
  { id: 'backups', label: 'Backups', disabled: true },
  { id: 'settings', label: 'Settings' },
];

export default function VMDetails() {
  const { id = '' } = useParams();
  const toast = useToast();
  const [tab, setTab] = useState('overview');
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState<PowerAction | null>(null);
  const { data: vm, error, loading, refresh } = usePolling(async () =>
    unwrap(api.GET('/vms/{id}', { params: { path: { id } } })),
  );

  const runAction = useCallback(
    async (action: PowerAction) => {
      setBusy(true);
      try {
        await unwrap(api.POST(`/vms/{id}/${action}`, { params: { path: { id } } }));
        toast.push('success', `${vm?.name ?? id}: ${action === 'stop' ? 'force stop' : action} requested`);
        await refresh();
      } catch (e) {
        toast.push('error', e instanceof ApiError ? `${e.code} — ${e.message}` : String(e));
      } finally {
        setBusy(false);
        setConfirm(null);
      }
    },
    [id, refresh, toast, vm?.name],
  );

  if (loading && !vm) {
    return (
      <div className="page page-wide">
        <TableSkeleton rows={4} cols={4} />
      </div>
    );
  }
  if (error || !vm) {
    return (
      <div className="page page-wide">
        <EmptyState
          title="Failed to load VM"
          message={error ?? 'Unknown error'}
          action={<Link className="btn" to="/vms">← Back to Virtual Machines</Link>}
        />
      </div>
    );
  }

  const running = vm.state === 'running';
  const menu: MenuItem[] = [
    { label: 'Force Stop', danger: true, onSelect: () => setConfirm('stop'), disabled: busy || !running, title: 'Pull the power cable — data loss possible' },
    { kind: 'separator' },
    // TODO(backend): no DELETE /vms/{id} endpoint yet.
    { label: 'Delete', danger: true, disabled: true, title: 'VM deletion is not available yet' },
  ];

  return (
    <div className="page page-wide">
      <Link to="/vms" className="back-link">← Virtual Machines</Link>

      {/* Header */}
      <header className="page-head vm-detail-head">
        <div>
          <h1 className="vm-title">
            {vm.name} <StateBadge state={vm.state} />
          </h1>
          <p className="subtitle">{vm.os ?? 'Unknown guest OS'}</p>
        </div>
        <div className="vm-detail-actions">
          <button className="btn" disabled={busy || running || vm.state !== 'stopped'} onClick={() => void runAction('start')} title="Power on">
            Start
          </button>
          <button className="btn" disabled={busy || !running} onClick={() => void runAction('shutdown')} title="Graceful ACPI shutdown">
            Shutdown
          </button>
          <button className="btn" disabled={busy || !running} onClick={() => void runAction('reboot')} title="Graceful ACPI reboot">
            Reboot
          </button>
          <ActionMenu items={menu} label="More actions" />
          <button className="btn" disabled title="Console access is not available yet">
            Open Console
          </button>
        </div>
      </header>

      <Tabs tabs={TABS} active={tab} onChange={setTab} />

      {tab === 'overview' && <Overview vm={vm} />}
      {tab === 'hardware' && <Hardware vm={vm} />}
      {tab === 'disks' && <Disks vm={vm} />}
      {tab === 'network' && <NetworkTab vm={vm} />}
      {tab === 'settings' && <SettingsTab vm={vm} />}

      <ConfirmDialog
        open={confirm === 'stop'}
        title={`Force stop "${vm.name}"?`}
        message={<>This is equivalent to pulling the power cable. The guest OS will <strong>not</strong> shut down cleanly and data loss is possible.</>}
        confirmLabel="Force Stop"
        busy={busy}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void runAction('stop')}
      />
    </div>
  );
}

/* ---------- tabs ---------- */

function Overview({ vm }: { vm: VirtualMachine }) {
  const memAlloc = vm.memoryBytes;
  return (
    <>
      <section className="metric-grid">
        <MetricCard
          label="CPU Usage"
          value={<ProgressCellUnavailable />}
          hint={`${vm.vcpus} vCPU allocated`}
          used={null}
        />
        <MetricCard
          label="Memory Usage"
          value={formatBytes(memAlloc, 1)}
          hint="allocated (guest usage not exposed by the API)"
          used={null}
        />
        <MetricCard
          label="Disk Usage"
          value={formatBytes(vm.disks.reduce((s, d) => s + d.sizeBytes, 0), 1)}
          hint={`${vm.disks.length} disk(s) · capacity (used space not exposed)`}
          used={null}
        />
        <MetricCard
          label="Network"
          value={<span className="metric-unavailable">n/a</span>}
          hint="rx/tx throughput not exposed by the API"
          used={null}
        />
      </section>
      {/* Historical graphs need a metrics endpoint (e.g. GET /vms/{id}/metrics) */}
      <div className="mini-chart-note">
        Historical graphs are unavailable — the API does not expose metrics history yet.
      </div>

      <section className="grid-2">
        <InfoCard title="General Information">
          <KV k="Name" v={vm.name} />
          <KV k="Status" v={<StateBadge state={vm.state} />} />
          <KV k="Guest OS" v={vm.os ?? '—'} />
          <KV k="Uptime" v={formatUptime(vm.uptimeSeconds)} />
          <KV k="Boot Time" v={vm.bootTime ? new Date(vm.bootTime).toLocaleString() : '—'} />
        </InfoCard>
        <InfoCard title="System">
          <KV k="vCPU" v={vm.vcpus} />
          <KV k="Memory" v={`${formatBytes(vm.memoryBytes, 1)} (allocated)`} />
          <KV k="Disks" v={vm.disks.length} />
          <KV k="Network Interfaces" v={vm.networkInterfaces.length} />
        </InfoCard>
        <InfoCard title="Network">
          <KV k="Primary IP" v={vm.ipAddress ?? '—'} />
          {vm.networkInterfaces.slice(0, 3).map((n) => (
            <KV key={n.name} k={n.name} v={n.ipAddress ?? n.macAddress ?? '—'} />
          ))}
        </InfoCard>
        <InfoCard title="Storage">
          {vm.disks.map((d) => (
            <KV key={d.name} k={`${d.name} (${d.format})`} v={formatBytes(d.sizeBytes, 1)} />
          ))}
          {vm.disks.length === 0 && <div className="cell-sub">No disks attached</div>}
        </InfoCard>
      </section>
    </>
  );
}

function ProgressCellUnavailable() {
  return <span className="metric-unavailable">n/a</span>;
}

function Hardware({ vm }: { vm: VirtualMachine }) {
  return (
    <section className="grid-2">
      <InfoCard title="Compute">
        <KV k="vCPU" v={vm.vcpus} />
        <KV k="Memory" v={`${formatBytes(vm.memoryBytes, 1)} (allocated)`} />
        <KV k="State" v={<StateBadge state={vm.state} />} />
      </InfoCard>
      <InfoCard title="Boot">
        <KV k="Boot Time" v={vm.bootTime ? new Date(vm.bootTime).toLocaleString() : '—'} />
        <KV k="Uptime" v={formatUptime(vm.uptimeSeconds)} />
      </InfoCard>
    </section>
  );
}

function Disks({ vm }: { vm: VirtualMachine }) {
  if (vm.disks.length === 0) {
    return <div className="card"><EmptyState title="No disks" message="This VM has no disks attached." /></div>;
  }
  return (
    <div className="card table-card">
      <table className="table">
        <thead>
          <tr><th>Disk</th><th>Bus</th><th>Format</th><th>Capacity</th><th>Used</th></tr>
        </thead>
        <tbody>
          {vm.disks.map((d) => (
            <tr key={d.name}>
              <td className="mono">{d.name}</td>
              <td className="cell-dim">{d.bus ?? '—'}</td>
              <td>{d.format}</td>
              <td>{formatBytes(d.sizeBytes, 1)}</td>
              <td><ProgressMetric used={null} label="Disk usage" unavailable unavailableReason="Per-disk usage is not exposed by the API yet" /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function NetworkTab({ vm }: { vm: VirtualMachine }) {
  if (vm.networkInterfaces.length === 0) {
    return <div className="card"><EmptyState title="No network interfaces" /></div>;
  }
  return (
    <div className="card table-card">
      <table className="table">
        <thead>
          <tr><th>Interface</th><th>Model</th><th>MAC Address</th><th>IP Address</th></tr>
        </thead>
        <tbody>
          {vm.networkInterfaces.map((n) => (
            <tr key={n.name}>
              <td className="mono">{n.name}</td>
              <td className="cell-dim">{n.model}</td>
              <td className="mono">{n.macAddress ?? '—'}</td>
              <td className="mono">{n.ipAddress ?? <span className="cell-unavailable">n/a</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function SettingsTab({ vm }: { vm: VirtualMachine }) {
  return (
    <div className="card">
      <EmptyState
        title="Settings"
        message="Editing VM hardware configuration (vCPU, memory, disks) is not supported by the API yet."
        action={<KV k="Identifier" v={<span className="mono">{vm.id}</span>} />}
      />
    </div>
  );
}
