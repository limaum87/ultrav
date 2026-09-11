import { useEffect, useRef, useState } from 'react';
import { api, unwrap } from '../api/client';
import { formatRate } from '../lib/hooks';

/**
 * Live session-scoped metric history for one VM. The UltraV API exposes
 * current metrics only (no time-series endpoint yet), so while the details
 * page is open we sample the metrics endpoint and accumulate the last
 * `maxPoints` samples in memory. The buffer starts empty: charts grow as the
 * user keeps the page open and are discarded on navigation.
 */

export type MetricSample = {
  t: number; // epoch ms
  cpuPercent: number | null;
  memPercent: number | null;
  rxBytesPerSecond: number | null;
  txBytesPerSecond: number | null;
};

const SAMPLE_INTERVAL_MS = 5000;

export function useMetricHistory(id: string, maxPoints = 120) {
  const [samples, setSamples] = useState<MetricSample[]>([]);
  const buf = useRef<MetricSample[]>([]);

  useEffect(() => {
    let alive = true;
    buf.current = []; // new VM -> new buffer

    const tick = async () => {
      try {
        const vm = await unwrap(api.GET('/vms/{id}', { params: { path: { id } } }));
        if (!alive) return;
        const m = vm.metrics;
        const sample: MetricSample = {
          t: Date.now(),
          cpuPercent: m?.cpuPercent ?? null,
          memPercent:
            m?.memoryUsedBytes != null && vm.memoryBytes > 0
              ? (m.memoryUsedBytes / vm.memoryBytes) * 100
              : null,
          rxBytesPerSecond: m?.networkRxBytesPerSecond ?? null,
          txBytesPerSecond: m?.networkTxBytesPerSecond ?? null,
        };
        buf.current = [...buf.current, sample].slice(-maxPoints);
        setSamples(buf.current);
      } catch {
        // transient poll errors (e.g. during power actions) just skip a point
      }
    };

    void tick();
    const timer = setInterval(() => void tick(), SAMPLE_INTERVAL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [id, maxPoints]);

  return samples;
}

/* ---------- sparkline (pure SVG) ---------- */

function sparklinePath(values: (number | null)[], w: number, h: number, max: number): string {
  const pts = values
    .map((v, i) => ({ v, x: (i / Math.max(1, values.length - 1)) * w }))
    .filter((p): p is { v: number; x: number } => p.v != null)
    .map((p) => {
      const y = h - (Math.min(p.v, max) / max) * (h - 2) - 1;
      return `${p.x.toFixed(1)},${y.toFixed(1)}`;
    });
  if (pts.length === 0) return '';
  return `M${pts.join(' L')}`;
}

function areaPath(values: (number | null)[], w: number, h: number, max: number): string {
  const line = sparklinePath(values, w, h, max);
  if (!line) return '';
  const xs = values
    .map((v, i) => (v != null ? (i / Math.max(1, values.length - 1)) * w : null))
    .filter((x): x is number => x != null);
  if (xs.length < 2) return '';
  return `${line} L${xs[xs.length - 1].toFixed(1)},${h} L${xs[0].toFixed(1)},${h} Z`;
}

function Sparkline({
  values,
  max,
  stroke,
  fill,
}: {
  values: (number | null)[];
  max: number;
  stroke: string;
  fill: string;
}) {
  const W = 100;
  const H = 36;
  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className="spark"
      role="img"
      aria-hidden
    >
      {/* 50% reference gridline */}
      <line x1="0" y1={H / 2} x2={W} y2={H / 2} className="spark-grid" />
      <path d={areaPath(values, W, H, max)} fill={fill} />
      <path d={sparklinePath(values, W, H, max)} fill="none" stroke={stroke} strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

/* ---------- chart cards ---------- */

function ChartCard({
  title,
  current,
  hint,
  children,
}: {
  title: string;
  current: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="card live-chart">
      <div className="live-chart-head">
        <div>
          <span className="card-title" style={{ margin: 0 }}>{title}</span>
          <div className="live-chart-now">{current}</div>
        </div>
        {hint && <span className="cell-sub">{hint}</span>}
      </div>
      {children}
    </div>
  );
}

export function LiveCharts({ samples }: { samples: MetricSample[] }) {
  const last = samples[samples.length - 1];
  const spanMin = samples.length > 1 ? ((samples[samples.length - 1].t - samples[0].t) / 60000).toFixed(0) : null;

  const cpu = samples.map((s) => s.cpuPercent);
  const mem = samples.map((s) => s.memPercent);
  const rx = samples.map((s) => s.rxBytesPerSecond);
  const tx = samples.map((s) => s.txBytesPerSecond);

  const rxMax = Math.max(1, ...rx.filter((v): v is number => v != null));
  const txMax = Math.max(1, ...tx.filter((v): v is number => v != null));
  const netMax = Math.max(rxMax, txMax); // same scale for both lines

  return (
    <section className="metric-grid">
      <ChartCard
        title="CPU"
        current={last?.cpuPercent != null ? `${last.cpuPercent.toFixed(0)}%` : '—'}
        hint={spanMin ? `últimos ${spanMin} min (sessão)` : 'coletando…'}
      >
        <Sparkline values={cpu} max={100} stroke="var(--electric, #38bdf8)" fill="rgba(56, 189, 248, 0.12)" />
      </ChartCard>

      <ChartCard
        title="Memória"
        current={last?.memPercent != null ? `${last.memPercent.toFixed(0)}%` : '—'}
        hint={spanMin ? `últimos ${spanMin} min (sessão)` : 'coletando…'}
      >
        <Sparkline values={mem} max={100} stroke="var(--violet, #8b7cff)" fill="rgba(139, 124, 255, 0.12)" />
      </ChartCard>

      <div className="card live-chart" style={{ gridColumn: 'span 2' }}>
        <div className="live-chart-head">
          <div>
            <span className="card-title" style={{ margin: 0 }}>Rede</span>
            <div className="live-chart-now">
              {last ? `↓ ${formatRate(last.rxBytesPerSecond)} · ↑ ${formatRate(last.txBytesPerSecond)}` : '—'}
            </div>
          </div>
          <span className="cell-sub">{spanMin ? `últimos ${spanMin} min (sessão)` : 'coletando…'}</span>
        </div>
        <div className="live-chart-legend">
          <span><i className="dot" style={{ background: '#34d399' }} /> rx (download)</span>
          <span><i className="dot" style={{ background: '#4da3ff' }} /> tx (upload)</span>
        </div>
        <svg viewBox="0 0 100 36" preserveAspectRatio="none" className="spark" role="img" aria-hidden>
          <line x1="0" y1="18" x2="100" y2="18" className="spark-grid" />
          <path d={areaPath(rx, 100, 36, netMax)} fill="rgba(52, 211, 153, 0.10)" />
          <path d={sparklinePath(rx, 100, 36, netMax)} fill="none" stroke="#34d399" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
          <path d={areaPath(tx, 100, 36, netMax)} fill="rgba(77, 163, 255, 0.10)" />
          <path d={sparklinePath(tx, 100, 36, netMax)} fill="none" stroke="#4da3ff" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
        </svg>
      </div>
    </section>
  );
}
