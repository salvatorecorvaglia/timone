import { expect, it } from 'vitest';
import { pageRange, pagination, queryInteger } from './pagination';
import { shouldInvalidate } from './updates';

it('normalizes invalid page bounds and describes only returned rows', () => {
  for (const query of [
    'offset=-1&limit=0',
    'offset=NaN&limit=Infinity',
    'offset=5001&limit=201',
    'offset=2.5&limit=',
  ]) {
    expect(pagination(new URLSearchParams(query))).toEqual({ offset: 0, limit: 25 });
  }
  expect(pagination(new URLSearchParams('offset=200&limit=200'))).toEqual({
    offset: 200,
    limit: 200,
  });
  expect(pageRange(5000, 0, 16)).toBe('0');
  expect(pageRange(25, 3, 28)).toBe('26–28');
  for (const value of ['-1', '1.5', 'Infinity', '0x10', '1e2', '5001', '']) {
    expect(queryInteger(new URLSearchParams({ goffset: value }), 'goffset', 0, 0, 5000)).toBe(0);
  }
  expect(queryInteger(new URLSearchParams('goffset=5000'), 'goffset', 0, 0, 5000)).toBe(5000);
});
it('refreshes cross-namespace delivery associations without widening workload invalidation', () => {
  for (const kind of ['Application', 'Kustomization', 'HelmRelease']) {
    expect(
      shouldInvalidate('/api/v1/applications?namespace=production', {
        scopes: [{ kind, namespace: 'platform' }],
      }),
    ).toBe(true);
  }
  expect(
    shouldInvalidate('/api/v1/applications?namespace=production', {
      scopes: [{ kind: 'Pod', namespace: 'platform' }],
    }),
  ).toBe(false);
});

import { collectionHealth } from './coverage';
it('distinguishes an empty readable collection from absent or partial coverage', () => {
  expect(collectionHealth('Pod', { counts: { Pod: 0 }, warnings: [] }, 'healthy')).toBe('healthy');
  expect(collectionHealth('Pod', { counts: {}, warnings: [] }, 'healthy')).toBe('unknown');
  expect(
    collectionHealth(
      'Pod',
      { counts: { Pod: 4 }, warnings: [{ kind: 'Pod', message: 'Truncated' }] },
      'healthy',
    ),
  ).toBe('unknown');
});
