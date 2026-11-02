import { fireEvent, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { NetworkDetails, BatchDetails } from './NetworkBatch';
import { mockAPI, show } from './test-helpers';
it('shows published endpoint evidence and preserves explicit source selection in the URL', async () => {
  const fetch = mockAPI();
  show(
    <NetworkDetails kind="Service" namespace="production" name="checkout-api" />,
    '/resources/Service/production/checkout-api?tab=network',
  );
  await screen.findByRole('table', { name: 'Published network endpoints' });
  expect(screen.getByText(/This view does not test connectivity/)).toBeVisible();
  expect(screen.getByRole('table', { name: 'Published network endpoints' })).toHaveTextContent(
    'checkout-api-7c8d9f-k9x2p',
  );
  fireEvent.change(screen.getByRole('textbox', { name: 'Source Pod' }), {
    target: { value: 'payments-7c8d9f-k9x2p' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Analyze source Pod' }));
  expect(screen.getByTestId('location')).toHaveTextContent('sourcePod=payments-7c8d9f-k9x2p');
  await screen.findByRole('button', { name: 'Export network analysis' });
  expect(
    fetch.mock.calls.some(([path]) => String(path).includes('sourceNamespace=production')),
  ).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Clear source Pod' }));
  expect(screen.getByTestId('location')).not.toHaveTextContent('sourcePod=');
});
it('shows retained CronJob executions and reported scheduling configuration', async () => {
  mockAPI();
  show(<BatchDetails kind="CronJob" namespace="platform" name="database-backup" />);
  const jobs = await screen.findByRole('table', { name: 'Retained Job executions' });
  expect(jobs).toHaveTextContent('database-backup-29183040');
  expect(jobs).toHaveTextContent('complete');
  expect(screen.getByText('0 2 * * * · Europe/Rome')).toBeVisible();
  expect(screen.getByText(/missing history does not establish/)).toBeVisible();
  expect(screen.getByRole('button', { name: 'Export execution analysis' })).toBeVisible();
});
it('shows Job failed-Pod counts separately from controller terminal state', async () => {
  mockAPI();
  show(<BatchDetails kind="Job" namespace="platform" name="database-backup-29183040" />);
  const jobs = await screen.findByRole('table', { name: 'Retained Job executions' });
  expect(jobs).toHaveTextContent('Failed Pods: Not reported');
  expect(jobs).toHaveTextContent('complete');
  expect(screen.queryByText('Schedule / time zone')).not.toBeInTheDocument();
});
