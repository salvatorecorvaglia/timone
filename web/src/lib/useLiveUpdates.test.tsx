import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { useLiveUpdates } from './useLiveUpdates';

class Stream extends EventTarget {
  static current: Stream;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor() {
    super();
    Stream.current = this;
  }
  close() {
    this.closed = true;
  }
  invalidate(data = '{"scopes":[{"kind":"Pod","namespace":"production"}]}') {
    this.dispatchEvent(new MessageEvent('invalidate', { data }));
  }
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it('finishes slow refreshes and coalesces changes into one trailing refresh', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', Stream);
  const client = new QueryClient();
  const key = ['/api/v1/overview?namespace=production'];
  client.setQueryData(key, 0);
  let completed = 0;
  let aborted = 0;
  const fetch = vi.fn(
    ({ signal }: { signal: AbortSignal }) =>
      new Promise<number>((resolve, reject) => {
        const timer = setTimeout(() => resolve(++completed), 2000);
        signal.addEventListener('abort', () => {
          aborted++;
          clearTimeout(timer);
          reject(new Error('aborted'));
        });
      }),
  );
  const observer = new QueryObserver(client, {
    queryKey: key,
    queryFn: fetch,
    staleTime: Infinity,
  });
  const unsubscribe = observer.subscribe(() => {});
  const hook = renderHook(() => useLiveUpdates(), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    ),
  });
  try {
    act(() => Stream.current.invalidate());
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(fetch).toHaveBeenCalledTimes(1);
    for (let i = 0; i < 10; i++) act(() => Stream.current.invalidate());
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(client.getQueryData(key)).toBe(1);
    expect(fetch).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(3000));
    expect(client.getQueryData(key)).toBe(2);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(aborted).toBe(0);
    expect(hook.result.current).toBe('Updates live');
    act(() => Stream.current.invalidate('invalid JSON'));
    hook.unmount();
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(Stream.current.closed).toBe(true);
  } finally {
    hook.unmount();
    unsubscribe();
    client.clear();
  }
});

it('closes the old stream and cancels queries immediately on workspace replacement', () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', Stream);
  const client = new QueryClient();
  const cancel = vi.spyOn(client, 'cancelQueries').mockImplementation(() => new Promise(() => {}));
  const hook = renderHook(() => useLiveUpdates(), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    ),
  });
  act(() => window.dispatchEvent(new Event('timone:workspace-changed')));
  act(() => window.dispatchEvent(new Event('timone:workspace-changed')));
  expect(cancel).toHaveBeenCalledTimes(1);
  expect(Stream.current.closed).toBe(true);
  hook.unmount();
  client.clear();
});
