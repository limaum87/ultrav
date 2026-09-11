import { useCallback, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { api, unwrap, ApiError, type Host, type VirtualMachine } from '../api/client';
import { formatBytes, formatUptime, usePolling } from '../lib/hooks';
import {
  ActionMenu,
  ConfirmDialog,
  EmptyState,
  MetricCard,
  ProgressMetric,
  StateBadge,
  TableSkeleton,
  type MenuItem,
} from '../components/ui';
import { CreateVMWizard } from '../components/CreateVMWizard';
import {
  Boxes, CirclePlay, Cpu, MemoryStick, HardDrive, Monitor,
  ExternalLink, SquareTerminal, Play, Power, RotateCcw, OctagonX, Trash2,
} from 'lucide-react';
import { useToast } from '../components/Toast';

type PowerAction = 'start' | 'shutdown' | 'reboot' | 'stop';
type StatusFilter = 'all' | 'running' | 'stopped' | 'other';
type SortKey = 'name' | 'state' | 'vcpus' | 'memory' | 'uptime';

/** TODO(backend): tags are not part of the VirtualMachine schema yet;
 * the Tags column and filter render explicit unavailable states. */
const TAGS_AVAILABLE = false;

export default function VirtualMachines() {
  const toast = useToast();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [query, setQuery] = useState(searchParams.get('q') ?? '');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [sort, setSort] = useState<SortKey>('name');
  const [view, setView] = useState<'table' | 'grid'>('table');
  const [busy, setBusy] = useState<string | null>(null);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [confirm, setConfirm] = useState<{ vm: VirtualMachine; action: PowerAction } | null>(null);

  const { data, error, loading, refresh } = usePolling(async () => {
    const [vms, host] = await Promise.all([
      unwrap(api.GET('/vms')),
      unwrap(api.GET('/host')),
    ]);
    return { vms, host } satisfies {
      vms: { items: VirtualMachine[]; total: number };
      host: Host;
    };
  });

  const runAction = useCallback(
    async (vm: VirtualMachine, action: PowerAction) => {
      setBusy(vm.id);
      try {
        await unwrap(api.POST(`/vms/{id}/${action}`, { params: { path: { id: vm.id } } }));
        toast.push('success', `${vm.name}: ${action === 'stop' ? 'force stop' : action} requested`);
        await refresh();
      } catch (e) {
        toast.push(
          'error',
          e instanceof ApiError ? `${vm.name}: ${e.code} — ${e.message}` : String(e),
        );
      } finally {
        setBusy(null);
        setConfirm(null);
      }
    },
    [refresh, toast],
  );

  const items = data?.vms.items ?? [];
  const host = data?.host;

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    let list = items.filter((vm) => {
      if (q && !vm.name.toLowerCase().includes(q) && !(vm.os ?? '').toLowerCase().includes(q)) return false;
      if (status === 'running') return vm.state === 'running';
      if (status === 'stopped') return vm.state === 'stopped';
      if (status === 'other') return vm.state !== 'running' && vm.state !== 'stopped';
      return true;
    });
    list = [...list].sort((a, b) => {
      switch (sort) {
        case 'state': return a.state.localeCompare(b.state);
        case 'vcpus': return b.vcpus - a.vcpus;
        case 'memory': return b.memoryBytes - a.memoryBytes;
        case 'uptime': return (b.uptimeSeconds ?? 0) - (a.uptimeSeconds ?? 0);
        default: return a.name.localeCompare(b.name);
      }
    });
    return list;
  }, [items, query, status, sort]);

  const running = items.filter((v) => v.state === 'running').length;
  const memUsed = host ? formatBytes(host.memoryUsedBytes, 0) : '—';
  const memTotal = host ? formatBytes(host.memoryTotalBytes, 0) : '—';
  const storUsed = host ? formatBytes(host.storageUsedBytes ?? 0, 1) : '—';
  const storTotal = host ? formatBytes(host.storageTotalBytes ?? 0, 1) : '—';
  const storPct =
    host?.storageTotalBytes && host.storageUsedBytes != null
      ? (host.storageUsedBytes / host.storageTotalBytes) * 100
      : null;

  const vmMenu = (vm: VirtualMachine): MenuItem[] => {
    const runningState = vm.state === 'running';
    const startable = vm.state === 'stopped' || vm.state === 'error';
    const mi = (Icon: typeof Play) => <Icon size={14} strokeWidth={1.75} aria-hidden />;
    return [
      { label: 'Open', icon: mi(ExternalLink), to: `/vms/${vm.id}` },
      // TODO(backend): no console endpoint yet (requires websocket/noVNC proxy).
      { label: 'Open Console', icon: mi(SquareTerminal), disabled: true, title: 'Console access is not available yet' },
      { kind: 'separator' },
      { label: 'Start', icon: mi(Play), onSelect: () => void runAction(vm, 'start'), disabled: busy === vm.id || !startable, title: startable ? 'Power on' : 'VM is not stopped' },
      { label: 'Shutdown', icon: mi(Power), onSelect: () => void runAction(vm, 'shutdown'), disabled: busy === vm.id || !runningState, title: 'Graceful ACPI shutdown' },
      { label: 'Reboot', icon: mi(RotateCcw), onSelect: () => void runAction(vm, 'reboot'), disabled: busy === vm.id || !runningState, title: 'Graceful ACPI reboot' },
      { kind: 'separator' },
      { label: 'Force Stop', icon: mi(OctagonX), danger: true, onSelect: () => setConfirm({ vm, action: 'stop' }), disabled: busy === vm.id || !runningState, title: 'Pull the power cable — data loss possible' },
      // TODO(backend): no DELETE /vms/{id} endpoint yet.
      { label: 'Delete', icon: mi(Trash2), danger: true, disabled: true, title: 'VM deletion is not available yet' },
    ];
  };

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Virtual Machines</h1>
          <p className="subtitle">Manage and monitor your virtual machines on {host?.hostname ?? '…'}.</p>
        </div>
        <button className="btn btn-primary" onClick={() => setWizardOpen(true)}>
          + Create VM
        </button>
      </header>

      <CreateVMWizard
        open={wizardOpen}
        onClose={() => setWizardOpen(false)}
        onCreated={() => void refresh()}
      />

      {error && <div className="alert error">Failed to reach API: {error}</div>}

      {/* Summary cards — real data from /vms + /host */}
      <section className="metric-grid">
        <MetricCard
          label="Total VMs"
          value={loading ? '' : items.length}
          loading={loading}
          icon={<Boxes size={24} className="ic ic-blue" strokeWidth={1.75} aria-hidden />}
        />
        <MetricCard
          label="Running"
          value={running}
          tone="ok"
          loading={loading}
          icon={<CirclePlay size={24} className="ic ic-green" strokeWidth={1.75} aria-hidden />}
        />
        <MetricCard
          label="CPU Usage"
          value={host ? `${(host.cpu.usagePercent ?? 0).toFixed(0)}%` : '—'}
          used={host ? (host.cpu.usagePercent ?? 0) : null}
          hint={host ? `${host.cpu.threads} threads` : undefined}
          loading={loading}
          icon={<Cpu size={24} className="ic ic-electric" strokeWidth={1.75} aria-hidden />}
          barTone="blue"
        />
        <MetricCard
          label="Memory Usage"
          value={`${memUsed} / ${memTotal}`}
          used={host ? (host.memoryUsedBytes / host.memoryTotalBytes) * 100 : null}
          hint={host ? `${Math.round((host.memoryUsedBytes / host.memoryTotalBytes) * 100)}% used` : undefined}
          loading={loading}
          icon={<MemoryStick size={24} className="ic ic-violet" strokeWidth={1.75} aria-hidden />}
          barTone="violet"
        />
        <MetricCard
          label="Storage Usage"
          value={`${storUsed} / ${storTotal}`}
          used={storPct}
          hint={storPct != null ? `${Math.round(storPct)}% used` : undefined}
          loading={loading}
          icon={<HardDrive size={24} className="ic ic-purple" strokeWidth={1.75} aria-hidden />}
          barTone="purple"
        />
      </section>

      {/* Filters + view toggle */}
      <div className="filter-bar">
        <div className="filter-search">
          <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
            <circle cx="7" cy="7" r="4.5" />
            <path d="m10.5 10.5 3 3" strokeLinecap="round" />
          </svg>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search by name or OS…"
            aria-label="Search VMs"
          />
        </div>
        <select className="filter-select" value={status} onChange={(e) => setStatus(e.target.value as StatusFilter)} aria-label="Filter by status">
          <option value="all">All statuses</option>
          <option value="running">Running</option>
          <option value="stopped">Stopped</option>
          <option value="other">Other</option>
        </select>
        <select
          className="filter-select"
          value=""
          onChange={() => {}}
          disabled
          title="Tags are not exposed by the API yet"
          aria-label="Filter by tag"
        >
          <option value="">All tags (n/a)</option>
        </select>
        <select className="filter-select" value={sort} onChange={(e) => setSort(e.target.value as SortKey)} aria-label="Sort by">
          <option value="name">Sort: Name</option>
          <option value="state">Sort: Status</option>
          <option value="vcpus">Sort: vCPU</option>
          <option value="memory">Sort: Memory</option>
          <option value="uptime">Sort: Uptime</option>
        </select>
        <div className="filter-spacer" />
        <div className="view-toggle" role="group" aria-label="View mode">
          <button className={view === 'table' ? 'active' : ''} onClick={() => setView('table')} title="Table view">
            table
          </button>
          <button className={view === 'grid' ? 'active' : ''} onClick={() => setView('grid')} title="Grid view">
            grid
          </button>
        </div>
        <button className="icon-btn" onClick={() => void refresh()} title="Refresh" aria-label="Refresh">
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden>
            <path d="M13.5 8a5.5 5.5 0 1 1-1.6-3.9M13.5 1.5v3h-3" />
          </svg>
        </button>
      </div>

      {/* VM list */}
      <div className="card table-card">
        {loading ? (
          <TableSkeleton rows={5} cols={8} />
        ) : filtered.length === 0 ? (
          <EmptyState
            title={items.length === 0 ? 'No virtual machines yet' : 'No VMs match the current filters'}
            message={
              items.length === 0
                ? 'Create your first virtual machine to get started.'
                : 'Try adjusting the search or status filter.'
            }
            action={
              items.length === 0 ? (
                <button className="btn btn-primary" onClick={() => setWizardOpen(true)}>
                  + Create VM
                </button>
              ) : undefined
            }
          />
        ) : view === 'grid' ? (
          <div className="vm-grid">
            {filtered.map((vm) => (
              <Link to={`/vms/${vm.id}`} key={vm.id} className="vm-grid-card">
                <div className="vm-grid-head">
                  <span className="vm-grid-name">{vm.name}</span>
                  <StateBadge state={vm.state} />
                </div>
                <div className="vm-grid-sub">{vm.os ?? 'Unknown OS'}</div>
                <div className="vm-grid-meta">
                  <span>{vm.vcpus} vCPU</span>
                  <span>{formatBytes(vm.memoryBytes, 0)}</span>
                  <span>{vm.ipAddress ?? 'no IP'}</span>
                </div>
              </Link>
            ))}
          </div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Status</th>
                <th>vCPU</th>
                <th>CPU Usage</th>
                <th>Memory</th>
                <th>IP Address</th>
                <th>Uptime</th>
                <th>Host</th>
                <th>Tags</th>
                <th className="th-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((vm) => (
                <tr
                  key={vm.id}
                  className={`row-clickable${busy === vm.id ? ' row-busy' : ''}`}
                  tabIndex={0}
                  title={`Open ${vm.name} details`}
                  onClick={(e) => {
                    // don't hijack clicks on links, buttons or the action menu
                    if ((e.target as HTMLElement).closest('a, button, .action-menu')) return;
                    navigate(`/vms/${vm.id}`);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') navigate(`/vms/${vm.id}`);
                  }}
                >
                  <td>
                    <Link className="vm-link" to={`/vms/${vm.id}`}>
                      <Monitor size={13} className="vm-link-icon" strokeWidth={1.75} aria-hidden />
                      {vm.name}
                    </Link>
                    {vm.os && <div className="cell-sub">{vm.os}</div>}
                  </td>
                  <td>
                    <StateBadge state={vm.state} />
                  </td>
                  <td>{vm.vcpus}</td>
                  <td>
                    <ProgressMetric used={vm.metrics?.cpuPercent ?? null} label="CPU" />
                  </td>
                  <td>
                    <div className="cell-stack">
                      <span>{formatBytes(vm.metrics?.memoryUsedBytes ?? vm.memoryBytes, 1)}</span>
                      <span className="cell-sub">
                        {vm.metrics?.memoryUsedBytes != null
                          ? `of ${formatBytes(vm.memoryBytes, 1)}`
                          : 'allocated'}
                      </span>
                    </div>
                  </td>
                  <td className="mono">{vm.ipAddress ?? <span className="cell-unavailable">n/a</span>}</td>
                  <td>{vm.state === 'running' ? formatUptime(vm.uptimeSeconds) : '—'}</td>
                  <td className="cell-dim">{host?.hostname ?? '—'}</td>
                  <td>
                    {/* TODO(backend): tags not exposed by the API */}
                    <span className="cell-unavailable" title="Tags are not exposed by the API yet">
                      {TAGS_AVAILABLE ? '' : '—'}
                    </span>
                  </td>
                  <td className="td-actions">
                    <ActionMenu items={vmMenu(vm)} label={`Actions for ${vm.name}`} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <ConfirmDialog
        open={confirm?.action === 'stop'}
        title={`Force stop "${confirm?.vm.name ?? ''}"?`}
        message={
          <>
            This is equivalent to pulling the power cable. The guest OS will <strong>not</strong> shut
            down cleanly and data loss is possible. Prefer a graceful shutdown when available.
          </>
        }
        confirmLabel="Force Stop"
        busy={busy === confirm?.vm.id}
        onCancel={() => setConfirm(null)}
        onConfirm={() => confirm && void runAction(confirm.vm, 'stop')}
      />
    </div>
  );
}
