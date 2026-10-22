import { screen, fireEvent, waitFor } from '@testing-library/react';
import { vi, describe, it, expect } from 'vitest';
import { App } from '../app/App';
import { GlobalResults } from './GlobalSearch';
import { show, mockAPI } from './test-helpers';

describe('Global resource search', () => {
  const kinds = [{ name: 'Pod' }, { name: 'Gateway' }, { name: 'ReferenceGrant' }];
  it('does not request resource searches until two characters are entered', async () => {
    const api = mockAPI();
    show(<GlobalResults search="x" namespace="" kinds={kinds} onSelect={vi.fn()} />);
    expect(screen.getByText('Find a resource')).toBeInTheDocument();
    await screen.findByRole('option', { name: 'Database · platform.timone.io' });
    expect(api.mock.calls.some(([url]) => String(url).startsWith('/api/v1/search'))).toBe(false);
  });
  it('navigates from the shell directly to the selected custom resource and version', async () => {
    mockAPI();
    show(<App />);
    await screen.findByText('Demo environment');
    fireEvent.keyDown(window, { key: 'k', metaKey: true });
    fireEvent.change(await screen.findByLabelText('Search all resources'), {
      target: { value: 'orders-db' },
    });
    const results = await screen.findAllByRole('button', { name: /Inspect Database.*orders-db/ });
    fireEvent.click(results.find((b) => b.textContent?.includes('production'))!);
    expect(await screen.findByRole('heading', { name: 'orders-db' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent(
      '/custom-resources/databases.platform.timone.io/production/orders-db?version=v1',
    );
  });
  it('combines type and health filters and resets pagination', async () => {
    const api = mockAPI();
    show(<GlobalResults search="orders-db" namespace="" kinds={kinds} onSelect={vi.fn()} />);
    await screen.findAllByRole('button', { name: /Inspect Database.*orders-db/ });
    await screen.findByRole('option', { name: 'Database · platform.timone.io' });
    fireEvent.change(screen.getByLabelText('Global search resource type'), {
      target: { value: 'custom:databases.platform.timone.io' },
    });
    await waitFor(() =>
      expect(
        api.mock.calls.some(([url]) =>
          String(url).includes('definition=databases.platform.timone.io'),
        ),
      ).toBe(true),
    );
    fireEvent.change(screen.getByLabelText('Global search health'), {
      target: { value: 'warning' },
    });
    expect(await screen.findByText('1 matches in observed collections')).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /Inspect Database.*orders-db/ })).toHaveLength(1);
  });
  it('loads the next stable page and exposes per-collection coverage', async () => {
    const select = vi.fn();
    mockAPI();
    show(<GlobalResults search="production" namespace="" kinds={kinds} onSelect={select} />);
    await screen.findByText('1–20');
    const first = screen
      .getAllByRole('button')
      .find((b) => b.textContent?.includes('Cluster scoped'))?.textContent;
    fireEvent.click(screen.getByRole('button', { name: 'Next search results' }));
    expect(await screen.findByText('21–40')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Previous search results' })).toBeEnabled();
    if (first) expect(screen.queryByText(first)).not.toBeInTheDocument();
    fireEvent.click(screen.getByText('Collection coverage (31)'));
    expect(screen.getByText('ReferenceGrant', { selector: 'span' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Previous search results' }));
    expect(await screen.findByText('1–20')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Next search results' }));
    await screen.findByText('21–40');
    fireEvent.change(screen.getByLabelText('Global search resource type'), {
      target: { value: 'Pod' },
    });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Previous search results' })).toBeDisabled(),
    );
    expect(await screen.findByText(/matches in observed collections/)).toBeInTheDocument();
  });
  it('distinguishes no matches from unreadable collections', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    api.mockImplementation((input, options) =>
      String(input).startsWith('/api/v1/search')
        ? Promise.resolve(
            new Response(
              JSON.stringify({
                items: [],
                total: 0,
                limit: 20,
                offset: 0,
                complete: false,
                state: 'partial',
                coverage: [{ kind: 'Secret', state: 'unavailable' }],
                warnings: [{ kind: 'Secret', message: 'Collection read denied' }],
              }),
            ),
          )
        : original(input, options),
    );
    show(<GlobalResults search="orders" namespace="production" kinds={kinds} onSelect={vi.fn()} />);
    expect(await screen.findByText('No matches in readable collections')).toBeInTheDocument();
    expect(screen.getByText('At least 0 matches · incomplete coverage')).toBeInTheDocument();
    expect(screen.getByText(/Collection read denied/)).toBeInTheDocument();
    expect(screen.queryByText('No matching resources')).not.toBeInTheDocument();
  });
  it('hides previous results while a new term is being debounced', async () => {
    mockAPI();
    show(<App />);
    await screen.findByText('Demo environment');
    fireEvent.keyDown(window, { key: 'k', metaKey: true });
    const input = await screen.findByLabelText('Search all resources');
    fireEvent.change(input, { target: { value: 'orders-db' } });
    await screen.findAllByRole('button', { name: /Inspect Database.*orders-db/ });
    fireEvent.change(input, { target: { value: 'missing-resource' } });
    expect(
      screen.queryByRole('button', { name: /Inspect Database.*orders-db/ }),
    ).not.toBeInTheDocument();
    expect(await screen.findByText('No matching resources')).toBeInTheDocument();
  });
  it('shows loading and request failure without treating it as no results', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    let release: ((value: Response) => void) | undefined;
    api.mockImplementation((input, options) =>
      String(input).startsWith('/api/v1/search')
        ? release
          ? Promise.resolve(
              new Response(
                JSON.stringify({ error: { message: 'Search is temporarily unavailable' } }),
                { status: 503 },
              ),
            )
          : new Promise<Response>((resolve) => {
              release = resolve;
            })
        : original(input, options),
    );
    show(<GlobalResults search="orders-db" namespace="" kinds={kinds} onSelect={vi.fn()} />);
    expect(screen.getByText(/Loading cluster data/)).toBeInTheDocument();
    await waitFor(() => expect(release).toBeDefined());
    release?.(
      new Response(JSON.stringify({ error: { message: 'Search is temporarily unavailable' } }), {
        status: 503,
      }),
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('Search is temporarily unavailable');
    expect(screen.queryByText('No matching resources')).not.toBeInTheDocument();
    api.mockImplementation(original);
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(
      await screen.findByRole('button', { name: /Inspect Database production\/orders-db/ }),
    ).toBeInTheDocument();
  });
});
