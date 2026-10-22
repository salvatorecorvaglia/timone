import { screen, fireEvent, waitFor } from '@testing-library/react';
import { Route, Routes } from 'react-router-dom';
import { vi, describe, it, expect } from 'vitest';
import { App } from '../app/App';
import { ResourceDetail } from './ResourceDetail';
import { CustomResources } from './CustomResources';
import { show, mockAPI } from './test-helpers';

describe('Custom resource explorer', () => {
  it('navigates from the workspace to CRD types, filters by name and opens projected details', async () => {
    mockAPI();
    show(<App />);
    fireEvent.click(await screen.findByRole('link', { name: 'Custom resources' }));
    expect(await screen.findByRole('heading', { name: 'Custom resources' })).toBeInTheDocument();
    await screen.findByRole('link', { name: 'payments-db' });
    fireEvent.change(screen.getByLabelText('Search custom resources'), {
      target: { value: 'payments' },
    });
    await waitFor(() =>
      expect(screen.queryByRole('link', { name: 'orders-db' })).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('location')).toHaveTextContent('search=payments');
    fireEvent.click(await screen.findByRole('link', { name: 'payments-db' }));
    expect(await screen.findByRole('heading', { name: 'payments-db' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Observed YAML' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Container logs' })).not.toBeInTheDocument();
  });
  it('preserves explicit type/version/health selections in shareable URLs', async () => {
    const api = mockAPI();
    show(
      <CustomResources namespace="" />,
      '/custom-resources?definition=databases.platform.timone.io&version=v1beta1',
    );
    await screen.findByRole('link', { name: 'payments-db' });
    expect(screen.getByLabelText('Custom resource version')).toHaveValue('v1beta1');
    fireEvent.change(screen.getByLabelText('Custom resource health'), {
      target: { value: 'warning' },
    });
    await waitFor(() =>
      expect(screen.queryByRole('link', { name: 'payments-db' })).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('location')).toHaveTextContent('version=v1beta1');
    expect(screen.getByTestId('location')).toHaveTextContent('status=warning');
    expect(api.mock.calls.some(([input]) => String(input).includes('version=v1beta1'))).toBe(true);
  });
  it('keeps cluster-scoped custom types visible under a namespace filter', async () => {
    mockAPI();
    show(
      <CustomResources namespace="production" />,
      '/custom-resources?definition=databases.platform.timone.io&namespace=production',
    );
    fireEvent.change(await screen.findByLabelText('Custom resource type'), {
      target: { value: 'clusterprofiles.platform.timone.io' },
    });
    expect(await screen.findByRole('link', { name: 'production-baseline' })).toBeInTheDocument();
    expect(screen.getByText(/namespace filter does not restrict this type/)).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('namespace=production');
  });
  it('shows UID-correlated events and keyboard-accessible observed YAML without custom secrets', async () => {
    mockAPI();
    show(
      <Routes>
        <Route
          path="/custom-resources/:definition/:namespace/:name"
          element={<ResourceDetail custom />}
        />
      </Routes>,
      '/custom-resources/databases.platform.timone.io/production/orders-db',
    );
    const overview = await screen.findByRole('tab', { name: 'Overview' });
    fireEvent.keyDown(overview, { key: 'ArrowRight' });
    expect(await screen.findByLabelText('Resource YAML')).toHaveTextContent('kind: Database');
    expect(screen.getByText(/This YAML is not a complete manifest/)).toBeInTheDocument();
    expect(screen.getByLabelText('Resource YAML')).not.toHaveTextContent(
      'SIMULATED-OMITTED-CUSTOM-SECRET',
    );
    fireEvent.click(screen.getByRole('tab', { name: 'Events (1)' }));
    expect(
      await screen.findByText(/Simulated controller report: backup destination/),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Database/orders-db' })).toHaveAttribute(
      'href',
      expect.stringContaining('version='),
    );
  });
  it('explains disabled browsing and preserves discovery/permission failures', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [],
          warnings: [],
          enabled: false,
          mode: 'cluster',
          message: 'Custom resource browsing is disabled.',
        }),
      ),
    );
    const view = show(<CustomResources namespace="" />);
    expect(
      await screen.findByRole('heading', { name: 'Enable custom resource browsing' }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText('Custom resource type')).not.toBeInTheDocument();
    view.unmount();
    vi.mocked(globalThis.fetch).mockImplementation(
      async () =>
        new Response(JSON.stringify({ error: { message: 'Kubernetes RBAC denied this read' } }), {
          status: 403,
        }),
    );
    show(<CustomResources namespace="" />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Kubernetes RBAC denied this read');
  });
});
