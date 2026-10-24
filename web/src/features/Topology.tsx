import { buildGraph } from './topologyGraph';
export { buildGraph } from './topologyGraph';
import { ScopedLink as Link } from '../components/navigation';
import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Background,
  BackgroundVariant,
  ControlButton,
  Panel as FlowPanel,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  useReactFlow,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {
  ArrowDownLeft,
  ArrowRight,
  ArrowUpRight,
  Box,
  Database,
  GitBranch,
  Layers3,
  Network,
  Search,
  Plus,
  Minus,
  Maximize,
  X,
} from 'lucide-react';
import { RoutingReferences, RoutingDetail } from './RoutingReferences';
import { resourceURL, useAPI } from '../lib/api';
import type { Graph, Resource } from '../types';
import { Empty, ErrorState, Loading, PageTitle, Panel, Status, Warnings } from '../components/ui';
type ResourceNode = Node<{ resource: Resource }, 'resource'>;
export function topologyHealthColor(health: string) {
  return (
    { healthy: 'var(--healthy)', warning: 'var(--warning)', critical: 'var(--critical)' }[health] ??
    'var(--graph-unknown)'
  );
}
function GraphControls() {
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  return (
    <FlowPanel
      position="bottom-left"
      className="react-flow__controls"
      role="group"
      aria-label="Topology controls"
    >
      <ControlButton aria-label="Zoom in" onClick={() => void zoomIn()}>
        <Plus size={16} />
      </ControlButton>
      <ControlButton aria-label="Zoom out" onClick={() => void zoomOut()}>
        <Minus size={16} />
      </ControlButton>
      <ControlButton aria-label="Fit view" onClick={() => void fitView()}>
        <Maximize size={16} />
      </ControlButton>
    </FlowPanel>
  );
}
function GraphNode({ data }: NodeProps<ResourceNode>) {
  const r = data.resource;
  const Icon = [
    'Service',
    'Ingress',
    'Gateway',
    'GatewayClass',
    'ReferenceGrant',
    'HTTPRoute',
  ].includes(r.kind)
    ? Network
    : ['PersistentVolumeClaim', 'Secret', 'ConfigMap'].includes(r.kind)
      ? Database
      : ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(r.kind)
        ? Layers3
        : Box;
  return (
    <div className={`graph-node ${r.kind.toLowerCase()}`}>
      <Handle type="target" position={Position.Left} />
      <div className="graph-node-icon">
        <Icon size={17} />
      </div>
      <div>
        <small>{r.kind}</small>
        <strong title={r.name}>{r.name}</strong>
      </div>
      <span className={`status-dot ${r.health}`} />
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
const nodeTypes = { resource: GraphNode };
export function Topology({ namespace }: { namespace: string }) {
  const [p, setParams] = useSearchParams();
  const search = p.get('search') ?? '';
  const group = p.get('group') ?? '';
  const data = useAPI<Graph>(`/api/v1/topology?${new URLSearchParams({ namespace, group })}`);
  const [selected, setSelected] = useState('');
  const [selectedEdge, setSelectedEdge] = useState('');
  const [showPods, setShowPods] = useState(true);
  const [showNamespaces, setShowNamespaces] = useState(false);
  const [labels, setLabels] = useState(false);
  const requestedLimit = Number(p.get('nodes') ?? 60);
  const limit = [30, 60, 120, 200].includes(requestedLimit) ? requestedLimit : 60;
  const update = (k: string, v: string) => {
    const n = new URLSearchParams(p);
    if (v) n.set(k, v);
    else n.delete(k);
    setParams(n, { replace: true });
  };
  const graph = useMemo(
    () =>
      data.data
        ? buildGraph(data.data, {
            search,
            group,
            limit,
            showNamespaces,
            showPods,
            selected,
            labels,
          })
        : { nodes: [], edges: [], total: 0 },
    [data.data, search, group, limit, showNamespaces, showPods, selected, labels],
  );
  const resource = data.data?.nodes.find((r) => r.id === selected);
  const edge = data.data?.edges.find(
    (e) => e.id === selectedEdge && graph.edges.some((v) => v.id === e.id),
  );
  const selectEdge = (id: string) => {
    setSelectedEdge(id);
    setSelected('');
  };
  const relations =
    data.data?.edges.filter((e) => e.source === selected || e.target === selected) ?? [];
  return (
    <>
      <PageTitle
        eyebrow="SEE HOW IT FITS TOGETHER"
        title="Topology"
        description="Explore declared relationships. Inspect Gateway listener policies, Service ports, ReferenceGrants and controller reports. These checks do not establish observed traffic."
        action={
          <span className="tag">
            <GitBranch size={13} />
            Configuration relationships
          </span>
        }
      />
      <Warnings items={data.data?.warnings} />
      <div className="filterbar">
        <div className="search-field">
          <Search size={15} />
          <input
            placeholder="Highlight resources by name or kind…"
            aria-label="Search topology"
            value={search}
            maxLength={200}
            onChange={(e) => update('search', e.target.value)}
          />
        </div>
        <select
          aria-label="Topology resource types"
          value={group}
          onChange={(e) => update('group', e.target.value)}
        >
          <option value="">All resource types</option>
          <option value="workloads">Workloads</option>
          <option value="networking">Networking & pods</option>
          <option value="configuration">Configuration & pods</option>
        </select>
        <select
          aria-label="Maximum graph nodes"
          value={limit}
          onChange={(e) => update('nodes', e.target.value)}
        >
          {[30, 60, 120, 200].map((n) => (
            <option key={n} value={n}>
              Up to {n} nodes
            </option>
          ))}
        </select>
      </div>
      <div className="graph-options">
        <label>
          <input
            type="checkbox"
            checked={showPods}
            onChange={(e) => setShowPods(e.target.checked)}
          />
          Pods
        </label>
        <label>
          <input
            type="checkbox"
            checked={showNamespaces}
            onChange={(e) => setShowNamespaces(e.target.checked)}
          />
          Namespace containers
        </label>
        <label>
          <input type="checkbox" checked={labels} onChange={(e) => setLabels(e.target.checked)} />
          Relationship labels
        </label>
        <span>
          {graph.nodes.length} nodes · {graph.edges.length} relationships
        </span>
      </div>
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} />
      ) : (
        <div className={`topology-layout ${resource || edge ? 'with-selection' : ''}`}>
          <div
            className="graph-container"
            role="region"
            aria-label="Interactive Kubernetes topology"
          >
            {graph.nodes.length ? (
              <ReactFlow
                key={`${namespace}:${group}:${limit}:${showPods}:${showNamespaces}`}
                nodes={graph.nodes}
                edges={graph.edges}
                nodeTypes={nodeTypes}
                fitView
                minZoom={0.1}
                maxZoom={1.8}
                onNodeClick={(_, n) => {
                  setSelected(n.id);
                  setSelectedEdge('');
                }}
                onEdgeClick={(_, e) => {
                  if (data.data?.edges.find((v) => v.id === e.id)?.routing) selectEdge(e.id);
                }}
                onPaneClick={() => {
                  setSelected('');
                  setSelectedEdge('');
                }}
                nodesDraggable={false}
                nodesConnectable={false}
                proOptions={{ hideAttribution: false }}
              >
                <Background
                  variant={BackgroundVariant.Dots}
                  gap={22}
                  size={1}
                  color="var(--graph-grid)"
                />
                <GraphControls />
                <MiniMap
                  nodeColor={(n) => topologyHealthColor((n.data.resource as Resource).health)}
                  maskColor="var(--graph-mask)"
                  pannable
                  zoomable
                />
                <div className="graph-corner-label">
                  <Network size={14} />
                  DECLARED TOPOLOGY<span>Pan to explore · Scroll to zoom</span>
                </div>
              </ReactFlow>
            ) : (
              <Empty title="No resources in this graph" />
            )}
          </div>
          {edge && data.data && (
            <RoutingDetail edge={edge} graph={data.data} onClose={() => setSelectedEdge('')} />
          )}
          {resource && (
            <Panel
              title="Selected resource"
              className="topology-detail"
              action={
                <button
                  className="icon-button"
                  aria-label="Clear topology selection"
                  onClick={() => setSelected('')}
                >
                  <X size={16} />
                </button>
              }
            >
              <div className="topology-selection">
                <span className="eyebrow">{resource.kind}</span>
                <h2>{resource.name}</h2>
                <span className="namespace-tag">{resource.namespace || 'Cluster scoped'}</span>
                <Status value={resource.health} label={resource.status} />
                <Link to={resourceURL(resource)} className="button primary">
                  Inspect resource
                  <ArrowUpRight size={14} />
                </Link>
              </div>
              <div className="detail-section">
                <h3>Incoming & outgoing</h3>
                {relations.map((e) => {
                  const r = data.data.nodes.find(
                    (n) => n.id === (e.source === selected ? e.target : e.source),
                  );
                  return r ? (
                    <Link className="graph-relation" to={resourceURL(r)} key={e.id}>
                      {e.source === selected ? (
                        <ArrowUpRight size={14} />
                      ) : (
                        <ArrowDownLeft size={14} />
                      )}
                      <span>
                        <small>
                          {e.relation} · {e.confidence}
                        </small>
                        {r.name}
                      </span>
                      <ArrowRight size={13} />
                    </Link>
                  ) : null;
                })}
                {!relations.length && (
                  <p className="muted">No declared relationships in the current snapshot.</p>
                )}
              </div>
            </Panel>
          )}
        </div>
      )}
      <div className="graph-legend">
        <span>
          <i />
          Ownership / routing
        </span>
        <span>
          <i className="dotted" />
          Service selector
        </span>
        <span>
          <i className="dashed" />
          Configuration / volume reference
        </span>
        <span className="legend-note">
          Routing: blue checks passed · red blocked/rejected · gray incomplete. No traffic
          observation.
        </span>
      </div>
      {(data.data?.truncated || graph.total > graph.nodes.length) && (
        <p className="data-warning">
          Graph complexity is limited. Increase the node limit or select a namespace.
        </p>
      )}
      {data.data && (
        <RoutingReferences
          graph={data.data}
          visible={new Set(graph.nodes.map((n) => n.id))}
          selected={selectedEdge}
          onSelect={selectEdge}
        />
      )}
      <p className="footnote">
        Selection highlights direct neighbors. Resource types without a supported relation remain
        unconnected. Layout is deterministic and grouped by namespace.
      </p>
    </>
  );
}
