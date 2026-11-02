import { ScopedLink as Link } from '../components/navigation';
import { useEffect, useState } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowLeft,
  ArrowRight,
  Box,
  Braces,
  Clock3,
  Code2,
  FileText,
  Layers3,
  ShieldCheck,
  Terminal,
} from 'lucide-react';
import { stringify } from 'yaml';
import { IncidentInvestigation, WorkloadLogInvestigation, StorageDetails } from './Investigation';
import { SchedulingDetails, RolloutDetails, NodeMaintenanceDetails } from './Operations';
import { TerminationDetails } from './Termination';
import { EfficiencyDetails, ConfigurationDetails } from './ResourceInsights';
import { NetworkDetails, BatchDetails } from './NetworkBatch';
import { FavoriteResource } from './SavedWorkspace';
import { ServiceAccountGrants } from './ServiceAccountGrants';
import { EventCard } from './EventCard';
import { ago, request, useAPI } from '../lib/api';
import type { Cluster, Detail } from '../types';
import {
  TableScroll,
  CopyButton,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  Status,
  Warnings,
} from '../components/ui';

function download(filename: string, content: string, type: string) {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function obj(v: unknown): Record<string, unknown> {
  return typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
}
export function ResourceDetail({ custom = false }: { custom?: boolean }) {
  const target = useParams();
  return <ResourceInvestigation key={JSON.stringify([custom, target])} custom={custom} />;
}
function ResourceInvestigation({ custom = false }: { custom?: boolean }) {
  const { kind: builtinKind = '', definition = '', namespace = '_', name = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const ns = namespace === '_' ? '' : namespace;
  const version = params.get('version') ?? '';
  const data = useAPI<Detail>(
    custom
      ? `/api/v1/custom-resources/${encodeURIComponent(definition)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}?version=${encodeURIComponent(version)}`
      : `/api/v1/resources/${encodeURIComponent(builtinKind)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
  );
  const kind = custom ? (data.data?.resource.kind ?? 'Custom resource') : builtinKind;
  const requestedTab = params.get('tab') ?? 'summary';
  const validTab =
    ['summary', 'manifest', 'events'].includes(requestedTab) ||
    (!custom &&
      (requestedTab === 'incident' ||
        (['Pod', 'PersistentVolumeClaim', 'Namespace'].includes(builtinKind) &&
          requestedTab === 'termination') ||
        (['Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(
          builtinKind,
        ) &&
          ['efficiency', 'configuration'].includes(requestedTab)) ||
        (['Service', 'Ingress', 'HTTPRoute', 'Gateway'].includes(builtinKind) &&
          requestedTab === 'network') ||
        (['Job', 'CronJob'].includes(builtinKind) && requestedTab === 'batch') ||
        (builtinKind === 'Pod' && requestedTab === 'scheduling') ||
        (builtinKind === 'Node' && requestedTab === 'maintenance') ||
        (['Deployment', 'StatefulSet', 'DaemonSet'].includes(builtinKind) &&
          requestedTab === 'rollout') ||
        ([
          'Pod',
          'Deployment',
          'StatefulSet',
          'DaemonSet',
          'ReplicaSet',
          'Job',
          'PersistentVolumeClaim',
          'PersistentVolume',
        ].includes(builtinKind) &&
          requestedTab === 'storage') ||
        (['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(builtinKind) &&
          requestedTab === 'logs') ||
        (builtinKind === 'Pod' && requestedTab === 'logs') ||
        (builtinKind === 'ServiceAccount' && requestedTab === 'grants')));
  const tab = validTab ? requestedTab : 'summary';
  useEffect(() => {
    if (validTab) return;
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete('tab');
        return next;
      },
      { replace: true },
    );
  }, [validTab, setParams]);
  const cluster = useAPI<Cluster>('/api/v1/cluster');
  const [container, setContainer] = useState('');
  const [previous, setPrevious] = useState(false);
  const [tail, setTail] = useState('100');
  const [since, setSince] = useState('');
  const [follow, setFollow] = useState(false);
  const [logSearch, setLogSearch] = useState('');

  const logs = useQuery({
    queryKey: ['logs', ns, name, container, previous, tail, since],
    enabled: tab === 'logs' && kind === 'Pod' && !custom,
    queryFn: ({ signal }) =>
      request<{ logs: string }>(
        `/api/v1/pods/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/logs?container=${encodeURIComponent(container)}&tail=${tail}&previous=${previous}&sinceTime=${encodeURIComponent(since ? new Date(since).toISOString() : '')}`,
        { signal },
      ),
    retry: false,
    refetchInterval: follow && tab === 'logs' && !previous ? 2000 : false,
  });
  if (data.isPending) return <Loading />;
  if (data.error) return <ErrorState error={data.error} retry={() => void data.refetch()} />;
  const d = data.data;
  const backURL = custom
    ? `/custom-resources?${new URLSearchParams({ definition, version, ...(ns ? { namespace: ns } : {}) })}`
    : `/resources?kind=${kind}${ns ? `&namespace=${encodeURIComponent(ns)}` : ''}`;
  const metadata = obj(d.manifest.metadata);
  const spec = obj(d.manifest.spec);
  const status = obj(d.manifest.status);
  const conditions = Array.isArray(status.conditions) ? status.conditions : [];
  const refs = Array.isArray(metadata.ownerReferences) ? metadata.ownerReferences : [];
  const containers = Array.isArray(spec.containers) ? spec.containers : [];
  const logContainers = ['containers', 'initContainers', 'ephemeralContainers'].flatMap((field) =>
    Array.isArray(spec[field]) ? (spec[field] as unknown[]) : [],
  );
  return (
    <>
      <Link className="back-link" to={backURL}>
        <ArrowLeft size={14} />
        Back to {custom ? 'custom resources' : `${kind}s`}
      </Link>
      <PageTitle
        eyebrow={`${ns || 'CLUSTER SCOPED'} / ${kind}`}
        title={name}
        description={
          custom
            ? `${definition} · Metadata and reported conditions`
            : `${kind} resource · Current observed state`
        }
        action={<Status value={d.resource.health} label={d.resource.status} />}
      />
      <Warnings items={d.warnings} />
      <div className="detail-meta">
        <span>
          <Box size={14} />
          {kind}
        </span>
        <span>
          <Layers3 size={14} />
          {ns || 'Cluster scoped'}
        </span>
        <span>
          <Clock3 size={14} />
          Created {ago(d.resource.createdAt)}
        </span>
        <div className="log-controls">
          <FavoriteResource resource={d.resource} />
          {!custom && (
            <Link
              className="button"
              to={`/topology?${new URLSearchParams({ focusKind: kind, focusNS: ns, focusName: name, depth: '1' })}`}
            >
              Explore dependencies
            </Link>
          )}
          {!custom && (
            <Link
              className="button"
              to={`/snapshots?${new URLSearchParams({ kind, resourceNamespace: ns, name })}`}
            >
              Capture / compare snapshots
            </Link>
          )}
          <CopyButton text={`${kind}/${name}`} label="Copy reference" />
          <button
            className="button"
            onClick={() =>
              download(
                'investigation.json',
                JSON.stringify(
                  {
                    schemaVersion: 1,
                    cluster: cluster.data?.name,
                    namespaceScope: cluster.data?.scope,
                    mode: cluster.data?.mode,
                    exportedAt: new Date().toISOString(),
                    detail: d,
                  },
                  null,
                  2,
                ),
                'application/json',
              )
            }
          >
            Export JSON
          </button>
          <button
            className="button"
            onClick={() =>
              download(
                'investigation.md',
                [
                  '# Kubernetes investigation',
                  `Cluster: ${cluster.data?.name ?? 'Unavailable'}`,
                  `Mode: ${cluster.data?.mode ?? 'Unavailable'}`,
                  '',
                  `Resource: ${kind}/${ns}/${name}`,
                  `Exported: ${new Date().toISOString()}`,
                  '',
                  d.history,
                  '',
                  d.redaction,
                  '',
                  '## Sanitized resource',
                  '',
                  '```yaml',
                  stringify(d.manifest),
                  '```',
                  '',
                  '## Events',
                  ...d.events.map((event) => JSON.stringify(event)),
                  '',
                  '## Coverage warnings',
                  ...d.warnings.map((warning) => `${warning.kind}: ${warning.message}`),
                ].join('\n'),
                'text/markdown',
              )
            }
          >
            Export Markdown
          </button>
        </div>
      </div>
      <div className="tabs" role="tablist" aria-label="Resource sections">
        {[
          { id: 'summary', name: 'Overview', icon: FileText },
          { id: 'manifest', name: custom ? 'Observed YAML' : 'Manifest', icon: Code2 },
          { id: 'events', name: `Events (${d.events.length})`, icon: Clock3 },
          ...(kind === 'ServiceAccount' && !custom
            ? [{ id: 'grants', name: 'RBAC grants', icon: ShieldCheck }]
            : []),
          ...(!custom && ['Service', 'Ingress', 'HTTPRoute', 'Gateway'].includes(kind)
            ? [{ id: 'network', name: 'Network paths', icon: Layers3 }]
            : []),
          ...(!custom && ['Job', 'CronJob'].includes(kind)
            ? [{ id: 'batch', name: 'Executions', icon: Clock3 }]
            : []),
          ...(!custom &&
          ['Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(kind)
            ? [
                { id: 'efficiency', name: 'Resource efficiency', icon: Layers3 },
                { id: 'configuration', name: 'Configuration dependencies', icon: Braces },
              ]
            : []),
          ...(!custom && ['Pod', 'PersistentVolumeClaim', 'Namespace'].includes(kind)
            ? [{ id: 'termination', name: 'Deletion diagnostics', icon: Clock3 }]
            : []),
          ...(!custom ? [{ id: 'incident', name: 'Incident', icon: Clock3 }] : []),
          ...(!custom && kind === 'Pod'
            ? [{ id: 'scheduling', name: 'Scheduling', icon: Layers3 }]
            : []),
          ...(!custom && kind === 'Node'
            ? [{ id: 'maintenance', name: 'Maintenance', icon: ShieldCheck }]
            : []),
          ...(!custom && ['Deployment', 'StatefulSet', 'DaemonSet'].includes(kind)
            ? [{ id: 'rollout', name: 'Rollout', icon: Layers3 }]
            : []),
          ...(!custom &&
          [
            'Pod',
            'Deployment',
            'StatefulSet',
            'DaemonSet',
            'ReplicaSet',
            'Job',
            'PersistentVolumeClaim',
            'PersistentVolume',
          ].includes(kind)
            ? [{ id: 'storage', name: 'Storage', icon: Layers3 }]
            : []),
          ...(['Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(
            kind,
          ) && !custom
            ? [
                {
                  id: 'logs',
                  name: kind === 'Pod' ? 'Container logs' : 'Workload logs',
                  icon: Terminal,
                },
              ]
            : []),
        ].map((t) => (
          <button
            role="tab"
            aria-selected={tab === t.id}
            tabIndex={tab === t.id ? 0 : -1}
            onKeyDown={(e) => {
              if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
                const buttons = Array.from(
                  e.currentTarget.parentElement!.querySelectorAll<HTMLButtonElement>('[role=tab]'),
                );
                const i = buttons.indexOf(e.currentTarget);
                const next =
                  buttons[
                    (i + (e.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length
                  ];
                next.click();
                next.focus();
              }
            }}
            key={t.id}
            onClick={() => setParams({ ...Object.fromEntries(params), tab: t.id })}
          >
            <t.icon size={14} />
            {t.name}
          </button>
        ))}
      </div>
      <div role="tabpanel">
        {!custom && tab === 'termination' && (
          <TerminationDetails kind={kind} namespace={ns} name={name} />
        )}
        {!custom && tab === 'efficiency' && (
          <EfficiencyDetails kind={kind} namespace={ns} name={name} />
        )}
        {!custom && tab === 'configuration' && (
          <ConfigurationDetails kind={kind} namespace={ns} name={name} />
        )}
        {!custom && tab === 'network' && <NetworkDetails kind={kind} namespace={ns} name={name} />}
        {!custom && tab === 'batch' && <BatchDetails kind={kind} namespace={ns} name={name} />}
        {!custom && tab === 'scheduling' && <SchedulingDetails namespace={ns} name={name} />}
        {!custom && tab === 'rollout' && <RolloutDetails kind={kind} namespace={ns} name={name} />}
        {!custom && tab === 'maintenance' && (
          <NodeMaintenanceDetails name={name} namespace={params.get('namespace') ?? ''} />
        )}
        {tab === 'incident' && !custom && (
          <IncidentInvestigation kind={kind} namespace={ns} name={name} />
        )}
        {tab === 'storage' && !custom && <StorageDetails kind={kind} namespace={ns} name={name} />}
        {tab === 'logs' && kind !== 'Pod' && !custom && (
          <WorkloadLogInvestigation kind={kind} namespace={ns} name={name} />
        )}
        {tab === 'grants' && kind === 'ServiceAccount' && !custom && (
          <ServiceAccountGrants namespace={ns} name={name} />
        )}
        {tab === 'summary' && !!d.related?.length && (
          <Panel title="Scaling, availability, quotas and endpoints">
            {d.related.map((r) => (
              <div key={r.id}>
                <Link
                  to={`/resources/${r.kind}/${encodeURIComponent(r.namespace || '_')}/${encodeURIComponent(r.name)}`}
                >
                  {r.kind} · {r.name}
                </Link>
                <span> · {r.status}</span>
              </div>
            ))}
          </Panel>
        )}
        {tab === 'summary' &&
          [
            'HorizontalPodAutoscaler',
            'PodDisruptionBudget',
            'ResourceQuota',
            'LimitRange',
            'EndpointSlice',
          ].includes(kind) && (
            <Panel title="Operational configuration and observed status">
              <pre className="code-block">
                {stringify({
                  spec: d.manifest.spec,
                  status: d.manifest.status,
                  addressType: d.manifest.addressType,
                  ports: d.manifest.ports,
                  endpoints: d.manifest.endpoints,
                })}
              </pre>
            </Panel>
          )}
        {tab === 'summary' && (
          <div className="detail-grid">
            <Panel title="Resource information">
              <dl className="property-list">
                <dt>Name</dt>
                <dd className="mono">{name}</dd>
                <dt>Namespace</dt>
                <dd>{ns || 'Cluster scoped'}</dd>
                {kind === 'Pod' && !custom && (
                  <>
                    <dt>ServiceAccount</dt>
                    <dd>
                      <Link
                        to={`/resources/ServiceAccount/${encodeURIComponent(ns)}/${encodeURIComponent(String(obj(d.manifest.spec).serviceAccountName || 'default'))}?tab=grants`}
                      >
                        {String(obj(d.manifest.spec).serviceAccountName || 'default')} · RBAC grants
                      </Link>
                    </dd>
                  </>
                )}
                <dt>API version</dt>
                <dd>{String(d.manifest.apiVersion ?? 'Unknown')}</dd>
                <dt>Status</dt>
                <dd>
                  <Status value={d.resource.health} label={d.resource.status} />
                </dd>
                <dt>Ready</dt>
                <dd>{d.resource.ready || 'Not reported'}</dd>
                <dt>UID</dt>
                <dd className="mono wrap">{String(metadata.uid ?? 'Not reported')}</dd>
                <dt>Created</dt>
                <dd>{d.resource.createdAt || 'Not reported'}</dd>
              </dl>
              {refs.length > 0 && (
                <div className="detail-section">
                  <h3>Owned by</h3>
                  {refs.map((ref, i) => {
                    const r = obj(ref);
                    return (
                      <Link
                        className="related-link"
                        key={i}
                        to={`/resources/${String(r.kind)}/${namespace}/${String(r.name)}`}
                      >
                        <Box size={14} />
                        {String(r.kind)}/{String(r.name)}
                        <ArrowRight size={14} />
                      </Link>
                    );
                  })}
                </div>
              )}
            </Panel>
            <Panel title="Labels & conditions">
              <div className="label-list">
                {Object.entries(d.resource.labels ?? {}).map(([k, v]) => (
                  <div key={k}>
                    <code>{k}</code>
                    <span>{String(v)}</span>
                  </div>
                ))}
                {!Object.keys(d.resource.labels ?? {}).length && (
                  <p className="muted">No labels reported.</p>
                )}
              </div>
              <div className="detail-section">
                <h3>Reported conditions</h3>
                {conditions.length ? (
                  conditions.map((c, i) => {
                    const condition = obj(c);
                    return (
                      <div key={i} className="condition-row">
                        <strong>{String(condition.type)}</strong>
                        <span className="mono">{String(condition.status)}</span>
                        <small>{String(condition.message ?? condition.reason ?? '')}</small>
                      </div>
                    );
                  })
                ) : (
                  <p className="muted">No conditions reported for this resource.</p>
                )}
              </div>
            </Panel>
            {containers.length > 0 && (
              <Panel title="Containers" className="span-full">
                <TableScroll>
                  <table>
                    <thead>
                      <tr>
                        <th>Name</th>
                        <th>Image</th>
                        <th>Requests</th>
                        <th>Limits</th>
                      </tr>
                    </thead>
                    <tbody>
                      {containers.map((v, i) => {
                        const c = obj(v),
                          resources = obj(c.resources);
                        return (
                          <tr key={i}>
                            <td>{String(c.name)}</td>
                            <td>
                              <code>{String(c.image)}</code>
                            </td>
                            <td>
                              <code>{JSON.stringify(resources.requests ?? 'Not configured')}</code>
                            </td>
                            <td>
                              <code>{JSON.stringify(resources.limits ?? 'Not configured')}</code>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </TableScroll>
              </Panel>
            )}
          </div>
        )}
        {tab === 'manifest' && (
          <Panel
            title={custom ? 'Safe resource projection' : 'Sanitized manifest'}
            action={<CopyButton text={stringify(d.manifest)} label="Copy YAML" />}
          >
            <div className="manifest-notice">
              <Braces size={15} />
              {d.redaction}
            </div>
            <pre className="code-block" aria-label="Resource YAML">
              <code>{stringify(d.manifest)}</code>
            </pre>
          </Panel>
        )}
        {tab === 'events' && (
          <Panel title="Related events">
            {d.events.length ? (
              d.events.map((e) => (
                <EventCard
                  key={e.id}
                  event={e}
                  resourceHref={
                    custom ? `?${new URLSearchParams({ version, tab: 'summary' })}` : undefined
                  }
                />
              ))
            ) : (
              <Empty
                title="No related events retained"
                description="The Kubernetes API does not retain an unlimited event history."
              />
            )}
          </Panel>
        )}
        {tab === 'logs' && kind === 'Pod' && !custom && (
          <Panel
            title="Container logs"
            subtitle="Selected tail, bounded to 128 KiB. Live refresh retrieves the current tail every two seconds. Application output may contain sensitive information."
            action={
              <div className="log-controls">
                <select
                  aria-label="Container"
                  value={container}
                  onChange={(e) => setContainer(e.target.value)}
                >
                  <option value="">Default container</option>
                  {logContainers.map((c, i) => (
                    <option key={i} value={String(obj(c).name)}>
                      {String(obj(c).name)}
                    </option>
                  ))}
                </select>
                <label>
                  <input
                    type="checkbox"
                    checked={previous}
                    onChange={(e) => setPrevious(e.target.checked)}
                  />{' '}
                  Previous instance
                </label>
                <select
                  aria-label="Log lines"
                  value={tail}
                  onChange={(e) => setTail(e.target.value)}
                >
                  {['100', '500', '1000'].map((n) => (
                    <option key={n} value={n}>
                      {n} lines
                    </option>
                  ))}
                </select>
                <label>
                  Since{' '}
                  <input
                    aria-label="Logs since"
                    type="datetime-local"
                    value={since}
                    onChange={(e) => setSince(e.target.value)}
                  />
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={follow}
                    disabled={previous}
                    onChange={(e) => setFollow(e.target.checked)}
                  />{' '}
                  Live refresh
                </label>
                <input
                  aria-label="Search log lines"
                  placeholder="Filter retrieved lines"
                  value={logSearch}
                  onChange={(e) => setLogSearch(e.target.value)}
                />
                {logs.data && (
                  <button
                    className="button"
                    onClick={() => download('container.log', logs.data.logs, 'text/plain')}
                  >
                    Download logs
                  </button>
                )}
                <button className="button" onClick={() => void logs.refetch()}>
                  Reload
                </button>
                {logs.data && <CopyButton text={logs.data.logs} />}
              </div>
            }
          >
            {logs.data && new TextEncoder().encode(logs.data.logs).length >= 131072 && (
              <p className="footnote">Log output reached the 128 KiB limit and may be truncated.</p>
            )}
            {logs.dataUpdatedAt > 0 && (
              <p className="footnote">
                Last successful log retrieval: {new Date(logs.dataUpdatedAt).toLocaleString()}
              </p>
            )}
            {logs.isPending ? (
              <Loading />
            ) : logs.error ? (
              <ErrorState error={logs.error} />
            ) : (
              <pre className="code-block logs">
                {logs.data.logs
                  .split('\n')
                  .filter((line) => line.toLowerCase().includes(logSearch.toLowerCase()))
                  .join('\n') || 'No matching logs returned.'}
              </pre>
            )}
          </Panel>
        )}
      </div>
      <p className="footnote">{d.history}</p>
    </>
  );
}
