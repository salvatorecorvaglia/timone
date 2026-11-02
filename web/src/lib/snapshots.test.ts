import { expect, it } from 'vitest';
import { compareSnapshots, parseSnapshot } from './snapshots';
const snapshot = {
  schemaVersion: 1,
  cluster: 'demo',
  clusterKey: 'cluster-a',
  mode: 'demo',
  capturedAt: '2026-01-01T00:00:00Z',
  root: 'Deployment:production:web',
  resources: [
    {
      resource: {
        id: 'Deployment:production:web',
        kind: 'Deployment',
        name: 'web',
        namespace: 'production',
        status: 'Ready',
        health: 'healthy',
      },
      uid: 'uid',
      configuration: {
        replicas: 2,
        pod: {
          containers: [
            { name: 'web', image: 'web:v1' },
            { name: 'sidecar', image: 'proxy:v1' },
          ],
        },
      },
      conditions: [],
    },
  ],
  warnings: [],
  limitations: [],
};
it('compares replicas and images without reporting container reorder as a change', () => {
  const a = parseSnapshot(JSON.stringify(snapshot));
  const b = parseSnapshot(JSON.stringify(snapshot));
  b.resources[0].configuration.replicas = 3;
  const pod = b.resources[0].configuration.pod as { containers: { name: string; image: string }[] };
  pod.containers.reverse();
  pod.containers.find((c) => c.name === 'web')!.image = 'web:v2';
  const result = compareSnapshots(a, b);
  expect(result.rows).toHaveLength(2);
  expect(result.rows.map((r) => r.path)).toContain('/configuration/replicas');
  expect(result.rows.find((r) => r.path.endsWith('/web/image'))?.after).toBe('"web:v2"');
});
it('rejects wrong schemas, duplicate resources and oversized input', () => {
  expect(() => parseSnapshot('{"schemaVersion":99}')).toThrow();
  expect(() =>
    parseSnapshot(
      JSON.stringify({ ...snapshot, resources: [...snapshot.resources, ...snapshot.resources] }),
    ),
  ).toThrow(/duplicate/);
  expect(() => parseSnapshot(' '.repeat(2 * 1024 * 1024 + 1))).toThrow(/2 MiB/);
});
it('reports missing identities as not observed and notices UID replacement', () => {
  const a = parseSnapshot(JSON.stringify(snapshot));
  const b = parseSnapshot(JSON.stringify(snapshot));
  b.resources[0].uid = 'new-uid';
  expect(compareSnapshots(a, b).rows[0].path).toBe('/uid');
  b.resources = [];
  const result = compareSnapshots(a, b);
  expect(result.rows[0].after).toBe('Not observed');
});
