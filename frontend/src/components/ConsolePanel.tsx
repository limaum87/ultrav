import { useEffect, useRef, useState } from 'react';
import RFB from '@novnc/novnc';
import type { VirtualMachine } from '../api/client';
import { getToken } from '../api/client';
import { StateBadge } from './ui';
import { Monitor } from 'lucide-react';

/**
 * VNC console panel (noVNC) backed by the WebSocket proxy at
 * /api/v1/vms/{id}/console. Reconnects when the VM state changes so a
 * freshly started VM gets a live console without a manual reload.
 */
export function ConsolePanel({ vm }: { vm: VirtualMachine }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected'>('disconnected');
  const [unavailable, setUnavailable] = useState(false);
  const [retry, setRetry] = useState(0);
  const running = vm.state === 'running';

  // Preflight: a failed WS handshake gives noVNC no error details, so ask the
  // endpoint first to distinguish "no graphics device" from transient errors.
  useEffect(() => {
    if (!running) return;
    let cancelled = false;
    const token = getToken();
    const q = token ? `?token=${encodeURIComponent(token)}` : '';
    fetch(`/api/v1/vms/${encodeURIComponent(vm.id)}/console${q}`, {
      headers: { Connection: 'Upgrade', Upgrade: 'websocket' },
    })
      .then(async (res) => {
        if (res.status === 409) {
          const body = await res.json().catch(() => null);
          if (body?.error?.code === 'CONSOLE_UNAVAILABLE' && !cancelled) setUnavailable(true);
        } else if (!cancelled) setUnavailable(false);
      })
      // A 101 upgrade makes fetch() throw — that means the endpoint is alive.
      .catch(() => { if (!cancelled) setUnavailable(false); });
    return () => { cancelled = true; };
  }, [vm.id, running]);

  useEffect(() => {
    if (!running || unavailable) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    // The auth middleware accepts ?token= because browsers cannot set
    // Authorization headers on a WebSocket handshake.
    const token = getToken();
    const q = token ? `?token=${encodeURIComponent(token)}` : '';
    const url = `${proto}://${location.host}/api/v1/vms/${encodeURIComponent(vm.id)}/console${q}`;
    setStatus('connecting');

    let disposed = false;
    let rfb: RFB | null = null;
    const timer = setTimeout(() => {
      if (disposed || !containerRef.current) return;
      rfb = new RFB(containerRef.current, url, {
        credentials: { password: undefined },
      });
      rfb.scaleViewport = true;
      rfb.resizeSession = false;
      rfb.addEventListener('connect', () => setStatus('connected'));
      rfb.addEventListener('disconnect', (e: Event & { detail?: { clean: boolean } }) => {
        setStatus('disconnected');
        if (!e.detail?.clean) {
          // The proxy may not be ready right after VM start; retry shortly.
          setTimeout(() => setRetry((n) => n + 1), 3000);
        }
      });
      rfbRef.current = rfb;
    }, 50);

    return () => {
      disposed = true;
      clearTimeout(timer);
      rfb?.disconnect();
      rfbRef.current = null;
    };
  }, [vm.id, running, retry, unavailable]);

  if (unavailable) {
    return (
      <div className="card console-status">
        <Monitor size={24} strokeWidth={1.75} aria-hidden />
        <p>
          <strong>{vm.name}</strong> has no graphical console configured. Add a VNC
          <code> &lt;graphics&gt;</code> device to its domain XML and restart the VM — VMs created by
          UltraV from now on include one automatically.
        </p>
      </div>
    );
  }

  if (!running) {
    return (
      <div className="card console-status">
        <Monitor size={24} strokeWidth={1.75} aria-hidden />
        <p>
          <strong>{vm.name}</strong> is <StateBadge state={vm.state} /> — the console is only
          available while the VM is running. Start the VM to get a graphical console.
        </p>
      </div>
    );
  }

  return (
    <div className="card console-card">
      <div className="console-toolbar">
        <span className={`console-dot console-dot-${status}`} />
        {status === 'connecting' ? 'Connecting to console…' : status === 'connected' ? 'Console connected' : 'Console disconnected — retrying…'}
      </div>
      <div ref={containerRef} className="console-screen" />
    </div>
  );
}
