import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// В Docker-сети backend доступен как http://backend:8080,
// при локальном запуске — http://localhost:8080.
const proxyTarget = process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173,
    strictPort: true,
    proxy: {
      '/healthz': proxyTarget,
      '/readyz': proxyTarget,
      '/api': proxyTarget,
    },
  },
})
