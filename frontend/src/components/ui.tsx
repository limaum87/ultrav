import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import type { VirtualMachine } from '../api/client';
import { EllipsisVertical } from 'lucide-react';
import { formatUptime } from '../lib/hooks';

export function StateBadge({ state }: { state: VirtualMachine['state'] }) {
  return (
    <span className={`state state-${state}`}>
      <span className="state-dot" />
      {state === 'shutting-down' ? 'shutting down' : state}
    </span>
  );
}

/* ---------- Metric card (page-level summary) ---------- */

export function MetricCard({
  label,
  value,
  hint,
  tone,
  used,
  loading,
  icon,
  barTone = 'blue',
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  tone?: 'ok' | 'warn' | 'danger';
  /** 0-100; renders a bar when provided. */
  used?: number | null;
  loading?: boolean;
  /** Vivid Lucide icon rendered inside a 44x44 tinted container. */
  icon?: ReactNode;
  /** Bar color under normal utilization; amber >75%, red >90%. */
  barTone?: 'blue' | 'green' | 'violet' | 'purple';
}) {
  const pct = used == null ? null : Math.min(100, Math.max(0, used));
  const fillCls =
    pct == null ? '' : pct > 90 ? ' crit' : pct > 75 ? ' warn' : ` tone-${barTone}`;
  return (
    <div className="metric-card card">
      {icon && <div className="metric-icon">{icon}</div>}
      <div className="metric-body">
        <div className="metric-label">{label}</div>
        {loading ? (
          <span className="skeleton skeleton-metric" />
        ) : (
          <div className={`metric-value${tone ? ` metric-${tone}` : ''}`}>{value}</div>
        )}
        {pct != null && !loading && (
          <div className="metric-bar">
            <div className={`metric-bar-fill${fillCls}`} style={{ width: `${pct}%` }} />
          </div>
        )}
        {hint != null && !loading && <div className="metric-hint">{hint}</div>}
      </div>
    </div>
  );
}

/* ---------- Compact inline progress (table cells) ---------- */

/**
 * Used for per-VM metrics. The current API does not expose per-VM CPU/memory
 * utilization, so pass `unavailable` and the cell renders an explicit `n/a`
 * state instead of a fabricated value. Requires a backend addition such as
 * `VirtualMachine.metrics` { cpuPercent, memoryUsedBytes } to light up.
 */
export function ProgressMetric({
  used,
  label,
  unavailable,
  unavailableReason,
}: {
  /** 0-100, or null when the backend does not expose this metric. */
  used: number | null;
  label: string;
  unavailable?: boolean;
  unavailableReason?: string;
}) {
  if (unavailable || used == null) {
    return (
      <span className="cell-unavailable" title={unavailableReason}>
        n/a
      </span>
    );
  }
  return (
    <div className="cell-progress" title={label}>
      <span className="cell-progress-pct">{Math.round(used)}%</span>
      <div className="metric-bar">
        <div
          className={`metric-bar-fill${used > 90 ? ' crit' : used > 75 ? ' warn' : ''}`}
          style={{ width: `${Math.min(100, used)}%` }}
        />
      </div>
    </div>
  );
}

/* ---------- Dropdown action menu (three-dot) ---------- */

export interface MenuItem {
  kind?: 'separator';
  label?: string;
  icon?: ReactNode;
  to?: string;
  onSelect?: () => void;
  disabled?: boolean;
  title?: string;
  danger?: boolean;
}

export function ActionMenu({ items, label = 'Actions' }: { items: MenuItem[]; label?: string }) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onEsc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', onDoc);
    document.addEventListener('keydown', onEsc);
    return () => {
      document.removeEventListener('mousedown', onDoc);
      document.removeEventListener('keydown', onEsc);
    };
  }, [open]);

  return (
    <div className="action-menu" ref={rootRef}>
      <button
        className={`action-menu-trigger${open ? ' open' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        title={label}
      >
        <EllipsisVertical size={16} strokeWidth={2} aria-hidden />
      </button>
      {open && (
        <div className="action-menu-popover" role="menu">
          {items.map((item, i) => {
            if (item.kind === 'separator') return <div key={i} className="menu-sep" />;
            const cls =
              'menu-item' +
              (item.danger ? ' danger' : '') +
              (item.disabled ? ' disabled' : '');
            const content = (
              <>
                {item.icon && <span className="menu-icon">{item.icon}</span>}
                {item.label}
              </>
            );
            if (item.disabled) {
              return (
                <span key={i} className={cls} title={item.title}>
                  {content}
                </span>
              );
            }
            if (item.to) {
              return (
                <Link key={i} to={item.to} className={cls} role="menuitem" onClick={() => setOpen(false)}>
                  {content}
                </Link>
              );
            }
            return (
              <button
                key={i}
                className={cls}
                role="menuitem"
                title={item.title}
                onClick={() => {
                  setOpen(false);
                  item.onSelect?.();
                }}
              >
                {content}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

/* ---------- Confirmation dialog (destructive actions) ---------- */

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  onConfirm,
  onCancel,
  busy,
}: {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
  busy?: boolean;
}) {
  if (!open) return null;
  return (
    <div className="wiz-overlay" onClick={onCancel}>
      <div className="wiz confirm" onClick={(e) => e.stopPropagation()} role="alertdialog">
        <div className="wiz-head">
          <h2>{title}</h2>
          <button className="wiz-close" onClick={onCancel} aria-label="Close">
            ×
          </button>
        </div>
        <div className="wiz-body">{message}</div>
        <div className="wiz-foot">
          <span />
          <div className="wiz-foot-right">
            <button className="btn" onClick={onCancel} disabled={busy}>
              Cancel
            </button>
            <button className="btn btn-danger-solid" onClick={onConfirm} disabled={busy}>
              {busy ? 'Working…' : confirmLabel}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ---------- Empty & loading states ---------- */

export function EmptyState({
  icon = '◌',
  title,
  message,
  action,
}: {
  icon?: ReactNode;
  title: string;
  message?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <div className="empty-icon">{icon}</div>
      <div className="empty-title">{title}</div>
      {message && <div className="empty-msg">{message}</div>}
      {action}
    </div>
  );
}

export function TableSkeleton({ rows = 5, cols = 8 }: { rows?: number; cols?: number }) {
  return (
    <div className="table-skeleton">
      {Array.from({ length: rows }).map((_, r) => (
        <div className="skeleton-row" key={r}>
          {Array.from({ length: cols }).map((__, c) => (
            <span
              className="skeleton"
              key={c}
              style={{ width: c === 0 ? '46%' : `${50 + ((r * 7 + c * 13) % 30)}%` }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

export function TagChip({ label }: { label: string }) {
  return <span className="tag-chip">{label}</span>;
}

/* ---------- legacy shared primitives (kept) ---------- */

export function Gauge({
  label,
  value,
  max,
  used,
}: {
  label: string;
  value: string;
  max: string;
  used: number;
}) {
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

export function VmActions({
  vm,
  busy,
  onAction,
  size = 'sm',
}: {
  vm: VirtualMachine;
  busy: boolean;
  onAction: (action: 'start' | 'shutdown' | 'reboot' | 'stop') => void;
  size?: 'sm' | 'md';
}) {
  const running = vm.state === 'running';
  const active = vm.state === 'running' || vm.state === 'shutting-down';
  return (
    <div className={`actions actions-${size}`}>
      <button
        className="btn"
        disabled={busy || running || vm.state !== 'stopped'}
        onClick={() => onAction('start')}
        title="Power on this virtual machine"
      >
        Start
      </button>
      <button
        className="btn"
        disabled={busy || vm.state !== 'running'}
        onClick={() => onAction('shutdown')}
        title="Graceful ACPI shutdown (guest OS shuts down cleanly)"
      >
        Shutdown
      </button>
      <button
        className="btn"
        disabled={busy || vm.state !== 'running'}
        onClick={() => onAction('reboot')}
        title="Graceful ACPI reboot"
      >
        Reboot
      </button>
      <button
        className="btn btn-danger"
        disabled={busy || !active}
        onClick={() => {
          if (window.confirm(
            `Force stop "${vm.name}"?\n\nThis is equivalent to pulling the power cable. The guest OS will NOT shut down cleanly and data loss is possible.`,
          )) {
            onAction('stop');
          }
        }}
        title="Force stop — like pulling the power cable"
      >
        Force Stop
      </button>
    </div>
  );
}

export function KV({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="kv">
      <span className="kv-key">{k}</span>
      <span className="kv-val">{v ?? '—'}</span>
    </div>
  );
}

export function uptimeOf(vm: VirtualMachine) {
  return formatUptime(vm.uptimeSeconds);
}
