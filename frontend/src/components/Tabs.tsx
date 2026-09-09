import { type ReactNode } from 'react';

export interface TabDef {
  id: string;
  label: string;
  /** Rendered as a disabled tab when the feature has no backend yet. */
  disabled?: boolean;
  title?: string;
}

export function Tabs({
  tabs,
  active,
  onChange,
}: {
  tabs: TabDef[];
  active: string;
  onChange: (id: string) => void;
}) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map((t) =>
        t.disabled ? (
          <span key={t.id} className="tab disabled" title={t.title ?? 'Not available yet'} role="tab" aria-disabled>
            {t.label}
          </span>
        ) : (
          <button
            key={t.id}
            role="tab"
            aria-selected={active === t.id}
            className={`tab${active === t.id ? ' active' : ''}`}
            onClick={() => onChange(t.id)}
          >
            {t.label}
          </button>
        ),
      )}
    </div>
  );
}

export function InfoCard({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="card info-card">
      <div className="info-card-head">
        <h2 className="card-title">{title}</h2>
        {action}
      </div>
      {children}
    </div>
  );
}

/**
 * Small historical sparkline. The API does not expose metrics history yet
 * (would require a metrics endpoint such as GET /vms/{id}/metrics?range=1h),
 * so we render an explicit empty-history state instead of fake data.
 */
export function MiniChart({
  history,
  height = 40,
}: {
  history?: number[] | null;
  height?: number;
}) {
  if (!history || history.length < 2) {
    return (
      <div className="mini-chart empty" style={{ height }}>
        no history
      </div>
    );
  }
  const max = Math.max(...history, 1);
  const w = 100;
  const pts = history.map((v, i) => `${(i / (history.length - 1)) * w},${height - (v / max) * (height - 2) - 1}`);
  return (
    <svg className="mini-chart" viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" style={{ height }} aria-hidden>
      <polyline points={pts.join(' ')} fill="none" stroke="var(--accent)" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
      <polyline points={`0,${height} ${pts.join(' ')} ${w},${height}`} fill="var(--accent-soft)" stroke="none" />
    </svg>
  );
}
