import { screen, fireEvent } from '@testing-library/react';
import { expect, it } from 'vitest';
import { SchedulingDetails, RolloutDetails, NodeMaintenanceDetails } from './Operations';
import { mockAPI, show } from './test-helpers';
it('shows per-node scheduling evidence without claiming eligibility', async () => {
  mockAPI();
  show(<SchedulingDetails namespace="production" name="notification-worker-7c8d9f-m4n7q" />);
  await screen.findByRole('table', { name: 'Node scheduling constraints' });
  expect(screen.getByText(/Passing these checks does not establish/)).toBeVisible();
  expect(
    screen.getByRole('checkbox', { name: 'Show only nodes with observed constraints' }),
  ).toBeVisible();
  expect(screen.getByRole('button', { name: 'Export scheduling analysis' })).toBeVisible();
  fireEvent.click(
    screen.getByRole('checkbox', { name: 'Show only nodes with observed constraints' }),
  );
});
it('compares rollout counters, revisions and owned Pod evidence', async () => {
  mockAPI();
  show(<RolloutDetails kind="Deployment" namespace="production" name="checkout-api" />);
  await screen.findByRole('table', { name: 'Rollout revisions' });
  expect(screen.getByRole('table', { name: 'Rollout Pods' })).toHaveTextContent('checkout-api');
  expect(screen.getByText('Desired generation')).toBeVisible();
  expect(screen.getByText('Reported target revision')).toBeVisible();
  expect(screen.getByRole('button', { name: 'Export rollout analysis' })).toBeVisible();
  expect(screen.getAllByRole('link', { name: 'Scheduling' }).length).toBeGreaterThan(0);
});
it('shows shared disruption budgets and exposes no drain action', async () => {
  mockAPI();
  show(<NodeMaintenanceDetails name="harbor-worker-01" namespace="" />);
  await screen.findByRole('table', { name: 'Node maintenance Pod checks' });
  expect(screen.getByRole('table', { name: 'Shared node disruption budgets' })).toBeVisible();
  expect(screen.getByText(/No drain, cordon or eviction action is available/)).toBeVisible();
  expect(screen.queryByRole('button', { name: /^(Drain|Cordon|Evict)/ })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Export maintenance analysis' })).toBeVisible();
});
