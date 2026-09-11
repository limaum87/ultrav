import { useCallback, useEffect, useState } from 'react';
import { api, unwrap, ApiError, type User } from '../api/client';
import { useAuth } from '../lib/auth';
import { EmptyState } from '../components/ui';
import {
  Users as UsersIcon,
  UserPlus,
  KeyRound,
  Trash2,
  ShieldCheck,
  Eye,
  X,
  Plus,
  Check,
} from 'lucide-react';

const MIN_PASSWORD = 8;

const errText = (e: unknown) => (e instanceof ApiError ? e.message : String(e));

function formatDate(iso: string) {
  try {
    return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  } catch {
    return iso;
  }
}

/* ---------- create user modal ---------- */

function CreateUserModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<'admin' | 'viewer'>('viewer');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!open) return null;

  const valid = username.trim().length > 0 && password.length >= MIN_PASSWORD;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await unwrap(api.POST('/users', { body: { username: username.trim(), password, role } }));
      onCreated();
      onClose();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label="Create user">
      <div className="wiz" style={{ maxWidth: 460 }}>
        <header className="wiz-head">
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-blue">
              <UserPlus size={18} className="ic ic-blue" aria-hidden />
            </span>
            <h2>Create user</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            <X size={16} aria-hidden />
          </button>
        </header>
        {error && <div className="alert error">{error}</div>}
        <div className="wiz-form" style={{ padding: 20 }}>
          <label className="field">
            <span>Username</span>
            <input
              autoFocus
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="alice"
              disabled={submitting}
            />
          </label>
          <label className="field">
            <span>Password (min. {MIN_PASSWORD} characters)</span>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              disabled={submitting}
            />
          </label>
          <label className="field">
            <span>Role</span>
            <select value={role} onChange={(e) => setRole(e.target.value as 'admin' | 'viewer')} disabled={submitting}>
              <option value="viewer">Viewer — read-only access</option>
              <option value="admin">Admin — full access incl. user management</option>
            </select>
          </label>
        </div>
        <footer className="wiz-foot">
          <button className="btn" onClick={onClose} disabled={submitting}>
            Cancel
          </button>
          <button className="btn btn-primary btn-with-icon" onClick={() => void submit()} disabled={!valid || submitting}>
            <Plus size={14} strokeWidth={2} aria-hidden />
            Create user
          </button>
        </footer>
      </div>
    </div>
  );
}

/* ---------- reset password modal ---------- */

function ResetPasswordModal({
  user,
  onClose,
  onReset,
}: {
  user: User | null;
  onClose: () => void;
  onReset: () => void;
}) {
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setPassword('');
    setError(null);
  }, [user]);

  if (!user) return null;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await unwrap(api.PUT('/users/{id}/password', { params: { path: { id: user.id } }, body: { password } }));
      onReset();
      onClose();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label={`Reset password for ${user.username}`}>
      <div className="wiz" style={{ maxWidth: 460 }}>
        <header className="wiz-head">
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-blue">
              <KeyRound size={18} className="ic ic-blue" aria-hidden />
            </span>
            <h2>Reset password — {user.username}</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            <X size={16} aria-hidden />
          </button>
        </header>
        {error && <div className="alert error">{error}</div>}
        <div className="wiz-form" style={{ padding: 20 }}>
          <label className="field">
            <span>New password (min. {MIN_PASSWORD} characters)</span>
            <input
              autoFocus
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              disabled={submitting}
            />
          </label>
          <p className="wiz-note" style={{ margin: 0, fontSize: 13, opacity: 0.7 }}>
            The user's current password stops working immediately after the reset. Active sessions remain valid until
            their token expires.
          </p>
        </div>
        <footer className="wiz-foot">
          <button className="btn" onClick={onClose} disabled={submitting}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={() => void submit()}
            disabled={password.length < MIN_PASSWORD || submitting}
          >
            Reset password
          </button>
        </footer>
      </div>
    </div>
  );
}

/* ---------- confirm delete ---------- */

function ConfirmDelete({ user, onConfirm, onClose, busy }: { user: User | null; onConfirm: () => void; onClose: () => void; busy: boolean }) {
  if (!user) return null;
  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label={`Delete user ${user.username}`}>
      <div className="wiz" style={{ maxWidth: 420 }}>
        <header className="wiz-head">
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-red">
              <Trash2 size={18} className="ic ic-red" aria-hidden />
            </span>
            <h2>Delete user</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={busy}>
            <X size={16} aria-hidden />
          </button>
        </header>
        <div style={{ padding: 20, fontSize: 14 }}>
          Remove user <strong>{user.username}</strong> ({user.role})? This cannot be undone.
        </div>
        <footer className="wiz-foot">
          <button className="btn" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="btn btn-danger" onClick={onConfirm} disabled={busy}>
            {busy ? 'Deleting…' : 'Delete user'}
          </button>
        </footer>
      </div>
    </div>
  );
}

/* ---------- users panel (admin) ---------- */

function UsersPanel({ me }: { me: User }) {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [resetTarget, setResetTarget] = useState<User | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const data = await unwrap(api.GET('/users'));
      setUsers(data.items ?? []);
      setError(null);
    } catch (e) {
      setError(errText(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const doDelete = async () => {
    if (!deleteTarget) return;
    setBusy(true);
    try {
      await unwrap(api.DELETE('/users/{id}', { params: { path: { id: deleteTarget.id } } }));
      setDeleteTarget(null);
      await load();
    } catch (e) {
      setError(errText(e));
      setDeleteTarget(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="card table-card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '14px 16px 0' }}>
          <h2 className="card-title" style={{ margin: 0 }}>
            Local users
          </h2>
          <button className="btn btn-primary btn-with-icon" onClick={() => setShowCreate(true)}>
            <UserPlus size={14} strokeWidth={2} aria-hidden />
            Add user
          </button>
        </div>
        {error && (
          <div className="alert error" style={{ margin: '12px 16px 0' }}>
            {error}
          </div>
        )}
        {loading ? (
          <div style={{ padding: 24 }}>
            <EmptyState title="Loading…" message="" />
          </div>
        ) : users.length === 0 ? (
          <EmptyState title="No users" message="Add a local user to grant access to UltraV." />
        ) : (
          <table className="table" style={{ marginTop: 8 }}>
            <thead>
              <tr>
                <th>Username</th>
                <th>Role</th>
                <th>Created</th>
                <th className="th-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td className="vm-link">
                    {u.username}
                    {u.id === me.id && <span className="cell-sub"> (you)</span>}
                  </td>
                  <td>
                    <span className={`state${u.role === 'admin' ? ' state-running' : ''}`}>
                      {u.role === 'admin' ? (
                        <ShieldCheck size={12} strokeWidth={2} aria-hidden />
                      ) : (
                        <Eye size={12} strokeWidth={2} aria-hidden />
                      )}
                      {u.role === 'admin' ? 'admin' : 'viewer'}
                    </span>
                  </td>
                  <td className="cell-dim">{formatDate(u.createdAt)}</td>
                  <td className="td-actions">
                    <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                      <button
                        className="btn actions-md-btn"
                        title="Reset password"
                        onClick={() => setResetTarget(u)}
                      >
                        <KeyRound size={13} strokeWidth={1.75} aria-hidden /> Reset password
                      </button>
                      <button
                        className="btn actions-md-btn btn-danger-ghost"
                        title={u.id === me.id ? 'You cannot delete your own user' : `Delete ${u.username}`}
                        disabled={u.id === me.id}
                        onClick={() => setDeleteTarget(u)}
                      >
                        <Trash2 size={13} strokeWidth={1.75} aria-hidden /> Remove
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <CreateUserModal open={showCreate} onClose={() => setShowCreate(false)} onCreated={() => void load()} />
      <ResetPasswordModal user={resetTarget} onClose={() => setResetTarget(null)} onReset={() => void load()} />
      <ConfirmDelete user={deleteTarget} onConfirm={() => void doDelete()} onClose={() => setDeleteTarget(null)} busy={busy} />
    </>
  );
}

/* ---------- account panel (own password) ---------- */

function AccountPanel({ me }: { me: User }) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const valid = current.length > 0 && next.length >= MIN_PASSWORD && next === confirm;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    setDone(false);
    try {
      await unwrap(api.PUT('/auth/password', { body: { currentPassword: current, newPassword: next } }));
      setDone(true);
      setCurrent('');
      setNext('');
      setConfirm('');
    } catch (e) {
      setError(errText(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 520, padding: 20 }}>
      <h2 className="card-title">Change my password</h2>
      <p className="cell-sub" style={{ margin: '0 0 14px' }}>
        Signed in as <strong>{me.username}</strong> ({me.role}).
      </p>
      {error && <div className="alert error">{error}</div>}
      {done && (
        <div className="alert success" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <Check size={14} aria-hidden /> Password updated.
        </div>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) void submit();
        }}
      >
        <label className="field">
          <span>Current password</span>
          <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} disabled={submitting} />
        </label>
        <label className="field">
          <span>New password (min. {MIN_PASSWORD} characters)</span>
          <input type="password" value={next} onChange={(e) => setNext(e.target.value)} disabled={submitting} />
        </label>
        <label className="field">
          <span>Confirm new password</span>
          <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} disabled={submitting} />
          {confirm && next !== confirm && <small className="field-error">Passwords do not match</small>}
        </label>
        <button className="btn btn-primary" type="submit" disabled={!valid || submitting}>
          {submitting ? 'Saving…' : 'Change password'}
        </button>
      </form>
    </div>
  );
}

/* ---------- page ---------- */

export default function Settings() {
  const { user } = useAuth();
  const [tab, setTab] = useState<'users' | 'account'>('users');
  const isAdmin = user?.role === 'admin';

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Settings</h1>
          <p className="subtitle">User management and account preferences</p>
        </div>
      </header>

      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        {isAdmin && (
          <button className={`btn${tab === 'users' ? ' btn-primary' : ''}`} onClick={() => setTab('users')}>
            <UsersIcon size={14} strokeWidth={1.75} aria-hidden style={{ marginRight: 6 }} />
            Users
          </button>
        )}
        <button className={`btn${tab === 'account' || !isAdmin ? ' btn-primary' : ''}`} onClick={() => setTab('account')}>
          Account
        </button>
      </div>

      {tab === 'users' && isAdmin && user ? <UsersPanel me={user} /> : <AccountPanel me={user!} />}
    </div>
  );
}
