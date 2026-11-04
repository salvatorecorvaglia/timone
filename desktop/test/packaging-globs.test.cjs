const test = require('node:test');
const assert = require('node:assert/strict');
const { createRequire } = require('node:module');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

const builderRequire = createRequire(require.resolve('electron-builder'));
const appRequire = createRequire(builderRequire.resolve('app-builder-lib'));
const asar = appRequire('@electron/asar');
const asarRequire = createRequire(appRequire.resolve('@electron/asar'));
const legacy = asarRequire('minimatch');
const glob = asarRequire('glob');

test('legacy callable matching and modern named matching use patched minimatch', () => {
  const modern = appRequire('minimatch');
  assert.equal(typeof legacy, 'function');
  for (const [file, pattern, matches] of [
    ['main.cjs', '*.cjs', true],
    ['assets/icon.png', 'assets/*.{png,svg}', true],
    ['assets/icon.ico', 'assets/*.{png,svg}', false],
    ['nested/main.cjs', '**/*.cjs', true],
    ['.hidden', '*', false],
  ]) {
    assert.equal(legacy(file, pattern), matches);
    assert.equal(modern.minimatch(file, pattern), matches);
  }
  assert.equal(new legacy.Minimatch('**/*.cjs').match('nested/main.cjs'), true);
});

test('legacy glob selection and ASAR packing preserve included files and exclusions', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'timone-globs-'));
  try {
    const source = path.join(directory, 'source');
    await fs.mkdir(path.join(source, 'nested'), { recursive: true });
    await fs.writeFile(path.join(source, 'main.cjs'), 'main');
    await fs.writeFile(path.join(source, 'nested', 'preload.cjs'), 'preload');
    await fs.writeFile(path.join(source, 'nested', 'ignored.txt'), 'ignored');
    assert.deepEqual(glob.sync('**/*.cjs', { cwd: source, ignore: '**/preload.cjs' }), ['main.cjs']);
    const archive = path.join(directory, 'app.asar');
    await asar.createPackage(source, archive);
    assert.equal(asar.extractFile(archive, 'main.cjs').toString(), 'main');
    assert.equal(asar.extractFile(archive, 'nested/preload.cjs').toString(), 'preload');
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});
