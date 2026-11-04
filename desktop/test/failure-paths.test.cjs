const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const { createRequire } = require('node:module');
const fs = require('node:fs');
const path = require('node:path');
const { EventEmitter } = require('node:events');
const { Writable } = require('node:stream');
const { createHash } = require('node:crypto');

function load(file, overrides, extra = '', globals = {}) {
  const filename = path.join(__dirname, '..', file);
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(filename, 'utf8') + extra, {
    module, exports: module.exports, __dirname: path.dirname(filename), process, console,
    setTimeout, clearTimeout, ...globals,
    require: name => Object.hasOwn(overrides, name) ? overrides[name] : createRequire(filename)(name),
  }, { filename });
  return module.exports;
}

for (const phase of ['readiness', 'authentication', 'settings', 'navigation']) {
  test('connection recovers from backend exit during ' + phase, async () => {
    let candidate;
    let failAt = phase;
    let stopped = 0;
    const exit = stage => {
      if (stage !== failAt) return;
      candidate.child.exitCode = 1;
      candidate.emit('stopped', 1);
    };
    class Backend extends EventEmitter {
      constructor() {
        super();
        candidate = this;
        this.child = { exitCode: null, signalCode: null };
        this.origin = 'http://127.0.0.1:12345';
        this.ready = Promise.resolve().then(() => exit('readiness'));
      }
      async login() { exit('authentication'); return 'cookie'; }
      async stop() { stopped++; }
    }
    const app = new EventEmitter();
    app.getPath = () => '/unused';
    app.requestSingleInstanceLock = () => true;
    app.whenReady = () => new Promise(() => {});
    const main = load('main.cjs', {
      electron: { app },
      './backend.cjs': { Backend },
      './settings.cjs': { validateSettings: x => x, saveSettings: async () => exit('settings') },
      './updater.cjs': {},
    }, `
module.exports = {
 connect,
 initialize() {
  settings = {};
  desktopSession = { clearStorageData: async () => {}, cookies: { set: async () => {} } };
  window = { loadURL: async () => globalThis.navigate(), loadFile: async () => {} };
 },
 state: () => ({ busy, backend })
};`, { navigate: () => exit('navigation') });
    main.initialize();
    await assert.rejects(main.connect({ mode: 'local' }), /backend stopped/i);
    assert.equal(main.state().busy, false);
    assert.equal(main.state().backend, undefined);
    assert.equal(stopped, 1);
    failAt = null;
    await main.connect({ mode: 'local' });
    assert.equal(main.state().backend, candidate);
  });
}

function downloader() {
  let request;
  let file;
  let diskError = false;
  const timers = new Map();
  const download = load('mac-self-update.cjs', {
    electron: { net: { request: () => {
      request = new EventEmitter();
      request.end = () => {};
      request.abort = () => { request.aborted = true; };
      return request;
    } } },
    'node:fs': { ...fs, createWriteStream: () => {
      file = new Writable({ write(_chunk, _encoding, callback) {
        callback(diskError ? new Error('disk full') : undefined);
      } });
      return file;
    } },
  }, '\nmodule.exports.downloadAsset = downloadAsset;', {
    setTimeout: (fn, ms) => { const id = {}; timers.set(id, { fn, ms }); return id; },
    clearTimeout: id => timers.delete(id),
  });
  return {
    start(size = 3) { return download.downloadAsset(download.releaseAssetUrl('1.2.0', 'asset.zip'), '/unused', size, () => {}); },
    respond(data, end = true) {
      const response = new EventEmitter();
      response.statusCode = 200;
      response.pause = () => {};
      response.resume = () => {};
      request.emit('response', response);
      if (data) response.emit('data', Buffer.from(data));
      if (end) response.emit('end');
    },
    expire(ms) { [...timers.values()].find(timer => timer.ms === ms).fn(); },
    diskFailure() { diskError = true; },
    get request() { return request; },
    get file() { return file; },
    timers,
  };
}

for (const [duration, message] of [[30000, /stalled/], [900000, /timed out/]]) {
  test('download deadline aborts transport, closes file, clears timers and permits retry: ' + duration, async () => {
    const d = downloader();
    const pending = d.start();
    const rejected = assert.rejects(pending, message);
    d.respond('a', false);
    d.expire(duration);
    await rejected;
    assert.equal(d.request.aborted, true);
    assert.equal(d.file.destroyed, true);
    assert.equal(d.timers.size, 0);
    const retry = d.start();
    d.respond('abc');
    assert.equal(await retry, createHash('sha512').update('abc').digest('base64'));
    assert.equal(d.timers.size, 0);
  });
}

test('partial transfer and disk errors reject and release deadline timers', async () => {
  const partial = downloader();
  const rejected = assert.rejects(partial.start(), /ended early/);
  partial.respond('a');
  await rejected;
  assert.equal(partial.timers.size, 0);
  const disk = downloader();
  disk.diskFailure();
  const failed = assert.rejects(disk.start(), /disk full/);
  disk.respond('abc', false);
  await failed;
  assert.equal(disk.request.aborted, true);
  assert.equal(disk.timers.size, 0);
});
