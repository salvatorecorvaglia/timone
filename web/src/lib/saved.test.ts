import { expect, it } from 'vitest';
import { parseSavedEntries, preferenceProfile, savedURL } from './saved';
it('keeps only supported local destinations and filter parameters', () => {
  expect(savedURL('/resources?kind=Pod&namespace=production&token=secret&gsearch=1')).toBe(
    '/resources?kind=Pod&namespace=production',
  );
  for (const href of [
    'https://evil.example',
    '//evil.example',
    '/\\evil.example',
    'javascript:alert(1)',
    '/api/v1/resources',
  ])
    expect(() => savedURL(href)).toThrow();
});
it('partitions preferences by opaque cluster identity and configured scope', () => {
  expect(preferenceProfile('cluster-a', 'production')).not.toBe(
    preferenceProfile('cluster-b', 'production'),
  );
  expect(preferenceProfile('cluster-a', 'production')).not.toBe(
    preferenceProfile('cluster-a', 'staging'),
  );
});
it('rejects invalid, oversized and duplicate preference entries', () => {
  const item = {
    id: 'entry',
    type: 'view',
    name: 'Production',
    href: '/resources?namespace=production',
    createdAt: new Date().toISOString(),
  };
  expect(parseSavedEntries(JSON.stringify({ schemaVersion: 1, items: [item] }))).toHaveLength(1);
  expect(() =>
    parseSavedEntries(JSON.stringify({ schemaVersion: 1, items: [item, item] })),
  ).toThrow();
  expect(() =>
    parseSavedEntries(
      JSON.stringify({ schemaVersion: 1, items: [{ ...item, href: '//evil.example' }] }),
    ),
  ).toThrow();
  expect(() => parseSavedEntries(' '.repeat(256 * 1024 + 1))).toThrow();
});
