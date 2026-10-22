import { expect, it } from 'vitest';
import { mockAPI } from './test-helpers';
import type { ResourcePage } from '../types';

it('resource fixtures enforce namespace, page, order and request parameter contracts', async () => {
  mockAPI();
  const load = async (query: string) =>
    (await fetch(`/api/v1/resources?kind=Pod&${query}`)).json() as Promise<ResourcePage>;
  const first = await load('namespace=production&limit=2&offset=0&sort=name');
  const next = await load('namespace=production&limit=2&offset=2&sort=name');
  expect(first.items).toHaveLength(2);
  expect(next.items).toHaveLength(2);
  expect(first.items.every((r) => r.namespace === 'production')).toBe(true);
  expect(next.items.some((r) => first.items.some((a) => a.id === r.id))).toBe(false);
  const reversed = await load('namespace=production&limit=200&desc=true');
  expect(reversed.items.at(-1)?.id).toBe(first.items[0].id);
  expect((await fetch('/api/v1/resources?kind=Pod&limti=2')).status).toBe(400);
  expect((await fetch('/api/v1/resources?kind=Pod&limit=201')).status).toBe(400);
});
