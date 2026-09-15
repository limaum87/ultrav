import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api, unwrap, ApiError, type VirtualMachine } from '../api/client';
import type { components } from '../api/schema';
import { formatBytes, formatRate, formatUptime, usePolling } from '../lib/hooks';
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
import { ConsolePanel } from '../components/ConsolePanel';
import { LiveCharts, useMetricHistory } from '../components/LiveCharts';
import { useToast } from '../components/Toast';
import {
  Play, Power, RotateCcw, SquareTerminal, OctagonX, Trash2,
  Cpu, MemoryStick, HardDrive, ArrowDownUp, Monitor, Settings2,
} from 'lucide-react';

type PowerAction = 'start' | 'shutdown' | 'reboot' | 'stop';

type IsoList = components['schemas']['IsoList'];
type NetworkList = components['schemas']['NetworkList'];
const GiB = 1024 * 1024 * 1024;

const TABS = [
  { id: 'overview', label: 'Overview', icon: Monitor },
  { id: 'console', label: 'Console', icon: SquareTerminal },
  // TODO(backend): snapshots and backups are roadmap items (phases 3-5).
  { id: 'hardware', label: 'Hardware', icon: Cpu },
  { id: 'disks', label: 'Disks', icon: HardDrive },
  { id: 'network', label: 'Network', icon: ArrowDownUp },
  // TODO(backend): snapshots and backups are roadmap items (phases 3-5).
  { id: 'snapshots', label: 'Snapshots', disabled: true },
  { id: 'backups', label: 'Backups', disabled: true },
  { id: 'settings', label: 'Settings', icon: Settings2 },
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
  const mi = (Icon: typeof Play) => <Icon size={14} strokeWidth={1.75} aria-hidden />;
  const menu: MenuItem[] = [
    { label: 'Force Stop', icon: mi(OctagonX), danger: true, onSelect: () => setConfirm('stop'), disabled: busy || !running, title: 'Pull the power cable — data loss possible' },
    { kind: 'separator' },
    // TODO(backend): no DELETE /vms/{id} endpoint yet.
    { label: 'Delete', icon: mi(Trash2), danger: true, disabled: true, title: 'VM deletion is not available yet' },
  ];

  return (
    <div className="page page-wide">
      <Link to="/vms" className="back-link">← Virtual Machines</Link>

      {/* Header */}
      <header className="page-head vm-detail-head">
        <div>
          <h1 className="vm-title">
            <Monitor size={22} className="vm-title-icon" strokeWidth={1.75} aria-hidden />
            {vm.name} <StateBadge state={vm.state} />
          </h1>
          <p className="subtitle">{vm.os ?? 'Unknown guest OS'}</p>
        </div>
        <div className="vm-detail-actions">
          <button className="btn btn-with-icon" disabled={busy || running || vm.state !== 'stopped'} onClick={() => void runAction('start')} title="Power on">
            <Play size={14} strokeWidth={2} aria-hidden /> Start
          </button>
          <button className="btn btn-with-icon" disabled={busy || !running} onClick={() => void runAction('shutdown')} title="Graceful ACPI shutdown">
            <Power size={14} strokeWidth={2} aria-hidden /> Shutdown
          </button>
          <button className="btn btn-with-icon" disabled={busy || !running} onClick={() => void runAction('reboot')} title="Graceful ACPI reboot">
            <RotateCcw size={14} strokeWidth={2} aria-hidden /> Reboot
          </button>
          <ActionMenu items={menu} label="More actions" />
          <button
            className="btn btn-with-icon"
            disabled={vm.state !== 'running'}
            onClick={() => window.open(`/vms/${encodeURIComponent(vm.id)}/console`, '_blank', 'noopener')}
            title={vm.state === 'running' ? 'Open the graphical console in a new tab' : 'Console requires a running VM'}
          >
            <SquareTerminal size={14} strokeWidth={2} aria-hidden /> Open Console
          </button>
        </div>
      </header>

      <Tabs
        tabs={TABS.map(({ icon: Icon, ...t }) => ({
          ...t,
          icon: Icon ? <Icon size={14} strokeWidth={1.75} aria-hidden /> : undefined,
        }))}
        active={tab}
        onChange={setTab}
      />

      {tab === 'overview' && <Overview vm={vm} />}
      {tab === 'console' && <ConsolePanel vm={vm} />}
      {tab === 'hardware' && <Hardware vm={vm} />}
      {tab === 'disks' && <Disks vm={vm} />}
      {tab === 'network' && <NetworkTab vm={vm} />}
      {tab === 'settings' && <SettingsTab vm={vm} onChanged={() => void refresh()} />}

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
  // Session-scoped metric history: sampled client-side while the page is open.
  const samples = useMetricHistory(vm.id);
  const m = vm.metrics;
  const running = vm.state === 'running';
  const cpuPct = m?.cpuPercent ?? null;
  const memUsed = m?.memoryUsedBytes ?? null;
  const rx = m?.networkRxBytesPerSecond ?? null;
  const tx = m?.networkTxBytesPerSecond ?? null;

  const diskTotal = vm.disks.reduce((s, d) => s + d.sizeBytes, 0);
  const diskUsed = vm.disks.reduce((s, d) => s + (d.usedBytes ?? 0), 0);
  const diskReported = vm.disks.some((d) => d.usedBytes != null);
  const diskPct = diskReported && diskTotal > 0 ? (diskUsed / diskTotal) * 100 : null;
  const memPct = memUsed != null && vm.memoryBytes > 0 ? (memUsed / vm.memoryBytes) * 100 : null;

  return (
    <>
      <section className="metric-grid">
        <MetricCard
          label="CPU Usage"
          value={cpuPct != null ? `${cpuPct.toFixed(0)}%` : <span className="metric-unavailable">n/a</span>}
          used={cpuPct}
          barTone="blue"
          hint={running ? `${vm.vcpus} vCPU allocated` : 'VM not running'}
          icon={<Cpu size={24} className="ic ic-electric" strokeWidth={1.75} aria-hidden />}
        />
        <MetricCard
          label="Memory Usage"
          value={
            memUsed != null
              ? `${formatBytes(memUsed, 1)} / ${formatBytes(vm.memoryBytes, 1)}`
              : formatBytes(vm.memoryBytes, 1)
          }
          used={memPct}
          barTone="violet"
          hint={
            memUsed != null
              ? `${Math.round(memPct ?? 0)}% of allocated`
              : 'allocated (guest usage not reported)'
          }
          icon={<MemoryStick size={24} className="ic ic-violet" strokeWidth={1.75} aria-hidden />}
        />
        <MetricCard
          label="Disk Usage"
          value={
            diskPct != null
              ? `${formatBytes(diskUsed, 1)} / ${formatBytes(diskTotal, 1)}`
              : formatBytes(diskTotal, 1)
          }
          used={diskPct}
          barTone="purple"
          hint={
            diskPct != null
              ? `${Math.round(diskPct)}% allocated · ${vm.disks.length} disk(s)`
              : `${vm.disks.length} disk(s) · capacity (usage not reported)`
          }
          icon={<HardDrive size={24} className="ic ic-purple" strokeWidth={1.75} aria-hidden />}
        />
        <MetricCard
          label="Network"
          value={
            rx != null || tx != null ? (
              <span className="metric-rate">↓ {formatRate(rx)} · ↑ {formatRate(tx)}</span>
            ) : (
              <span className="metric-unavailable">n/a</span>
            )
          }
          hint={running ? 'rx / tx throughput' : 'VM not running'}
          icon={<ArrowDownUp size={24} className="ic ic-blue" strokeWidth={1.75} aria-hidden />}
        />
      </section>
      {/* The API exposes current metrics only (no time-series endpoint yet),
          so the charts below are built from samples collected live while this
          page is open (session scope, in-memory). */}
      <LiveCharts samples={samples} />

      <section className="grid-2">
        <InfoCard title="General Information">
          <KV k="Name" v={vm.name} />
          <KV k="Status" v={<StateBadge state={vm.state} />} />
          <KV k="Guest OS" v={vm.os ?? '—'} />
          <KV k="Uptime" v={formatUptime(vm.uptimeSeconds)} />
          <KV k="Boot Time" v={vm.bootTime ? new Date(vm.bootTime).toLocaleString() : '—'} />
        </InfoCard>
        <InfoCard title="System">
          <KV k="Operating system type" v={vm.osType ?? '—'} />
          <KV k="vCPU" v={vm.vcpus} />
          <KV
            k="Memory"
            v={memUsed != null ? `${formatBytes(memUsed, 1)} / ${formatBytes(vm.memoryBytes, 1)}` : `${formatBytes(vm.memoryBytes, 1)} (allocated)`}
          />
          <KV k="Disks" v={vm.disks.length} />
          <KV k="Network Interfaces" v={vm.networkInterfaces.length} />
        </InfoCard>
        <InfoCard title="Network">
          <KV k="Primary IP" v={vm.ipAddress ?? '—'} />
          <KV k="Inbound" v={formatRate(rx)} />
          <KV k="Outbound" v={formatRate(tx)} />
          {vm.networkInterfaces.slice(0, 3).map((n) => (
            <KV key={n.name} k={n.name} v={n.ipAddress ?? n.macAddress ?? '—'} />
          ))}
        </InfoCard>
        <InfoCard title="Storage">
          {vm.disks.map((d) => (
            <KV
              key={d.name}
              k={`${d.name} (${d.format})`}
              v={
                d.usedBytes != null
                  ? `${formatBytes(d.usedBytes, 1)} / ${formatBytes(d.sizeBytes, 1)}`
                  : formatBytes(d.sizeBytes, 1)
              }
            />
          ))}
          {vm.disks.length === 0 && <div className="cell-sub">No disks attached</div>}
        </InfoCard>
      </section>
      <PerformanceProfileCard vm={vm} />
    </>
  );
}

function PerformanceProfileCard({ vm }: { vm: VirtualMachine }) {
  const p = vm.performanceProfile;
  if (!p) {
    return (
      <section className="grid-2">
        <InfoCard title="Performance Profile">
          <div className="cell-sub">
            No performance profile (VM created before per-OS profiles existed).
          </div>
        </InfoCard>
      </section>
    );
  }
  return (
    <section className="grid-2">
      <InfoCard title="Performance Profile">
        <KV k="OS type" v={vm.osType ?? '—'} />
        <KV k="CPU mode" v={p.cpuMode ?? '—'} />
        <KV k="Disk bus" v={p.diskBus ?? '—'} />
        <KV k="Disk cache" v={p.cache ?? '—'} />
        <KV k="IO threads" v={p.ioThreads ? String(p.ioThreads) : '—'} />
      </InfoCard>
      <InfoCard title="Hyper-V Enlightenments">
        {p.hypervEnlightenments && p.hypervEnlightenments.length > 0 ? (
          p.hypervEnlightenments.map((e) => (
            <KV key={e} k={e} v="on" />
          ))
        ) : (
          <div className="cell-sub">
            {vm.osType === 'windows'
              ? 'None applied (limited host capabilities).'
              : 'Not applicable (Linux profile).'}
          </div>
        )}
      </InfoCard>
    </section>
  );
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
              <td>
                <ProgressMetric
                  used={d.usedBytes != null && d.sizeBytes > 0 ? (d.usedBytes / d.sizeBytes) * 100 : null}
                  label="Disk usage"
                  unavailableReason="Disk usage is not reported by the hypervisor"
                />
              </td>
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

function SettingsTab({ vm, onChanged }: { vm: VirtualMachine; onChanged: () => void }) {
  const toast = useToast();
  const [vcpus, setVcpus] = useState(String(vm.vcpus));
  const [memoryGiB, setMemoryGiB] = useState(String(+(vm.memoryBytes / GiB).toFixed(2)));
  const [isoId, setIsoId] = useState<string>(vm.isoId ?? '');
  const [networkId, setNetworkId] = useState<string>(vm.networkInterfaces?.[0]?.network ?? '');
  const [isos, setIsos] = useState<IsoList | null>(null);
  const [networks, setNetworks] = useState<NetworkList | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    unwrap(api.GET('/storage/isos'))
      .then(setIsos)
      .catch(() => setIsos({ items: [], total: 0 }));
    unwrap(api.GET('/networks'))
      .then(setNetworks)
      .catch(() => setNetworks({ items: [], total: 0 }));
  }, []);

  const vcpusNum = Number(vcpus);
  const memoryBytes = Math.round(Number(memoryGiB) * GiB);
  const hardwareDirty = vcpusNum !== vm.vcpus || memoryBytes !== vm.memoryBytes;
  const isoDirty = isoId !== (vm.isoId ?? '');
  const networkDirty = networkId !== (vm.networkInterfaces?.[0]?.network ?? '');
  const valid =
    Number.isFinite(vcpusNum) && vcpusNum >= 1 && vcpusNum <= 64 &&
    Number.isFinite(memoryBytes) && memoryBytes >= 16 * 1024 * 1024;
  const stopped = vm.state === 'stopped';

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const body: components['schemas']['VirtualMachineUpdate'] = {};
      if (hardwareDirty) {
        if (vcpusNum !== vm.vcpus) body.vcpus = vcpusNum;
        if (memoryBytes !== vm.memoryBytes) body.memoryBytes = memoryBytes;
      }
      if (isoDirty) body.isoId = isoId;
      if (networkDirty) body.networkId = networkId;
      await unwrap(api.PATCH('/vms/{id}', { params: { path: { id: vm.id } }, body }));
      toast.push('success', `${vm.name}: settings updated${!stopped && (hardwareDirty || isoDirty) ? ' (applies on next boot)' : ''}`);
      onChanged();
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code} — ${e.message}` : String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 560, padding: 20 }}>
      <h2 className="card-title">VM settings</h2>
      {!stopped && (
        <div className="alert" style={{ marginBottom: 12 }}>
          vCPU and memory can only be changed while the VM is stopped. ISO changes apply on the next boot.
        </div>
      )}
      {error && <div className="alert error">{error}</div>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) void save();
        }}
      >
        <label className="field">
          <span>vCPUs</span>
          <input
            type="number"
            min={1}
            max={64}
            value={vcpus}
            onChange={(e) => setVcpus(e.target.value)}
            disabled={!stopped || saving}
          />
        </label>
        <label className="field">
          <span>Memory (GiB)</span>
          <input
            type="number"
            min={16 / 1024}
            step="any"
            value={memoryGiB}
            onChange={(e) => setMemoryGiB(e.target.value)}
            disabled={!stopped || saving}
          />
        </label>
        <label className="field">
          <span>Install media (ISO)</span>
          <select value={isoId} onChange={(e) => setIsoId(e.target.value)} disabled={saving}>
            <option value="">— No ISO attached —</option>
            {(isos?.items ?? []).map((iso) => (
              <option key={iso.id} value={iso.id}>
                {iso.fileName} ({formatBytes(iso.sizeBytes, 1)})
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>Network</span>
          <select
            value={networkId}
            onChange={(e) => setNetworkId(e.target.value)}
            disabled={!stopped || saving}
          >
            {(networks?.items ?? []).map((net) => (
              <option key={net.id} value={net.id} disabled={net.state !== 'active'}>
                {net.name} ({net.mode}){net.state !== 'active' ? ' — inactive' : ''}
              </option>
            ))}
          </select>
          {!stopped && <small className="field-hint">Stop the VM to change its network.</small>}
        </label>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <button className="btn btn-primary" type="submit" disabled={!valid || saving || (!hardwareDirty && !isoDirty && !networkDirty)}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
          {!stopped && hardwareDirty && (
            <span className="cell-sub">Stop the VM to edit vCPU / memory</span>
          )}
        </div>
      </form>
    </div>
  );
}
