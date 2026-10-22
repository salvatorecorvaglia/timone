import { randomBytes } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const require = createRequire(new URL('../../web/package.json', import.meta.url));
const result = spawnSync(process.execPath, [require.resolve('@playwright/test/cli'), 'test', '--config', fileURLToPath(new URL('./config.mjs', import.meta.url)), ...process.argv.slice(2)], {
  stdio: 'inherit',
  env: { ...process.env, TIMONE_BROWSER_TOKEN: randomBytes(32).toString('hex') },
});
process.exit(result.status ?? 1);
