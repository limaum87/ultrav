import { useState, type FormEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../lib/auth';
import { Server } from 'lucide-react';

export default function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
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
          ? 'Usuário ou senha inválidos'
          : 'Falha ao autenticar. Verifique se o backend está acessível.',
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <div className="login-brand">
          <span className="brand-mark">UV</span>
          <div>
            <h1>UltraV</h1>
            <p>Virtualization Manager</p>
          </div>
        </div>
        {error && (
          <div className="login-error" role="alert">
            {error}
          </div>
        )}
        <label className="field">
          <span>Usuário</span>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
        </label>
        <label className="field">
          <span>Senha</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        <button className="btn btn-primary login-submit" disabled={busy}>
          {busy ? 'Entrando…' : 'Entrar'}
        </button>
        <p className="login-hint">
          <Server size={12} aria-hidden /> Acesso local — o usuário inicial é criado pelo backend.
        </p>
      </form>
    </div>
  );
}
