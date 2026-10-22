import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { writeFileSync } from 'node:fs';
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'embed-placeholder',
      apply: 'build',
      closeBundle() {
        writeFileSync(
          new URL('./dist/.gitkeep', import.meta.url),
          'Build assets with pnpm --dir web run build.\n',
        );
      },
    },
  ],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      '/mcp': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    css: false,
    coverage: {
      provider: 'v8',
      thresholds: { statements: 75, branches: 70, functions: 65, lines: 75 },
      reporter: ['text', 'json-summary', 'lcov'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.*',
        'src/test-setup.ts',
        'src/features/test-helpers.tsx',
        'src/types/**',
      ],
    },
  },
  build: { emptyOutDir: true, chunkSizeWarningLimit: 1100 },
});
