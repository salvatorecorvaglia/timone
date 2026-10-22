import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { Route, Routes } from 'react-router-dom';
import { vi, describe, it, expect } from 'vitest';
import { App } from '../app/App';
import { Resources } from './Resources';
import { ResourceDetail } from './ResourceDetail';
import { HelmRelease } from './Helm';
import { Issues, Timeline, MetricsPage, Settings, Integrations } from './Observability';
import { APIError, request } from '../lib/api';
import type { Graph, Allocation } from '../types';
import { buildGraph } from './Topology';
import { NodeBudgets } from './NodeBudgets';
import { show, mockAPI, fixtures } from './test-helpers';

describe('Timone product flows', () => {
  it('shows an explicitly simulated workspace and navigates to real resource details', async () => {
    mockAPI();
    show(<App />);
    expect(await screen.findByText('Demo environment')).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: 'Cluster overview' })).toBeInTheDocument();
    expect(screen.getByText('No real cluster is connected.', { exact: false })).toBeInTheDocument();
    expect(await screen.findByText('4 issues need your attention')).toBeInTheDocument();
    const links = screen.getAllByRole('link', { name: /checkout-api/ });
    fireEvent.click(links[0]);
    expect(await screen.findByRole('heading', { name: 'checkout-api' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Manifest' })).toBeInTheDocument();
  });
  it('sends combinable table filters through the URL and displays the backend matches', async () => {
    mockAPI();
    show(<Resources namespace="" />, '/resources');
    await screen.findByText('storefront-7c8d9f-k9x2p');
    fireEvent.change(screen.getByRole('textbox', { name: 'Search resources' }), {
      target: { value: 'payments' },
    });
    await waitFor(() =>
      expect(screen.queryByText('storefront-7c8d9f-k9x2p')).not.toBeInTheDocument(),
    );
    expect(await screen.findByText('payments-7c8d9f-k9x2p')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('search=payments');
    fireEvent.click(screen.getByRole('button', { name: 'Clear resource search' }));
    await screen.findByText('storefront-7c8d9f-k9x2p');
    fireEvent.change(screen.getByRole('combobox', { name: 'Resource health' }), {
      target: { value: 'critical' },
    });
    await waitFor(() =>
      expect(screen.queryByText('payments-7c8d9f-k9x2p')).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('location')).toHaveTextContent('status=critical');
  });
  it('supports keyboard tab navigation and shows redaction information before YAML', async () => {
    mockAPI();
    show(
      <Routes>
        <Route path="/resources/:kind/:namespace/:name" element={<ResourceDetail />} />
      </Routes>,
      '/resources/Deployment/production/checkout-api',
    );
    const tab = await screen.findByRole('tab', { name: 'Overview' });
    fireEvent.keyDown(tab, { key: 'ArrowRight' });
    expect(await screen.findByLabelText('Resource YAML')).toHaveTextContent('kind: Deployment');
    expect(screen.getByRole('tab', { name: 'Manifest' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByText(/Secret payloads, nested annotations/)).toBeInTheDocument();
  });
  it('separates observed evidence from root-cause hypotheses', async () => {
    mockAPI();
    show(
      <Routes>
        <Route path="/issues/:id" element={<Issues namespace="" />} />
      </Routes>,
      '/issues/issue-Deployment%3Aproduction%3Acheckout-api',
    );
    expect(await screen.findByRole('heading', { name: /Observed evidence/ })).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { name: /Potential causes · hypotheses/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Inspect resource/ })).toHaveAttribute(
      'href',
      '/resources/Deployment/production/checkout-api',
    );
  });
  it('shows the active administrator diagnostic thresholds from the backend', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [],
          warnings: [],
          diagnosticPolicy: {
            startupGraceSeconds: 0,
            warningEventWindowSeconds: 3600,
            restartThreshold: 12,
          },
        }),
      ),
    );
    show(<Issues namespace="" />);
    const disclosure = await screen.findByText('Active detection policy');
    fireEvent.click(disclosure);
    expect(disclosure.closest('details')).toHaveAttribute('open');
    expect(screen.getByText('0 seconds')).toBeInTheDocument();
    expect(screen.getByText('3600 seconds')).toBeInTheDocument();
    expect(screen.getByText('At least 12')).toBeInTheDocument();
    expect(
      screen.getByText(/Changing these thresholds requires restarting Timone/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Restart counts are cumulative across Pod containers, not a rate/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Startup\/readiness grace: 5 minutes/)).not.toBeInTheDocument();
  });
  it('does not fabricate diagnostic thresholds when the backend policy is unavailable', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ items: [], warnings: [] })),
    );
    show(<Issues namespace="" />);
    await screen.findByText('No matching issues');
    expect(screen.queryByText('Active detection policy')).not.toBeInTheDocument();
    expect(screen.queryByText('300 seconds')).not.toBeInTheDocument();
  });
  it('investigates a custom issue and opens its exact definition and served version', async () => {
    mockAPI();
    show(
      <Routes>
        <Route path="/issues/:id" element={<Issues namespace="" />} />
      </Routes>,
      '/issues/issue-databases.platform.timone.io%3Aproduction%3Aorders-db',
    );
    expect(await screen.findByRole('heading', { name: /Observed evidence/ })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Inspect resource/ })).toHaveAttribute(
      'href',
      '/custom-resources/databases.platform.timone.io/production/orders-db?version=v1',
    );
    expect(screen.getAllByText(/Ready=False at generation 2/).length).toBeGreaterThan(0);
    expect(screen.getByText('Custom resource coverage · available')).toBeInTheDocument();
    expect(screen.queryByText('SIMULATED-OMITTED-CUSTOM-SECRET')).not.toBeInTheDocument();
  });
  it('searches issues by custom definition without including unrelated workloads', async () => {
    mockAPI();
    show(<Issues namespace="" />, '/issues');
    await screen.findByText('Custom resource reports not ready');
    fireEvent.change(screen.getByLabelText('Search issues or affected resources…'), {
      target: { value: 'databases.platform.timone.io' },
    });
    expect(screen.getByText('Custom resource reports not ready')).toBeInTheDocument();
    expect(screen.queryByText('Container repeatedly failing')).not.toBeInTheDocument();
  });
  it('explains partial custom diagnostics and unverified readiness', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [],
          warnings: [{ kind: 'CustomResource', message: 'Some issue candidates omitted' }],
          customDiagnostics: {
            state: 'partial',
            typesInspected: 2,
            resourcesObserved: 2010,
            readinessUnknown: 7,
            candidatesOmitted: 10,
            candidateLimit: 2000,
            message: 'Only enabled custom types were inspected.',
          },
        }),
      ),
    );
    show(<Issues namespace="" />);
    const summary = await screen.findByText('Custom resource coverage · partial');
    fireEvent.click(summary);
    expect(summary.closest('details')).toHaveAttribute('open');
    expect(screen.getByText('2010')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
    expect(screen.getByText('10')).toBeInTheDocument();
    expect(
      screen.getByText(/Stale, missing or ambiguous conditions cannot establish current readiness/),
    ).toBeInTheDocument();
    expect(screen.getByText('Some issue candidates omitted')).toBeInTheDocument();
  });
  it('shows retained events and their occurrence counts', async () => {
    mockAPI();
    show(<Timeline namespace="" />);
    expect(
      (await screen.findAllByText(/Back-off restarting failed container/))[0],
    ).toBeInTheDocument();
    expect(screen.getAllByText(/13 occurrences/).length).toBeGreaterThan(0);
    expect(screen.getByText(/Timone does not persist history/)).toBeInTheDocument();
  });
  it('combines shareable event kind and exact resource filters using backend results', async () => {
    mockAPI();
    show(<Timeline namespace="" />, '/events');
    await screen.findAllByText(/Back-off restarting/);
    fireEvent.change(screen.getByRole('combobox', { name: 'Event resource kind' }), {
      target: { value: 'Job' },
    });
    await waitFor(() => expect(screen.queryByText(/Back-off restarting/)).not.toBeInTheDocument());
    expect(await screen.findByText('Job completed successfully')).toBeInTheDocument();
    fireEvent.change(screen.getByRole('textbox', { name: 'Exact event resource name' }), {
      target: { value: 'database-backup-29183040' },
    });
    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent(
        'kind=Job&resource=database-backup-29183040',
      ),
    );
    fireEvent.click(screen.getByRole('link', { name: 'Clear filters' }));
    expect((await screen.findAllByText(/Back-off restarting/))[0]).toBeInTheDocument();
  });
  it('preserves absolute event time boundaries in a shared URL', async () => {
    const api = mockAPI();
    show(
      <Timeline namespace="" />,
      '/events?since=2026-10-15T10%3A00%3A00Z&until=2026-10-15T12%3A00%3A00Z',
    );
    await screen.findByText('Job completed successfully');
    expect((screen.getByLabelText('Events from') as HTMLInputElement).value).toMatch(
      /^2026-10-15T/,
    );
    expect(
      api.mock.calls.some(([input]) => {
        const u = new URL(String(input), 'http://timone.test');
        return (
          u.pathname === '/api/v1/events' &&
          u.searchParams.get('since') === '2026-10-15T10:00:00Z' &&
          u.searchParams.get('until') === '2026-10-15T12:00:00Z'
        );
      }),
    ).toBe(true);
  });
  it('shows configured demand even when measured metrics are unavailable', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          ...(fixtures['/api/v1/metrics'] as object),
          available: false,
          items: [],
          message: 'Metrics Server read denied',
        }),
      ),
    );
    show(<MetricsPage namespace="" />);
    expect(
      await screen.findByRole('heading', { name: 'Metrics Server is not available' }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { name: 'Configured requests, limits & capacity' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Unscheduled')).toBeInTheDocument();
    expect(screen.getAllByText(/% requested/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/% of request/)).not.toBeInTheDocument();
  });
  it('shows finite node limits above allocatable without clamping the ratio', () => {
    const budget = {
      state: 'finite' as const,
      total: 6,
      knownSubtotal: 6,
      finitePods: 2,
      missingPods: 0,
      invalidPods: 0,
    };
    const row: Allocation = {
      node: 'worker',
      pods: 2,
      scope: 'namespace production only',
      cpuRequest: 1,
      memoryRequest: 0,
      cpuAllocatable: 2,
      memoryAllocatable: null,
      cpuLimits: budget,
      memoryLimits: { ...budget, total: 512 * 2 ** 20, knownSubtotal: 512 * 2 ** 20 },
    };
    show(<NodeBudgets allocations={[row]} />);
    expect(
      screen.getByRole('table', { name: 'Configured node resource budgets' }),
    ).toBeInTheDocument();
    expect(screen.getByText('6.000 cores')).toBeInTheDocument();
    expect(screen.getByText('300.0% of allocatable · Over allocatable')).toBeInTheDocument();
    expect(screen.getByText('Allocatable comparison unavailable')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'worker' })).toHaveAttribute(
      'href',
      '/resources/Node/_/worker',
    );
    expect(screen.getByText('namespace production only')).toBeInTheDocument();
  });
  it('keeps missing and invalid node limit totals distinct from finite subtotals', () => {
    const budget = {
      state: 'incomplete' as const,
      total: null,
      knownSubtotal: 2,
      finitePods: 1,
      missingPods: 1,
      invalidPods: 0,
    };
    const row: Allocation = {
      node: '',
      pods: 2,
      scope: 'all readable namespaces',
      cpuRequest: 1,
      memoryRequest: 0,
      cpuAllocatable: null,
      memoryAllocatable: null,
      cpuLimits: budget,
      memoryLimits: {
        ...budget,
        state: 'unknown',
        knownSubtotal: 128 * 2 ** 20,
        missingPods: 0,
        invalidPods: 1,
      },
    };
    show(<NodeBudgets allocations={[row]} />);
    expect(screen.getByText('Unscheduled')).toBeInTheDocument();
    expect(screen.getByText('Incomplete limit coverage')).toBeInTheDocument();
    expect(screen.getByText('Unknown limit total')).toBeInTheDocument();
    expect(screen.getByText('2.000 cores from fully bounded Pods only')).toBeInTheDocument();
    expect(screen.getByText('128 MiB from fully bounded Pods only')).toBeInTheDocument();
    expect(screen.getByText('1 finite · 1 missing/zero · 0 invalid')).toBeInTheDocument();
    expect(screen.queryByText(/% of allocatable/)).not.toBeInTheDocument();
  });
  it('shows unavailable node limit coverage without fabricating zero counts', () => {
    const budget = {
      state: 'unavailable' as const,
      total: null,
      knownSubtotal: null,
      finitePods: null,
      missingPods: null,
      invalidPods: null,
    };
    const row: Allocation = {
      node: 'worker',
      pods: null,
      scope: 'all readable namespaces',
      cpuRequest: null,
      memoryRequest: null,
      cpuAllocatable: 2,
      memoryAllocatable: 2 ** 30,
      cpuLimits: budget,
      memoryLimits: budget,
    };
    show(<NodeBudgets allocations={[row]} />);
    const cells = within(
      screen.getByRole('table', { name: 'Configured node resource budgets' }),
    ).getAllByRole('cell');
    expect(cells[4]).toHaveTextContent(/^Unavailable$/);
    expect(cells[5]).toHaveTextContent(/^Unavailable$/);
    expect(screen.queryByText(/0 finite/)).not.toBeInTheDocument();
    expect(screen.queryByText(/% of allocatable/)).not.toBeInTheDocument();
  });
  it('compares measured container consumption to readable request and limit budgets', async () => {
    mockAPI();
    show(<MetricsPage namespace="" />);
    await screen.findByRole('heading', { name: 'Metrics' });
    expect(screen.getByRole('columnheader', { name: 'CPU request / limit' })).toBeInTheDocument();
    expect(screen.getAllByText('0.100 cores / 0.500 cores').length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Request comparison unavailable/).length).toBeGreaterThan(0);
  });
  it('navigates from Helm metadata to retained history and compares revisions through the URL', async () => {
    mockAPI();
    show(
      <Routes>
        <Route path="/helm" element={<Integrations namespace="" filter="Helm" />} />
        <Route path="/helm/:namespace/:name" element={<HelmRelease />} />
      </Routes>,
      '/helm',
    );
    const links = await screen.findAllByRole('link', { name: 'checkout-api' });
    fireEvent.click(links[0]);
    expect(await screen.findByRole('heading', { name: 'Retained revisions' })).toBeInTheDocument();
    await screen.findByRole('table', { name: 'Revision metadata comparison' });
    fireEvent.change(screen.getByRole('combobox', { name: 'From Helm revision' }), {
      target: { value: '1' },
    });
    expect(
      await screen.findByText('Service', { selector: '.helm-inventory code' }),
    ).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('from=1&to=3');
    expect(
      screen.getByText(/No visible differences do not establish equality of redacted fields/),
    ).toBeInTheDocument();
  });
  it('inspects reported GitOps state without treating OutOfSync as confirmed drift', async () => {
    mockAPI();
    show(<Integrations namespace="" filter="GitOps" />, '/gitops');
    expect(await screen.findByText('OutOfSync', { exact: true })).toBeInTheDocument();
    const summaries = screen.getAllByText('Inspect reported state');
    for (const summary of summaries) fireEvent.click(summary);
    expect(
      screen.getByText(/Controller reports OutOfSync; the desired revision may have changed/),
    ).toBeInTheDocument();
    expect(screen.getByText('Controller reports drift')).toBeInTheDocument();
    expect(screen.getAllByText('Not assessed').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Observed generation matches').length).toBeGreaterThan(0);
  });
  it('keeps Helm opt-in failures visible instead of showing an empty history', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify({
            error: {
              message: 'Helm details disabled. Administrator must set TIMONE_HELM_DETAILS=true',
            },
          }),
          { status: 403 },
        ),
    );
    show(
      <Routes>
        <Route path="/helm/:namespace/:name" element={<HelmRelease />} />
      </Routes>,
      '/helm/production/checkout-api',
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('Helm details disabled');
    expect(screen.queryByRole('combobox', { name: 'From Helm revision' })).not.toBeInTheDocument();
  });
  it('directs unauthenticated proxy users through their organization without offering token login', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify({
            authenticated: false,
            authRequired: true,
            mode: 'proxy',
            identity: { name: '', method: '' },
          }),
        ),
    );
    show(<App />);
    expect(await screen.findByRole('alert')).toHaveTextContent('configured identity proxy');
    expect(screen.queryByLabelText('Access token', { exact: true })).not.toBeInTheDocument();
  });
  it('shows proxy identity and excludes private proxy credentials from MCP configuration', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    api.mockImplementation((input, options) =>
      String(input) === '/api/v1/session'
        ? Promise.resolve(
            new Response(
              JSON.stringify({
                authenticated: true,
                authRequired: true,
                mode: 'proxy',
                identity: { name: 'alice@example.org', method: 'proxy' },
              }),
            ),
          )
        : original(input, options),
    );
    show(<Settings />, '/settings');
    expect(await screen.findByText(/Workspace identity: alice@example.org/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
    expect(screen.getByText(/never distribute that credential to clients/)).toBeInTheDocument();
  });
  it('notifies the shell when its trusted proxy authentication expires', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify({ error: { message: 'Identity proxy authentication required' } }),
          { status: 401, headers: { 'X-Timone-Auth-Required': 'proxy' } },
        ),
    );
    const listener = vi.fn();
    window.addEventListener('timone:unauthorized', listener);
    await expect(request('/api/v1/overview')).rejects.toBeInstanceOf(APIError);
    expect(listener).toHaveBeenCalledOnce();
    window.removeEventListener('timone:unauthorized', listener);
  });
  it('shows a loading state while resources are pending', async () => {
    let release: ((value: Response) => void) | undefined;
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );
    show(<MetricsPage namespace="" />);
    expect(screen.getByText(/Loading cluster data/)).toBeInTheDocument();
    release?.(new Response(JSON.stringify(fixtures['/api/v1/metrics'])));
    expect(await screen.findByRole('heading', { name: 'Metrics' })).toBeInTheDocument();
  });
  it('reports missing metrics rather than rendering fabricated usage', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          available: false,
          source: 'metrics.k8s.io',
          message: 'Metrics Server unavailable or RBAC denied',
          items: [],
        }),
      ),
    );
    show(<MetricsPage namespace="" />);
    expect(
      await screen.findByRole('heading', { name: 'Metrics Server is not available' }),
    ).toBeInTheDocument();
    expect(screen.queryByText('0.00 cores')).not.toBeInTheDocument();
  });
  it('reports a backend error and offers retry', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(JSON.stringify({ error: { message: 'Kubernetes RBAC denied this read' } }), {
          status: 403,
        }),
    );
    show(<Resources namespace="production" />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Kubernetes RBAC denied this read');
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });
  it('opens global search with the keyboard and closes it with Escape', async () => {
    mockAPI();
    show(<App />);
    await screen.findByText('Demo environment');
    fireEvent.keyDown(window, { key: 'k', metaKey: true });
    expect(
      await screen.findByRole('dialog', { name: 'Global resource search' }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole('textbox', { name: 'Search all resources' }),
    ).toBeInTheDocument();
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });
  it('builds a bounded topology and highlights direct neighbors without fabricating edges', () => {
    const input = fixtures['/api/v1/topology'] as Graph;
    const selected = 'Service:production:checkout-api';
    const g = buildGraph(input, {
      search: 'checkout',
      group: '',
      limit: 60,
      showNamespaces: false,
      showPods: true,
      selected,
      labels: false,
    });
    expect(g.nodes.length).toBeLessThanOrEqual(60);
    expect(g.edges.filter((e) => e.source === selected)).toHaveLength(3);
    const n = g.nodes.find((n) => n.id === 'Pod:production:checkout-api-7c8d9f-m4n7q');
    expect(n?.style?.opacity).toBe(1);
    const unrelated = g.nodes.find((n) => n.id === 'Service:production:payments');
    expect(unrelated?.style?.opacity).toBe(0.3);
    for (const e of g.edges) {
      expect(input.edges.some((actual) => actual.id === e.id)).toBe(true);
    }
  });
  it('distinguishes cluster API failure from a functioning application shell', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    api.mockImplementation((input, options) =>
      String(input).startsWith('/api/v1/connection')
        ? Promise.resolve(
            new Response(
              JSON.stringify({
                ...(fixtures['/api/v1/connection'] as object),
                state: 'disconnected',
                mode: 'cluster',
                message: 'Kubernetes API connection failed',
                auth: 'token',
                discovery: 'client-go discovery',
              }),
            ),
          )
        : original(input, options),
    );
    show(<App />);
    expect(await screen.findByText('API disconnected')).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: 'Cluster overview' })).toBeInTheDocument();
  });
  it('shows simulated observer status in connection settings without implying a live cluster', async () => {
    mockAPI();
    show(<Settings />, '/settings');
    expect(await screen.findByText('simulated')).toBeInTheDocument();
    expect(screen.getByText(/No Kubernetes informer is started/)).toBeInTheDocument();
  });
  it('keeps auth failures as typed errors and notifies the shell', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { message: 'Authentication required' } }), {
        status: 401,
        headers: { 'WWW-Authenticate': 'Bearer' },
      }),
    );
    const listener = vi.fn();
    window.addEventListener('timone:unauthorized', listener);
    await expect(request('/api/v1/overview')).rejects.toBeInstanceOf(APIError);
    expect(listener).toHaveBeenCalledOnce();
    window.removeEventListener('timone:unauthorized', listener);
  });
  it('keeps a Kubernetes 401 separate from the Timone login session', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { message: 'Kubernetes authentication failed' } }), {
        status: 401,
      }),
    );
    const listener = vi.fn();
    window.addEventListener('timone:unauthorized', listener);
    await expect(request('/api/v1/resources?kind=Pod')).rejects.toMatchObject({
      status: 401,
      message: 'Kubernetes authentication failed',
    });
    expect(listener).not.toHaveBeenCalled();
    window.removeEventListener('timone:unauthorized', listener);
  });
});
