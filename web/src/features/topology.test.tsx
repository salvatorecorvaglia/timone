import { describe, expect, it } from 'vitest';
import { buildGraph, topologyHealthColor } from './Topology';
import { fixtures } from './test-helpers';
import type { Graph } from '../types';
const graph = fixtures['/api/v1/topology'] as Graph;
const options = {
  search: '',
  group: '',
  limit: 200,
  showNamespaces: true,
  showPods: true,
  selected: '',
  labels: false,
};
describe('topology visibility and layout', () => {
  it('preserves warning and unknown health in the minimap', () => {
    expect(['healthy', 'warning', 'critical', 'unknown'].map(topologyHealthColor)).toEqual([
      'var(--healthy)',
      'var(--warning)',
      'var(--critical)',
      'var(--graph-unknown)',
    ]);
  });
  it.each(['accepted', 'resolvedRefs'] as const)(
    'marks a false controller %s condition as critical',
    (condition) => {
      const routing = graph.edges.find((edge) => edge.routing)!;
      const result = buildGraph(
        {
          ...graph,
          edges: [
            {
              ...routing,
              routing: {
                ...routing.routing!,
                state: 'allowed',
                accepted: 'True',
                resolvedRefs: 'True',
                [condition]: 'False',
              },
            },
          ],
        },
        options,
      );
      expect(result.edges[0].style?.stroke).toBe('var(--critical)');
    },
  );
  it('never retains edges whose endpoints are hidden by caps or filters', () => {
    for (const group of ['', 'workloads', 'configuration', 'networking']) {
      const result = buildGraph(graph, { ...options, group, limit: 3, showPods: false });
      const ids = new Set(result.nodes.map((n) => n.id));
      expect(result.nodes.length).toBeLessThanOrEqual(3);
      for (const edge of result.edges) {
        expect(ids.has(edge.source) && ids.has(edge.target)).toBe(true);
      }
      expect(result.nodes.some((n) => n.data.resource.kind === 'Pod')).toBe(false);
    }
  });
  it('keeps layout stable when highlighting search matches', () => {
    const initial = buildGraph(graph, options);
    const highlighted = buildGraph(graph, { ...options, search: 'no-such-resource' });
    expect(highlighted.nodes.map((n) => n.position)).toEqual(initial.nodes.map((n) => n.position));
    expect(highlighted.nodes.every((n) => n.style?.opacity === 0.3)).toBe(true);
    expect(highlighted.total).toBe(initial.total);
  });
  it('highlights direct neighbors and labels selected relationships', () => {
    const edge = graph.edges[0];
    expect(edge).toBeDefined();
    const result = buildGraph(graph, { ...options, selected: edge.source });
    expect(result.nodes.find((n) => n.id === edge.source)?.selected).toBe(true);
    expect(result.nodes.find((n) => n.id === edge.target)?.style?.opacity).toBe(1);
    expect(result.edges.find((e) => e.id === edge.id)?.label).toBe(edge.relation);
  });
});

import { fireEvent, screen, waitFor } from '@testing-library/react';
import { vi } from 'vitest';
import { Topology } from './Topology';
import { mockAPI, show } from './test-helpers';

it('recovers from empty filters and normalizes an invalid node limit', async () => {
  mockAPI();
  show(<Topology namespace="" />, '/topology?nodes=-10');
  await screen.findByRole('region', { name: 'Interactive Kubernetes topology' });
  expect(screen.getByLabelText('Maximum graph nodes')).toHaveValue('60');
  fireEvent.change(screen.getByLabelText('Topology resource types'), {
    target: { value: 'configuration' },
  });
  await waitFor(() =>
    expect(screen.getByTestId('location').textContent).toContain('group=configuration'),
  );
  fireEvent.click(screen.getByRole('checkbox', { name: 'Pods' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Namespace containers' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Relationship labels' }));
  expect(screen.getByRole('checkbox', { name: 'Relationship labels' })).toBeChecked();
  vi.restoreAllMocks();
});
