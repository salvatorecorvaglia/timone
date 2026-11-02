import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ScopedLink as Link } from '../components/navigation';
import { Empty, ErrorState, Loading, Panel, ResourceLink, TableScroll } from '../components/ui';
import { resourceURL } from '../lib/api';
import type { Resource, Warning, Issue, KEvent } from '../types';
import { CheckList, Coverage, useAssessment, type Check } from './Operations';
import { saveInvestigation } from './Investigation';
import { EventCard } from './EventCard';
import { RunbookLinks } from './RunbookLinks';

interface Assessment {
  resource: Resource;
  observedAt: string;
  state: string;
  checks: Check[];
  warnings: Warning[];
  limitations: string[];
}
interface Direction {
  state: string;
  policies: Resource[];
  checks: Check[];
}
interface Network extends Assessment {
  source?: Resource;
  paths: {
    reference: string;
    service: Resource;
    servicePort: number;
    portName: string;
    protocol: string;
    targetPort: string;
    checks: Check[];
    endpoints: {
      slice: Resource;
      pod?: Resource;
      addresses: string[];
      ready: boolean | null;
      serving: boolean | null;
      terminating: boolean;
      port: number | null;
      checks: Check[];
      ingress: Direction;
      egress: Direction;
    }[];
  }[];
}
interface Batch extends Assessment {
  schedule: string;
  timeZone: string;
  concurrencyPolicy: string;
  suspended: boolean;
  lastScheduleTime: string;
  lastSuccessfulTime: string;
  startingDeadlineSeconds: number | null;
  successfulHistoryLimit: number | null;
  failedHistoryLimit: number | null;
  jobs: {
    resource: Resource;
    state: string;
    startTime: string;
    completionTime: string;
    active: number | null;
    succeeded: number | null;
    failed: number | null;
    completions: number | null;
    parallelism: number | null;
    backoffLimit: number | null;
    backoffLimitPerIndex: number | null;
    deadlineSeconds: number | null;
    completionMode: string;
    completedIndexes: string;
    failedIndexes: string;
    checks: Check[];
  }[];
  pods: { resource: Resource; job: Resource; checks: Check[] }[];
  issues: Issue[];
  events: KEvent[];
}
type Target = { kind: string; namespace: string; name: string };
function endpoint(target: Target, suffix: string) {
  return `/api/v1/resources/${encodeURIComponent(target.kind)}/${encodeURIComponent(target.namespace || '_')}/${encodeURIComponent(target.name)}/${suffix}`;
}
function PolicyResult({ title, data }: { title: string; data: Direction }) {
  return (
    <div>
      <strong>
        {title}: {data.state}
      </strong>
      <CheckList checks={data.checks} />
      {data.policies.map((policy) => (
        <div key={policy.id}>
          <ResourceLink resource={policy} />
        </div>
      ))}
    </div>
  );
}
export function NetworkDetails(target: Target) {
  const [params, setParams] = useSearchParams();
  const sourcePod = params.get('sourcePod') ?? '';
  const sourceNamespace = params.get('sourceNamespace') ?? '';
  const [pod, setPod] = useState(sourcePod);
  const [namespace, setNamespace] = useState(sourceNamespace || target.namespace);
  const queryParams = new URLSearchParams();
  if (sourcePod) queryParams.set('sourcePod', sourcePod);
  if (sourceNamespace) queryParams.set('sourceNamespace', sourceNamespace);
  const query = useAssessment<Network>(
    endpoint(target, 'network') + (queryParams.size ? `?${queryParams}` : ''),
  );
  function apply(clear = false) {
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      next.delete('sourcePod');
      next.delete('sourceNamespace');
      if (!clear && pod.trim()) {
        next.set('sourcePod', pod.trim());
        if (namespace.trim()) next.set('sourceNamespace', namespace.trim());
      }
      return next;
    });
    if (clear) setPod('');
  }
  return (
    <Panel
      title="Network path analysis"
      subtitle="Declared paths and native policy rules. This view does not test connectivity."
    >
      <form
        className="log-controls"
        onSubmit={(e) => {
          e.preventDefault();
          apply();
        }}
      >
        <label>
          Source namespace
          <input
            aria-label="Source namespace"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
          />
        </label>
        <label>
          Source Pod
          <input aria-label="Source Pod" value={pod} onChange={(e) => setPod(e.target.value)} />
        </label>
        <button className="button" type="submit">
          Analyze source Pod
        </button>
        <button className="button" type="button" onClick={() => apply(true)}>
          Clear source Pod
        </button>
      </form>
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh network analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          {query.data.source && (
            <p>
              Policy source: <ResourceLink resource={query.data.source} />
            </p>
          )}
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'network-analysis.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export network analysis
          </button>
          {!query.data.paths.length && (
            <Empty
              title="No supported paths available"
              description="Review coverage warnings and reference checks."
            />
          )}
          {query.data.paths.map((path, i) => (
            <section className="detail-section" key={`${path.reference}-${i}`}>
              <h3>
                <ResourceLink resource={path.service} /> · {path.servicePort}/{path.protocol} →{' '}
                {path.targetPort}
              </h3>
              <p>
                {path.reference} · Port name: {path.portName || '(unnamed)'}
              </p>
              <CheckList checks={path.checks} />
              <TableScroll>
                <table aria-label="Published network endpoints">
                  <thead>
                    <tr>
                      <th>EndpointSlice / Pod</th>
                      <th>Address / port</th>
                      <th>Reported flags</th>
                      <th>Native policies / evidence</th>
                    </tr>
                  </thead>
                  <tbody>
                    {path.endpoints.map((ep, j) => (
                      <tr key={`${ep.slice.id}-${j}`}>
                        <td>
                          <ResourceLink resource={ep.slice} />
                          {ep.pod && (
                            <div>
                              <ResourceLink resource={ep.pod} />
                            </div>
                          )}
                        </td>
                        <td>
                          {ep.addresses.join(', ')} : {ep.port ?? 'Unknown'}
                        </td>
                        <td>
                          Ready: {String(ep.ready ?? 'unspecified (consumer default true)')}
                          <br />
                          Serving: {String(ep.serving ?? 'unspecified')}
                          <br />
                          Terminating: {String(ep.terminating)}
                        </td>
                        <td>
                          <CheckList checks={ep.checks} />
                          <PolicyResult title="Backend ingress" data={ep.ingress} />
                          <PolicyResult title="Source egress" data={ep.egress} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </TableScroll>
            </section>
          ))}
        </>
      )}
    </Panel>
  );
}
export function BatchDetails(target: Target) {
  const query = useAssessment<Batch>(endpoint(target, 'batch'));
  const value = (n: number | null) => n ?? 'Not reported';
  return (
    <Panel
      title="Batch execution analysis"
      subtitle="Retained Job and Pod evidence, matched by controller UID."
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh execution analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'batch-analysis.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export execution analysis
          </button>
          {target.kind === 'CronJob' && (
            <dl className="property-list">
              <dt>Schedule / time zone</dt>
              <dd>
                {query.data.schedule} · {query.data.timeZone}
              </dd>
              <dt>Concurrency / suspended</dt>
              <dd>
                {query.data.concurrencyPolicy} / {String(query.data.suspended)}
              </dd>
              <dt>Last reported schedule</dt>
              <dd>{query.data.lastScheduleTime || 'Not reported'}</dd>
              <dt>Last reported success</dt>
              <dd>{query.data.lastSuccessfulTime || 'Not reported'}</dd>
              <dt>Starting deadline (seconds)</dt>
              <dd>{value(query.data.startingDeadlineSeconds)}</dd>
              <dt>Successful / failed history limits</dt>
              <dd>
                {value(query.data.successfulHistoryLimit)} / {value(query.data.failedHistoryLimit)}
              </dd>
            </dl>
          )}
          <TableScroll>
            <table aria-label="Retained Job executions">
              <thead>
                <tr>
                  <th>Job / state</th>
                  <th>Reported counts</th>
                  <th>Configuration</th>
                  <th>Evidence</th>
                </tr>
              </thead>
              <tbody>
                {query.data.jobs.map((job) => (
                  <tr key={job.resource.id}>
                    <td>
                      <ResourceLink resource={job.resource} />
                      <p>{job.state}</p>
                    </td>
                    <td>
                      Active: {value(job.active)}
                      <br />
                      Succeeded: {value(job.succeeded)}
                      <br />
                      Failed Pods: {value(job.failed)}
                    </td>
                    <td>
                      Completions: {value(job.completions)} · Parallelism: {value(job.parallelism)}
                      <br />
                      Backoff limit: {value(job.backoffLimit)} · Per index:{' '}
                      {value(job.backoffLimitPerIndex)}
                      <br />
                      Active deadline (seconds): {value(job.deadlineSeconds)}
                      <br />
                      Mode: {job.completionMode || 'Not reported'}
                    </td>
                    <td>
                      Start: {job.startTime || 'Not reported'}
                      <br />
                      Completion: {job.completionTime || 'Not reported'}
                      <br />
                      Completed indexes: {job.completedIndexes || 'Not reported'}
                      <br />
                      Failed indexes: {job.failedIndexes || 'Not reported'}
                      <CheckList checks={job.checks} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          {!query.data.jobs.length && (
            <Empty
              title="No retained owned executions"
              description="History limits, TTL or access restrictions can hide past executions."
            />
          )}
          <TableScroll>
            <table aria-label="Job Pod attempts">
              <thead>
                <tr>
                  <th>Pod</th>
                  <th>Job</th>
                  <th>Evidence</th>
                </tr>
              </thead>
              <tbody>
                {query.data.pods.map((pod) => (
                  <tr key={pod.resource.id}>
                    <td>
                      <ResourceLink resource={pod.resource} />
                      <div>
                        <Link to={`${resourceURL(pod.resource)}?tab=logs`}>Logs</Link> ·{' '}
                        <Link to={`${resourceURL(pod.resource)}?tab=scheduling`}>Scheduling</Link> ·{' '}
                        <Link to={`${resourceURL(pod.resource)}?tab=incident`}>Incident</Link>
                      </div>
                    </td>
                    <td>
                      <ResourceLink resource={pod.job} />
                    </td>
                    <td>
                      <CheckList checks={pod.checks} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          <h3>Current diagnostic signals</h3>
          {query.data.issues.map((issue) => (
            <div key={issue.id}>
              <p>{issue.title}</p>
              <RunbookLinks issue={issue} />
            </div>
          ))}
          {!query.data.issues.length && (
            <p>No current diagnostic signals in the readable evidence.</p>
          )}
          <h3>Retained execution events</h3>
          {query.data.events.map((event) => (
            <EventCard key={event.id} event={event} />
          ))}
        </>
      )}
    </Panel>
  );
}
