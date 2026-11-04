import { useState } from 'react';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { App } from '../app/App';
import { SavedWorkspaceProvider, FavoriteResource, SavedWorkspace } from './SavedWorkspace';
import { RunbookLinks } from './RunbookLinks';
import { WorkloadLogInvestigation } from './WorkloadLogs';
import type { Cluster, Detail, Issue } from '../types';
import { fixtures, mockAPI, show } from './test-helpers';

it('saves URL filters and reopens a named view', async () => {
  mockAPI();
  show(<App />, '/resources?kind=Pod&namespace=production&status=critical');
  await screen.findByRole('heading', { name: 'Resources' });
  fireEvent.click(screen.getByText('Save this view'));
  fireEvent.change(screen.getByRole('textbox', { name: 'Saved view name' }), {
    target: { value: 'Production failures' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save view' }));
  await screen.findByText('Saved in this browser.');
  fireEvent.click(screen.getByRole('link', { name: 'Open saved workspace' }));
  const link = await screen.findByRole('link', { name: 'Production failures' });
  expect(link).toHaveAttribute('href', '/resources?kind=Pod&namespace=production&status=critical');
  fireEvent.click(link);
  await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('status=critical'));
});
it('keeps favorites usable in the current session when persistence is denied', async () => {
  const cluster = { ...(fixtures['/api/v1/cluster'] as Cluster), key: 'test-cluster' };
  const resource = (fixtures['/api/v1/resources/Deployment/production/checkout-api'] as Detail)
    .resource;
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new DOMException('Storage denied', 'SecurityError');
  });
  show(
    <SavedWorkspaceProvider cluster={cluster}>
      <FavoriteResource resource={resource} />
      <SavedWorkspace />
    </SavedWorkspaceProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Favorite resource' }));
  expect(screen.getByRole('button', { name: 'Remove favorite' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  expect(screen.getByRole('link', { name: 'Deployment/checkout-api' })).toBeVisible();
  expect(screen.getByText(/Saved for this session only/)).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Remove Deployment/checkout-api' }));
  expect(screen.queryByRole('link', { name: 'Deployment/checkout-api' })).not.toBeInTheDocument();
});
it('renders matching HTTPS runbook references and filters unsafe protocols', () => {
  const issue = {
    ruleIDs: ['container-failure'],
    runbooks: [
      { id: 'ok', title: 'Crash runbook', url: 'https://docs.example/runbook' },
      { id: 'bad', title: 'Unsafe', url: 'javascript:alert(1)' },
    ],
  } as Issue;
  show(<RunbookLinks issue={issue} />);
  expect(screen.getByRole('link', { name: 'Crash runbook' })).toHaveAttribute(
    'rel',
    'noopener noreferrer',
  );
  expect(screen.queryByRole('link', { name: 'Unsafe' })).not.toBeInTheDocument();
  expect(screen.getByText('container-failure')).toBeVisible();
});
it('discovers and applies an explicit source selection without losing its Pod UID', async () => {
  const fetcher = mockAPI();
  show(<WorkloadLogInvestigation kind="Deployment" namespace="production" name="checkout-api" />);
  fireEvent.click(screen.getByText('Select Pod/container sources'));
  const catalog = fixtures['/api/v1/resources/Deployment/production/checkout-api/log-sources'] as {
    sources: { pod: string; container: string; uid: string }[];
  };
  const source = catalog.sources.at(-1)!;
  const option = await screen.findByRole('checkbox', { name: source.pod + '/' + source.container });
  fireEvent.click(option);
  fireEvent.click(screen.getByRole('button', { name: 'Apply selected sources' }));
  await waitFor(() => {
    expect(
      fetcher.mock.calls.some(([url]) => {
        const u = new URL(String(url), 'http://timone.test');
        const selected = u.searchParams.get('sources');
        return (
          selected !== null && JSON.stringify(JSON.parse(selected)) === JSON.stringify([source])
        );
      }),
    ).toBe(true);
  });
});

function ProfileHarness() {
  const base = fixtures['/api/v1/cluster'] as Cluster;
  const resource = (fixtures['/api/v1/resources/Deployment/production/checkout-api'] as Detail)
    .resource;

  const [key, setKey] = useState('cluster-a');
  return (
    <>
      <button onClick={() => setKey('cluster-a')}>Use cluster A</button>
      <button onClick={() => setKey('cluster-b')}>Use cluster B</button>
      <SavedWorkspaceProvider cluster={{ ...base, key }}>
        <FavoriteResource resource={resource} />
        <SavedWorkspace />
      </SavedWorkspaceProvider>
    </>
  );
}

it('separates favorites for identical display names on different cluster identities', () => {
  show(<ProfileHarness />);
  fireEvent.click(screen.getByRole('button', { name: 'Favorite resource' }));
  expect(screen.getByRole('link', { name: 'Deployment/checkout-api' })).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Use cluster B' }));
  expect(screen.queryByRole('link', { name: 'Deployment/checkout-api' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Favorite resource' })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  fireEvent.click(screen.getByRole('button', { name: 'Use cluster A' }));
  expect(screen.getByRole('link', { name: 'Deployment/checkout-api' })).toBeVisible();
});
