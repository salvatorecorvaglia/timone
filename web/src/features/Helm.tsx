import { ScopedLink as Link } from '../components/navigation';
import { useParams, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, History, ShieldCheck } from 'lucide-react';
import { ago, request, useAPI } from '../lib/api';
import type { HelmComparison, HelmHistory, HelmResource } from '../types';
import {
  TableScroll,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  Warnings,
} from '../components/ui';

function Inventory({ items, label }: { items: HelmResource[]; label: string }) {
  return (
    <div className="helm-inventory">
      <h3>
        {label} <span className="count-label">{items.length}</span>
      </h3>
      {items.length ? (
        <ul>
          {items.map((r) => (
            <li key={`${r.apiVersion}:${r.kind}:${r.namespace}:${r.name}`}>
              <code>{r.kind}</code>
              <strong>{r.name}</strong>
              <small>
                {r.namespace || 'Namespace unspecified'} · {r.apiVersion}
              </small>
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">No resource identities {label.toLowerCase()}.</p>
      )}
    </div>
  );
}
export function HelmRelease() {
  const { namespace = '', name = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const base = `/api/v1/helm/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;
  const history = useAPI<HelmHistory>(base);
  const from = params.get('from') ?? String(history.data?.revisions[1]?.revision ?? '');
  const to = params.get('to') ?? String(history.data?.revisions[0]?.revision ?? '');
  const comparison = useQuery({
    queryKey: [`${base}/compare`, from, to],
    queryFn: ({ signal }) =>
      request<HelmComparison>(`${base}/compare?${new URLSearchParams({ from, to })}`, { signal }),
    enabled: !!history.data && !!from && !!to && from !== to,
    refetchInterval: 30000,
    retry: false,
  });
  const select = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    next.set('from', from);
    next.set('to', to);
    next.set(key, value);
    setParams(next, { replace: true });
  };
  return (
    <>
      <Link className="back-link" to={`/helm?namespace=${encodeURIComponent(namespace)}`}>
        <ArrowLeft size={14} />
        Back to Helm
      </Link>
      <PageTitle
        eyebrow={`${namespace} / HELM`}
        title={name}
        description="Retained release revisions and restricted comparisons, read directly from Helm storage."
        action={
          <span className="tag">
            <ShieldCheck size={13} />
            Read-only history
          </span>
        }
      />
      {history.isPending ? (
        <Loading />
      ) : history.error ? (
        <ErrorState error={history.error} retry={() => void history.refetch()} />
      ) : (
        <>
          <Warnings items={history.data.warnings} />
          <Panel
            title="Retained revisions"
            subtitle={history.data.retention}
            action={
              <span className="tag">
                <History size={13} />
                {history.data.mode === 'demo' ? 'Simulated' : 'Helm Secret storage'}
              </span>
            }
          >
            <TableScroll>
              <table>
                <thead>
                  <tr>
                    <th>Revision</th>
                    <th>Status</th>
                    <th>Chart</th>
                    <th>Application version</th>
                    <th>Deployed</th>
                    <th>Resource identities</th>
                  </tr>
                </thead>
                <tbody>
                  {history.data.revisions.map((r) => (
                    <tr key={r.revision}>
                      <td className="mono">{r.revision}</td>
                      <td>{r.status}</td>
                      <td>
                        <strong>{r.chart || 'Unavailable'}</strong>
                        <small className="table-subtitle">
                          {r.chartVersion || 'Version unavailable'}
                        </small>
                      </td>
                      <td className="mono">{r.appVersion || 'Unavailable'}</td>
                      <td>
                        <time title={r.deployedAt}>
                          {r.deployedAt ? ago(r.deployedAt) : 'Unavailable'}
                        </time>
                      </td>
                      <td>{r.inventoryAvailable ? r.inventory.length : 'Unavailable'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!history.data.revisions.length && (
                <Empty
                  title="No revisions could be decoded"
                  description="Review the storage warnings above. Unreadable or expired revisions are not reconstructed."
                />
              )}
            </TableScroll>
          </Panel>
          {history.data.revisions.length >= 2 && (
            <>
              <Panel
                title="Compare revisions"
                subtitle="Compare retained metadata, resource identities, sanitized manifest fields and sanitized stored values. Secret payloads stay redacted."
              >
                <div className="filterbar revision-controls">
                  <label>
                    From revision{' '}
                    <select
                      aria-label="From Helm revision"
                      value={from}
                      onChange={(e) => select('from', e.target.value)}
                    >
                      {history.data.revisions.map((r) => (
                        <option key={r.revision} value={r.revision}>
                          {r.revision} · {r.status}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    To revision{' '}
                    <select
                      aria-label="To Helm revision"
                      value={to}
                      onChange={(e) => select('to', e.target.value)}
                    >
                      {history.data.revisions.map((r) => (
                        <option key={r.revision} value={r.revision}>
                          {r.revision} · {r.status}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                {from === to ? (
                  <Empty title="Choose two different revisions" />
                ) : comparison.isPending ? (
                  <Loading />
                ) : comparison.error ? (
                  <ErrorState error={comparison.error} />
                ) : (
                  comparison.data && (
                    <>
                      <TableScroll>
                        <table aria-label="Revision metadata comparison">
                          <thead>
                            <tr>
                              <th>Field</th>
                              <th>Revision {from}</th>
                              <th>Revision {to}</th>
                            </tr>
                          </thead>
                          <tbody>
                            {(['chart', 'chartVersion', 'appVersion', 'status'] as const).map(
                              (field) => (
                                <tr key={field}>
                                  <td>
                                    {
                                      {
                                        chart: 'Chart',
                                        chartVersion: 'Chart version',
                                        appVersion: 'Application version',
                                        status: 'Reported status',
                                      }[field]
                                    }
                                  </td>
                                  <td className="mono">
                                    {comparison.data.from[field] || 'Unavailable'}
                                  </td>
                                  <td className="mono">
                                    {comparison.data.to[field] || 'Unavailable'}
                                  </td>
                                </tr>
                              ),
                            )}
                          </tbody>
                        </table>
                      </TableScroll>
                      {comparison.data.inventoryComparable ? (
                        <div className="helm-inventory-grid">
                          <Inventory items={comparison.data.added} label="Added" />
                          <Inventory items={comparison.data.removed} label="Removed" />
                        </div>
                      ) : (
                        <Empty
                          title="Resource inventory comparison unavailable"
                          description="At least one revision lacks a complete, unambiguous inventory."
                        />
                      )}
                      <h3>Sanitized Helm values differences</h3>
                      {comparison.data.valuesComparable ? (
                        <>
                          <TableScroll>
                            <table aria-label="Helm values differences">
                              <thead>
                                <tr>
                                  <th>JSON pointer</th>
                                  <th>Change</th>
                                  <th>Revision {from}</th>
                                  <th>Revision {to}</th>
                                </tr>
                              </thead>
                              <tbody>
                                {comparison.data.valuesChanges.map((c, i) => (
                                  <tr key={i}>
                                    <td className="mono">{c.path || '/'}</td>
                                    <td>{c.change}</td>
                                    <td>
                                      <pre>{JSON.stringify(c.before, null, 2)}</pre>
                                    </td>
                                    <td>
                                      <pre>{JSON.stringify(c.after, null, 2)}</pre>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </TableScroll>
                          {!comparison.data.valuesChanges.length && (
                            <p>No visible values differences; redacted values may differ.</p>
                          )}
                          {comparison.data.valuesTruncated && (
                            <p className="data-warning">
                              Values differences truncated at 1000 entries.
                            </p>
                          )}
                        </>
                      ) : (
                        <p>Values comparison unavailable for these revisions.</p>
                      )}
                      <p className="footnote">
                        Stored user configuration values, not computed chart defaults. Automatic
                        redaction covers credential fields, environment values, private keys and
                        credential-bearing URLs; unrecognized secrets cannot be guaranteed absent.
                        Real clusters require TIMONE_HELM_DETAILS=true and Secret read permissions.
                      </p>
                      <h3>Sanitized manifest differences</h3>
                      {comparison.data.configurationComparable ? (
                        <>
                          <TableScroll>
                            <table aria-label="Sanitized Helm configuration differences">
                              <thead>
                                <tr>
                                  <th>Resource</th>
                                  <th>JSON pointer</th>
                                  <th>Change</th>
                                  <th>Before</th>
                                  <th>After</th>
                                </tr>
                              </thead>
                              <tbody>
                                {comparison.data.changes.map((c, i) => (
                                  <tr key={i}>
                                    <td>
                                      {c.resource.kind}/{c.resource.name}
                                    </td>
                                    <td className="mono wrap">{c.path || '/'}</td>
                                    <td>{c.change}</td>
                                    <td>
                                      <pre>{JSON.stringify(c.before, null, 2)}</pre>
                                    </td>
                                    <td>
                                      <pre>{JSON.stringify(c.after, null, 2)}</pre>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </TableScroll>
                          {!comparison.data.changes.length && (
                            <p className="muted">
                              No visible differences. Redacted fields are not compared.
                            </p>
                          )}
                          {comparison.data.changesTruncated && (
                            <p className="data-warning">Differences limited to 1000 entries.</p>
                          )}
                        </>
                      ) : (
                        <p className="muted">Sanitized configuration comparison unavailable.</p>
                      )}
                      <p className="panel-footnote">{comparison.data.limitations}</p>
                    </>
                  )
                )}
              </Panel>
            </>
          )}
          {history.data.revisions.length === 1 && (
            <Panel>
              <Empty
                title="One retained revision"
                description="A comparison needs two readable retained revisions. Timone does not reconstruct deleted history."
              />
            </Panel>
          )}
        </>
      )}
    </>
  );
}
