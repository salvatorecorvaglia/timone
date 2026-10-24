import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import { useAPI } from './api';

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

it('shares completed shell reads with delayed feature mounts and still refreshes invalidated data', async () => {
  vi.useFakeTimers();
  const fetch = vi
    .spyOn(globalThis, 'fetch')
    .mockImplementation(async () => new Response('{"items":[]}'));
  const client = new QueryClient();
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const path = '/api/v1/overview?namespace=production';
  const shell = renderHook(() => useAPI(path), { wrapper });
  await act(() => vi.advanceTimersByTimeAsync(100));
  expect(shell.result.current.isSuccess).toBe(true);
  const page = renderHook(() => useAPI(path), { wrapper });
  await act(() => vi.advanceTimersByTimeAsync(100));
  expect(page.result.current.isSuccess).toBe(true);
  expect(fetch).toHaveBeenCalledTimes(1);
  await act(() => client.invalidateQueries({ queryKey: [path] }));
  expect(fetch).toHaveBeenCalledTimes(2);
  // The periodic fallback remains active when no watch events arrive.
  await act(() => vi.advanceTimersByTimeAsync(30000));
  expect(fetch).toHaveBeenCalledTimes(3);
  shell.unmount();
  page.unmount();
  client.clear();
});

it('keeps namespace and investigation queries independent', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response('{}'));
  const client = new QueryClient();
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const hook = renderHook(
    () => [
      useAPI('/api/v1/overview?namespace=production'),
      useAPI('/api/v1/overview?namespace=staging'),
      useAPI('/api/v1/applications?namespace=production'),
    ],
    { wrapper },
  );
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  expect(fetch).toHaveBeenCalledTimes(3);
  hook.unmount();
  client.clear();
});
