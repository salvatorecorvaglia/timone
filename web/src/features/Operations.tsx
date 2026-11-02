import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ScopedLink as Link } from '../components/navigation';
import { request, resourceURL } from '../lib/api';
import type { Resource, Issue, KEvent, Warning } from '../types';
import {
  Empty,
  ErrorState,
  Loading,
  Panel,
  ResourceLink,
  TableScroll,
  Warnings,
} from '../components/ui';
import { EventCard } from './EventCard';
import { RunbookLinks } from './RunbookLinks';
import { saveInvestigation } from './Investigation';

export interface Check {
  code: string;
  state: string;
  message: string;
}
interface BaseAssessment {
  observedAt: string;
  state: string;
  warnings: Warning[];
  limitations: string[];
  checks: Check[];
}
interface Scheduling extends BaseAssessment {
  pod: Resource;
  requests: Record<string, number | null>;
  events: KEvent[];
  nodes: { resource: Resource; state: string; checks: Check[] }[];
}
interface Rollout extends BaseAssessment {
  resource: Resource;
  generation: number | null;
  observedGeneration: number | null;
  desired: number | null;
  updated: number | null;
  ready: number | null;
  available: number | null;
  currentRevision: string;
  targetRevision: string;
  strategy: string;
  partition: number;
  targetImages: string[];
  revisions: {
    resource: Resource;
    revision: string;
    role: string;
    replicas: number | null;
    ready: number | null;
    images: string[];
  }[];
  pods: { resource: Resource; revision: string; images: string[]; imageIDs: string[] }[];
  issues: Issue[];
  events: KEvent[];
}
interface Maintenance extends BaseAssessment {
  node: Resource;
  scope: string;
  pods: {
    resource: Resource;
    controller?: Resource;
    class: string;
    checks: Check[];
    pdbs: Resource[];
  }[];
  budgets: {
    resource: Resource;
    targetPods: number;
    disruptionsAllowed: number | null;
    message: string;
  }[];
}
export function CheckList({ checks }: { checks: Check[] }) {
  return (
    <ul>
      {checks.map((item, i) => (
        <li key={item.code + i}>
          <strong>{item.state}</strong> · {item.code}: {item.message}
        </li>
      ))}
    </ul>
  );
}
export function Coverage({ data }: { data: BaseAssessment }) {
  return (
    <>
      <p>
        Assessment: {data.state} · Observed: {data.observedAt}
      </p>
      <Warnings items={data.warnings} />
      {data.limitations.map((text) => (
        <p className="footnote" key={text}>
          {text}
        </p>
      ))}
      <CheckList checks={data.checks} />
    </>
  );
}
export function useAssessment<T>(path: string) {
  return useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<T>(path, { signal }),
    retry: false,
    staleTime: 30000,
  });
}
export function SchedulingDetails({ namespace, name }: { namespace: string; name: string }) {
  const [constraintsOnly, setConstraintsOnly] = useState(false);
  const query = useAssessment<Scheduling>(
    `/api/v1/resources/Pod/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scheduling`,
  );
  return (
    <Panel
      title="Scheduling constraints"
      subtitle="Observed placement constraints for this Pod. Passing these checks does not establish that a scheduler can place it."
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh scheduling analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <p>
            Configured CPU request: {query.data.requests.cpu ?? 'Unavailable'} cores · Configured
            memory request: {query.data.requests.memory ?? 'Unavailable'} bytes
          </p>
          <label>
            <input
              type="checkbox"
              checked={constraintsOnly}
              onChange={(e) => setConstraintsOnly(e.target.checked)}
            />
            Show only nodes with observed constraints
          </label>
          <TableScroll>
            <table aria-label="Node scheduling constraints">
              <thead>
                <tr>
                  <th>Node</th>
                  <th>Assessment</th>
                  <th>Checked evidence</th>
                </tr>
              </thead>
              <tbody>
                {query.data.nodes
                  .filter((node) => !constraintsOnly || node.state === 'constraints-observed')
                  .map((node) => (
                    <tr key={node.resource.id}>
                      <td>
                        <ResourceLink resource={node.resource} />
                      </td>
                      <td>{node.state}</td>
                      <td>
                        <CheckList checks={node.checks} />
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </TableScroll>
          {!query.data.nodes.length && (
            <Empty
              title="Node inventory unavailable"
              description="No placement conclusion can be drawn without readable Node objects."
            />
          )}
          <h4>Retained Pod events</h4>
          {query.data.events.map((event) => (
            <EventCard key={event.id} event={event} />
          ))}
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'scheduling-assessment.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export scheduling analysis
          </button>
        </>
      )}
    </Panel>
  );
}
export function RolloutDetails({
  kind,
  namespace,
  name,
}: {
  kind: string;
  namespace: string;
  name: string;
}) {
  const query = useAssessment<Rollout>(
    `/api/v1/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/rollout`,
  );
  return (
    <Panel
      title="Rollout investigation"
      subtitle="Desired configuration, observed controller generation, retained revisions and current owned Pods."
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh rollout analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <dl className="property-list">
            <dt>Desired generation</dt>
            <dd>{query.data.generation ?? 'Unavailable'}</dd>
            <dt>Observed generation</dt>
            <dd>{query.data.observedGeneration ?? 'Unavailable'}</dd>
            <dt>Strategy</dt>
            <dd>
              {query.data.strategy} · Partition {query.data.partition}
            </dd>
            <dt>Desired / updated / ready / available</dt>
            <dd>
              {query.data.desired ?? '?'} / {query.data.updated ?? '?'} / {query.data.ready ?? '?'}{' '}
              / {query.data.available ?? '?'}
            </dd>
            <dt>Reported current revision</dt>
            <dd>{query.data.currentRevision || 'Not reported'}</dd>
            <dt>Reported target revision</dt>
            <dd>{query.data.targetRevision || 'Not reported'}</dd>
          </dl>
          <h4>Desired template images</h4>
          <ul>
            {query.data.targetImages.map((image) => (
              <li key={image}>
                <code>{image}</code>
              </li>
            ))}
          </ul>
          <h4>Readable retained revisions</h4>
          <TableScroll>
            <table aria-label="Rollout revisions">
              <thead>
                <tr>
                  <th>Resource</th>
                  <th>Revision</th>
                  <th>Association</th>
                  <th>Replicas / ready</th>
                  <th>Template images</th>
                </tr>
              </thead>
              <tbody>
                {query.data.revisions.map((revision) => (
                  <tr key={revision.resource.id}>
                    <td>
                      <ResourceLink resource={revision.resource} />
                    </td>
                    <td>{revision.revision || 'Not reported'}</td>
                    <td>{revision.role}</td>
                    <td>
                      {revision.replicas ?? '?'} / {revision.ready ?? '?'}
                    </td>
                    <td>{revision.images.join(', ')}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          <h4>Current controller-owned Pods</h4>
          <TableScroll>
            <table aria-label="Rollout Pods">
              <thead>
                <tr>
                  <th>Pod</th>
                  <th>Status / revision hash</th>
                  <th>Declared images</th>
                  <th>Runtime image IDs</th>
                  <th>Investigate</th>
                </tr>
              </thead>
              <tbody>
                {query.data.pods.map((pod) => (
                  <tr key={pod.resource.id}>
                    <td>
                      <ResourceLink resource={pod.resource} />
                    </td>
                    <td>
                      {pod.resource.status} · {pod.revision || 'Not reported'}
                    </td>
                    <td>{pod.images.join(', ')}</td>
                    <td>{pod.imageIDs.join(', ') || 'Not reported'}</td>
                    <td>
                      <Link to={resourceURL(pod.resource) + '?tab=logs'}>Logs</Link> ·{' '}
                      <Link to={resourceURL(pod.resource) + '?tab=scheduling'}>Scheduling</Link> ·{' '}
                      <Link to={resourceURL(pod.resource) + '?tab=incident'}>Incident</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          <h4>Current workload signals</h4>
          {query.data.issues.map((issue) => (
            <div key={issue.id}>
              <strong>{issue.title}</strong>
              <ul>
                {issue.evidence.map((fact) => (
                  <li key={fact}>{fact}</li>
                ))}
              </ul>
              <RunbookLinks issue={issue} />
            </div>
          ))}
          <h4>Retained rollout events</h4>
          {query.data.events.map((event) => (
            <EventCard key={event.id} event={event} />
          ))}
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'rollout-assessment.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export rollout analysis
          </button>
        </>
      )}
    </Panel>
  );
}
export function NodeMaintenanceDetails({ name, namespace }: { name: string; namespace: string }) {
  const query = useAssessment<Maintenance>(
    `/api/v1/nodes/${encodeURIComponent(name)}/maintenance?${new URLSearchParams({ namespace })}`,
  );
  return (
    <Panel
      title="Node maintenance assessment"
      subtitle="Read-only preflight evidence. No drain, cordon or eviction action is available."
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh maintenance analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <p>Inventory scope: {query.data.scope || 'All readable namespaces'}</p>
          <h4>Shared disruption budgets</h4>
          <TableScroll>
            <table aria-label="Shared node disruption budgets">
              <thead>
                <tr>
                  <th>PDB</th>
                  <th>Selected node Pods</th>
                  <th>Current allowance</th>
                  <th>Interpretation</th>
                </tr>
              </thead>
              <tbody>
                {query.data.budgets.map((budget) => (
                  <tr key={budget.resource.id}>
                    <td>
                      <ResourceLink resource={budget.resource} />
                    </td>
                    <td>{budget.targetPods}</td>
                    <td>{budget.disruptionsAllowed ?? 'Unavailable'}</td>
                    <td>{budget.message}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          <TableScroll>
            <table aria-label="Node maintenance Pod checks">
              <thead>
                <tr>
                  <th>Pod</th>
                  <th>Controller / class</th>
                  <th>PDBs</th>
                  <th>Checks</th>
                </tr>
              </thead>
              <tbody>
                {query.data.pods.map((pod) => (
                  <tr key={pod.resource.id}>
                    <td>
                      <ResourceLink resource={pod.resource} />
                    </td>
                    <td>
                      {pod.class}
                      {pod.controller && (
                        <>
                          {' '}
                          · <ResourceLink resource={pod.controller} />
                        </>
                      )}
                    </td>
                    <td>
                      {pod.pdbs.map((pdb) => (
                        <div key={pdb.id}>
                          <ResourceLink resource={pdb} />
                        </div>
                      ))}
                    </td>
                    <td>
                      <CheckList checks={pod.checks} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          {!query.data.pods.length && (
            <Empty
              title="No readable Pods in this node inventory"
              description="Check configured scope and collection warnings before drawing conclusions."
            />
          )}
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'maintenance-assessment.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export maintenance analysis
          </button>
        </>
      )}
    </Panel>
  );
}
