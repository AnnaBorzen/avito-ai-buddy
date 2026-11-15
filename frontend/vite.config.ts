import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';
import { apiMiddleware, makeApi } from './server/http.mjs';
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'VITE_');
  function connect(server: any) {
    if (env.VITE_USE_MOCK_API === 'false') return;
    const db = makeApi();
    server.middlewares.use(apiMiddleware(db));
    server.httpServer?.once('close', () => db.close());
  }
  return {
    plugins: [
      react(),
      {
        name: 'buddy-demo-service',
        configureServer: connect,
        configurePreviewServer: connect,
      },
    ],
    test: {
      include: ['tests/**/*.test.{mjs,ts,tsx}'],
      globals: true,
      environment: 'jsdom',
      pool: 'threads',
      minWorkers: 1,
      maxWorkers: 1,
      coverage: {
        provider: 'v8',
        all: true,
        include: ['src/**/*.{ts,tsx}', 'server/**/*.mjs'],
        exclude: ['src/**/*.d.ts'],
        reportsDirectory: fileURLToPath(new URL('./coverage/',import.meta.url)),
        reporter: ['text', 'html', 'json-summary', 'lcov'],
        thresholds: { lines: 81 },
      },
    },
  };
});
