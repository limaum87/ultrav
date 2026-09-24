import { useMemo, useState } from 'react';
import { ListChecks, XCircle } from 'lucide-react';
import { api, unwrap, ApiError, type Task } from '../api/client';
import { usePolling } from '../lib/hooks';
import { EmptyState, MetricCard } from '../components/ui';
import { useToast } from '../components/Toast';

type StatusFilter = '' | 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';

const TYPE_LABEL: Record<string, string> = {
  'vm-create': 'VM creation',
  'backup-create': 'Backup',
  'backup-restore': 'Restore',
};

function StatusBadge({ status }: { status: Task['status'] }) {
  return (
    <span className={`state state-${status === 'cancelling' ? 'shutting-down' : status === 'queued' ? 'idle' : status}`}>
      <span className="state-dot" />
      {status === 'cancelling' ? 'cancelling' : status}
    </span>
  );
}

function formatWhen(iso: string | null | undefined): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleTimeString();
}

export default function Tasks() {
  const [filter, setFilter] = useState<StatusFilter>('');
  const tasks = usePolling(async () => unwrap(api.GET('/tasks', { params: { query: { status: filter || undefined, limit: 200 } } })), 2000);
  const toast = useToast();
  const [cancelling, setCancelling] = useState<string | null>(null);

  const items = useMemo(() => tasks.data?.items ?? [], [tasks.data]);
  const active = items.filter((t) => t.status === 'queued' || t.status === 'running' || t.status === 'cancelling').length;
  const failed = items.filter((t) => t.status === 'failed').length;

  const cancel = async (id: string) => {
    setCancelling(id);
    try {
      await unwrap(api.POST('/tasks/{id}/cancel', { params: { path: { id } } }));
      toast.push('info', `Cancellation of task ${id} requested`);
    } catch (e) {
      toast.push('error', e instanceof ApiError ? `${e.code} — ${e.message}` : String(e));
    } finally {
      setCancelling(null);
      void tasks.refresh();
    }
  };

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Tasks</h1>
          <p className="subtitle">Async operations (VM creation, backups, restores) and their history</p>
        </div>
        <select
          value={filter}
          onChange={(e) => setFilter(e.target.value as StatusFilter)}
          aria-label="Filter by status"
          style={{ minWidth: 160 }}
        >
          <option value="">All statuses</option>
          <option value="queued">queued</option>
          <option value="running">running</option>
          <option value="succeeded">succeeded</option>
          <option value="failed">failed</option>
          <option value="cancelled">cancelled</option>
        </select>
      </header>

      <section className="metric-grid">
        <MetricCard
          label="Active"
          value={active}
          hint="queued + running"
          loading={tasks.loading}
          icon={<ListChecks size={24} className="ic ic-blue" strokeWidth={1.75} aria-hidden />}
        />
        <MetricCard
          label="Failures"
          value={failed}
          hint="in the current history"
          loading={tasks.loading}
          tone={failed > 0 ? 'danger' : undefined}
          icon={<XCircle size={24} className="ic ic-blue" strokeWidth={1.75} aria-hidden />}
        />
      </section>

      <div className="card" style={{ padding: 0 }}>
        {tasks.loading ? (
          <EmptyState title="Loading…" message="Fetching tasks" />
        ) : items.length === 0 ? (
          <EmptyState
            title="No tasks"
            message="Create a VM or run a backup: the operation shows up here in real time."
          />
        ) : (
          <div className="table-card">
            <table className="table">
              <thead>
                <tr>
                  <th>Task</th>
                  <th>Type</th>
                  <th>Resource</th>
                  <th>Status</th>
                  <th>Progress</th>
                  <th>Started</th>
                  <th>Finished</th>
                  <th aria-label="Ações" />
                </tr>
              </thead>
              <tbody>
                {items.map((t) => {
                  const terminal = t.status === 'succeeded' || t.status === 'failed' || t.status === 'cancelled';
                  return (
                    <tr key={t.id}>
                      <td>
                        <code>{t.id}</code>
                      </td>
                      <td>{TYPE_LABEL[t.type] ?? t.type}</td>
                      <td>{t.resourceName ?? t.resourceId ?? '—'}</td>
                      <td>
                        <StatusBadge status={t.status} />
                        {t.status === 'failed' && t.error && (
                          <div className="metric-hint" style={{ color: 'var(--danger, #c0392b)' }}>{t.error}</div>
                        )}
                        {!terminal && t.message && <div className="metric-hint">{t.message}</div>}
                        {t.warnings?.map((w, i) => (
                          <div key={i} className="metric-hint">⚠ {w}</div>
                        ))}
                      </td>
                      <td>
                        <div className="cell-progress" title={`${t.progress}%`}>
                          <span className="cell-progress-pct">{t.progress}%</span>
                          <div className="metric-bar">
                            <div
                              className={`metric-bar-fill${t.status === 'failed' ? ' crit' : ''}`}
                              style={{ width: `${t.progress}%` }}
                            />
                          </div>
                        </div>
                      </td>
                      <td>{formatWhen(t.startedAt ?? t.createdAt)}</td>
                      <td>{formatWhen(t.finishedAt)}</td>
                      <td>
                        {t.cancellable && (
                          <button className="btn btn-sm" disabled={cancelling === t.id} onClick={() => void cancel(t.id)}>
                            {cancelling === t.id ? 'Cancelling…' : 'Cancel'}
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
