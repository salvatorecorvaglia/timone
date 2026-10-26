import { _electron as electron, expect } from '@playwright/test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'timone-desktop-test-'));
const executablePath = process.env.TIMONE_TEST_EXECUTABLE;
const app = await electron.launch({
  ...(executablePath ? { executablePath: path.resolve(executablePath), args: [] } : { args: [path.join(root, 'main.cjs')] }),
  env: { ...process.env, KUBECONFIG: path.join(temporary, 'missing-kubeconfig'), TIMONE_DISABLE_UPDATE_CHECK: '1' },
  timeout: 45000,
});
try {
  // Never write test preferences to the user's installed desktop profile.
  await app.evaluate(({ app }, directory) => { app.setPath('userData', directory); }, temporary);
  const page = await app.firstWindow();
  const packaged = await app.evaluate(({ app }) => app.isPackaged);
  assert.equal(packaged, Boolean(executablePath));
  await expect(page.getByRole('button', { name: 'Open cluster' })).toBeVisible();
  const demoButton = page.getByRole('button', { name: 'Explore the demo' });
  if (packaged) await expect(demoButton).toBeHidden();
  else await expect(demoButton).toBeVisible();
  assert.equal(await page.evaluate(() => typeof window.require), 'undefined');
  await page.getByRole('checkbox', { name: 'Allow trusted credential plugins' }).check();
  await app.evaluate(({ dialog }, selected) => {
    dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [selected] });
  }, path.join(temporary, 'selected-missing-kubeconfig'));
  await page.getByRole('button', { name: 'Choose file…' }).click();
  await expect(page.getByRole('checkbox', { name: 'Allow trusted credential plugins' })).not.toBeChecked();
  await page.getByRole('button', { name: 'Open cluster' }).click();
  await expect(page.getByRole('alert')).toContainText(/kubeconfig|context/i, { timeout: 20000 });
  if (packaged) {
    // Packaged builds deliberately disable demo mode, including direct IPC calls.
    const result = await page.evaluate(() => window.timoneSetup.connect({ mode: 'demo', context: '', allowExecAuth: false }));
    assert.equal(result.ok, false);
    assert.match(result.error, /Demo mode is available only in development/);
    await expect(demoButton).toBeHidden();
    await expect(page.getByRole('button', { name: 'Open cluster' })).toBeEnabled();
    const updateBridge = await page.evaluate(async () => {
      const bridge = window.timoneDesktop.update;
      const state = await bridge.state();
      return { keys: Object.keys(bridge).sort(), state };
    });
    assert.deepEqual(updateBridge.keys, ['check', 'install', 'onEvent', 'openReleasePage', 'state']);
    assert.equal(updateBridge.state.ok, true);
    assert.equal(updateBridge.state.value.status, 'idle');
    assert.ok(['manual', 'self', 'squirrel'].includes(updateBridge.state.value.mode));
    await page.screenshot({ path: path.join(root, 'test-results/desktop.png') });
    console.log('Packaged desktop integration passed: kubeconfig consent, startup failure, demo rejection and update bridge.');
  } else {
    await page.getByRole('button', { name: 'Explore the demo' }).click();
    await page.waitForURL(/^http:\/\/127\.0\.0\.1:\d+\//, { timeout: 30000 });
    await expect(page.getByRole('link', { name: 'Settings', exact: true })).toBeVisible();
    const origin = new URL(page.url()).origin;
    assert.equal(new URL(page.url()).search, '');
    assert.deepEqual(await page.evaluate(() => ({ node: typeof window.require, setup: typeof window.timoneSetup, cookies: document.cookie })), { node: 'undefined', setup: 'undefined', cookies: '' });
    const updateBridge = await page.evaluate(async () => {
      const bridge = window.timoneDesktop.update;
      const state = await bridge.state();
      return { keys: Object.keys(bridge).sort(), state };
    });
    assert.deepEqual(updateBridge.keys, ['check', 'install', 'onEvent', 'openReleasePage', 'state']);
    assert.equal(updateBridge.state.ok, true);
    assert.equal(updateBridge.state.value.status, 'idle');
    assert.equal(updateBridge.state.value.reason, 'development');
    assert.equal(
      await page.evaluate(() => {
        const unsubscribe = window.timoneDesktop.update.onEvent(() => {});
        if (typeof unsubscribe !== 'function') return 'no-unsubscribe';
        unsubscribe();
        return 'ok';
      }),
      'ok',
    );
    const prefs = await app.evaluate(({ BrowserWindow }) => {
      const p = BrowserWindow.getAllWindows()[0].webContents.getLastWebPreferences();
      return { sandbox: p.sandbox, contextIsolation: p.contextIsolation, nodeIntegration: p.nodeIntegration };
    });
    assert.deepEqual(prefs, { sandbox: true, contextIsolation: true, nodeIntegration: false });
    assert.equal((await fetch(`${origin}/api/v1/overview`)).status, 401);
    const cookie = await app.evaluate(async ({ BrowserWindow }) => {
      const cookies = await BrowserWindow.getAllWindows()[0].webContents.session.cookies.get({ name: 'timone_session' });
      return { httpOnly: cookies[0]?.httpOnly, session: cookies[0]?.session, sameSite: cookies[0]?.sameSite };
    });
    assert.deepEqual(cookie, { httpOnly: true, session: true, sameSite: 'strict' });
    await page.evaluate(() => { window.location.href = 'file:///etc/passwd'; });
    await page.waitForTimeout(200);
    assert.equal(new URL(page.url()).origin, origin);
    const response = await page.evaluate(async () => (await fetch('/api/v1/overview')).status);
    assert.equal(response, 200);
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Change connection', exact: true })).toBeVisible();
    await expect(page.getByText('Private desktop session.', { exact: false })).toBeVisible();
    // Expiry/clear produces a reconnect flow, never an unknown token prompt.
    await app.evaluate(async ({ BrowserWindow }) => { await BrowserWindow.getAllWindows()[0].webContents.session.clearStorageData(); });
    await page.reload();
    await expect(page.getByRole('button', { name: 'Reconnect', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Reconnect', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Change connection', exact: true })).toBeVisible();
    await page.screenshot({ path: path.join(root, 'test-results/desktop.png') });
    await page.getByRole('button', { name: 'Change connection', exact: true }).click();
    // Graceful backend shutdown allows eight seconds, with a ten-second desktop
    // termination bound. Wait for that contract rather than Playwright's 5s default.
    await expect(page.getByRole('button', { name: 'Explore the demo' })).toBeVisible({ timeout: 15000 });
    await expect.poll(async () => { try { await fetch(`${origin}/api/v1/session`); return false; } catch { return true; } }).toBe(true);
    await page.getByRole('button', { name: 'Explore the demo' }).click();
    await page.waitForURL(/^http:\/\/127\.0\.0\.1:\d+\//, { timeout: 30000 });
    const finalOrigin = new URL(page.url()).origin;
    await app.close();
    await assert.rejects(fetch(`${finalOrigin}/api/v1/session`));
    const persisted = await fs.readFile(path.join(temporary, 'connection.json'), 'utf8');
    assert.equal(persisted.includes('token'), false);
    console.log('Desktop integration passed: kubeconfig consent, startup failure, demo, isolation, authentication, navigation, reconnect, switch and shutdown.');
  }
} finally {
  await app.close().catch(() => {});
  await fs.rm(temporary, { recursive: true, force: true });
}
