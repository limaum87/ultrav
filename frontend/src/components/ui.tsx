import type { VirtualMachine } from '../api/client';
import { formatUptime } from '../lib/hooks';

export function StateBadge({ state }: { state: VirtualMachine['state'] }) {
  return (
    <span className={`state state-${state}`}>
      <span className="state-dot" />
      {state}
    </span>
  );
}

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
