const { spawn } = require('node:child_process');
const { randomBytes } = require('node:crypto');
const { EventEmitter } = require('node:events');
const http = require('node:http');
const path = require('node:path');

function backendEnvironment(settings, inherited = process.env) {
  // Desktop owns listener and authentication configuration. Cloud provider
  // credentials/PATH remain available to explicitly trusted exec plugins.
  const env = Object.fromEntries(Object.entries(inherited).filter(([key]) => !key.startsWith('TIMONE_')));
  env.TIMONE_TOKEN = randomBytes(32).toString('hex');
  env.TIMONE_ENV = inherited.TIMONE_ENV || 'production';
  env.TIMONE_ACCESS_LOG = 'false';
  env.TIMONE_AUTH_MODE = 'token';
  env.TIMONE_ALLOW_EXEC_AUTH = String(settings.allowExecAuth === true);
  if (settings.kubeconfig) env.KUBECONFIG = settings.kubeconfig;
  if (process.platform === 'darwin') {
    env.PATH = [...new Set([env.PATH || '/usr/bin:/bin', '/opt/homebrew/bin', '/usr/local/bin', path.join(env.HOME || '', '.local/bin')])].join(path.delimiter);
  }
  return env;
}

function startupOrigin(address) {
  if (typeof address !== 'string' || !/^127\.0\.0\.1:[0-9]{1,5}$/.test(address)) throw new Error('Invalid backend listen address');
  const port = Number(address.slice(address.lastIndexOf(':') + 1));
  if (port < 1 || port > 65535) throw new Error('Invalid backend listen port');
  return `http://${address}`;
}

class Backend extends EventEmitter {
  #token;
  constructor(binary, settings, { env = process.env, startupTimeout = 45000, shutdownTimeout = 10000 } = {}) {
    super();
    this.origin = null;
    this.stopping = false;
    this.shutdownTimeout = shutdownTimeout;
    const childEnv = backendEnvironment(settings, env);
    this.#token = childEnv.TIMONE_TOKEN;
    const args = ['--mode', settings.mode, '--listen', '127.0.0.1:0', '--no-browser', '--parent-pipe', '--context', settings.context || ''];
    this.child = spawn(binary, args, { env: childEnv, stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true, shell: false });
    this.exited = new Promise((resolve) => {
      this.child.once('exit', (code, signal) => {
        resolve({ code, signal });
        if (!this.stopping) this.emit('stopped', code);
      });
      this.child.once('error', () => resolve({ code: null, signal: null }));
    });
    // Stderr may contain provider diagnostics. Never persist or forward raw logs.
    this.child.stderr.resume();
    this.child.stdin.on('error', () => {});
    this.ready = new Promise((resolve, reject) => {
      let buffered = '';
      let lastError = 'Timone could not start. Check your kubeconfig or try the demo.';
      let settled = false;
      const finish = (error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        this.child.stdout.off('data', consume);
        this.child.stdout.resume();
        if (error) reject(error); else resolve(this.origin);
      };
      const timer = setTimeout(() => {
        finish(new Error('Connection timed out. Check cluster access and credential plugins.'));
        void this.stop();
      }, startupTimeout);
      const consume = (chunk) => {
        buffered += chunk.toString('utf8');
        if (buffered.length > 65536) {
          finish(new Error('Backend startup output exceeded its limit.'));
          void this.stop();
          return;
        }
        let newline;
        while ((newline = buffered.indexOf('\n')) !== -1) {
          const line = buffered.slice(0, newline);
          buffered = buffered.slice(newline + 1);
          let entry;
          try { entry = JSON.parse(line); } catch { continue; }
          if (entry.msg === 'Timone stopped' && typeof entry.error === 'string') {
            lastError = entry.error.replaceAll(this.#token, '[redacted]').replace(/[\x00-\x1f\x7f]/g, ' ').slice(0, 1024);
          }
          if (entry.msg === 'Timone starting') {
            try { this.origin = startupOrigin(entry.address); finish(); }
            catch (error) { finish(error); void this.stop(); }
          }
        }
      };
      this.child.stdout.on('data', consume);
      this.child.once('error', () => finish(new Error('The bundled Timone backend could not be launched.')));
      this.child.once('exit', () => finish(new Error(lastError)));
    });
  }

  async login() {
    await this.ready;
    if (this.stopping) throw new Error('Workspace is closing.');
    const body = JSON.stringify({ token: this.#token });
    return new Promise((resolve, reject) => {
      // Direct loopback HTTP deliberately ignores environment proxy settings and
      // never follows redirects with the one-launch authentication token.
      const request = http.request(`${this.origin}/api/v1/session`, {
        method: 'POST', agent: false,
        headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) },
      }, (response) => {
        response.resume();
        const cookies = response.headers['set-cookie'] || [];
        const match = cookies.length === 1 && /^timone_session=([A-Za-z0-9_-]{43});/.exec(cookies[0]);
        if (response.statusCode !== 200 || !match || !/; HttpOnly(?:;|$)/i.test(cookies[0])) {
          reject(new Error('The desktop session could not be established.'));
          return;
        }
        resolve(match[1]);
      });
      request.setTimeout(10000, () => request.destroy(new Error('Session startup timed out.')));
      request.on('error', () => reject(new Error('The desktop session could not be established.')));
      request.end(body);
    });
  }

  stop() {
    if (this.stopPromise) return this.stopPromise;
    this.stopping = true;
    this.stopPromise = (async () => {
      this.child.stdin.end();
      const timer = setTimeout(() => this.child.kill('SIGKILL'), this.shutdownTimeout);
      try { return await this.exited; }
      finally { clearTimeout(timer); this.#token = ''; }
    })();
    return this.stopPromise;
  }
}

module.exports = { Backend, backendEnvironment, startupOrigin };
