import { ScopedLink as Link } from '../components/navigation';
import { GitOpsDrift } from './GitOpsDrift';

import {
  Activity,
  ArrowUpRight,
  GitBranch,
  Info,
  Network,
  Plug,
  Search,
  Terminal,
} from 'lucide-react';
import { useAPI } from '../lib/api';
import type { Integration, IntegrationRecord } from '../types';
import {
  TableScroll,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Status,
  Warnings,
} from '../components/ui';
import { useFilters } from '../lib/filters';

export function Integrations({ namespace, filter }: { namespace: string; filter?: string }) {
  const data = useAPI<{ items: Integration[]; mode: string }>(
    `/api/v1/integrations?namespace=${encodeURIComponent(namespace)}`,
  );
  const { search, update } = useFilters();
  const title = filter ?? 'Integrations';
  const items =
    data.data?.items
      .filter(
        (i) =>
          !filter ||
          (filter === 'GitOps' ? ['Argo CD', 'Flux'].includes(i.name) : i.name === filter),
      )
      .map((i) => ({
        ...i,
        records: i.records.filter((r) =>
          `${r.name} ${r.resource.namespace} ${r.status} ${r.revision ?? ''}`
            .toLowerCase()
            .includes(search.toLowerCase()),
        ),
      })) ?? [];
  return (
    <>
      <PageTitle
        eyebrow="CONNECTED OPERATIONS"
        title={title}
        description={
          filter === 'Helm'
            ? 'Discover Helm release metadata and revisions visible to this connection.'
            : filter === 'GitOps'
              ? 'Inspect synchronization and health directly from Argo CD and Flux custom resources.'
              : 'Understand which cluster integrations are available and what each one can provide.'
        }
        action={
          <span className="tag">
            <Plug size={13} />
            Read-only inspection
          </span>
        }
      />
      <div className="filterbar">
        <div className="search-field">
          <Search size={15} />
          <input
            aria-label="Search integration resources"
            placeholder="Search release, application, status or revision…"
            value={search}
            maxLength={200}
            onChange={(e) => update('search', e.target.value)}
          />
        </div>
      </div>
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} />
      ) : (
        <div className="integration-grid">
          {items.map((i) => (
            <Panel
              key={i.name}
              title={i.name}
              action={
                <Status
                  value={
                    i.status === 'available'
                      ? 'healthy'
                      : i.status === 'no resources'
                        ? 'unknown'
                        : 'warning'
                  }
                  label={i.status}
                />
              }
            >
              <div className="integration-description">
                <span className="integration-logo">
                  {i.name === 'Argo CD' ? (
                    <GitBranch size={27} />
                  ) : i.name === 'Flux' ? (
                    <Network size={27} />
                  ) : i.name === 'Helm' ? (
                    <CompassIcon />
                  ) : (
                    <Activity size={27} />
                  )}
                </span>
                <p>{i.message}</p>
              </div>
              <Warnings items={i.warnings} />
              {i.detected !== undefined && (
                <p className="integration-note">
                  Prometheus service discovery:{' '}
                  {i.detected
                    ? 'a matching readable Service was found'
                    : 'no matching readable Service found; absence is not conclusive'}
                  .
                </p>
              )}
              {i.records.length ? (
                <TableScroll>
                  <table>
                    <thead>
                      <tr>
                        <th>Resource</th>
                        <th>Status</th>
                        <th>Revision</th>
                        <th>Reported health / chart</th>
                      </tr>
                    </thead>
                    <tbody>
                      {i.records.map((r) => (
                        <tr key={r.resource.id}>
                          <td>
                            {i.name === 'Helm' && i.detailsEnabled ? (
                              <Link
                                className="resource-link"
                                to={`/helm/${encodeURIComponent(r.resource.namespace)}/${encodeURIComponent(r.name)}`}
                              >
                                {r.name}
                                <ArrowUpRight size={13} />
                              </Link>
                            ) : (
                              <ResourceLink resource={r.resource} />
                            )}
                            <small className="table-subtitle">
                              {r.resource.namespace} · {r.resource.kind}
                              {r.retainedRevisions
                                ? ` · ${r.retainedRevisions} stored revisions`
                                : ''}
                            </small>
                            {i.name !== 'Helm' && (
                              <>
                                <IntegrationEvidence record={r} />
                                <GitOpsDrift resource={r.resource} />
                              </>
                            )}
                          </td>
                          <td>
                            <span className="mono">{r.status || 'Unknown'}</span>
                          </td>
                          <td>
                            <code>{r.revision || 'Not reported'}</code>
                          </td>
                          <td>{r.health || r.chart || 'Not reported'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </TableScroll>
              ) : (
                <div className="integration-empty">
                  <Info size={16} />
                  {search
                    ? 'No integration records match the search.'
                    : i.status === 'partial'
                      ? 'Readable controller collections contain no records; unavailable collections are listed above.'
                      : i.status === 'no resources'
                        ? 'API access succeeded; no matching resources were found.'
                        : 'This integration is unavailable. See prerequisites above.'}
                </div>
              )}
              {i.truncated && (
                <p className="data-warning">Collection truncated; select a namespace.</p>
              )}
              <div className="panel-footnote">
                {data.data.mode === 'demo'
                  ? 'Simulated integration data'
                  : 'Reported Kubernetes state'}
                <span>State-changing operations unavailable</span>
              </div>
            </Panel>
          ))}
        </div>
      )}
      {filter === 'Helm' && (
        <p className="footnote">
          History requires TIMONE_HELM_DETAILS=true and Secret list permission. Comparisons cover
          chart metadata, sanitized values and desired manifest fields. Raw values, raw manifests,
          hooks and write operations are unavailable.
        </p>
      )}
    </>
  );
}

function IntegrationEvidence({ record: r }: { record: IntegrationRecord }) {
  return (
    <details className="integration-evidence">
      <summary>Inspect reported state</summary>
      <dl>
        <dt>Observation</dt>
        <dd>
          {r.observation === 'current'
            ? 'Observed generation matches'
            : r.observation === 'stale'
              ? 'Generation behind desired state'
              : 'Freshness unreported'}
        </dd>
        <dt>Drift</dt>
        <dd>{r.drift || 'Not assessed'}</dd>
        {r.attemptedRevision && (
          <>
            <dt>Attempted revision</dt>
            <dd>
              <code>{r.attemptedRevision}</code>
            </dd>
          </>
        )}
        {r.operation && (
          <>
            <dt>Last operation</dt>
            <dd>{r.operation}</dd>
          </>
        )}
        {r.observedAt && (
          <>
            <dt>Controller reported</dt>
            <dd>{new Date(r.observedAt).toLocaleString()}</dd>
          </>
        )}
      </dl>
      {r.conditions.map((c, index) => (
        <div className="integration-condition" key={`${c.type}:${index}`}>
          <strong>
            {c.type}
            {c.status && `=${c.status}`}{' '}
            {c.stale && <span className="warning">· previous generation</span>}
          </strong>
          <code>{c.reason}</code>
          <p>{c.message || 'No condition message reported.'}</p>
        </div>
      ))}
      {r.evidence.length > 0 && (
        <>
          <h3>Observed evidence</h3>
          <ul>
            {r.evidence.map((e) => (
              <li key={e}>{e}</li>
            ))}
          </ul>
        </>
      )}
      {r.recommendations.length > 0 && (
        <>
          <h3>Investigation steps</h3>
          <ul>
            {r.recommendations.map((e) => (
              <li key={e}>{e}</li>
            ))}
          </ul>
        </>
      )}
    </details>
  );
}

function CompassIcon() {
  return <Terminal size={27} />;
}
