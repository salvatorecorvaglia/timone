import { screen, fireEvent } from '@testing-library/react';
import { vi, describe, it, expect } from 'vitest';
import type { Graph } from '../types';
import { buildGraph } from './Topology';
import { RoutingReferences, RoutingDetail } from './RoutingReferences';
import { show, fixtures } from './test-helpers';

describe('Gateway routing evidence', () => {
  it('offers keyboard-accessible inspection of each distinct declared reference', () => {
    const graph = fixtures['/api/v1/topology'] as Graph;
    const select = vi.fn();
    show(
      <RoutingReferences
        graph={graph}
        visible={new Set(graph.nodes.map((n) => n.id))}
        selected=""
        onSelect={select}
      />,
    );
    expect(screen.getByText(/Neither confirms observed traffic/)).toBeInTheDocument();
    const inspect = screen.getByRole('button', {
      name: 'Inspect preview-route to storefront spec.rules[0].backendRefs[1] Evidence',
    });
    inspect.focus();
    expect(inspect).toHaveFocus();
    fireEvent.click(inspect);
    const edge = graph.edges.find((e) => e.routing?.reference === 'spec.rules[0].backendRefs[1]')!;
    expect(select).toHaveBeenCalledWith(edge.id);
    expect(screen.getAllByText('Blocked configuration').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Checks passed').length).toBeGreaterThan(0);
  });
  it('shows controller scope, exact evidence and resource navigation', () => {
    const graph = fixtures['/api/v1/topology'] as Graph;
    const edge = graph.edges.find(
      (e) => e.relation === 'parent reference' && e.routing?.accepted === 'False',
    )!;
    const close = vi.fn();
    show(<RoutingDetail edge={edge} graph={graph} onClose={close} />);
    expect(screen.getByText('Accepted: False · ResolvedRefs: False')).toBeInTheDocument();
    expect(screen.getByText(/default\/Same policy excludes/)).toBeInTheDocument();
    expect(screen.getByText(/TLS, filters, listener readiness/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'preview-route' })).toHaveAttribute(
      'href',
      '/resources/HTTPRoute/staging/preview-route',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Close reference evidence' }));
    expect(close).toHaveBeenCalledOnce();
  });
  it('labels incomplete checks and never attributes route-wide conditions to a backend', () => {
    const graph = structuredClone(fixtures['/api/v1/topology'] as Graph);
    const edge = graph.edges.find((e) => e.relation === 'backend reference')!;
    edge.routing!.state = 'unknown';
    show(
      <RoutingReferences
        graph={graph}
        visible={new Set([edge.source, edge.target])}
        selected={edge.id}
        onSelect={vi.fn()}
      />,
    );
    expect(screen.getByText('Incomplete checks')).toBeInTheDocument();
    expect(screen.getByText('Not assessed per backend')).toBeInTheDocument();
    expect(screen.queryByText('Controller report')).not.toBeInTheDocument();
  });
  it('colors blocked routing separately and preserves configuration confidence', () => {
    const graph = fixtures['/api/v1/topology'] as Graph;
    const result = buildGraph(graph, {
      search: '',
      group: 'networking',
      limit: 200,
      showNamespaces: false,
      showPods: false,
      selected: '',
      labels: true,
    });
    const edge = graph.edges.find((e) => e.routing?.accepted === 'False')!;
    expect(result.edges.find((e) => e.id === edge.id)?.style?.stroke).toBe('var(--critical)');
    expect(edge.confidence).toBe('declared');
    const invisible = vi.fn();
    show(<RoutingReferences graph={graph} visible={new Set()} selected="" onSelect={invisible} />);
    expect(screen.queryByRole('button', { name: /Inspect/ })).not.toBeInTheDocument();
  });
});
