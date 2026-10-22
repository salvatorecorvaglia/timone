import { _electron as electron, expect } from '@playwright/test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const fixture = process.env.TIMONE_DESKTOP_ACCEPTANCE_DIR;
assert.ok(fixture, 'Explicit disposable acceptance directory required');
const kubeconfig = path.join(fixture, 'kubeconfig.json');
const raw = JSON.parse(await fs.readFile(kubeconfig, 'utf8'));
assert.equal(raw['current-context'], 'desktop-a');
assert.equal(raw.clusters[0].name, 'kind-timone-desktop-acceptance');
const url = new URL(raw.clusters[0].cluster.server);
assert.equal(url.protocol, 'https:');
assert.ok(['127.0.0.1', '[::1]'].includes(url.hostname));
const original = await fs.readFile(kubeconfig, 'utf8');
const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'timone-desktop-cluster-'));
const executablePath = process.env.TIMONE_TEST_EXECUTABLE;
const app = await electron.launch({
  ...(executablePath ? { executablePath: path.resolve(executablePath), args: [] } : { args: [path.join(root, 'main.cjs')] }),
  env: { ...process.env, KUBECONFIG: kubeconfig }, timeout: 45000,
});
let page;
try {
  await app.evaluate(({ app }, directory) => app.setPath('userData', directory), temporary);
  page = await app.firstWindow();
  await page.locator('#context').fill('desktop-a');
  await page.getByRole('button', { name: 'Open cluster' }).click();
  await page.waitForURL(/^http:\/\/127\.0\.0\.1:\d+\//, { timeout: 45000 });
  // Workspace switching reloads the renderer. Read through Electron's session
  // so authenticated API checks survive navigation without hiding API errors.
  const request = (endpoint) => app.evaluate(async ({ BrowserWindow }, endpoint) => {
    const contents = BrowserWindow.getAllWindows()[0].webContents;
    const response = await contents.session.fetch(new URL(endpoint, contents.getURL()).href);
    return { status: response.status, ok: response.ok, body: await response.text() };
  }, endpoint);
  const read = async (endpoint) => {
    const response = await request(endpoint);
    if (!response.ok) throw new Error(`API ${endpoint}: ${response.status}`);
    return JSON.parse(response.body);
  };
  await expect.poll(async () => (await read('/api/v1/connection')).state, { timeout: 30000 }).toBe('connected');
  assert.equal((await read('/api/v1/overview')).mode, 'cluster');
  assert.equal((await read('/api/v1/cluster')).scope, 'timone-desktop-a');
  const containsService = async (name) => JSON.stringify(await read('/api/v1/resources?kind=Service')).includes(name);
  await expect.poll(() => containsService('desktop-proof-a'), { timeout: 15000 }).toBe(true);
  assert.equal(await containsService('desktop-proof-b'), false);
  await page.locator('.cluster-switch').click();
  await page.getByLabel('Kubeconfig context').selectOption('desktop-b');
  await page.getByLabel('Workspace namespace ceiling').fill('timone-desktop-b');
  await page.locator('.cluster-popover input[type="checkbox"]').check();
  await page.getByRole('button', { name: 'Switch workspace', exact: true }).click();
  await expect.poll(async () => (await read('/api/v1/cluster')).name, { timeout: 30000 }).toBe('desktop-b');
  await expect.poll(() => containsService('desktop-proof-b'), { timeout: 15000 }).toBe(true);
  assert.equal(await containsService('desktop-proof-a'), false);
  assert.equal(await fs.readFile(kubeconfig, 'utf8'), original, 'Context switching modified kubeconfig');
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Change connection', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open cluster' })).toBeVisible({ timeout: 15000 });
  await page.locator('#context').fill('desktop-exec');
  await page.getByRole('checkbox', { name: 'Allow trusted credential plugins' }).uncheck();
  await page.getByRole('button', { name: 'Open cluster' }).click();
  await expect(page.getByRole('alert')).toContainText(/exec|plugin/i, { timeout: 20000 });
  await page.getByRole('checkbox', { name: 'Allow trusted credential plugins' }).check();
  await page.getByRole('button', { name: 'Open cluster' }).click();
  await page.waitForURL(/^http:\/\/127\.0\.0\.1:\d+\//, { timeout: 45000 });
  await expect.poll(async () => (await read('/api/v1/connection')).state, { timeout: 30000 }).toBe('connected');
  assert.equal((await read('/api/v1/cluster')).name, 'desktop-exec');
  assert.equal(await containsService('desktop-proof-a'), true);
  const { status } = await request('/api/v1/resources?kind=Secret');
  assert.notEqual(status, 200);
  await page.screenshot({ path: path.join(root, 'test-results/desktop-cluster.png') });
  const origin = new URL(page.url()).origin;
  await app.close();
  await assert.rejects(fetch(`${origin}/api/v1/session`));
  const preferences = await fs.readFile(path.join(temporary, 'connection.json'), 'utf8');
  assert.equal(preferences.includes('token'), false);
  assert.equal(preferences.includes('client-key'), false);
  console.log('Real Kubernetes desktop acceptance passed: read-only identity, real services, namespace isolation, UI context switching, unchanged kubeconfig, exec consent, authenticated plugin and shutdown.');
} catch (error) {
  if (page && !page.isClosed()) await page.screenshot({ path: path.join(root, 'test-results/desktop-cluster-failure.png') }).catch(() => {});
  throw error;
} finally {
  await app.close().catch(() => {});
  await fs.rm(temporary, { recursive: true, force: true });
}
