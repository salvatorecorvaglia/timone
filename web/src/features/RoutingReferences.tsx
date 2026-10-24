import { ScopedLink as Link } from '../components/navigation';

import { X } from 'lucide-react';
import { TableScroll, Panel } from '../components/ui';
import { resourceURL } from '../lib/api';
import type { Graph, GraphEdge, RoutingAssessment } from '../types';

const labels: Record<RoutingAssessment['state'], string> = {
  allowed: 'Checks passed',
  denied: 'Blocked configuration',
  unknown: 'Incomplete checks',
};

export function RoutingReferences({
  graph,
  visible,
  selected,
  onSelect,
}: {
  graph: Graph;
  visible: Set<string>;
  selected: string;
  onSelect: (id: string) => void;
}) {
  const edges = graph.edges.filter(
    (e) => e.routing && visible.has(e.source) && visible.has(e.target),
  );
  if (!edges.length) return null;
  return (
    <Panel
      title="Gateway routing references"
      action={<span className="tag">{edges.length} references</span>}
    >
      <p className="routing-note">
        Configuration checks and controller reports are separate. Neither confirms observed traffic.
      </p>
      <TableScroll>
        <table aria-label="Gateway routing references">
          <thead>
            <tr>
              <th>Declared reference</th>
              <th>Configuration</th>
              <th>Controller Accepted</th>
              <th>Inspect</th>
            </tr>
          </thead>
          <tbody>
            {edges.slice(0, 100).map((e) => {
              const source = graph.nodes.find((n) => n.id === e.source);
              const target = graph.nodes.find((n) => n.id === e.target);
              return (
                <tr key={e.id} className={e.id === selected ? 'routing-selected' : undefined}>
                  <td>
                    <span>
                      {source?.name} → {target?.name}
                    </span>
                    <small className="routing-path">{e.routing!.reference}</small>
                  </td>
                  <td>
                    <span className={`routing-state ${e.routing!.state}`}>
                      {labels[e.routing!.state]}
                    </span>
                  </td>
                  <td>{e.routing!.accepted ?? 'Not assessed per backend'}</td>
                  <td>
                    <button
                      className="button"
                      aria-label={`Inspect ${source?.name} to ${target?.name} ${e.routing!.reference} Evidence`}
                      onClick={() => onSelect(e.id)}
                    >
                      Evidence
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </TableScroll>
      {edges.length > 100 && (
        <p className="data-warning">
          Showing the first 100 visible references. Narrow the namespace or resource filters.
        </p>
      )}
    </Panel>
  );
}

export function RoutingDetail({
  edge,
  graph,
  onClose,
}: {
  edge: GraphEdge;
  graph: Graph;
  onClose: () => void;
}) {
  const a = edge.routing;
  if (!a) return null;
  return (
    <Panel
      title="Reference evidence"
      className="topology-detail"
      action={
        <button className="icon-button" aria-label="Close reference evidence" onClick={onClose}>
          <X size={16} />
        </button>
      }
    >
      <div className="topology-selection">
        <span className="eyebrow">
          {edge.relation} · {edge.confidence}
        </span>
        {[edge.source, edge.target].map((id, i) => {
          const r = graph.nodes.find((n) => n.id === id);
          return (
            r && (
              <div key={id}>
                <small>
                  {i === 0 ? 'From' : 'To'} · {r.kind} · {r.namespace || 'Cluster scoped'}
                </small>
                <Link className="graph-relation" to={resourceURL(r)}>
                  {r.name}
                </Link>
              </div>
            )
          );
        })}
        <code className="routing-path">{a.reference}</code>
        <span className={`routing-state ${a.state}`}>{labels[a.state]}</span>
      </div>
      {a.accepted && (
        <div className="detail-section">
          <h3>Controller report</h3>
          <p>
            Accepted: {a.accepted} · ResolvedRefs: {a.resolvedRefs}
          </p>
          <p className="muted">
            Conditions apply to this route and parent. Unknown includes missing or stale evidence.
          </p>
        </div>
      )}
      <div className="detail-section">
        <h3>Collected evidence</h3>
        <ul className="routing-evidence">
          {a.evidence.map((v, i) => (
            <li key={i}>{v}</li>
          ))}
        </ul>
      </div>
      <div className="detail-section">
        <h3>Assessment limits</h3>
        {a.limitations.map((v, i) => (
          <p className="muted" key={i}>
            {v}
          </p>
        ))}
      </div>
    </Panel>
  );
}
