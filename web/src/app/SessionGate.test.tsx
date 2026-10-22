import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { show } from '../features/test-helpers';
import { SessionGate } from './SessionGate';

afterEach(() => vi.restoreAllMocks());
function session(authenticated: boolean, mode = 'token') {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify({ authenticated, mode }), {
      headers: { 'Content-Type': 'application/json' },
    }),
  );
}
describe('session boundary', () => {
  it('keeps protected children hidden while startup is pending', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
    show(
      <SessionGate>
        <div>Protected workspace</div>
      </SessionGate>,
    );
    expect(screen.queryByText('Protected workspace')).not.toBeInTheDocument();
  });
  it('opens an authenticated workspace and hides it on session expiry', async () => {
    session(true);
    show(
      <SessionGate>
        <div>Protected workspace</div>
      </SessionGate>,
    );
    await screen.findByText('Protected workspace');
    window.dispatchEvent(new Event('timone:unauthorized'));
    await screen.findByLabelText('Access token');
    expect(screen.queryByText('Protected workspace')).not.toBeInTheDocument();
  });
  it('directs proxy sessions to their identity provider', async () => {
    session(false, 'proxy');
    show(
      <SessionGate>
        <div>Protected workspace</div>
      </SessionGate>,
    );
    await screen.findByText(/configured identity proxy/);
    expect(screen.queryByLabelText('Access token')).not.toBeInTheDocument();
  });
  it('recovers from a rejected token without revealing protected content', async () => {
    const fetch = session(false);
    show(
      <SessionGate>
        <div>Protected workspace</div>
      </SessionGate>,
    );
    const input = await screen.findByLabelText('Access token');
    fetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { message: 'Invalid token' } }), { status: 401 }),
    );
    fireEvent.change(input, { target: { value: 'a'.repeat(32) } });
    fireEvent.submit(input.closest('form')!);
    await screen.findByRole('alert');
    expect(screen.queryByText('Protected workspace')).not.toBeInTheDocument();
    fetch.mockResolvedValueOnce(new Response('{}'));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Open workspace/ })).toBeEnabled(),
    );
    fireEvent.submit(input.closest('form')!);
    await screen.findByText('Protected workspace');
  });
});
