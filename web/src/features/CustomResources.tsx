import { ScopedLink as Link } from '../components/navigation';
import { useSearchParams } from 'react-router-dom';
import { ArrowRight, ChevronLeft, ChevronRight, RefreshCw, Search } from 'lucide-react';
import { ago, useAPI } from '../lib/api';
import { pagination } from '../lib/pagination';
import type { CustomCatalog, CustomPage, CustomType } from '../types';
import {
  TableScroll,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  Status,
  Warnings,
} from '../components/ui';

export function CustomResources({ namespace }: { namespace: string }) {
  const catalog = useAPI<CustomCatalog>('/api/v1/custom-resource-types');
  const [params, setParams] = useSearchParams();
  if (catalog.isPending) return <Loading />;
  if (catalog.error)
    return <ErrorState error={catalog.error} retry={() => void catalog.refetch()} />;
  const definition = params.get('definition') || catalog.data.items[0]?.definition;
  const selected = catalog.data.items.find((t) => t.definition === definition);
  return (
    <>
      <PageTitle
        eyebrow="CLUSTER EXTENSIONS"
        title="Custom resources"
        description="Inspect operator-managed resources by their exact API identity."
        action={
          <button className="button" onClick={() => void catalog.refetch()}>
            <RefreshCw size={14} />
            Refresh types
          </button>
        }
      />
      <Warnings items={catalog.data.warnings} />
      <div className="manifest-notice">{catalog.data.message}</div>
      {!catalog.data.enabled ? (
        <Empty
          title="Enable custom resource browsing"
          description="An administrator must configure explicit CRD names and read permissions. See the custom-resource guide in the repository docs."
        />
      ) : !catalog.data.items.length ? (
        <Empty
          title="No enabled types available"
          description="Review discovery warnings, CRD installation and Kubernetes read permissions. Unavailable types are not treated as empty resource collections."
        />
      ) : (
        <>
          <Panel
            title="Installed types"
            subtitle="Only explicitly enabled, established definitions with served versions are listed."
          >
            <div className="filter-bar custom-type-select">
              <label>
                Resource type
                <select
                  aria-label="Custom resource type"
                  value={definition}
                  onChange={(e) => {
                    const next = new URLSearchParams();
                    if (namespace) next.set('namespace', namespace);
                    next.set('definition', e.target.value);
                    setParams(next);
                  }}
                >
                  {!selected && <option value={definition}>Unavailable type: {definition}</option>}
                  {catalog.data.items.map((t) => (
                    <option key={t.definition} value={t.definition}>
                      {t.kind} · {t.group}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </Panel>
          {selected ? (
            <CustomCollection key={selected.definition} type={selected} namespace={namespace} />
          ) : (
            <Empty
              title="Selected type unavailable"
              description="The shared URL names a type that is disabled, missing or unreadable. Choose an available type above."
            />
          )}
        </>
      )}
      <p className="footnote">
        Health reflects only a generation-matched Ready condition. Other controller conventions
        remain unknown. Active collections refresh every 30 seconds; no custom-resource change
        history is retained.
      </p>
    </>
  );
}
function CustomCollection({ type, namespace }: { type: CustomType; namespace: string }) {
  const [params, setParams] = useSearchParams();
  const version = params.get('version') || type.preferredVersion;
  const search = params.get('search') ?? '';
  const status = params.get('status') ?? '';
  const { limit, offset } = pagination(params);
  const sort = params.get('sort') ?? 'name';
  const q = new URLSearchParams({
    namespace,
    version,
    search,
    status,
    sort,
    limit: String(limit),
    offset: String(offset),
    desc: params.get('desc') ?? 'false',
  });
  const data = useAPI<CustomPage>(
    `/api/v1/custom-resources/${encodeURIComponent(type.definition)}?${q}`,
  );
  const presentation = data.data?.type.presentations?.[version];
  const wide = params.get('wide') === 'true';
  const columns =
    presentation?.columns
      .map((column, index) => ({ column, index }))
      .filter(({ column }) => wide || column.priority === 0) ?? [];
  const serverTable =
    columns.length > 0 &&
    columns[0]?.column.jsonPath === '.metadata.name' &&
    data.data?.items.every((r) => r.printerCells);
  function update(key: string, value: string) {
    const next = new URLSearchParams(params);
    next.set('definition', type.definition);
    if (value) next.set(key, value);
    else next.delete(key);
    if (key !== 'offset') next.delete('offset');
    setParams(next, { replace: true });
  }
  return (
    <Panel
      title={type.kind}
      subtitle={`${type.group} · ${type.namespaced ? 'Namespaced' : 'Cluster scoped; namespace filter does not restrict this type'}`}
      action={
        <button className="button subtle" onClick={() => void data.refetch()}>
          <RefreshCw size={14} />
          Refresh resources
        </button>
      }
    >
      {presentation && (
        <details className="diagnostic-policy">
          <summary>OpenAPI v3 schema · {version}</summary>
          <p>
            Schema structure and validation. Embedded defaults/examples are omitted.{' '}
            {presentation.truncated ? 'Schema or columns truncated at safety bounds.' : ''}
          </p>
          <pre aria-label="OpenAPI v3 schema">{JSON.stringify(presentation.schema, null, 2)}</pre>
        </details>
      )}
      <div className="filter-bar custom-filters">
        <label>
          Version
          <select
            aria-label="Custom resource version"
            value={version}
            onChange={(e) => update('version', e.target.value)}
          >
            {!type.versions.includes(version) && (
              <option value={version}>Not served: {version}</option>
            )}
            {type.versions.map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
        </label>
        <label className="search-field">
          <Search size={15} />
          <input
            aria-label="Search custom resources"
            placeholder="Search name or namespace…"
            value={search}
            onChange={(e) => update('search', e.target.value)}
          />
        </label>
        <label>
          Health
          <select
            aria-label="Custom resource health"
            value={status}
            onChange={(e) => update('status', e.target.value)}
          >
            <option value="">All states</option>
            <option value="healthy">Ready reported</option>
            <option value="warning">Not ready reported</option>
            <option value="unknown">Unknown / stale</option>
          </select>
        </label>
        <label>
          Sort
          <select
            aria-label="Custom resource sort"
            value={sort}
            onChange={(e) => update('sort', e.target.value)}
          >
            <option value="name">Name</option>
            <option value="namespace">Namespace</option>
            <option value="status">Health</option>
            <option value="createdAt">Created</option>
          </select>
        </label>
        <button
          className="button subtle"
          aria-label={`${params.get('desc') === 'true' ? 'Descending' : 'Ascending'} custom resource sort; reverse order`}
          onClick={() => update('desc', params.get('desc') === 'true' ? 'false' : 'true')}
        >
          {params.get('desc') === 'true' ? 'Descending' : 'Ascending'}
        </button>
      </div>
      {presentation && (
        <label>
          <input
            type="checkbox"
            checked={wide}
            onChange={(e) => update('wide', String(e.target.checked))}
          />{' '}
          Wide printer columns
        </label>
      )}
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} retry={() => void data.refetch()} />
      ) : (
        <>
          {!serverTable && (
            <p className="muted">
              Server printer cells are unavailable; showing safe summary columns.
            </p>
          )}
          <TableScroll>
            <table aria-label="Custom resources">
              <thead>
                <tr>
                  {serverTable ? (
                    <>
                      {type.namespaced && !namespace && <th>Namespace</th>}
                      {columns.map(({ column, index }) => (
                        <th key={index} title={column.description}>
                          {column.name}
                        </th>
                      ))}
                    </>
                  ) : (
                    <>
                      <th>Name</th>
                      <th>Namespace</th>
                      <th>Reported state</th>
                      <th>Created</th>
                    </>
                  )}
                  <th>
                    <span className="sr-only">Details</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.data.items.map((r) => {
                  const href = `/custom-resources/${encodeURIComponent(type.definition)}/${encodeURIComponent(r.namespace || '_')}/${encodeURIComponent(r.name)}?version=${encodeURIComponent(data.data.version)}`;
                  return (
                    <tr key={r.id}>
                      {serverTable ? (
                        <>
                          {type.namespaced && !namespace && <td>{r.namespace}</td>}
                          {columns.map(({ column, index }) => (
                            <td key={index}>
                              {index === 0 && column.name === 'Name' ? (
                                <Link className="resource-link" to={href}>
                                  {r.name}
                                </Link>
                              ) : (
                                (r.printerCells?.[index] ?? '<none>')
                              )}
                            </td>
                          ))}
                        </>
                      ) : (
                        <>
                          <td>
                            <Link className="resource-link" to={href}>
                              {r.name}
                            </Link>
                          </td>
                          <td>{r.namespace || 'Cluster scoped'}</td>
                          <td>
                            <Status value={r.health} label={r.status} />
                          </td>
                          <td>
                            <time title={r.createdAt}>{ago(r.createdAt)}</time>
                          </td>
                        </>
                      )}
                      <td>
                        <Link className="icon-button" aria-label={`Inspect ${r.name}`} to={href}>
                          <ArrowRight size={15} />
                        </Link>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {!data.data.items.length && <Empty />}
          </TableScroll>
          {data.data.truncated && (
            <div className="data-warning">
              Collection truncated at 5,000 instances. Select a namespace or narrow the server
              scope.
            </div>
          )}
          <div className="pagination">
            <span>
              {data.data.total
                ? `${Math.min(offset + 1, data.data.total)}–${Math.min(offset + limit, data.data.total)}`
                : '0'}{' '}
              of {data.data.total} resources
            </span>
            <label>
              Rows
              <select
                aria-label="Custom resources per page"
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
              aria-label="Previous custom resource page"
              disabled={offset === 0}
              onClick={() => update('offset', String(Math.max(0, offset - limit)))}
            >
              <ChevronLeft size={16} />
            </button>
            <button
              className="icon-button"
              aria-label="Next custom resource page"
              disabled={offset + limit >= data.data.total || offset + limit > 5000}
              onClick={() => update('offset', String(offset + limit))}
            >
              <ChevronRight size={16} />
            </button>
          </div>
        </>
      )}
    </Panel>
  );
}
