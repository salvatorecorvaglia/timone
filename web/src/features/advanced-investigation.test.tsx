import { fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it } from 'vitest';
import { App } from '../app/App';
import { mockAPI, show } from './test-helpers';

it('offers safe investigation exports and advanced log selection', async () => {
  const fetcher = mockAPI();
  show(<App />, '/resources/Pod/production/checkout-api-7c8d9f-m4n7q?tab=logs');
  // The detail route is lazy-loaded; allow time for its module under coverage/CPU load.
  await screen.findByRole('button', { name: 'Export JSON' }, { timeout: 5000 });
  expect(screen.getByRole('button', { name: 'Export Markdown' })).toBeVisible();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Previous instance' }));
  fireEvent.change(screen.getByRole('combobox', { name: 'Log lines' }), {
    target: { value: '500' },
  });
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => String(url).includes('tail=500&previous=true'))).toBe(
      true,
    ),
  );
  expect(screen.getByRole('checkbox', { name: 'Live refresh' })).toBeDisabled();
});
