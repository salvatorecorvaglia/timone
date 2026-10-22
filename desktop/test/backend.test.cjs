const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');
const { once } = require('node:events');
const { Backend, backendEnvironment, startupOrigin } = require('../backend.cjs');
const { readSettings, saveSettings, validateSettings } = require('../settings.cjs');
const { isExternalURL, sameOrigin } = require('../security.cjs');

const binary = path.join(__dirname, '../resources', process.platform === 'win32' ? 'timone.exe' : 'timone');
process.env.TIMONE_ENV = 'development';

const demo = { mode: 'demo', context: '', kubeconfig: '', allowExecAuth: false };

test('desktop owns auth/listener settings, preserves cloud credential environment, opts exec in explicitly', () => {
  const inherited = { TIMONE_TOKEN: 'old', TIMONE_TOKEN_FILE: '/old', TIMONE_LISTEN: '0.0.0.0:80', TIMONE_AUTH_MODE: 'proxy', TIMONE_ALLOW_EXEC_AUTH: 'true', KUBECONFIG: '/current', AWS_PROFILE: 'development', PATH: '/usr/bin' };
  const env = backendEnvironment(demo, inherited);
  assert.match(env.TIMONE_TOKEN, /^[a-f0-9]{64}$/);
  assert.notEqual(env.TIMONE_TOKEN, backendEnvironment(demo, inherited).TIMONE_TOKEN);
  assert.equal(env.TIMONE_TOKEN_FILE, undefined);
  assert.equal(env.TIMONE_LISTEN, undefined);
  assert.equal(env.TIMONE_AUTH_MODE, 'token');
  assert.equal(env.TIMONE_ALLOW_EXEC_AUTH, 'false');
  assert.equal(env.KUBECONFIG, '/current');
  assert.equal(env.AWS_PROFILE, 'development');
  assert.equal(backendEnvironment({ ...demo, kubeconfig: '/selected', allowExecAuth: true }, inherited).KUBECONFIG, '/selected');
  assert.equal(backendEnvironment({ ...demo, allowExecAuth: true }, inherited).TIMONE_ALLOW_EXEC_AUTH, 'true');
});

test('URLs and persisted settings reject unsafe inputs', async () => {
  for (const value of ['0.0.0.0:80', '127.0.0.1:0', '127.0.0.1:65536', 'localhost:80', '127.0.0.1:80/evil']) assert.throws(() => startupOrigin(value));
  assert.equal(startupOrigin('127.0.0.1:12345'), 'http://127.0.0.1:12345');
  for (const url of ['file:///etc/passwd', 'javascript:alert(1)', 'https://user:password@example.com', 'http://example.com']) assert.equal(isExternalURL(url), false);
  assert.equal(isExternalURL('https://github.com/salvatorecorvaglia/timone'), true);
  assert.equal(sameOrigin('http://127.0.0.1:42/api', 'http://127.0.0.1:42'), true);
  assert.equal(sameOrigin('http://127.0.0.1:43/api', 'http://127.0.0.1:42'), false);
  assert.throws(() => validateSettings({ mode: 'incluster' }));
  assert.throws(() => validateSettings({ mode: 'local', context: 'bad\ncontext' }));
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'timone-settings-'));
  try {
    assert.deepEqual(await readSettings(directory), { ...demo, mode: 'local' });
    await saveSettings(directory, { ...demo, token: 'never-persist', kubeconfig: '/a config' });
    assert.deepEqual(await readSettings(directory), { ...demo, kubeconfig: '/a config' });
    const raw = await fs.readFile(path.join(directory, 'connection.json'), 'utf8');
    assert.equal(raw.includes('never-persist'), false);
    if (process.platform !== 'win32') assert.equal((await fs.stat(path.join(directory, 'connection.json'))).mode & 0o777, 0o600);
    await fs.writeFile(path.join(directory, 'connection.json'), '{broken');
    assert.equal((await readSettings(directory)).mode, 'local');
  } finally { await fs.rm(directory, { recursive: true, force: true }); }
});

test('actual bundled backend authenticates without a URL token and exits on pipe closure', { timeout: 30000 }, async () => {
  const backend = new Backend(binary, demo);
  try {
    const origin = await backend.ready;
    assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/);
    assert.equal((await fetch(`${origin}/api/v1/overview`)).status, 401);
    const cookie = await backend.login();
    const response = await fetch(`${origin}/api/v1/overview`, { headers: { Cookie: `timone_session=${cookie}` } });
    assert.equal(response.status, 200);
    assert.equal((await response.json()).mode, 'demo');
    const session = await fetch(`${origin}/api/v1/session`, { headers: { Cookie: `timone_session=${cookie}` } });
    assert.equal((await session.json()).authenticated, true);
    assert.equal((await fetch(`${origin}/api/v1/overview?token=${cookie}`)).status, 401);
  } finally {
    const result = await backend.stop();
    assert.equal(result.code, 0);
    assert.equal(result.signal, null);
    assert.equal(await backend.stop(), result);
  }
});

test('bad kubeconfig yields an actionable startup error and no surviving backend', { timeout: 15000 }, async () => {
  const backend = new Backend(binary, { ...demo, mode: 'local', kubeconfig: path.join(os.tmpdir(), 'timone-no-such-kubeconfig') });
  try { await assert.rejects(backend.ready, /kubeconfig|context/i); }
  finally { await backend.stop(); }
});

test('backend terminates even if its supervising process is killed', { timeout: 20000 }, async () => {
  const script = `const { Backend } = require(${JSON.stringify(path.join(__dirname, '../backend.cjs'))}); const b = new Backend(${JSON.stringify(binary)}, ${JSON.stringify(demo)}); b.ready.then(origin => console.log(origin));`;
  const parent = spawn(process.execPath, ['-e', script], { stdio: ['ignore', 'pipe', 'pipe'] });
  let origin;
  try {
    origin = await new Promise((resolve, reject) => {
      let output = '';
      parent.stdout.on('data', (chunk) => { output += chunk; if (output.includes('\n')) resolve(output.trim()); });
      parent.once('exit', () => reject(new Error('Supervisor exited before startup')));
      parent.once('error', reject);
    });
    assert.equal((await fetch(`${origin}/api/v1/session`)).status, 200);
    const exited = once(parent, 'exit');
    parent.kill('SIGKILL');
    await exited;
    let stopped = false;
    for (let i = 0; i < 100; i++) {
      try { await fetch(`${origin}/api/v1/session`, { signal: AbortSignal.timeout(200) }); }
      catch { stopped = true; break; }
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
    assert.equal(stopped, true, 'orphaned backend still accepts requests');
  } finally { if (parent.exitCode === null && parent.signalCode === null) parent.kill('SIGKILL'); }
});
