import { render } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import type { ReactNode } from 'react';
import { vi } from 'vitest';
import fixtureData from '../test-fixtures.json';
import type { ResourcePage } from '../types';
export const fixtures: Record<string, unknown> = fixtureData;
export function mockAPI() {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const u = new URL(String(input), 'http://timone.test');
    let key = u.pathname;
    if (key === '/api/v1/namespaces')
      return new Response(
        JSON.stringify({
          total: 4,
          offset: 0,
          limit: 200,
          truncated: false,
          items: [
            { name: 'production' },
            { name: 'platform' },
            { name: 'staging' },
            { name: 'kube-system' },
          ],
        }),
      );
    if (key.endsWith('/grants'))
      key += `?limit=${u.searchParams.get('limit')}&offset=${u.searchParams.get('offset')}`;
    if (key === '/api/v1/search' && u.searchParams.get('search')) {
      u.searchParams.sort();
      key += `?${u.searchParams}`;
    }
    if (key === '/api/v1/authorization')
      key += `?kind=${u.searchParams.get('kind')}&namespace=${u.searchParams.get('namespace')}&verb=${u.searchParams.get('verb')}`;
    if (key === '/api/v1/resources') {
      const allowed = new Set([
        'kind',
        'namespace',
        'search',
        'status',
        'phase',
        'sort',
        'desc',
        'limit',
        'offset',
      ]);
      const limit = Number(u.searchParams.get('limit') ?? 50);
      const offset = Number(u.searchParams.get('offset') ?? 0);
      const sort = u.searchParams.get('sort') || 'name';
      if (
        [...u.searchParams].some(
          ([k]) => !allowed.has(k) || u.searchParams.getAll(k).length !== 1,
        ) ||
        !Number.isInteger(limit) ||
        limit < 1 ||
        limit > 200 ||
        !Number.isInteger(offset) ||
        offset < 0 ||
        offset > 5000 ||
        !['name', 'namespace', 'status', 'restarts', 'createdAt'].includes(sort)
      )
        return new Response(JSON.stringify({ error: { message: 'Invalid resource request' } }), {
          status: 400,
        });
      const kind = u.searchParams.get('kind') || 'Pod';
      const page = fixtures[`${key}?kind=${kind}&limit=25`] as ResourcePage | undefined;
      if (!page)
        return new Response(
          JSON.stringify({ error: { message: `Missing resource fixture: ${kind}` } }),
          { status: 404 },
        );
      const ns = u.searchParams.get('namespace');
      const search = (u.searchParams.get('search') || '').toLowerCase();
      const status = u.searchParams.get('status');
      const phase = u.searchParams.get('phase');
      const items = page.items.filter(
        (r) =>
          (!ns || !r.namespace || r.namespace === ns) &&
          (!status || r.health === status) &&
          (!phase || r.status === phase) &&
          `${r.name} ${r.namespace} ${r.kind}`.toLowerCase().includes(search),
      );
      const field =
        sort === 'status' ? 'health' : (sort as 'name' | 'namespace' | 'restarts' | 'createdAt');
      items.sort((a, b) => {
        const av = a[field],
          bv = b[field];
        const comparison =
          typeof av === 'number' && typeof bv === 'number'
            ? av - bv
            : String(av).localeCompare(String(bv));
        return (
          (comparison || a.id.localeCompare(b.id)) *
          (u.searchParams.get('desc') === 'true' ? -1 : 1)
        );
      });
      return new Response(
        JSON.stringify({
          ...page,
          items: items.slice(offset, offset + limit),
          total: items.length,
          limit,
          offset,
        }),
        { headers: { 'Content-Type': 'application/json' } },
      );
    }
    if (key === '/api/v1/events' && u.searchParams.get('kind')) {
      key += '?kind=' + u.searchParams.get('kind');
      if (u.searchParams.get('resource')) key += '&resource=' + u.searchParams.get('resource');
    }
    if (key === '/api/v1/metrics/history') {
      const q = new URLSearchParams({
        kind: u.searchParams.get('kind') ?? '',
        ...(u.searchParams.get('namespace') ? { namespace: u.searchParams.get('namespace')! } : {}),
        name: u.searchParams.get('name') ?? '',
        ...(u.searchParams.get('container') ? { container: u.searchParams.get('container')! } : {}),
        range: u.searchParams.get('range') ?? '1h',
      });
      key += `?${q}`;
    }
    if (key.endsWith('/compare'))
      key += `?from=${u.searchParams.get('from')}&to=${u.searchParams.get('to')}`;
    if (key === '/api/v1/custom-resources/databases.platform.timone.io') {
      if (u.searchParams.get('search')) key += '?search=' + u.searchParams.get('search');
      else if (u.searchParams.get('status')) key += '?status=' + u.searchParams.get('status');
      if (u.searchParams.get('version') === 'v1beta1')
        key += (key.includes('?') ? '&' : '?') + 'version=v1beta1';
    }
    return new Response(
      JSON.stringify(fixtures[key] ?? { error: { message: `Missing fixture: ${key}` } }),
      { status: fixtures[key] ? 200 : 404, headers: { 'Content-Type': 'application/json' } },
    );
  });
}
function Location() {
  const l = useLocation();
  return (
    <output data-testid="location">
      {l.pathname}
      {l.search}
    </output>
  );
}
export function show(ui: ReactNode, path = '/') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        {ui}
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
