import { useState, type CSSProperties, type FormEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../lib/auth';
import {
  ArrowRight,
  Eye,
  EyeOff,
  LockKeyhole,
  Server,
  ShieldCheck,
  UserRound,
} from 'lucide-react';
import wallpaper from '../assets/loginwallpaper.png';

export default function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(username.trim(), password);
      // Redirect to the page that triggered the login (or the dashboard).
      const from = (location.state as { from?: string } | null)?.from ?? '/';
      navigate(from, { replace: true });
    } catch (err) {
      setError(
        err instanceof Error && /credentials/i.test(err.message)
          ? 'Invalid username or password.'
          : 'Unable to sign in. Check that the backend is available.',
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-page" style={{ '--login-wallpaper': `url(${wallpaper})` } as CSSProperties}>
      <section className="login-intro" aria-label="UltraV overview">
        <div className="login-wordmark">
          <span className="brand-mark">UV</span>
          <span>UltraV</span>
        </div>
        <div className="login-intro-copy">
          <span className="login-eyebrow">Virtual infrastructure, under control</span>
          <h1>One clear view of every virtual machine.</h1>
          <p>Operate compute, storage, and networks from a focused management console.</p>
        </div>
        <div className="login-system-note">
          <span className="login-status-dot" aria-hidden />
          Management console
        </div>
      </section>

      <section className="login-panel">
        <form className="login-card" onSubmit={onSubmit}>
          <div className="login-card-heading">
            <span className="login-card-icon" aria-hidden><ShieldCheck size={21} /></span>
            <div>
              <span className="login-kicker">Secure access</span>
              <h2>Sign in to UltraV</h2>
            </div>
          </div>

          <p className="login-welcome">Enter your credentials to open the management console.</p>

          {error && (
            <div className="login-error" role="alert">
              {error}
            </div>
          )}

          <div className="login-field">
            <label htmlFor="login-username">Username</label>
            <span className="login-input-wrap">
              <UserRound size={17} aria-hidden />
              <input
                id="login-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                autoFocus
                placeholder="Enter your username"
                disabled={busy}
                required
              />
            </span>
          </div>

          <div className="login-field">
            <label htmlFor="login-password">Password</label>
            <span className="login-input-wrap">
              <LockKeyhole size={17} aria-hidden />
              <input
                id="login-password"
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                placeholder="Enter your password"
                disabled={busy}
                required
              />
              <button
                className="login-password-toggle"
                type="button"
                onClick={() => setShowPassword((visible) => !visible)}
                aria-label={showPassword ? 'Hide password' : 'Show password'}
                title={showPassword ? 'Hide password' : 'Show password'}
              >
                {showPassword ? <EyeOff size={17} /> : <Eye size={17} />}
              </button>
            </span>
          </div>

          <button className="login-submit" disabled={busy}>
            <span>{busy ? 'Signing in…' : 'Sign in'}</span>
            {!busy && <ArrowRight size={17} aria-hidden />}
          </button>

          <p className="login-hint">
            <Server size={13} aria-hidden />
            Your administrator manages access to this console.
          </p>
        </form>

        <p className="login-footer">UltraV · Virtualization Management Platform</p>
      </section>
    </main>
  );
}
