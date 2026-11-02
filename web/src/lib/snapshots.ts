import type { Resource, Warning } from '../types';
export interface ResourceSnapshot {
  schemaVersion: 1;
  cluster: string;
  clusterKey: string;
  mode: string;
  capturedAt: string;
  root: string;
  resources: {
    resource: Resource;
    uid: string;
    configuration: Record<string, unknown>;
    conditions: Record<string, unknown>[];
  }[];
  warnings: Warning[];
  limitations: string[];
}
export function parseSnapshot(text: string): ResourceSnapshot {
  if (new TextEncoder().encode(text).length > 2 * 1024 * 1024)
    throw new Error('Snapshot exceeds 2 MiB.');
  const value = JSON.parse(text);
  if (
    value?.schemaVersion !== 1 ||
    typeof value.cluster !== 'string' ||
    typeof value.clusterKey !== 'string' ||
    typeof value.root !== 'string' ||
    !Number.isFinite(Date.parse(value.capturedAt)) ||
    !Array.isArray(value.resources) ||
    value.resources.length > 200 ||
    !Array.isArray(value.warnings) ||
    !Array.isArray(value.limitations) ||
    value.warnings.length > 1000 ||
    value.limitations.length > 100 ||
    !value.limitations.every((v: unknown) => typeof v === 'string') ||
    !value.warnings.every(
      (v: { kind?: unknown; message?: unknown }) =>
        v && typeof v.kind === 'string' && typeof v.message === 'string',
    )
  ) {
    throw new Error('Unsupported or invalid snapshot schema.');
  }
  const ids = new Set<string>();
  for (const item of value.resources) {
    if (
      !item?.resource ||
      typeof item.resource.id !== 'string' ||
      typeof item.resource.kind !== 'string' ||
      typeof item.resource.name !== 'string' ||
      typeof item.resource.namespace !== 'string' ||
      typeof item.uid !== 'string' ||
      !item.configuration ||
      typeof item.configuration !== 'object' ||
      Array.isArray(item.configuration) ||
      !Array.isArray(item.conditions) ||
      ids.has(item.resource.id)
    ) {
      throw new Error('Invalid or duplicate resource identity in snapshot.');
    }
    ids.add(item.resource.id);
  }
  if (!ids.has(value.root)) throw new Error('Snapshot root is missing.');
  return value as ResourceSnapshot;
}
export interface SnapshotDifference {
  resource: string;
  path: string;
  before: string;
  after: string;
}
function flatten(value: unknown, path: string, out: Map<string, string>, depth = 0): void {
  if (depth > 32 || out.size > 50000)
    throw new Error('Snapshot structure exceeds comparison limits.');
  if (value === null || typeof value !== 'object') {
    out.set(path, JSON.stringify(value));
    return;
  }
  if (Array.isArray(value)) {
    if (!value.length) {
      out.set(path, '[]');
      return;
    }
    const names = value.map((v) => (v && typeof v === 'object' ? (v.name ?? v.type) : undefined));
    const named = names.every((n) => typeof n === 'string') && new Set(names).size === names.length;
    value.forEach((v, i) => {
      flatten(
        v,
        path +
          '/' +
          String(named ? names[i] : i)
            .replaceAll('~', '~0')
            .replaceAll('/', '~1'),
        out,
        depth + 1,
      );
    });
    return;
  }
  const entries = Object.entries(value).sort(([a], [b]) => a.localeCompare(b));
  if (!entries.length) out.set(path, '{}');
  for (const [key, entry] of entries)
    flatten(entry, path + '/' + key.replaceAll('~', '~0').replaceAll('/', '~1'), out, depth + 1);
}
export function compareSnapshots(before: ResourceSnapshot, after: ResourceSnapshot) {
  const rows: SnapshotDifference[] = [];
  let omitted = 0;
  const left = new Map(before.resources.map((r) => [r.resource.id, r]));
  const right = new Map(after.resources.map((r) => [r.resource.id, r]));
  const add = (row: SnapshotDifference) => {
    if (rows.length < 1000) rows.push(row);
    else omitted++;
  };
  for (const id of [...new Set([...left.keys(), ...right.keys()])].sort()) {
    const a = left.get(id),
      b = right.get(id);
    if (!a || !b) {
      add({
        resource: id,
        path: '/presence',
        before: a ? 'Observed' : 'Not observed',
        after: b ? 'Observed' : 'Not observed',
      });
      continue;
    }
    const av = new Map<string, string>(),
      bv = new Map<string, string>();
    flatten(
      {
        uid: a.uid,
        configuration: a.configuration,
        conditions: a.conditions,
        status: a.resource.status,
        health: a.resource.health,
      },
      '',
      av,
    );
    flatten(
      {
        uid: b.uid,
        configuration: b.configuration,
        conditions: b.conditions,
        status: b.resource.status,
        health: b.resource.health,
      },
      '',
      bv,
    );
    for (const key of [...new Set([...av.keys(), ...bv.keys()])].sort()) {
      if (av.get(key) !== bv.get(key))
        add({
          resource: id,
          path: key,
          before: av.get(key) ?? 'Not observed',
          after: bv.get(key) ?? 'Not observed',
        });
    }
  }
  return { rows, omitted };
}
