import AppShell from './components/AppShell';
import { useAuth } from './lib/auth';
import { Navigate, useLocation } from 'react-router-dom';

/** Layout guard: renders the app shell only for authenticated users. */
export default function App() {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      <div className="login-page">
        <div className="login-card" style={{ textAlign: 'center' }}>
          <span className="brand-mark">UV</span>
          <p>Carregando…</p>
        </div>
      </div>
    );
  }

  if (!user) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }

  return <AppShell />;
}
