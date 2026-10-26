import { afterEach, expect, it, vi } from 'vitest';

afterEach(() => {
  delete window.timoneDesktop;
  vi.resetModules();
});

it('fails clearly when the desktop bridge is unavailable', async () => {
  const { configureDesktop, reconnectDesktop } = await import('./desktop');
  await expect(configureDesktop()).rejects.toThrow('Could not open connection settings');
  await expect(reconnectDesktop()).rejects.toThrow('Could not renew the desktop session');
});

it('opens connection settings only after the main process accepts the request', async () => {
  window.timoneDesktop = { configure: vi.fn().mockResolvedValue({ ok: true }), reconnect: vi.fn() };
  const { configureDesktop } = await import('./desktop');
  await expect(configureDesktop()).resolves.toBeUndefined();
  expect(window.timoneDesktop.configure).toHaveBeenCalledOnce();
});

it('preserves actionable bridge failures without claiming reconnection succeeded', async () => {
  window.timoneDesktop = {
    configure: vi.fn().mockResolvedValue({ ok: false, error: 'Workspace is closing' }),
    reconnect: vi.fn().mockResolvedValue({ ok: false, error: 'Backend unavailable' }),
  };
  const { configureDesktop, reconnectDesktop } = await import('./desktop');
  await expect(configureDesktop()).rejects.toThrow('Workspace is closing');
  await expect(reconnectDesktop()).rejects.toThrow('Backend unavailable');
});

it('unwraps update state and actions from the desktop bridge', async () => {
  const state = {
    status: 'idle' as const,
    currentVersion: '1.1.0',
    version: null,
    percent: 0,
    bytesPerSecond: null,
    error: null,
    mode: 'squirrel' as const,
    canCheck: true,
    canInstall: true,
    reason: null,
    packageManager: null,
    command: null,
    checkedAt: null,
  };
  window.timoneDesktop = {
    configure: vi.fn(),
    reconnect: vi.fn(),
    update: {
      state: vi.fn().mockResolvedValue({ ok: true, value: state }),
      check: vi.fn().mockResolvedValue({ ok: true, value: state }),
      install: vi.fn().mockResolvedValue({ ok: true, value: true }),
      openReleasePage: vi.fn().mockResolvedValue({ ok: true, value: true }),
      onEvent: vi.fn(),
    },
  };
  const { getUpdateState, checkForUpdate, installUpdate, openReleasePage } = await import(
    './desktop'
  );
  await expect(getUpdateState()).resolves.toEqual(state);
  await expect(checkForUpdate()).resolves.toEqual(state);
  await expect(installUpdate()).resolves.toBe(true);
  await expect(openReleasePage()).resolves.toBe(true);
});

it('fails update actions clearly without the desktop bridge', async () => {
  const { getUpdateState, checkForUpdate, installUpdate, openReleasePage } = await import(
    './desktop'
  );
  await expect(getUpdateState()).rejects.toThrow('desktop app');
  await expect(checkForUpdate()).rejects.toThrow('desktop app');
  await expect(installUpdate()).rejects.toThrow('Could not start the update');
  await expect(openReleasePage()).rejects.toThrow('Could not open the releases page');
});
