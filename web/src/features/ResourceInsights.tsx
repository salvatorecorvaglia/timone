import { useSearchParams } from 'react-router-dom';
import { ScopedLink as Link } from '../components/navigation';
import { Empty, ErrorState, Loading, Panel, ResourceLink, TableScroll } from '../components/ui';
import type { Resource, Warning } from '../types';
import { resourceURL } from '../lib/api';
import { CheckList, Coverage, useAssessment, type Check } from './Operations';
import { saveInvestigation } from './Investigation';

type Target = { kind: string; namespace: string; name: string };
interface Assessment {
  resource: Resource;
  observedAt: string;
  state: string;
  checks: Check[];
  warnings: Warning[];
  limitations: string[];
}
interface Usage {
  metric: string;
  unit: string;
  samples: number;
  expected: number;
  coverage: number;
  median: number | null;
  p95: number | null;
  peak: number | null;
  request: number | null;
  limit: number | null;
  checks: Check[];
}
interface Efficiency extends Assessment {
  range: string;
  source: string;
  start: string;
  end: string;
  currentPods: number;
  analyzedPods: number;
  containers: {
    pod: Resource;
    container: string;
    class: string;
    state: string;
    message: string;
    start: string;
    end: string;
    stepSeconds: number;
    identity: string;
    usage: Usage[];
  }[];
}
interface Configuration extends Assessment {
  currentPods: number;
  references: {
    consumer: Resource;
    origin: string;
    container: string;
    path: string;
    kind: string;
    name: string;
    key: string;
    optional: boolean;
    consumption: string;
    update: string;
    resource?: Resource;
    state: string;
    immutable: boolean | null;
    checks: Check[];
  }[];
}
function path(target: Target, suffix: string) {
  return `/api/v1/resources/${encodeURIComponent(target.kind)}/${encodeURIComponent(target.namespace)}/${encodeURIComponent(target.name)}/${suffix}`;
}
function amount(value: number | null, metric: string) {
  if (value === null) return 'Unknown';
  return metric === 'cpu'
    ? `${(value * 1000).toFixed(1)} mCPU`
    : `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}
function ratio(value: number | null, baseline: number | null) {
  return value !== null && baseline !== null && baseline > 0
    ? `${((value / baseline) * 100).toFixed(1)}%`
    : 'Unknown';
}
export function EfficiencyDetails(target: Target) {
  const [params, setParams] = useSearchParams();
  const requested = params.get('efficiencyRange') ?? '24h';
  const range = ['1h', '6h', '24h'].includes(requested) ? requested : '24h';
  const query = useAssessment<Efficiency>(`${path(target, 'efficiency')}?range=${range}`);
  return (
    <Panel
      title="Resource efficiency analysis"
      subtitle="Sampled CPU and memory history compared with current container configuration. No automatic sizing or changes."
    >
      <div className="log-controls">
        <label>
          Observation window{' '}
          <select
            aria-label="Efficiency observation window"
            value={range}
            onChange={(e) =>
              setParams((previous) => {
                const next = new URLSearchParams(previous);
                next.set('efficiencyRange', e.target.value);
                return next;
              })
            }
          >
            <option value="1h">1 hour</option>
            <option value="6h">6 hours</option>
            <option value="24h">24 hours</option>
          </select>
        </label>
        <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
          Refresh efficiency analysis
        </button>
      </div>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <p>
            Source: {query.data.source} · Window: {query.data.start} → {query.data.end}
          </p>
          <p>
            {query.data.analyzedPods} Pod queries attempted / {query.data.currentPods} currently
            observed Pods. Workload totals and savings are not inferred.
          </p>
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'resource-efficiency.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export efficiency analysis
          </button>
          {!query.data.containers.length && (
            <Empty
              title="No current container evidence"
              description="Review history availability, ownership and coverage warnings."
            />
          )}
          <TableScroll>
            <table aria-label="Container resource efficiency">
              <thead>
                <tr>
                  <th>Pod / container</th>
                  <th>Metric / configuration</th>
                  <th>Sampled distribution</th>
                  <th>Coverage / evidence</th>
                </tr>
              </thead>
              <tbody>
                {query.data.containers.flatMap((container) =>
                  container.usage.map((usage) => (
                    <tr key={`${container.pod.id}/${container.container}/${usage.metric}`}>
                      <td>
                        <ResourceLink resource={container.pod} />
                        <p>
                          {container.container} · {container.class}
                        </p>
                        <p>{container.state}</p>
                        <p className="footnote">{container.message}</p>
                        <Link to={`${resourceURL(container.pod)}?tab=incident`}>
                          Incident & metric history
                        </Link>
                      </td>
                      <td>
                        {usage.metric}
                        <br />
                        Current request: {amount(usage.request, usage.metric)}
                        <br />
                        Current limit: {amount(usage.limit, usage.metric)}
                      </td>
                      <td>
                        Median: {amount(usage.median, usage.metric)}
                        <br />
                        P95: {amount(usage.p95, usage.metric)}
                        <br />
                        Sampled maximum: {amount(usage.peak, usage.metric)}
                        <br />
                        P95 / request: {ratio(usage.p95, usage.request)}
                        <br />
                        Maximum / limit: {ratio(usage.peak, usage.limit)}
                      </td>
                      <td>
                        {usage.samples}/{usage.expected} valid points ·{' '}
                        {(usage.coverage * 100).toFixed(1)}% of requested grid
                        <br />
                        Evaluation step: {container.stepSeconds}s<br />
                        Eligible window: {container.start} → {container.end}
                        <p className="footnote">{container.identity}</p>
                        <CheckList checks={usage.checks} />
                      </td>
                    </tr>
                  )),
                )}
              </tbody>
            </table>
          </TableScroll>
        </>
      )}
    </Panel>
  );
}
export function ConfigurationDetails(target: Target) {
  const query = useAssessment<Configuration>(path(target, 'configuration'));
  return (
    <Panel
      title="Configuration dependency analysis"
      subtitle="Explicit ConfigMap and Secret consumers. Desired templates and current Pods are shown separately; payloads are omitted."
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh configuration analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <p>{query.data.currentPods} current owned Pods observed.</p>
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'configuration-dependencies.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export configuration analysis
          </button>
          {!query.data.references.length && (
            <Empty
              title="No explicit ConfigMap or Secret references observed"
              description="This does not establish that the application has no configuration dependencies."
            />
          )}
          <TableScroll>
            <table aria-label="Configuration dependency references">
              <thead>
                <tr>
                  <th>Consumer / declaration</th>
                  <th>Reference</th>
                  <th>Availability</th>
                  <th>Update behavior / evidence</th>
                </tr>
              </thead>
              <tbody>
                {query.data.references.map((ref, i) => (
                  <tr key={`${ref.consumer.id}/${ref.path}/${i}`}>
                    <td>
                      <ResourceLink resource={ref.consumer} />
                      <p>
                        {ref.origin} · {ref.container || 'Pod-level'}
                      </p>
                      <code>{ref.path}</code>
                    </td>
                    <td>
                      {ref.kind} ·{' '}
                      {ref.resource ? <ResourceLink resource={ref.resource} /> : ref.name}
                      <br />
                      Selected key: {ref.key || '(all / no key selection)'}
                      <br />
                      Optional: {String(ref.optional)}
                    </td>
                    <td>
                      {ref.state}
                      <br />
                      Immutable: {String(ref.immutable ?? 'unspecified')}
                    </td>
                    <td>
                      {ref.consumption}
                      <p>{ref.update}</p>
                      <CheckList checks={ref.checks} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
        </>
      )}
    </Panel>
  );
}
