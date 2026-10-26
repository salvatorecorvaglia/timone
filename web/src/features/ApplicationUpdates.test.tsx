import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { UpdateState } from '../lib/desktop';

const available: UpdateState = {
  status: 'available',
  currentVersion: '1.1.0',
  version: '1.2.0',
  percent: 0,
  bytesPerSecond: null,
  error: null,
  mode: 'squirrel',
  canCheck: true,
  canInstall: true,
  reason: null,
  packageManager: null,
  command: null,
  checkedAt: '2026-10-26T10:00:00.000Z',
};

function installBridge(initial: UpdateState) {
  const update = {
    state: vi.fn(async () => ({ ok: true, value: initial })),
    check: vi.fn(async () => ({ ok: true, value: initial })),
    install: vi.fn(async () => ({ ok: true, value: true })),
    openReleasePage: vi.fn(async () => ({ ok: true, value: true })),
    onEvent: vi.fn((_callback: (state: UpdateState) => void) => () => {}),
  };
  window.timoneDesktop = { configure: vi.fn(), reconnect: vi.fn(), update };
  return update;
}

async function showPanel() {
  const { UpdaterProvider } = await import('../lib/updater');
  const { ApplicationUpdates } = await import('./ApplicationUpdates');
  render(
    <UpdaterProvider>
      <ApplicationUpdates />
    </UpdaterProvider>,
  );
}

async function showBanner() {
  const { UpdaterProvider } = await import('../lib/updater');
  const { UpdateBanner } = await import('../components/UpdateBanner');
  render(
    <UpdaterProvider>
      <UpdateBanner />
    </UpdaterProvider>,
  );
}

afterEach(() => {
  delete window.timoneDesktop;
  vi.resetModules();
});

it('offers a download when this build can install updates', async () => {
  const update = installBridge(available);
  vi.resetModules();
  await showPanel();

  expect(await screen.findByText('Update 1.2.0 available')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Download update' }));
  await waitFor(() => expect(update.install).toHaveBeenCalledOnce());
});

it('sends Homebrew installs to the package manager instead of overwriting them', async () => {
  const state: UpdateState = {
    ...available,
    mode: 'manual',
    canInstall: false,
    reason: 'package-manager',
    packageManager: 'homebrew',
    command: 'brew upgrade --cask timone-desktop',
  };
  const update = installBridge(state);
  vi.resetModules();
  await showPanel();

  expect(await screen.findByText('Homebrew (manual)')).toBeInTheDocument();
  expect(screen.getByText('brew upgrade --cask timone-desktop')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Download update' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Open GitHub release' }));
  await waitFor(() => expect(update.openReleasePage).toHaveBeenCalledOnce());
});

it('follows download progress and offers a restart when ready', async () => {
  const update = installBridge(available);
  vi.resetModules();
  await showPanel();

  const listener = update.onEvent.mock.calls[0]?.[0] as (next: UpdateState) => void;
  expect(listener).toBeTypeOf('function');
  act(() => listener({ ...available, status: 'downloading', percent: 42 }));
  expect(await screen.findByText('Downloading 42%')).toBeInTheDocument();
  expect(screen.getByLabelText('Update download progress')).toHaveAttribute('value', '42');

  act(() => listener({ ...available, status: 'ready', percent: 100 }));
  fireEvent.click(await screen.findByRole('button', { name: 'Restart and install' }));
  await waitFor(() => expect(update.install).toHaveBeenCalledOnce());
});

it('keeps an update event that arrives before the check response', async () => {
  const update = installBridge({ ...available, status: 'idle', version: null, checkedAt: null });
  vi.resetModules();
  await showPanel();

  const listener = update.onEvent.mock.calls[0]?.[0] as (next: UpdateState) => void;
  update.check.mockImplementation(async () => {
    act(() => listener(available));
    return { ok: true, value: { ...available, status: 'idle', version: null } };
  });

  fireEvent.click(await screen.findByRole('button', { name: 'Check for updates' }));
  expect(await screen.findByText('Update 1.2.0 available')).toBeInTheDocument();
  await waitFor(() => expect(update.check).toHaveBeenCalledOnce());
  // The stale snapshot returned by the invoke must not overwrite the event.
  expect(screen.getByText('Update 1.2.0 available')).toBeInTheDocument();
});

it('surfaces a manual check failure and retries', async () => {
  const update = installBridge({ ...available, status: 'idle', version: null, checkedAt: null });
  update.check.mockResolvedValue({
    ok: true,
    value: { ...available, status: 'error', error: 'Could not reach the update server.' },
  });
  vi.resetModules();
  await showPanel();

  fireEvent.click(await screen.findByRole('button', { name: 'Check for updates' }));
  expect(await screen.findByText('Could not reach the update server.')).toBeInTheDocument();
  expect(screen.getByText('Update failed')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(update.check).toHaveBeenCalledTimes(2));
});

it('shows the banner for an update and can dismiss it', async () => {
  installBridge(available);
  vi.resetModules();
  await showBanner();

  expect(await screen.findByText('Timone 1.2.0 is available')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Download' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss update notice' }));
  expect(screen.queryByText('Timone 1.2.0 is available')).not.toBeInTheDocument();
});
