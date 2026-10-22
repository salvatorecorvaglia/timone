import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { loadNamespaces } from './namespaces';
import { shouldInvalidate } from './updates';
import { withNamespace } from './navigation';
import { ScopedLink } from '../components/navigation';
import { Resources } from '../features/Resources';
import { ResourceDetail } from '../features/ResourceDetail';
import { EffectiveAuthorization } from '../features/EffectiveAuthorization';
import fixtures from '../test-fixtures.json';

function NextPod() {
  const navigate = useNavigate();
  return (
    <button onClick={() => navigate('/resources/Pod/production/other?tab=logs')}>Next Pod</button>
  );
}

function show(children: ReactNode, path = '/') {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })}
    >
      <MemoryRouter initialEntries={[path]}>{children}</MemoryRouter>
    </QueryClientProvider>,
  );
}
const pod = fixtures['/api/v1/resources/Pod/production/checkout-api-7c8d9f-m4n7q'];
describe('remediation contracts', () => {
  it('fetches every namespace page and preserves the exact pagination contract', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = new URL(String(input), 'http://timone.test');
      expect(url.searchParams.get('limit')).toBe('200');
      const offset = Number(url.searchParams.get('offset'));
      expect([0, 200]).toContain(offset);
      const items = Array.from({ length: offset === 0 ? 200 : 1 }, (_, i) => ({
        name: `ns-${offset + i}`,
      }));
      return new Response(
        JSON.stringify({ items, total: 201, offset, limit: 200, truncated: false }),
      );
    });
    const result = await loadNamespaces();
    expect(result.items).toHaveLength(201);
    expect(result.truncated).toBe(false);
    expect(fetch.mock.calls.map((c) => String(c[0]))).toEqual([
      '/api/v1/namespaces?limit=200&offset=0',
      '/api/v1/namespaces?limit=200&offset=200',
    ]);
  });
  it('fails explicitly if namespace pagination stops early', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({ items: [], total: 201, limit: 200, offset: 0, truncated: false }),
      ),
    );
    await expect(loadNamespaces()).rejects.toThrow('stopped before completion');
  });
  it('does not turn provider truncation into complete coverage', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [{ name: 'visible' }],
          total: 1,
          limit: 200,
          offset: 0,
          truncated: true,
        }),
      ),
    );
    expect((await loadNamespaces()).truncated).toBe(true);
  });
  it('preserves namespace context without overriding explicit destination scope', () => {
    expect(withNamespace('/resources/Pod/production/app?tab=logs', 'production')).toBe(
      '/resources/Pod/production/app?tab=logs&namespace=production',
    );
    expect(withNamespace('/issues?namespace=staging', 'production')).toBe(
      '/issues?namespace=staging',
    );
    expect(withNamespace('https://example.test/docs', 'production')).toBe(
      'https://example.test/docs',
    );
    show(<ScopedLink to="/issues">Investigate</ScopedLink>, '/?namespace=production');
    expect(screen.getByRole('link', { name: 'Investigate' })).toHaveAttribute(
      'href',
      '/issues?namespace=production',
    );
  });
  it('invalidates affected kinds and namespaces without polling unrelated metadata', () => {
    const change = { scopes: [{ kind: 'Pod', namespace: 'production' }] };
    expect(shouldInvalidate('/api/v1/resources?kind=Pod&namespace=production', change)).toBe(true);
    expect(shouldInvalidate('/api/v1/resources?kind=Pod&namespace=staging', change)).toBe(false);
    expect(shouldInvalidate('/api/v1/resources?kind=Service&namespace=production', change)).toBe(
      false,
    );
    expect(shouldInvalidate('/api/v1/connection', change)).toBe(false);
    expect(shouldInvalidate('/api/v1/overview?namespace=production', change)).toBe(true);
    expect(shouldInvalidate('/api/v1/metrics/history?kind=Pod', { full: true })).toBe(false);
    expect(
      shouldInvalidate('/api/v1/resources/Pod/production/app', {
        scopes: [{ kind: 'Event', namespace: 'production' }],
      }),
    ).toBe(true);
    expect(shouldInvalidate('/api/v1/resources?kind=Pod', { full: true })).toBe(true);
  });
  it('offers init and ephemeral logs and resets the selected container on target navigation', async () => {
    const paths: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const path = String(input);
      paths.push(path);
      if (path.includes('/logs')) return new Response(JSON.stringify({ logs: 'log data' }));
      const detail = structuredClone(pod);
      const spec: Record<string, unknown> = detail.manifest.spec;
      spec.initContainers = [{ name: 'bootstrap' }];
      spec.ephemeralContainers = [{ name: 'debugger' }];
      return new Response(JSON.stringify(detail));
    });
    show(
      <>
        <NextPod />
        <Routes>
          <Route path="/resources/:kind/:namespace/:name" element={<ResourceDetail />} />
        </Routes>
      </>,
      '/resources/Pod/production/checkout?tab=logs',
    );
    const selector = await screen.findByLabelText('Container');
    expect(screen.getByRole('option', { name: 'bootstrap' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'debugger' })).toBeInTheDocument();
    fireEvent.change(selector, { target: { value: 'bootstrap' } });
    await waitFor(() =>
      expect(paths.some((p) => p.includes('/checkout/logs?container=bootstrap'))).toBe(true),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Next Pod' }));
    await waitFor(() => expect(screen.getByLabelText('Container')).toHaveValue(''));
    expect(paths.some((p) => p.includes('/other/logs?container=&'))).toBe(true);
  });
  it('aborts an abandoned authorization review and resets namespace state', async () => {
    let signal: AbortSignal | undefined;
    vi.spyOn(globalThis, 'fetch').mockImplementation((_input, options) => {
      signal = options?.signal ?? undefined;
      return new Promise(() => {});
    });
    const view = show(<EffectiveAuthorization namespace="production" />);
    fireEvent.click(screen.getByRole('button', { name: 'Check effective access' }));
    const oldSignal = signal;
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <EffectiveAuthorization namespace="staging" />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(oldSignal?.aborted).toBe(true);
    expect(screen.getByLabelText('Authorization namespace')).toHaveValue('staging');
  });
  it('sends exact table filters and exposes sort direction to assistive technology', async () => {
    const paths: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const path = String(input);
      paths.push(path);
      return new Response(
        JSON.stringify(
          path.includes('/cluster')
            ? fixtures['/api/v1/cluster']
            : { items: [], total: 0, limit: 50, offset: 200, truncated: false },
        ),
      );
    });
    show(
      <Resources namespace="production" />,
      '/resources?kind=Pod&limit=50&offset=200&sort=name&desc=true',
    );
    const header = await screen.findByRole('columnheader', { name: 'Name' });
    expect(header).toHaveAttribute('aria-sort', 'descending');
    const url = new URL(
      paths.find((p) => p.startsWith('/api/v1/resources?'))!,
      'http://timone.test',
    );
    expect(Object.fromEntries(url.searchParams)).toMatchObject({
      kind: 'Pod',
      namespace: 'production',
      limit: '50',
      offset: '200',
      sort: 'name',
      desc: 'true',
    });
  });
});
