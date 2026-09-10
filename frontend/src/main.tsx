import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import App from './App';
import { AuthProvider } from './lib/auth';
import Login from './pages/Login';
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
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<App />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/vms" element={<VirtualMachines />} />
            <Route path="/vms/:id" element={<VMDetails />} />
            <Route path="/storage" element={<Storage />} />
            <Route path="/network" element={<NetworkPage />} />
            <Route path="/backups" element={<ComingSoon title="Backups" />} />
            <Route path="/tasks" element={<ComingSoon title="Tasks" />} />
            <Route path="/settings" element={<ComingSoon title="Settings" />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
);
