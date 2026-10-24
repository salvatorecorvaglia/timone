import { ScopedLink as Link } from '../components/navigation';

import { Activity, ArrowUpRight, ExternalLink } from 'lucide-react';
import { ago, memory, useAPI } from '../lib/api';
import { MetricsHistory } from './MetricsHistory';
import { NodeBudgets } from './NodeBudgets';
import type { Metrics } from '../types';
import {
  TableScroll,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Warnings,
} from '../components/ui';

function Budget({
  usage,
  request,
  limit,
  format,
}: {
  usage: number;
  request: number | null;
  limit: number | null;
  format: (v: number) => string;
}) {
  return (
    <>
      <span className="mono">
        {request == null ? 'Unavailable' : format(request)} /{' '}
        {limit == null ? 'Unavailable' : format(limit)}
      </span>
      <small className="table-subtitle">
        {request != null && request > 0
          ? `${((usage / request) * 100).toFixed(1)}% of request`
          : 'Request comparison unavailable'}{' '}
        ·{' '}
        {limit != null && limit > 0
          ? `${((usage / limit) * 100).toFixed(1)}% of limit`
          : 'Limit comparison unavailable'}
      </small>
    </>
  );
}

export function MetricsPage({ namespace }: { namespace: string }) {
  const data = useAPI<Metrics>(`/api/v1/metrics?namespace=${encodeURIComponent(namespace)}`);
  if (data.isPending) return <Loading />;
  if (data.error) return <ErrorState error={data.error} />;
  const nodes = data.data.items.filter((m) => m.kind === 'Node');
  return (
    <>
      <PageTitle
        eyebrow="CAPACITY & CONSUMPTION"
        title="Metrics"
        description="Current CPU and memory measurements from the available metrics integration."
        action={
          <span className="tag">
            <Activity size={13} />
            {data.data.source}
          </span>
        }
      />
      {!data.data.available ? (
        <Panel title="Metrics unavailable">
          <Empty title="Metrics Server is not available" description={data.data.message} />
          <div className="integration-note">
            <a
              className="text-link"
              href="https://github.com/kubernetes-sigs/metrics-server"
              target="_blank"
              rel="noreferrer"
            >
              Metrics Server prerequisites
              <ExternalLink size={13} />
            </a>
          </div>
        </Panel>
      ) : (
        <>
          <div className="metrics-summary">
            {data.data.message}
            <span>
              Last sample:{' '}
              {data.data.items[0]?.timestamp
                ? new Date(data.data.items[0].timestamp).toLocaleTimeString()
                : 'Unavailable'}
            </span>
          </div>
          <div className="metric-node-grid">
            {nodes.map((m) => (
              <Panel key={m.name}>
                <div className="metric-node-title">
                  <Activity size={17} />
                  <Link to={`/resources/Node/_/${m.name}`}>{m.name}</Link>
                  <ArrowUpRight size={14} />
                  <Link
                    className="text-link"
                    to={`/metrics?${new URLSearchParams({ ...(namespace ? { namespace } : {}), historyKind: 'Node', historyName: m.name })}`}
                  >
                    History
                  </Link>
                </div>
                <div className="metric-measure">
                  <span>CPU</span>
                  <strong>{m.cpu.toFixed(2)} cores</strong>
                  <small>
                    {m.cpuCapacity
                      ? `${((m.cpu / m.cpuCapacity) * 100).toFixed(1)}% of ${m.cpuCapacity} cores`
                      : 'Capacity unavailable'}
                  </small>
                </div>
                <div className="usage-track">
                  <i
                    style={{
                      width: m.cpuCapacity
                        ? `${Math.min(100, (m.cpu / m.cpuCapacity) * 100)}%`
                        : '0%',
                    }}
                  />
                </div>
                <div className="metric-measure">
                  <span>Memory</span>
                  <strong>{memory(m.memory)}</strong>
                  <small>
                    {m.memoryCapacity
                      ? `${((m.memory / m.memoryCapacity) * 100).toFixed(1)}% of ${memory(m.memoryCapacity)}`
                      : 'Capacity unavailable'}
                  </small>
                </div>
                <div className="usage-track memory">
                  <i
                    style={{
                      width: m.memoryCapacity
                        ? `${Math.min(100, (m.memory / m.memoryCapacity) * 100)}%`
                        : '0%',
                    }}
                  />
                </div>
              </Panel>
            ))}
          </div>
          <Panel
            title="Pod & container consumption"
            subtitle="Measured usage alongside configured container budgets. A missing budget is unavailable."
          >
            <TableScroll>
              <table>
                <thead>
                  <tr>
                    <th>Pod / container</th>
                    <th>Namespace</th>
                    <th>CPU cores</th>
                    <th>Memory</th>
                    <th>CPU request / limit</th>
                    <th>Memory request / limit</th>
                    <th>Sample time</th>
                    <th>History</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.items
                    .filter((m) => m.kind === 'Pod')
                    .sort((a, b) => b.cpu - a.cpu)
                    .map((m) => (
                      <tr key={`${m.namespace}:${m.name}:${m.container ?? ''}`}>
                        <td>
                          <Link
                            className="resource-link"
                            to={`/resources/Pod/${m.namespace}/${m.name}`}
                          >
                            {m.name}
                            <ArrowUpRight size={12} />
                          </Link>
                          <small className="table-subtitle">
                            {m.container || 'All containers (simulated)'}
                          </small>
                        </td>
                        <td>
                          <span className="namespace-tag">{m.namespace}</span>
                        </td>
                        <td className="mono">{m.cpu.toFixed(3)}</td>
                        <td className="mono">{memory(m.memory)}</td>
                        <td>
                          <Budget
                            usage={m.cpu}
                            request={m.cpuRequest}
                            limit={m.cpuLimit}
                            format={(v) => `${v.toFixed(3)} cores`}
                          />
                        </td>
                        <td>
                          <Budget
                            usage={m.memory}
                            request={m.memoryRequest}
                            limit={m.memoryLimit}
                            format={memory}
                          />
                        </td>
                        <td>{ago(m.timestamp)}</td>
                        <td>
                          <Link
                            className="text-link"
                            aria-label={`History for ${m.name} ${m.container ?? ''}`}
                            to={`/metrics?${new URLSearchParams({ ...(namespace ? { namespace } : {}), historyKind: 'Pod', historyNamespace: m.namespace, historyName: m.name, ...(m.container ? { historyContainer: m.container } : {}) })}`}
                          >
                            View history
                          </Link>
                        </td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </TableScroll>
            {!data.data.items.length && <Empty title="No metrics samples returned" />}
          </Panel>
        </>
      )}
      <Warnings items={data.data.warnings} />
      <MetricsHistory namespace={namespace} />
      <Panel
        title="Pod usage totals"
        subtitle="Aggregates require complete current container sets at one sample time. Missing or stale values remain unavailable."
      >
        <TableScroll>
          <table aria-label="Pod usage totals">
            <thead>
              <tr>
                <th>Pod</th>
                <th>State</th>
                <th>CPU cores</th>
                <th>Memory</th>
                <th>Container coverage</th>
                <th>Evidence</th>
              </tr>
            </thead>
            <tbody>
              {data.data.podTotals?.map((p) => (
                <tr key={p.resource.id}>
                  <td>
                    <ResourceLink resource={p.resource} />
                  </td>
                  <td>{p.state}</td>
                  <td>{p.cpu == null ? 'Unavailable' : p.cpu.toFixed(3)}</td>
                  <td>{p.memory == null ? 'Unavailable' : memory(p.memory)}</td>
                  <td>
                    {p.observed}/{p.expected}
                    {p.missing.length ? ` · missing ${p.missing.join(', ')}` : ''}
                  </td>
                  <td>{p.reason}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableScroll>
        {!data.data.podTotals?.length && <Empty title="Pod aggregate data unavailable" />}
      </Panel>
      <NodeBudgets allocations={data.data.allocations} />
      <p className="footnote">
        Requests describe configured scheduling demand from non-terminal Pod specs, including init
        container peaks, native sidecars, Pod-level overrides and overhead. In-place resize status
        and node-allocatable DRA claims are not included. Limits use init peaks and sidecars,
        Pod-level overrides and overhead on positive budgets. Missing or zero limits cannot
        establish a finite total. Namespace filters show only that namespace’s demand. Historical
        coverage depends on the separately configured monitoring integration and its retention.
      </p>
    </>
  );
}
