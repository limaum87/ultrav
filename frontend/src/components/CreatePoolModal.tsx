import { useState } from 'react';
import { api, unwrap, ApiError } from '../api/client';
import { Database, X, Plus } from 'lucide-react';

const NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;
const PATH_RE = /^\/[a-zA-Z0-9._/-]{0,254}$/;

export function CreatePoolModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [targetPath, setTargetPath] = useState('');
  const [autostart, setAutostart] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!open) return null;

  const nameErr = name && !NAME_RE.test(name)
    ? 'Name must start with a letter/digit (a-z0-9._-, max 64 chars)'
    : '';
  const pathErr = targetPath && (!PATH_RE.test(targetPath) || targetPath.includes('..'))
    ? 'Path must be absolute and cannot contain ".."'
    : '';
  const valid = NAME_RE.test(name) && PATH_RE.test(targetPath) && !targetPath.includes('..');

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await unwrap(
        api.POST('/storage/pools', {
          body: { name, type: 'dir', targetPath, autostart },
        }),
      );
      onCreated();
      onClose();
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label="Create storage pool">
      <div className="wiz">
        <header className="wiz-head">
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-blue">
              <Database size={18} className="ic ic-blue" aria-hidden />
            </span>
            <h2>Create Storage Pool</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            <X size={16} aria-hidden />
          </button>
        </header>

        {error && <div className="alert error">{error}</div>}

        <div className="wiz-form" style={{ padding: 20 }}>
          <label className="field">
            <span>Name</span>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="iso-store"
              disabled={submitting}
            />
            {nameErr && <small className="field-error">{nameErr}</small>}
          </label>
          <label className="field">
            <span>Target path (absolute, created if missing)</span>
            <input
              value={targetPath}
              onChange={(e) => setTargetPath(e.target.value)}
              placeholder="/var/lib/libvirt/isos"
              disabled={submitting}
              className="mono"
            />
            {pathErr && <small className="field-error">{pathErr}</small>}
          </label>
          <label className="field field-check">
            <input
              type="checkbox"
              checked={autostart}
              onChange={(e) => setAutostart(e.target.checked)}
              disabled={submitting}
            />
            <span>Start pool automatically on boot (autostart)</span>
          </label>
          <p className="wiz-note" style={{ margin: 0, fontSize: 13, opacity: 0.7 }}>
            Only directory-based pools (type <code>dir</code>) are supported for now.
          </p>
        </div>

        <footer className="wiz-foot">
          <button className="btn" onClick={onClose} disabled={submitting}>
            Cancel
          </button>
          <button className="btn btn-primary btn-with-icon" onClick={() => void submit()} disabled={!valid || submitting}>
            <Plus size={14} strokeWidth={2} aria-hidden />
            {submitting ? 'Creating…' : 'Create Pool'}
          </button>
        </footer>
      </div>
    </div>
  );
}
