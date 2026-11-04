import { WorkloadLogInvestigation } from './WorkloadLogs';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it } from 'vitest';
import { IncidentInvestigation, StorageDetails } from './Investigation';
import { mockAPI, show } from './test-helpers';

const target = { kind: 'Deployment', namespace: 'production', name: 'checkout-api' };

it('loads a shared incident interval and keeps application logs opt-in', async () => {
  const fetcher = mockAPI();
  show(<IncidentInvestigation {...target} />);
  await screen.findByRole('button', { name: 'Export incident JSON' });
  expect(
    screen.getByRole('checkbox', { name: 'Include application logs in investigation and exports' }),
  ).not.toBeChecked();
  expect(screen.getByRole('heading', { name: 'Current conditions and problems' })).toBeVisible();
  expect(screen.getByRole('heading', { name: 'Timeline in selected interval' })).toBeVisible();
  fireEvent.click(
    screen.getByRole('checkbox', { name: 'Include application logs in investigation and exports' }),
  );
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => String(url).includes('includeLogs=true'))).toBe(true),
  );
  const calls = fetcher.mock.calls
    .map(([url]) => String(url))
    .filter((url) => url.includes('/incident?'));
  const params = new URL(calls[0], 'http://timone.test').searchParams;
  expect(params.get('since')).toBeTruthy();
  expect(params.get('until')).toBeTruthy();
});

it('shows source coverage and pauses live workload log refresh for previous instances', async () => {
  const fetcher = mockAPI();
  show(<WorkloadLogInvestigation {...target} />);
  await screen.findByRole('table', { name: 'Log source coverage' });
  expect(screen.getByRole('button', { name: 'Download retrieved logs' })).toBeVisible();
  fireEvent.click(screen.getByRole('checkbox', { name: /Live refresh/ }));
  await waitFor(() => {
    const paths = fetcher.mock.calls.map(([url]) => String(url));
    expect(
      paths.some(
        (path) =>
          path.includes('/logs?') && !new URL(path, 'http://timone.test').searchParams.has('until'),
      ),
    ).toBe(true);
  });
  fireEvent.click(screen.getByRole('checkbox', { name: 'Previous instances' }));
  expect(screen.getByRole('checkbox', { name: /Live refresh/ })).toBeDisabled();
  fireEvent.change(screen.getByRole('textbox', { name: 'Search aggregated logs' }), {
    target: { value: 'does-not-exist' },
  });
  expect(await screen.findByText('No matching timestamped lines in this interval.')).toBeVisible();
});

it('renders storage references and controller evidence without exposing class parameters', async () => {
  mockAPI();
  show(<StorageDetails kind="StatefulSet" namespace="production" name="redis" />);
  await screen.findByRole('link', { name: 'redis-data' });
  expect(screen.getByText('Declared storage relationships')).toBeVisible();
  expect(screen.getByText('Retained storage and mount events')).toBeVisible();
  expect(screen.getByRole('link', { name: 'redis-data' })).toBeVisible();
});
