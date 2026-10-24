import type { Edge, Node } from '@xyflow/react';
import type { Graph, Resource } from '../types';

type ResourceNode = Node<{ resource: Resource }, 'resource'>;

const columns: Record<string, number> = {
  Node: 0,
  Namespace: 0,
  Gateway: 0,
  GatewayClass: 0,
  ReferenceGrant: 2,
  Ingress: 1,
  HTTPRoute: 1,
  Service: 2,
  Deployment: 3,
  StatefulSet: 3,
  DaemonSet: 3,
  Job: 3,
  CronJob: 2,
  ReplicaSet: 4,
  Pod: 5,
  Secret: 6,
  ConfigMap: 6,
  PersistentVolumeClaim: 6,
};
export function buildGraph(
  graph: Graph,
  options: {
    search: string;
    group: string;
    limit: number;
    showNamespaces: boolean;
    showPods: boolean;
    selected: string;
    labels: boolean;
  },
) {
  const filtered = graph.nodes.filter(
    (r) =>
      (options.showNamespaces || r.kind !== 'Namespace') &&
      (options.showPods || r.kind !== 'Pod') &&
      (!options.group ||
        (options.group === 'workloads'
          ? ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Pod', 'Job', 'CronJob']
          : options.group === 'networking'
            ? [
                'Service',
                'Ingress',
                'Gateway',
                'GatewayClass',
                'ReferenceGrant',
                'HTTPRoute',
                'Pod',
              ]
            : ['Secret', 'ConfigMap', 'PersistentVolumeClaim', 'Pod']
        ).includes(r.kind)),
  );
  const sliced = filtered.slice(0, options.limit);
  const visible = new Set(sliced.map((r) => r.id));
  const related = new Set<string>([options.selected]);
  for (const e of graph.edges) {
    if (e.source === options.selected) related.add(e.target);
    if (e.target === options.selected) related.add(e.source);
  }
  const counts = new Map<string, number>();
  const namespaces = [...new Set(sliced.map((r) => r.namespace))].sort();
  let yOffset = 0;
  const offsets = new Map<string, number>();
  for (const ns of namespaces) {
    const counts = new Map<number, number>();
    for (const r of sliced.filter((r) => r.namespace === ns)) {
      const c = columns[r.kind] ?? 2;
      counts.set(c, (counts.get(c) ?? 0) + 1);
    }
    offsets.set(ns, yOffset);
    yOffset += Math.max(1, ...counts.values()) * 98 + 80;
  }
  const nodes: ResourceNode[] = sliced.map((r) => {
    const col = columns[r.kind] ?? 2;
    const key = r.namespace + col;
    const row = counts.get(key) ?? 0;
    counts.set(key, row + 1);
    const match =
      !options.search || `${r.name} ${r.kind}`.toLowerCase().includes(options.search.toLowerCase());
    return {
      id: r.id,
      type: 'resource',
      data: { resource: r },
      position: { x: col * 270, y: (offsets.get(r.namespace) ?? 0) + row * 98 },
      selected: r.id === options.selected,
      style: { opacity: !match || (options.selected && !related.has(r.id)) ? 0.3 : 1 },
    };
  });
  const edges: Edge[] = graph.edges
    .filter((e) => visible.has(e.source) && visible.has(e.target))
    .map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      label:
        options.labels || e.source === options.selected || e.target === options.selected
          ? e.relation
          : undefined,
      type: 'smoothstep',
      style: {
        stroke: e.routing
          ? e.routing.state === 'denied' ||
            e.routing.accepted === 'False' ||
            e.routing.resolvedRefs === 'False'
            ? 'var(--critical)'
            : e.routing.state === 'unknown'
              ? 'var(--graph-unknown)'
              : 'var(--graph-allowed)'
          : e.source === options.selected || e.target === options.selected
            ? 'var(--accent)'
            : 'var(--graph-edge)',
        strokeWidth: 1.3,
        strokeDasharray:
          e.relation === 'selects'
            ? '4 4'
            : e.relation.includes('reference') || e.relation === 'mounts'
              ? '8 4'
              : undefined,
        opacity:
          options.selected && e.source !== options.selected && e.target !== options.selected
            ? 0.18
            : 1,
      },
      labelStyle: { fill: 'var(--graph-label)', fontSize: 10 },
      labelBgStyle: { fill: 'var(--graph-label-background)' },
    }));
  return { nodes, edges, total: filtered.length };
}
