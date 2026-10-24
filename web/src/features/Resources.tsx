import {
  ArrowDown,
  ArrowUpDown,
  Box,
  ChevronLeft,
  ChevronRight,
  Layers3,
  ListFilter,
  Search,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ScopedLink as Link } from '../components/navigation';
import {
  TableScroll,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Status,
} from '../components/ui';
import { ago, resourceURL, useAPI } from '../lib/api';
import { pageRange, pagination } from '../lib/pagination';
import { readPreference, writePreference } from '../lib/preferences';
import type { Cluster, ResourcePage } from '../types';
export function Resources({ namespace }: { namespace: string }) {
  const [params, setParams] = useSearchParams();
  const cluster = useAPI<Cluster>('/api/v1/cluster');
  const kind = params.get('kind') ?? 'Pod';
  const search = params.get('search') ?? '';
  const status = params.get('status') ?? '';
  const { offset, limit } = pagination(params);
  const phase = params.get('phase') ?? '';
  const q = new URLSearchParams({
    kind,
    namespace,
    search,
    status,
    offset: String(offset),
    limit: String(limit),
    sort: params.get('sort') ?? 'name',
    desc: params.get('desc') ?? 'false',
    phase,
  });
  const data = useAPI<ResourcePage>(`/api/v1/resources?${q}`);
  useEffect(() => {
    if (!data.data || data.isFetching || offset === 0 || offset < data.data.total) return;
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('offset', String(Math.max(0, Math.ceil(data.data!.total / limit) - 1) * limit));
        return next;
      },
      { replace: true },
    );
  }, [data.data, data.isFetching, offset, limit, setParams]);
  const [compact, setCompact] = useState(
    () => readPreference('timone-density', 'comfortable') === 'compact',
  );
  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    if (key !== 'offset') next.delete('offset');
    if (key === 'kind') {
      next.delete('phase');
      next.delete('status');
    }
    setParams(next, { replace: true });
  };
  const sort = (field: string) => {
    const next = new URLSearchParams(params);
    next.set('sort', field);
    next.delete('offset');
    next.set('desc', String(params.get('sort') === field && params.get('desc') !== 'true'));
    setParams(next);
  };
  const kinds = cluster.data?.kinds.map((k) => k.name) ?? [kind];
  return (
    <>
      <PageTitle
        eyebrow="KUBERNETES INVENTORY"
        title="Resources"
        description="Explore the resources behind your cluster. Every row leads to the source."
        action={
          <span className="tag">
            <Box size={12} />
            {data.data?.total ?? '—'} visible {kind}
            {data.data?.total === 1 ? '' : 's'}
          </span>
        }
      />
      <div className="filterbar">
        <div className="filter-select">
          <Layers3 size={15} />
          <select
            aria-label="Resource kind"
            value={kind}
            onChange={(e) => update('kind', e.target.value)}
          >
            {kinds.map((k) => (
              <option key={k}>{k}</option>
            ))}
          </select>
        </div>
        <div className="search-field">
          <Search size={15} />
          <input
            placeholder="Search by name or namespace…"
            aria-label="Search resources"
            value={search}
            maxLength={200}
            onChange={(e) => update('search', e.target.value)}
          />
          {search && (
            <button aria-label="Clear resource search" onClick={() => update('search', '')}>
              ×
            </button>
          )}
        </div>
        <div className="filter-select">
          <ListFilter size={14} />
          <select
            aria-label="Resource health"
            value={status}
            onChange={(e) => update('status', e.target.value)}
          >
            <option value="">All health states</option>
            <option value="healthy">Healthy</option>
            <option value="warning">Warning</option>
            <option value="critical">Critical</option>
            <option value="unknown">Unknown</option>
          </select>
        </div>
        <button
          className={`button density-button ${compact ? 'active' : ''}`}
          aria-pressed={compact}
          onClick={() => {
            setCompact(!compact);
            writePreference('timone-density', compact ? 'comfortable' : 'compact');
          }}
        >
          <ArrowUpDown size={14} />
          Density
        </button>
      </div>
      {phase && (
        <div className="active-filter">
          Pod state: {phase}
          <button onClick={() => update('phase', '')} aria-label="Clear pod state filter">
            ×
          </button>
        </div>
      )}
      <Panel>
        <div className="panel-header">
          <h2>
            {kind}s<span className="inline-count">{data.data?.total ?? '—'}</span>
          </h2>
          <span className="muted small">{namespace || 'All namespaces'} · Read only</span>
        </div>
        {data.isPending ? (
          <Loading />
        ) : data.error ? (
          <ErrorState error={data.error} retry={() => void data.refetch()} />
        ) : (
          <>
            <TableScroll className={`resource-table ${compact ? 'compact' : ''}`}>
              <table>
                <thead>
                  <tr>
                    {[
                      { name: 'Name', field: 'name' },
                      { name: 'Namespace', field: 'namespace' },
                      { name: 'Status', field: 'status' },
                      { name: 'Ready', field: '' },
                      { name: 'Restarts', field: 'restarts' },
                      { name: 'Created', field: 'createdAt' },
                    ].map((c) => (
                      <th
                        key={c.name}
                        aria-sort={
                          c.field && (params.get('sort') ?? 'name') === c.field
                            ? params.get('desc') === 'true'
                              ? 'descending'
                              : 'ascending'
                            : undefined
                        }
                      >
                        {c.field ? (
                          <button onClick={() => sort(c.field)}>
                            {c.name}
                            {(params.get('sort') ?? 'name') === c.field ? (
                              <ArrowDown
                                size={12}
                                style={{
                                  transform:
                                    params.get('desc') === 'true' ? undefined : 'rotate(180deg)',
                                }}
                              />
                            ) : (
                              <ArrowUpDown size={11} />
                            )}
                          </button>
                        ) : (
                          c.name
                        )}
                      </th>
                    ))}
                    <th>
                      <span className="sr-only">Details</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.items.map((r) => (
                    <tr key={r.id}>
                      <td>
                        <ResourceLink resource={r} />
                        <small className="table-subtitle">
                          {Object.entries(r.labels ?? {}).find(
                            ([k]) => k === 'app.kubernetes.io/name' || k === 'app',
                          )?.[1] ?? r.kind}
                        </small>
                      </td>
                      <td>
                        {r.namespace ? (
                          <Link
                            to={`?kind=${r.kind}&namespace=${encodeURIComponent(r.namespace)}`}
                            className="namespace-tag"
                          >
                            {r.namespace}
                          </Link>
                        ) : (
                          <span className="muted">Cluster scoped</span>
                        )}
                      </td>
                      <td>
                        <Status value={r.health} label={r.status || 'Unknown'} />
                      </td>
                      <td className="mono">{r.ready || '—'}</td>
                      <td className={`mono ${r.restarts > 4 ? 'text-warning' : ''}`}>
                        {r.kind === 'Pod' ? r.restarts : '—'}
                      </td>
                      <td>
                        <time title={r.createdAt}>{ago(r.createdAt)}</time>
                      </td>
                      <td>
                        <Link
                          className="icon-button"
                          to={resourceURL(r)}
                          aria-label={`Open ${r.name}`}
                        >
                          <ChevronRight size={16} />
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!data.data.items.length && <Empty />}
            </TableScroll>
            {data.data.truncated && (
              <div className="data-warning">
                Kubernetes collection truncated. Select a namespace for a narrower view.
              </div>
            )}
            <div className="pagination">
              <span>
                {pageRange(offset, data.data.items.length, data.data.total)} of {data.data.total}{' '}
                resources
              </span>
              <label>
                Rows
                <select
                  aria-label="Rows per page"
                  value={limit}
                  onChange={(e) => update('limit', e.target.value)}
                >
                  {[25, 50, 100, 200].map((n) => (
                    <option key={n}>{n}</option>
                  ))}
                </select>
              </label>
              <button
                className="icon-button"
                aria-label="Previous page"
                disabled={offset === 0}
                onClick={() => update('offset', String(Math.max(0, offset - limit)))}
              >
                <ChevronLeft size={16} />
              </button>
              <button
                className="icon-button"
                aria-label="Next page"
                disabled={offset + limit >= data.data.total}
                onClick={() => update('offset', String(offset + limit))}
              >
                <ChevronRight size={16} />
              </button>
            </div>
          </>
        )}
      </Panel>
      <p className="footnote">
        Health is derived from reported resource status. “Observed” means the resource has no health
        rule, and does not imply it is healthy.
      </p>
    </>
  );
}
