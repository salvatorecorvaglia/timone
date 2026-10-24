import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { gzipSync } from 'node:zlib';
import { fileURLToPath } from 'node:url';
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'embedded-assets',
      apply: 'build',
      writeBundle(options, bundle) {
        // Read final files after Vite's CSS and preload transformations.
        for (const fileName of Object.keys(bundle)) {
          if (!fileName.startsWith('assets/') || !/\.(js|css)$/.test(fileName)) continue;
          const destination = resolve(
            options.dir ?? fileURLToPath(new URL('./dist', import.meta.url)),
            fileName,
          );
          const source = readFileSync(destination);
          if (source.length < 512) continue;
          writeFileSync(`${destination}.gz`, gzipSync(source, { level: 9 }));
        }
      },
      closeBundle() {
        writeFileSync(
          new URL('./dist/.gitkeep', import.meta.url),
          'Build assets with pnpm --dir web run build.\n',
        );
      },
    },
  ],
  server: {
    fs: { allow: [fileURLToPath(new URL('..', import.meta.url))] },
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
