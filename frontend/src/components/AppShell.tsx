import { useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { api, unwrap, type Host } from '../api/client';
import { formatBytes, usePolling } from '../lib/hooks';
import { ToastProvider } from '../components/Toast';

/* ---------- icons (Lucide, unified icon system) ---------- */

import {
  LayoutDashboard,
  Monitor,
  HardDrive,
  Network,
  DatabaseBackup,
  ListChecks,
  Settings,
  Search,
  Bell,
  Server,
  BookOpen,
  type LucideIcon,
} from 'lucide-react';

const navIcon = (Icon: LucideIcon) => <Icon size={16} strokeWidth={1.75} aria-hidden />;

/* ---------- navigation ---------- */

const nav = [
  { to: '/', label: 'Dashboard', icon: navIcon(LayoutDashboard), end: true },
  { to: '/vms', label: 'Virtual Machines', icon: navIcon(Monitor), end: false },
  { to: '/storage', label: 'Storage', icon: navIcon(HardDrive), end: false },
  { to: '/network', label: 'Network', icon: navIcon(Network), end: false },
  { to: '/backups', label: 'Backups', icon: navIcon(DatabaseBackup), end: false, soon: true },
  { to: '/tasks', label: 'Tasks', icon: navIcon(ListChecks), end: false, soon: true },
  { to: '/settings', label: 'Settings', icon: navIcon(Settings), end: false, soon: true },
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
        <span className="host-name-id">
          <Server size={13} strokeWidth={1.75} aria-hidden />
          {host.hostname}
        </span>
        <span className={`state${online ? ' state-running' : ' state-error'}`}>
          <span className="state-dot" />
          {online ? 'Online' : 'Offline'}
        </span>
      </div>
      <div className="host-row host-sub">
        {kvm == null ? 'hypervisor' : kvm ? 'KVM' : 'no KVM'} · {formatBytes(host.memoryTotalBytes, 0)} ·{' '}
        {vmCount == null ? '—' : `${vmCount} VM${vmCount === 1 ? '' : 's'}`}
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
        <Search size={14} strokeWidth={1.75} aria-hidden />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search virtual machines…"
          aria-label="Search virtual machines"
        />
      </form>
      <div className="topbar-right">
        <button className="icon-btn" title="Notifications (coming in a later phase)" disabled>
          <Bell size={15} strokeWidth={1.75} aria-hidden />
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
              <BookOpen size={13} strokeWidth={1.75} aria-hidden /> API Docs
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
