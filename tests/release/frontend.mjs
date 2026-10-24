// Verify clean, repeatable embedded assets instead of relying on a fresh CI checkout.
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../../', import.meta.url));
const dist = new URL('../../web/dist/', import.meta.url);
const assets = new URL('assets/', dist);
const obsolete = new URL('timone-obsolete-build.js', assets);
const build = () => {
  const result = spawnSync(process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm', ['--dir', 'web', 'run', 'build'], { cwd: root, stdio: 'inherit' });
  if (result.status !== 0) throw new Error('Frontend production build failed');
};
build();
const before = readdirSync(assets).sort();
mkdirSync(assets, { recursive: true });
writeFileSync(obsolete, 'obsolete build artifact');
build();
if (existsSync(obsolete)) throw new Error('Obsolete assets survived a production rebuild');
if (!existsSync(new URL('index.html', dist)) || !existsSync(new URL('.gitkeep', dist))) throw new Error('Embedded frontend entrypoint or source placeholder missing');
if (JSON.stringify(before) !== JSON.stringify(readdirSync(assets).sort())) throw new Error('Repeated builds produced different asset sets');
const compressed = readdirSync(assets).filter(name => name.endsWith('.gz'));
if (!compressed.length) throw new Error('Precompressed assets are missing');
for (const name of compressed) {
  const decoded = gunzipSync(readFileSync(new URL(name, assets)));
  if (!decoded.equals(readFileSync(new URL(name.slice(0, -3), assets)))) throw new Error(`Precompressed asset differs from final output: ${name}`);
}
console.log('Repeated frontend builds remove obsolete assets, preserve the entrypoint, and generate matching gzip representations.');
