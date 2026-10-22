import { ScopedLink as Link } from '../components/navigation';
import { useState } from 'react';
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
import { ServiceAccountGrants } from './ServiceAccountGrants';
import { EventCard } from './EventCard';
import { ago, request, useAPI } from '../lib/api';
import type { Detail } from '../types';
import {
  CopyButton,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  Status,
  Warnings,
} from '../components/ui';

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
  const tab = params.get('tab') ?? 'summary';
  const [container, setContainer] = useState('');
  const logs = useQuery({
    queryKey: ['logs', ns, name, container],
    enabled: tab === 'logs' && kind === 'Pod' && !custom,
    queryFn: ({ signal }) =>
      request<{ logs: string }>(
        `/api/v1/pods/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/logs?container=${encodeURIComponent(container)}&tail=100`,
        { signal },
      ),
    retry: false,
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
        <CopyButton text={`${kind}/${name}`} label="Copy reference" />
      </div>
      <div className="tabs" role="tablist" aria-label="Resource sections">
        {[
          { id: 'summary', name: 'Overview', icon: FileText },
          { id: 'manifest', name: custom ? 'Observed YAML' : 'Manifest', icon: Code2 },
          { id: 'events', name: `Events (${d.events.length})`, icon: Clock3 },
          ...(kind === 'ServiceAccount' && !custom
            ? [{ id: 'grants', name: 'RBAC grants', icon: ShieldCheck }]
            : []),
          ...(kind === 'Pod' && !custom
            ? [{ id: 'logs', name: 'Container logs', icon: Terminal }]
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
        {tab === 'grants' && kind === 'ServiceAccount' && !custom && (
          <ServiceAccountGrants namespace={ns} name={name} />
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
                <div className="table-scroll">
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
                </div>
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
            subtitle="Last 100 lines, bounded to 128 KiB. Application output may contain sensitive information."
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
                <button className="button" onClick={() => void logs.refetch()}>
                  Reload
                </button>
                {logs.data && <CopyButton text={logs.data.logs} />}
              </div>
            }
          >
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
              <pre className="code-block logs">{logs.data.logs || 'No logs returned.'}</pre>
            )}
          </Panel>
        )}
      </div>
      <p className="footnote">{d.history}</p>
    </>
  );
}
