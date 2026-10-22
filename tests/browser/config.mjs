import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { defineConfig, devices } = require('@playwright/test');
export default defineConfig({
  testDir: '.', testMatch: '*.spec.mjs', timeout: 30000, globalTimeout: 900000,
  workers: 1, retries: 0,
  reporter: [['list'], ['html', { outputFolder: process.env.TIMONE_BROWSER_ARTIFACTS ? `${process.env.TIMONE_BROWSER_ARTIFACTS}/report` : fileURLToPath(new URL('../../playwright-report', import.meta.url)), open: 'never' }]],
  outputDir: process.env.TIMONE_BROWSER_ARTIFACTS ? `${process.env.TIMONE_BROWSER_ARTIFACTS}/results` : fileURLToPath(new URL('../../test-results', import.meta.url)),
  use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:18080', viewport: { width: 1440, height: 960 }, reducedMotion: 'reduce', screenshot: 'only-on-failure', trace: 'retain-on-failure', ...(process.env.TIMONE_BROWSER_EXECUTABLE ? { launchOptions: { executablePath: process.env.TIMONE_BROWSER_EXECUTABLE, args: ['--no-sandbox'] } } : {}) },
  webServer: {
    command: './bin/timone', cwd: fileURLToPath(new URL('../../', import.meta.url)),
    url: 'http://127.0.0.1:18080/healthz', timeout: 15000, reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 10000 },
    env: { ...process.env, TIMONE_ENV: 'development', TIMONE_MODE: 'demo', TIMONE_LISTEN: '127.0.0.1:18080', TIMONE_TOKEN: process.env.TIMONE_BROWSER_TOKEN ?? '', TIMONE_TOKEN_FILE: '', TIMONE_AUTH_MODE: 'token', TIMONE_TRUSTED_PROXIES: '', TIMONE_LOGIN_TRUSTED_PROXIES: '', TIMONE_RETAINED_MIB:'16', TIMONE_PUBLIC_URL: '', TIMONE_ACCESS_LOG: 'false', TIMONE_CUSTOM_RESOURCES: '', TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS: '300', TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS: '900', TIMONE_DIAGNOSTICS_RESTART_THRESHOLD: '5' },
  },
});
