import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { show } from '../features/test-helpers';
import { SessionGate } from './SessionGate';
const actions = vi.hoisted(() => ({ reconnect: vi.fn(), configure: vi.fn() }));
vi.mock('../lib/desktop', () => ({
  desktop: {},
  reconnectDesktop: actions.reconnect,
  configureDesktop: actions.configure,
}));
afterEach(() => {
  vi.restoreAllMocks();
  actions.reconnect.mockReset();
  actions.configure.mockReset();
});
it('locks connection controls while reconnecting and supports recovery after failure', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify({ authenticated: false, mode: 'token' })),
  );
  let reject!: (error: Error) => void;
  actions.reconnect.mockImplementation(
    () =>
      new Promise((_resolve, fail) => {
        reject = fail;
      }),
  );
  show(
    <SessionGate>
      <div>Protected workspace</div>
    </SessionGate>,
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Reconnect' }));
  expect(screen.getByRole('button', { name: 'Change connection' })).toBeDisabled();
  reject(new Error('Connection unavailable'));
  await screen.findByText('Connection unavailable');
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Change connection' })).toBeEnabled(),
  );
  actions.configure.mockResolvedValue(undefined);
  fireEvent.click(screen.getByRole('button', { name: 'Change connection' }));
  expect(actions.configure).toHaveBeenCalledOnce();
  expect(screen.queryByText('Protected workspace')).not.toBeInTheDocument();
});
it('shows startup failures without exposing workspace content', async () => {
  vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Server unavailable'));
  show(
    <SessionGate>
      <div>Protected workspace</div>
    </SessionGate>,
  );
  await screen.findByText('Server unavailable');
  expect(screen.queryByText('Protected workspace')).not.toBeInTheDocument();
});
