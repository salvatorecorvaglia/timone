const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const vm = require('node:vm');
const { createRequire } = require('node:module');
const { createHash } = require('node:crypto');
const { EventEmitter, once } = require('node:events');
const { Readable } = require('node:stream');
const { execFile, spawn } = require('node:child_process');
const { promisify } = require('node:util');
const exec = promisify(execFile);

for (const rollback of [false, true]) {
  test('real macOS staged bundle ' + (rollback ? 'rolls back after copy failure' : 'upgrades an isolated installed fixture'), {
    skip: process.platform !== 'darwin', timeout: 20000,
  }, async () => {
    const root = await fs.mkdtemp(path.join(os.tmpdir(), 'timone-update-acceptance-'));
    try {
      const bundle = async (location, version) => {
        await fs.mkdir(path.join(location, 'Contents'), { recursive: true });
        await fs.writeFile(path.join(location, 'Contents/Info.plist'), `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>io.github.salvatorecorvaglia.timone</string><key>CFBundleShortVersionString</key><string>${version}</string></dict></plist>`);
        await fs.writeFile(path.join(location, 'version.txt'), version);
      };
      const installed = path.join(root, 'Installed/Timone.app');
      const candidate = path.join(root, 'New/Timone.app');
      await bundle(installed, '1.2.0');
      await bundle(candidate, '1.2.1');
      const zip = path.join(root, 'release.zip');
      await exec('/usr/bin/ditto', ['-c', '-k', '--keepParent', candidate, zip]);
      const bytes = await fs.readFile(zip);
      const app = new EventEmitter();
      const filename = path.join(__dirname, '../mac-self-update.cjs');
      const module = { exports: {} };
      const localRequire = createRequire(filename);
      vm.runInNewContext(await fs.readFile(filename, 'utf8'), {
        module, process, console, setTimeout, clearTimeout,
        require: name => name === 'electron' ? { app, net: { request() {
          const request = new EventEmitter();
          request.abort = () => {};
          request.end = () => queueMicrotask(() => {
            const response = Readable.from([bytes]);
            response.statusCode = 200;
            request.emit('response', response);
          });
          return request;
        } } } : name === 'node:os' ? { ...os, tmpdir: () => root } : localRequire(name),
      }, { filename });
      const updater = module.exports;
      const staged = await updater.stageUpdate({
        version: '1.2.1',
        files: [{ url: `timone-desktop_1.2.1_mac_${process.arch}.zip`,
          size: bytes.length, sha512: createHash('sha512').update(bytes).digest('base64') }],
      }, () => {});
      assert.equal(await fs.readFile(path.join(installed, 'version.txt'), 'utf8'), '1.2.0');
      if (rollback) await fs.rm(staged.appPath, { recursive: true });
      const child = spawn('/bin/sh', ['-c', 'exit 0']);
      await once(child, 'exit');
      const script = updater.buildApplyScript(child.pid, staged.appPath, installed,
        staged.stageDir, false, path.join(root, 'apply.log'));
      if (rollback) await assert.rejects(exec('/bin/bash', ['-c', script]));
      else await exec('/bin/bash', ['-c', script]);
      assert.equal(await fs.readFile(path.join(installed, 'version.txt'), 'utf8'), rollback ? '1.2.0' : '1.2.1');
      await assert.rejects(fs.stat(staged.stageDir), { code: 'ENOENT' });
      assert.deepEqual(await fs.readdir(path.dirname(installed)), ['Timone.app']);
    } finally {
      await fs.rm(root, { recursive: true, force: true });
    }
  });
}
