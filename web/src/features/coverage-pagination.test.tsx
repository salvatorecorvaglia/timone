import { screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { Overview } from './Overview';
import { Resources } from './Resources';
import { mockAPI, show } from './test-helpers';

it('uses unknown styling for unreadable deployment coverage', async () => {
  const fetch = mockAPI();
  const original = fetch.getMockImplementation()!;
  fetch.mockImplementation(async (input, options) => {
    const response = await original(input, options);
    if (String(input).startsWith('/api/v1/overview')) {
      const data = await response.json();
      delete data.counts.Deployment;
      data.availableReplicas = 0;
      data.desiredReplicas = 0;
      data.warnings = [{ kind: 'Deployment', message: 'Denied' }];
      return new Response(JSON.stringify(data));
    }
    return response;
  });
  show(<Overview namespace="" />);
  const detail = await screen.findByText('Collection unavailable');
  expect(detail).toHaveClass('unknown');
  expect(detail).not.toHaveClass('healthy');
});
it('moves an obsolete offset back to the last nonempty page', async () => {
  const fetch = mockAPI();
  const original = fetch.getMockImplementation()!;
  fetch.mockImplementation(async (input, options) => {
    if (String(input).startsWith('/api/v1/resources?')) {
      const url = new URL(String(input), 'http://localhost');
      const offset = Number(url.searchParams.get('offset'));
      return new Response(
        JSON.stringify({ items: [], total: 16, offset, limit: 25, truncated: false }),
      );
    }
    return original(input, options);
  });
  show(<Resources namespace="" />, '/resources?offset=5000');
  await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('offset=0'));
  expect(await screen.findByText('0 of 16 resources')).toBeVisible();
  vi.restoreAllMocks();
});
