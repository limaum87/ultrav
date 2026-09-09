import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev server proxies API + contract endpoints to the Go backend.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/openapi.json': 'http://localhost:8080',
      '/docs': 'http://localhost:8080',
    },
  },
});
