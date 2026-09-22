import { useState } from 'react';
import { api, unwrap, ApiError, type BackupSchedule } from '../api/client';
import { X, Plus, CalendarClock } from 'lucide-react';

const TIME_RE = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

export function CreateScheduleModal({
  open,
  initial,
  onClose,
  onSaved,
}: {
  open: boolean;
  /** When set, the modal edits this schedule instead of creating one. */
  initial?: BackupSchedule | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const editing = !!initial;
  const [name, setName] = useState(initial?.name ?? '');
  const [time, setTime] = useState(initial?.time ?? '03:00');
  const [vmIds, setVmIds] = useState(
    initial ? initial.vmIds.join(', ') : '*',
  );
  const [type, setType] = useState<string>(initial?.type ?? 'auto');
  const [keepLast, setKeepLast] = useState(String(initial?.retentionKeepLast ?? 0));
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!open) return null;

  const targets = vmIds
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  const timeErr = time && !TIME_RE.test(time) ? 'Use HH:MM (00:00–23:59)' : '';
  const targetsErr = targets.length === 0 ? 'Informe ao menos um alvo ou "*"' : '';
  const keepErr =
    keepLast && (!/^\d+$/.test(keepLast) ? 'Deve ser um número ≥ 0' : '');
  const valid = TIME_RE.test(time) && targets.length > 0 && !keepErr;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      if (editing && initial) {
        await unwrap(
          api.PUT('/backup-schedules/{id}', {
            params: { path: { id: initial.id } },
            body: {
              time,
              vmIds: targets,
              type: type as 'auto' | 'full' | 'incremental',
              retentionKeepLast: Number(keepLast || 0),
              enabled,
            },
          }),
        );
      } else {
        await unwrap(
          api.POST('/backup-schedules', {
            body: {
              name,
              time,
              vmIds: targets,
              type: type as 'auto' | 'full' | 'incremental',
              retentionKeepLast: Number(keepLast || 0),
              enabled,
            },
          }),
        );
      }
      onSaved();
      onClose();
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label="Backup schedule">
      <div className="wiz">
        <header className="wiz-head">
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-blue">
              <CalendarClock size={18} className="ic ic-blue" aria-hidden />
            </span>
            <h2>{editing ? `Edit Schedule “${initial?.name}”` : 'New Backup Schedule'}</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            <X size={16} aria-hidden />
          </button>
        </header>

        {error && <div className="alert error">{error}</div>}

        <div className="wiz-form" style={{ padding: 20 }}>
          {!editing && (
            <label className="field">
              <span>Name</span>
              <input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="nightly"
                disabled={submitting}
              />
            </label>
          )}

          <label className="field">
            <span>Run daily at (HH:MM, horário do servidor)</span>
            <input
              value={time}
              onChange={(e) => setTime(e.target.value)}
              placeholder="03:00"
              disabled={submitting}
            />
            {timeErr && <small className="field-error">{timeErr}</small>}
          </label>

          <label className="field">
            <span>Target VMs (nomes separados por vírgula, ou * para todas)</span>
            <input
              value={vmIds}
              onChange={(e) => setVmIds(e.target.value)}
              placeholder="*"
              disabled={submitting}
            />
            {targetsErr && <small className="field-error">{targetsErr}</small>}
          </label>

          <label className="field">
            <span>Point type</span>
            <select value={type} onChange={(e) => setType(e.target.value)} disabled={submitting}>
              <option value="auto">auto (incremental quando houver cadeia)</option>
              <option value="full">full</option>
              <option value="incremental">incremental</option>
            </select>
          </label>

          <label className="field">
            <span>Retention — keep last N full chains (0 = manter tudo)</span>
            <input
              value={keepLast}
              onChange={(e) => setKeepLast(e.target.value)}
              inputMode="numeric"
              disabled={submitting}
            />
            {keepErr && <small className="field-error">{keepErr}</small>}
          </label>

          <label className="field field-inline">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} disabled={submitting} />
            <span>Enabled</span>
          </label>
        </div>

        <footer className="wiz-foot">
          <span />
          <div className="wiz-foot-right">
            <button className="btn" onClick={onClose} disabled={submitting}>
              Cancel
            </button>
            <button className="btn btn-primary" onClick={() => void submit()} disabled={submitting || !valid}>
              <Plus size={14} aria-hidden /> {editing ? 'Save' : 'Create Schedule'}
            </button>
          </div>
        </footer>
      </div>
    </div>
  );
}
