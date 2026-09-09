import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import App from './App';
import Dashboard from './pages/Dashboard';
import VirtualMachines from './pages/VirtualMachines';
import VMDetails from './pages/VMDetails';
import ComingSoon from './pages/ComingSoon';
import Storage from './pages/Storage';
import NetworkPage from './pages/Network';
import './styles.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route element={<App />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/vms" element={<VirtualMachines />} />
          <Route path="/vms/:id" element={<VMDetails />} />
          <Route path="/storage" element={<Storage />} />
          <Route path="/network" element={<NetworkPage />} />
          <Route path="/backups" element={<ComingSoon title="Backups" />} />
          <Route path="/settings" element={<ComingSoon title="Settings" />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
);
