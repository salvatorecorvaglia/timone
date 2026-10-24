import { ScopedLink as Link } from '../components/navigation';
import { EffectiveAuthorization } from './EffectiveAuthorization';
import { useSearchParams } from 'react-router-dom';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useAPI, resourceURL } from '../lib/api';
import type { GrantReport } from '../types';
import {
  TableScroll,
  CopyButton,
  Empty,
  ErrorState,
  Loading,
  Panel,
  ResourceLink,
  Warnings,
} from '../components/ui';

export function ServiceAccountGrants({ namespace, name }: { namespace: string; name: string }) {
  const [params, setParams] = useSearchParams();
  const offset = Number(params.get('grantOffset') ?? '0');
  const data = useAPI<GrantReport>(
    `/api/v1/serviceaccounts/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/grants?limit=25&offset=${offset}`,
  );
  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next);
  };
  if (data.isPending) return <Loading />;
  if (data.error) return <ErrorState error={data.error} retry={() => void data.refetch()} />;
  const report = data.data;
  const selected = report.items.find((g) => g.binding.id === params.get('grantBinding'));
  return (
    <>
      <EffectiveAuthorization
        key={`${namespace}/${name}`}
        namespace={namespace}
        serviceAccount={name}
      />
      <Warnings items={report.warnings} />
      <Panel
        title="Observed RBAC grants"
        action={
          <span className="tag">
            {report.mode === 'demo' ? 'Simulated RBAC' : 'Read-only inventory'}
          </span>
        }
      >
        <div className="grant-identity">
          <code>{report.identity}</code>
          <CopyButton text={report.identity} label="Copy identity" />
          <p>
            {report.complete
              ? 'Inventory complete for this page and workspace'
              : 'Inventory incomplete'}{' '}
            · Workspace: {report.scope || 'All readable namespaces'} · {report.total} observed
            matching bindings
          </p>
          <div className="check-chips">
            {report.groups.map((g) => (
              <span key={g}>{g}</span>
            ))}
          </div>
          <p className="muted">
            Binding declarations and role rules explain potential RBAC grants. They do not establish
            a live authorization decision. Missing entries do not prove denial.
          </p>
        </div>
      </Panel>
      <Panel
        title="Matching bindings"
        action={<span className="count-label">{report.items.length} on this page</span>}
      >
        <TableScroll>
          <table aria-label="ServiceAccount RBAC bindings">
            <thead>
              <tr>
                <th>Binding / role</th>
                <th>Binding scope</th>
                <th>Role resolution</th>
                <th>Inspect</th>
              </tr>
            </thead>
            <tbody>
              {report.items.map((g) => (
                <tr key={g.binding.id}>
                  <td>
                    <ResourceLink resource={g.binding} />
                    <small className="grant-role-ref">
                      {g.reference.kind}/{g.reference.name}
                    </small>
                  </td>
                  <td>{g.scope === 'cluster' ? 'Cluster / all namespaces' : g.namespace}</td>
                  <td>
                    {g.resolution}
                    {g.rulesTruncated ? ' · output incomplete' : ''}
                  </td>
                  <td>
                    <button
                      className="button"
                      aria-label={`Inspect RBAC binding ${g.binding.name}`}
                      aria-pressed={selected?.binding.id === g.binding.id}
                      onClick={() => update('grantBinding', g.binding.id)}
                    >
                      Rules & provenance
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableScroll>
        {!report.items.length && (
          <Empty
            title={
              report.complete
                ? 'No matching bindings in the observed scope'
                : 'No matches in readable binding collections'
            }
            description="This inventory cannot establish that the ServiceAccount has no permissions."
          />
        )}
        <div className="search-pagination">
          <button
            className="button"
            aria-label="Previous RBAC bindings"
            disabled={offset <= 0 || data.isFetching}
            onClick={() => {
              const n = new URLSearchParams(params);
              n.set('grantOffset', String(Math.max(0, offset - 25)));
              n.delete('grantBinding');
              setParams(n);
            }}
          >
            <ChevronLeft size={14} />
            Previous
          </button>
          <span>
            {report.items.length
              ? `${offset + 1}–${offset + report.items.length} of ${report.total} observed`
              : '0 observed matches'}
          </span>
          <button
            className="button"
            aria-label="Next RBAC bindings"
            disabled={
              offset + report.items.length >= report.total || offset >= 5000 || data.isFetching
            }
            onClick={() => {
              const n = new URLSearchParams(params);
              n.set('grantOffset', String(offset + 25));
              n.delete('grantBinding');
              setParams(n);
            }}
          >
            Next
            <ChevronRight size={14} />
          </button>
        </div>
        {offset >= 5000 && offset + report.items.length < report.total && (
          <p className="data-warning">Binding pagination is limited to offset 5,000.</p>
        )}
      </Panel>
      {selected && (
        <Panel
          title={`Binding: ${selected.binding.name}`}
          action={
            <button className="button" onClick={() => update('grantBinding', '')}>
              Close binding
            </button>
          }
        >
          <div className="grant-provenance">
            <h3>Matched subjects</h3>
            <ul>
              {selected.subjects.map((s) => (
                <li key={s}>
                  <code>{s}</code>
                </li>
              ))}
            </ul>
            <p>
              Role:{' '}
              {selected.role ? (
                <Link to={resourceURL(selected.role)}>
                  {selected.reference.kind}/{selected.reference.name}
                </Link>
              ) : (
                <span>
                  {selected.reference.kind}/{selected.reference.name} · unresolved
                </span>
              )}
            </p>
            {selected.notes.map((note, i) => (
              <p className="muted" key={i}>
                {note}
              </p>
            ))}
          </div>
          {!!selected.rules.length && (
            <TableScroll>
              <table aria-label="Observed RBAC policy rules">
                <thead>
                  <tr>
                    <th>API groups</th>
                    <th>Resources / URLs</th>
                    <th>Verbs</th>
                    <th>Resource names</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.rules.map((r, i) => (
                    <tr key={i}>
                      <td>
                        {r.apiGroups.length
                          ? r.apiGroups.map((g) => g || '(core)').join(', ')
                          : 'Non-resource request'}
                      </td>
                      <td className="grant-rule-values">
                        {[...r.resources, ...r.nonResourceURLs].join(', ')}
                        {!!r.nonResourceURLs.length && selected.scope === 'namespace' && (
                          <small>Non-resource URLs do not apply through a RoleBinding.</small>
                        )}
                      </td>
                      <td>{r.verbs.join(', ')}</td>
                      <td className="grant-rule-values">
                        {r.nonResourceURLs.length
                          ? 'Not applicable'
                          : r.resourceNames.length
                            ? r.resourceNames.join(', ')
                            : 'No name restriction in this rule'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableScroll>
          )}
          {!selected.rules.length && (
            <Empty
              title={
                selected.resolution === 'observed' && !selected.rulesTruncated
                  ? 'No rules in the observed role'
                  : 'Rules unavailable'
              }
              description={
                selected.resolution === 'observed' && !selected.rulesTruncated
                  ? 'The current role declares no rules; other bindings and authorizers can still contribute access.'
                  : 'No empty permission set is inferred from missing or invalid role evidence.'
              }
            />
          )}
        </Panel>
      )}
      <Panel title="Coverage and assessment limits" className="coverage-panel">
        <div>
          <h3>Collections read</h3>
          <ul>
            {report.coverage.map((c) => (
              <li key={c.kind}>
                {c.kind}: {c.state}
                {c.observed !== undefined ? ` · ${c.observed} observed` : ''}
              </li>
            ))}
          </ul>
        </div>
        <div>
          <h3>Interpretation</h3>
          <ul>
            {report.limitations.map((text, i) => (
              <li key={i}>{text}</li>
            ))}
          </ul>
        </div>
      </Panel>
      <p className="footnote">
        Observed {report.observedAt}. Active views refresh every 30 seconds.
      </p>
    </>
  );
}
