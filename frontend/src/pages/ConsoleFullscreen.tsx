import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { api, unwrap } from '../api/client';
import { usePolling } from '../lib/hooks';
import { useAuth } from '../lib/auth';
import { ConsolePanel } from '../components/ConsolePanel';
import { TableSkeleton } from '../components/ui';

/**
 * Fullscreen VNC console, opened in its own browser tab (no app shell).
 * Route lives outside the AppShell layout so only a slim toolbar and the
 * console fill the viewport.
 */
export default function ConsoleFullscreen() {
  const { id = '' } = useParams();
  const { user, loading: authLoading } = useAuth();
  const { data: vm, error, loading } = usePolling(async () =>
    unwrap(api.GET('/vms/{id}', { params: { path: { id } } })),
  );

  if (authLoading || (loading && !vm)) {
    return (
      <div className="console-fullscreen">
        <div className="console-full-body">
          <TableSkeleton rows={3} cols={3} />
        </div>
      </div>
    );
  }
  if (!user) {
    return (
      <div className="console-fullscreen">
        <div className="console-full-body console-full-center">Sessão expirada — volte à aba principal e faça login novamente.</div>
      </div>
    );
  }
  if (error || !vm) {
    return (
      <div className="console-fullscreen">
        <div className="console-full-body console-full-center">Failed to load VM: {error ?? 'unknown error'}</div>
      </div>
    );
  }

  return (
    <div className="console-fullscreen">
      <div className="console-full-toolbar">
        <Link to={`/vms/${vm.id}`} className="btn btn-with-icon console-full-back" title="Back to VM details">
          <ArrowLeft size={14} strokeWidth={2} aria-hidden /> Back
        </Link>
        <span className="console-full-title">{vm.name}</span>
      </div>
      <div className="console-full-body">
        <ConsolePanel vm={vm} />
      </div>
    </div>
  );
}
