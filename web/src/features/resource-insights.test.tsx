import { fireEvent, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { EfficiencyDetails, ConfigurationDetails } from './ResourceInsights';
import { mockAPI, show } from './test-helpers';
import { savedURL } from '../lib/saved';
import { shouldInvalidate } from '../lib/updates';

it('shows sampled distributions, current configuration and coverage for explicitly selected windows', async () => {
  const fetch = mockAPI();
  show(
    <EfficiencyDetails kind="Deployment" namespace="production" name="checkout-api" />,
    '/resources/Deployment/production/checkout-api?tab=efficiency',
  );
  const table = await screen.findByRole('table', { name: 'Container resource efficiency' });
  expect(table).toHaveTextContent('checkout-api');
  expect(table).toHaveTextContent('P95:');
  expect(table).toHaveTextContent('valid points');
  expect(screen.getByText(/No automatic sizing or changes/)).toBeVisible();
  expect(screen.getByRole('button', { name: 'Export efficiency analysis' })).toBeVisible();
  fireEvent.change(screen.getByRole('combobox', { name: 'Efficiency observation window' }), {
    target: { value: '1h' },
  });
  await screen.findByRole('table', { name: 'Container resource efficiency' });
  expect(screen.getByTestId('location')).toHaveTextContent('efficiencyRange=1h');
  expect(fetch.mock.calls.some(([path]) => String(path).endsWith('efficiency?range=1h'))).toBe(
    true,
  );
});
it('shows desired and current configuration references without payloads or inferred loaded versions', async () => {
  mockAPI();
  show(<ConfigurationDetails kind="Deployment" namespace="production" name="checkout-api" />);
  const table = await screen.findByRole('table', { name: 'Configuration dependency references' });
  expect(table).toHaveTextContent('checkout-db');
  expect(table).toHaveTextContent('key-unverified');
  expect(table).toHaveTextContent('desired-template');
  expect(table).toHaveTextContent('current-pod');
  expect(table).toHaveTextContent('running environment values are not refreshed automatically');
  expect(
    screen.getByText(/Secret payloads and their key inventory are never returned/),
  ).toBeVisible();
  expect(screen.getByRole('button', { name: 'Export configuration analysis' })).toBeVisible();
});
it('retains efficiency filters in saved views and invalidates dependencies when their inputs change', () => {
  expect(
    savedURL('/resources/Deployment/production/web?tab=efficiency&efficiencyRange=6h'),
  ).toContain('efficiencyRange=6h');
  expect(
    shouldInvalidate('/api/v1/resources/Deployment/production/web/configuration', {
      scopes: [{ kind: 'Secret', namespace: 'production' }],
    }),
  ).toBe(true);
  expect(
    shouldInvalidate('/api/v1/resources/Deployment/production/web/efficiency?range=24h', {
      scopes: [{ kind: 'Pod', namespace: 'production' }],
    }),
  ).toBe(true);
  expect(
    shouldInvalidate('/api/v1/resources/Deployment/production/web/configuration', {
      scopes: [{ kind: 'Secret', namespace: 'staging' }],
    }),
  ).toBe(false);
});
