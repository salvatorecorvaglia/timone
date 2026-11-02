import { fireEvent, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { TerminationDetails } from './Termination';
import { mockAPI, show } from './test-helpers';
import { shouldInvalidate } from '../lib/updates';

it('shows deletion timestamps and finalizers without offering mutation controls', async () => {
  mockAPI();
  show(<TerminationDetails kind="Pod" namespace="production" name="redis-0" />);
  const table = await screen.findByRole('table', { name: 'Deletion dependency evidence' });
  expect(table).toHaveTextContent('assigned-node');
  expect(table).toHaveTextContent('uid-verified');
  expect(screen.getByText('Time past deletion timestamp')).toBeVisible();
  expect(screen.getByText(/metadata.finalizers: demo.timone.io\/cleanup/)).toBeVisible();
  expect(screen.getByText(/Duration alone does not establish/)).toBeVisible();
  expect(screen.getByRole('button', { name: 'Export deletion analysis' })).toBeVisible();
  expect(
    screen.queryByRole('button', { name: /^(Force delete|Delete|Remove finalizer|Finalize)/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(
    screen.getByRole('checkbox', { name: 'Show only related resources marked for deletion' }),
  );
  expect(table).not.toHaveTextContent('assigned-node');
});
it('separates PVC protection requirements from declared consumers and backend evidence', async () => {
  mockAPI();
  show(
    <TerminationDetails kind="PersistentVolumeClaim" namespace="production" name="redis-data" />,
  );
  const table = await screen.findByRole('table', { name: 'Deletion dependency evidence' });
  expect(table).toHaveTextContent('redis-0');
  expect(table).toHaveTextContent('bound-volume');
  expect(table).toHaveTextContent('redis-attachment');
  expect(screen.getByText(/metadata.finalizers: kubernetes.io\/pvc-protection/)).toBeVisible();
  expect(table).toHaveTextContent('does not establish detach progress');
});
it('shows namespace-specific finalizers and makes incomplete API coverage explicit', async () => {
  mockAPI();
  show(<TerminationDetails kind="Namespace" namespace="" name="staging" />);
  await screen.findByRole('table', { name: 'Deletion inventory coverage' });
  expect(screen.getByText('spec.finalizers: kubernetes')).toBeVisible();
  expect(screen.getByRole('table', { name: 'Deletion dependency evidence' })).toHaveTextContent(
    'catalog-preview',
  );
  expect(
    screen.getByText(/custom resources and unavailable APIs may retain namespace content/),
  ).toBeVisible();
  expect(
    shouldInvalidate('/api/v1/resources/Namespace/_/staging/termination', {
      scopes: [{ kind: 'Pod', namespace: 'staging' }],
    }),
  ).toBe(true);
  expect(
    shouldInvalidate('/api/v1/resources/PersistentVolumeClaim/production/redis-data/termination', {
      scopes: [{ kind: 'VolumeAttachment', namespace: '' }],
    }),
  ).toBe(true);
});
