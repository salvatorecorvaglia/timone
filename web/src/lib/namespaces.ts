import { useQuery } from '@tanstack/react-query';
import { APIError, request } from './api';
import type { ResourcePage } from '../types';

export async function loadNamespaces(signal?: AbortSignal): Promise<ResourcePage> {
  const result: ResourcePage = { items: [], total: 0, offset: 0, limit: 5000, truncated: false };
  const seen = new Set<string>();
  for (let offset = 0; offset < 5000; offset += 200) {
    const page = await request<ResourcePage>(`/api/v1/namespaces?limit=200&offset=${offset}`, {
      signal,
    });
    if (
      !Array.isArray(page.items) ||
      !Number.isInteger(page.total) ||
      page.total < 0 ||
      page.offset !== offset ||
      page.items.length > 200
    )
      throw new APIError('Timone returned an invalid namespace page', 502);
    result.total = page.total;
    result.truncated ||= page.truncated;
    for (const item of page.items)
      if (!seen.has(item.name)) {
        result.items.push(item);
        seen.add(item.name);
      }
    if (offset + page.items.length >= page.total) return result;
    if (page.items.length === 0)
      throw new APIError('Namespace pagination stopped before completion', 502);
  }
  result.truncated = true;
  return result;
}
export function useNamespaces() {
  return useQuery({
    queryKey: ['/api/v1/namespaces'],
    queryFn: ({ signal }) => loadNamespaces(signal),
    staleTime: 30000,
    refetchInterval: 60000,
    retry: 1,
  });
}
