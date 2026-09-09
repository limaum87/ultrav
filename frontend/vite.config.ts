import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev server proxies API + contract endpoints to the Go backend.
export default defineConfig({
  plugins: [react()],
  // noVNC 1.7 uses top-level await; raise the build target accordingly.
  build: { target: 'es2022' },
  server: {
    port: 5173,
    proxy: {
      // WebSocket (VM console) needs upgrade headers in the dev proxy too.
      '/api': { target: 'http://localhost:8080', ws: true },
      '/openapi.json': 'http://localhost:8080',
      '/docs': 'http://localhost:8080',
    },
  },
});
