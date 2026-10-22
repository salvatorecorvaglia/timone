import { fireEvent, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { App } from '../app/App';
import { Settings } from './Settings';
import { mockAPI, show } from './test-helpers';

it('keeps startup and preference changes usable when browser storage is denied', async () => {
  mockAPI();
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new DOMException('Denied', 'SecurityError');
  });
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new DOMException('Denied', 'SecurityError');
  });
  show(<App />, '/settings');
  expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Light' }));
  expect(document.documentElement.dataset.theme).toBe('light');
});

it('shows a retryable sign-out failure and prevents duplicate revocation requests', async () => {
  const fetch = mockAPI();
  const original = fetch.getMockImplementation()!;
  let rejectLogout: ((error: Error) => void) | undefined;
  fetch.mockImplementation((input, options) => {
    if (options?.method === 'DELETE')
      return new Promise((_resolve, reject) => {
        rejectLogout = reject;
      });
    if (String(input) === '/api/v1/session')
      return Promise.resolve(
        new Response(JSON.stringify({ authenticated: true, authRequired: true, mode: 'token' })),
      );
    return original(input, options);
  });
  show(<Settings />);
  const button = await screen.findByRole('button', { name: 'Sign out' });
  fireEvent.click(button);
  expect(screen.getByRole('button', { name: 'Signing out…' })).toBeDisabled();
  fireEvent.click(button);
  expect(fetch.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(1);
  rejectLogout!(new Error('Offline'));
  expect(await screen.findByRole('alert')).toHaveTextContent('Your session may still be active');
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeEnabled();
});
