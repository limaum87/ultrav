import AppShell from './components/AppShell';
import { useAuth } from './lib/auth';
import { Navigate, useLocation } from 'react-router-dom';

/** Layout guard: renders the app shell only for authenticated users. */
export default function App() {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      <div className="login-page login-loading">
        <div className="login-loading-card">
          <span className="brand-mark">UV</span>
          <p>Loading…</p>
        </div>
      </div>
    );
  }

  if (!user) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }

  return <AppShell />;
}
