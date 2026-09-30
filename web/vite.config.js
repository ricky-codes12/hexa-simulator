import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

const apiTarget = process.env.VITE_API_TARGET || 'http://127.0.0.1:8080';
const webPort = Number(process.env.VITE_PORT || 5173);

export default defineConfig({
  plugins: [svelte()],
  // MapLibre starts its worker with { type: 'module' }.
  worker: { format: 'es' },
  server: {
    host: '127.0.0.1',
    port: webPort,
    strictPort: true,
    proxy: { '/api': apiTarget, '/healthz': apiTarget }
  }
});
