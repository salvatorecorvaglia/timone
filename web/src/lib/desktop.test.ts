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
