import { useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { api, unwrap, type Host } from '../api/client';
import { formatBytes, usePolling } from '../lib/hooks';
import { ToastProvider } from '../components/Toast';

/* ---------- icons (inline SVG, 16px grid) ---------- */

const I = {
  dashboard: 'M2 2h5v5H2zM9 2h5v3H9zM9 7h5v7H9zM2 9h5v5H2z',
  vm: 'M1.5 3h13v8h-13zM6 13h4M8 11v2',
  storage: 'M2 4h12v3H2zm0 5h12v3H2z',
  network: 'M8 1.5 14 5v6L8 14.5 2 11V5zM8 8.5 14 5M8 8.5 2 5M8 8.5v6',
  backup: 'M3 2h10v12H3zM6 5h4M6 8h4M6 11h4',
  tasks: 'M3 4.5 5 6.5 8.5 3M3 10.5 5 12.5 8.5 9M11 5h2M11 11h2',
  settings:
    'M8 5.8A2.2 2.2 0 1 0 8 10.2 2.2 2.2 0 0 0 8 5.8zM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M12.6 3.4l-1.4 1.4M4.8 11.2l-1.4 1.4',
};

const stroke = (d: string) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden
  >
    <path d={d} />
  </svg>
);

/* ---------- navigation ---------- */

const nav = [
  { to: '/', label: 'Dashboard', icon: stroke(I.dashboard), end: true },
  { to: '/vms', label: 'Virtual Machines', icon: stroke(I.vm), end: false },
  { to: '/storage', label: 'Storage', icon: stroke(I.storage), end: false },
  { to: '/network', label: 'Network', icon: stroke(I.network), end: false },
  { to: '/backups', label: 'Backups', icon: stroke(I.backup), end: false, soon: true },
  { to: '/tasks', label: 'Tasks', icon: stroke(I.tasks), end: false, soon: true },
  { to: '/settings', label: 'Settings', icon: stroke(I.settings), end: false, soon: true },
];

/* ---------- host status card (bottom of sidebar) ---------- */

function HostStatusCard({ host, vmCount, online }: { host: Host | null; vmCount: number | null; online: boolean }) {
  if (!host) {
    return (
      <div className="host-card">
        <div className="host-row">
          <span className="skeleton" style={{ width: '60%' }} />
        </div>
        <div className="host-row">
          <span className="skeleton" style={{ width: '85%' }} />
        </div>
      </div>
    );
  }
  const kvm = host.virtualization?.kvmEnabled;
  return (
    <div className="host-card" title={`${host.operatingSystem} · kernel ${host.kernel}`}>
      <div className="host-row host-name">
        <span>{host.hostname}</span>
        <span className={`state${online ? ' state-running' : ' state-error'}`}>
          <span className="state-dot" />
          {online ? 'Online' : 'Offline'}
        </span>
      </div>
      <div className="host-row host-sub">
        {kvm == null ? 'hypervisor' : kvm ? 'KVM' : 'no KVM'} · {formatBytes(host.memoryTotalBytes, 0)} ·{' '}
        {vmCount ?? '—'} VMs
      </div>
    </div>
  );
}

/* ---------- topbar ---------- */

function Topbar() {
  const navigate = useNavigate();
  const [q, setQ] = useState('');
  return (
    <header className="topbar">
      <form
        className="global-search"
        onSubmit={(e) => {
          e.preventDefault();
          const term = q.trim();
          navigate(term ? `/vms?q=${encodeURIComponent(term)}` : '/vms');
        }}
        role="search"
      >
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
          <circle cx="7" cy="7" r="4.5" />
          <path d="m10.5 10.5 3 3" strokeLinecap="round" />
        </svg>
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search virtual machines…"
          aria-label="Search virtual machines"
        />
      </form>
      <div className="topbar-right">
        <button className="icon-btn" title="Notifications (coming in a later phase)" disabled>
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" aria-hidden>
            <path d="M4 6.5a4 4 0 0 1 8 0c0 3 1 4 1 4H3s1-1 1-4M6.5 13a1.5 1.5 0 0 0 3 0" />
          </svg>
        </button>
        <div className="user-chip" title="Local administration (no authentication in this phase)">
          <span className="user-avatar">A</span>
          <span className="user-name">admin</span>
        </div>
      </div>
    </header>
  );
}

/* ---------- shell ---------- */

export default function AppShell() {
  // Sidebar host card polls slowly; pages poll their own data faster.
  const { data: hostData, error } = usePolling(
    async () => {
      const host = await unwrap(api.GET('/host'));
      const vms = await unwrap(api.GET('/vms'));
      return { host, vms } satisfies { host: Host; vms: { total: number } };
    },
    15000,
  );

  return (
    <ToastProvider>
      <div className="shell">
        <aside className="sidebar">
          <div className="brand">
            <span className="brand-mark">UV</span>
            <span className="brand-text">
              <span className="brand-name">UltraV</span>
              <span className="brand-tag">Virtualization Manager</span>
            </span>
          </div>
          <nav>
            {nav.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
              >
                <span className="nav-icon">{item.icon}</span>
                <span>{item.label}</span>
                {item.soon && <span className="badge-soon">soon</span>}
              </NavLink>
            ))}
          </nav>
          <div className="sidebar-footer">
            <HostStatusCard
              host={hostData?.host ?? null}
              vmCount={hostData?.vms.total ?? null}
              online={!error && !!hostData}
            />
            <a href="/docs" target="_blank" rel="noreferrer" className="docs-link">
              API Docs ↗
            </a>
          </div>
        </aside>
        <div className="main">
          <Topbar />
          <main className="content">
            <Outlet />
          </main>
        </div>
      </div>
    </ToastProvider>
  );
}
