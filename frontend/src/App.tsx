import { NavLink, Outlet } from 'react-router-dom';

const nav = [
  { to: '/', label: 'Dashboard', icon: '▸', end: true },
  { to: '/vms', label: 'Virtual Machines', icon: '▣', end: false },
  { to: '/storage', label: 'Storage', icon: '▤', end: false },
  { to: '/network', label: 'Network', icon: '◈', end: false },
  { to: '/backups', label: 'Backups', icon: '◍', end: false, soon: true },
  { to: '/settings', label: 'Settings', icon: '⚙', end: false, soon: true },
];

export default function App() {
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">◢</span>
          <span className="brand-name">UltraV</span>
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
          <a href="/docs" target="_blank" rel="noreferrer" className="docs-link">
            API Docs ↗
          </a>
        </div>
      </aside>
      <main className="content">
        <Outlet />
      </main>
    </div>
  );
}
