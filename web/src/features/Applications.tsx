import { ScopedLink as Link } from '../components/navigation';
import { useSearchParams } from 'react-router-dom';
import type { ApplicationPage } from '../types';
import { pagination, pageRange } from '../lib/pagination';
import { useAPI } from '../lib/api';
import {
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Status,
  Warnings,
} from '../components/ui';

export function Applications({ namespace }: { namespace: string }) {
  const [params, setParams] = useSearchParams();
  const search = params.get('search') ?? '';
  const status = params.get('status') ?? '';
  const { offset } = pagination(params, 20);
  const data = useAPI<ApplicationPage>(
    `/api/v1/applications?${new URLSearchParams({ namespace, search, status, limit: '20', offset: String(offset) })}`,
  );
  const selected = data.data?.items.find((a) => a.id === params.get('application'));
  const update = (key: string, value: string) =>
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      if (value) next.set(key, value);
      else next.delete(key);
      if (key === 'search' || key === 'status') next.delete('offset');
      return next;
    });
  return (
    <>
      <PageTitle
        eyebrow="APPLICATION OPERATIONS"
        title="Applications"
        description="Workloads, selected Services, referenced configuration and reported delivery state."
      />
      <div className="filterbar">
        <input
          aria-label="Search applications"
          placeholder="Search applications…"
          maxLength={200}
          value={search}
          onChange={(e) => update('search', e.target.value)}
        />
        <select
          aria-label="Application health"
          value={status}
          onChange={(e) => update('status', e.target.value)}
        >
          <option value="">All health states</option>
          {['healthy', 'warning', 'critical', 'unknown'].map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>
      <Warnings items={data.data?.warnings} />
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} retry={() => void data.refetch()} />
      ) : (
        <>
          <Panel
            title="Observed applications"
            subtitle={`${data.data.total} groups in readable inventory`}
          >
            <div className="table-scroll">
              <table aria-label="Applications">
                <thead>
                  <tr>
                    <th>Application</th>
                    <th>Namespace</th>
                    <th>Health</th>
                    <th>Resources</th>
                    <th>Issues</th>
                    <th>Grouping evidence</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.items.map((a) => (
                    <tr key={a.id}>
                      <td>
                        <button className="text-link" onClick={() => update('application', a.id)}>
                          {a.name}
                        </button>
                      </td>
                      <td>{a.namespace}</td>
                      <td>
                        <Status value={a.health} />
                      </td>
                      <td>
                        {a.resources.length}
                        {a.truncated ? ' (partial)' : ''}
                      </td>
                      <td>{a.issues.length}</td>
                      <td>{a.basis}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!data.data.items.length && (
              <Empty
                title="No matching applications"
                description="Change filters or inspect collection warnings. No application health is inferred from missing reads."
              />
            )}
            <div className="pagination">
              <button
                className="button"
                disabled={offset === 0}
                onClick={() => update('offset', String(Math.max(0, offset - 20)))}
              >
                Previous
              </button>
              <span>
                {pageRange(offset, data.data.items.length, data.data.total)} of {data.data.total}
              </span>
              <button
                className="button"
                disabled={offset + 20 >= data.data.total || offset >= 5000}
                onClick={() => update('offset', String(offset + 20))}
              >
                Next
              </button>
            </div>
          </Panel>
          {selected && (
            <Panel title={`${selected.name} · investigation`} subtitle={selected.basis}>
              {selected.truncated && (
                <p className="data-warning">
                  Application inventory or relationships truncated; inspect resource tables for
                  complete bounded collections.
                </p>
              )}
              <div className="detail-grid">
                <section>
                  <h3>Resources</h3>
                  <ul>
                    {selected.resources.map((r) => (
                      <li key={r.id}>
                        <span className="muted">{r.kind} </span>
                        <ResourceLink resource={r} />
                      </li>
                    ))}
                  </ul>
                </section>
                <section>
                  <h3>Issues</h3>
                  <ul>
                    {selected.issues.map((i) => (
                      <li key={i.id}>
                        <Link
                          className="text-link"
                          to={`/issues/${encodeURIComponent(i.id)}?namespace=${encodeURIComponent(selected.namespace)}`}
                        >
                          {i.title} · {i.resource.name}
                        </Link>
                      </li>
                    ))}
                  </ul>
                  {!selected.issues.length && (
                    <p className="muted">No observed issues; collection warnings still apply.</p>
                  )}
                  <h3>Delivery integrations</h3>
                  {selected.integrations.map((i, n) => (
                    <p key={n}>
                      <Link
                        className="text-link"
                        to={
                          i.resource.kind === 'Secret'
                            ? `/helm/${encodeURIComponent(i.resource.namespace)}/${encodeURIComponent(i.name)}`
                            : `/gitops?namespace=${encodeURIComponent(i.resource.namespace)}`
                        }
                      >
                        {i.name} · {i.status}
                      </Link>
                      <small className="table-subtitle">
                        {i.observation} · {i.revision}
                      </small>
                    </p>
                  ))}
                  {!selected.integrations.length && (
                    <p className="muted">
                      No verified delivery inventory or explicit Helm label association observed.
                    </p>
                  )}
                </section>
              </div>
              <details>
                <summary>Declared relationships ({selected.relationships.length})</summary>
                <ul>
                  {selected.relationships.map((e) => (
                    <li key={e.id}>
                      <code>{e.source}</code> → {e.relation} → <code>{e.target}</code>
                    </li>
                  ))}
                </ul>
              </details>
              <Link
                className="button"
                to={`/topology?namespace=${encodeURIComponent(selected.namespace)}`}
              >
                Explore namespace topology
              </Link>
            </Panel>
          )}
          <p className="footnote">{data.data.limitations}</p>
        </>
      )}
    </>
  );
}
