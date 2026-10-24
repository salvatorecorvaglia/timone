import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowUpRight, Box, ChevronLeft, ChevronRight } from 'lucide-react';
import { request } from '../lib/api';
import { queryInteger } from '../lib/pagination';
import type { CustomCatalog, SearchHit, SearchResult } from '../types';
import { Empty, ErrorState, Loading, Status, Warnings } from '../components/ui';

export function GlobalResults({
  search,
  namespace,
  kinds,
  onSelect,
}: {
  search: string;
  namespace: string;
  kinds: { name: string }[];
  onSelect: (r: SearchHit) => void;
}) {
  const [debounced, setDebounced] = useState(search.trim());
  const [params, setParams] = useSearchParams();
  const type = params.get('gtype') ?? '';
  const status = params.get('gstatus') ?? '';
  const filter = (key: string, value: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value) next.set(key, value);
        else next.delete(key);
        next.delete('goffset');
        return next;
      },
      { replace: true },
    );
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search.trim()), 250);
    return () => clearTimeout(timer);
  }, [search]);
  const current = search.trim() === debounced;
  const enabled = current && Array.from(debounced).length >= 2;
  const filters = new URLSearchParams({ search: debounced, namespace, limit: '20' });
  if (type.startsWith('custom:')) filters.set('definition', type.slice(7));
  else if (type) filters.set('kind', type);
  if (status) filters.set('status', status);
  const offset = queryInteger(params, 'goffset', 0, 0, 5000);
  const changePage = (value: number) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('goffset', String(value));
        return next;
      },
      { replace: true },
    );
  };
  filters.set('offset', String(offset));
  const path = `/api/v1/search?${filters}`;
  const data = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<SearchResult>(path, { signal }),
    enabled,
    retry: 1,
    staleTime: 0,
  });
  const catalog = useQuery({
    queryKey: ['/api/v1/custom-resource-types'],
    queryFn: ({ signal }) => request<CustomCatalog>('/api/v1/custom-resource-types', { signal }),
    staleTime: 30000,
    retry: 1,
  });
  return (
    <>
      <div className="global-search-filters">
        <select
          aria-label="Global search resource type"
          value={type}
          onChange={(e) => filter('gtype', e.target.value)}
        >
          <option value="">All configured types</option>
          <optgroup label="Built-in resources">
            {kinds.map((k) => (
              <option key={k.name} value={k.name}>
                {k.name}
              </option>
            ))}
          </optgroup>
          {!!catalog.data?.items.length && (
            <optgroup label="Enabled custom resources">
              {catalog.data.items.map((t) => (
                <option key={t.definition} value={`custom:${t.definition}`}>
                  {t.kind} · {t.group}
                </option>
              ))}
            </optgroup>
          )}
        </select>
        <select
          aria-label="Global search health"
          value={status}
          onChange={(e) => filter('gstatus', e.target.value)}
        >
          <option value="">All health states</option>
          {['healthy', 'warning', 'critical', 'unknown'].map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <span className="muted">
          {namespace || 'All accessible namespaces'} · cluster resources included
        </span>
      </div>
      {!enabled ? (
        <div className="search-results">
          <Empty
            title={current ? 'Find a resource' : 'Updating search…'}
            description="Enter at least two characters. Search matches names, namespaces, kinds and enabled custom definitions."
          />
        </div>
      ) : data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} retry={() => void data.refetch()} />
      ) : (
        <>
          <Warnings items={data.data.warnings} />
          <div className="search-summary" role="status" aria-live="polite">
            {data.data.complete
              ? `${data.data.total} matches in observed collections`
              : `At least ${data.data.total} matches · incomplete coverage`}
            {data.isFetching && <span> · Refreshing…</span>}
          </div>
          <div className="search-results">
            {data.data.items.map((r) => (
              <button
                key={r.id}
                aria-label={`Inspect ${r.kind} ${r.namespace || 'cluster'}/${r.name}${r.definition ? ` (${r.definition})` : ''}`}
                onClick={() => onSelect(r)}
              >
                <Box size={17} />
                <span>
                  <strong>{r.name}</strong>
                  <small>
                    {r.kind} · {r.namespace || 'Cluster scoped'}
                    {r.definition ? ` · ${r.definition}` : ''}
                  </small>
                </span>
                <Status value={r.health} label={r.status} />
                <ArrowUpRight size={14} />
              </button>
            ))}
            {!data.data.items.length && (
              <Empty
                title={
                  data.data.complete
                    ? 'No matching resources'
                    : 'No matches in readable collections'
                }
                description={
                  data.data.complete
                    ? 'Try another term or remove a filter.'
                    : 'Some collections could not be searched. Review coverage before treating a resource as absent.'
                }
              />
            )}
          </div>
          <div className="search-pagination">
            <button
              className="button"
              aria-label="Previous search results"
              disabled={offset === 0 || data.isFetching}
              onClick={() => changePage(Math.max(0, offset - 20))}
            >
              <ChevronLeft size={14} />
              Previous
            </button>
            <span>
              {data.data.items.length
                ? `${offset + 1}–${offset + data.data.items.length}`
                : '0 results'}
            </span>
            <button
              className="button"
              aria-label="Next search results"
              disabled={
                offset + data.data.items.length >= data.data.total ||
                offset >= 5000 ||
                data.isFetching
              }
              onClick={() => changePage(offset + 20)}
            >
              Next
              <ChevronRight size={14} />
            </button>
          </div>
          {offset >= 5000 && offset + data.data.items.length < data.data.total && (
            <p className="data-warning">Refine the search to see further matches.</p>
          )}
          <details className="search-coverage">
            <summary>Collection coverage ({data.data.coverage.length})</summary>
            <ul>
              {data.data.coverage.map((c, i) => (
                <li key={`${c.definition || c.kind}:${i}`}>
                  <span>{c.definition || c.kind}</span>
                  <span>
                    {c.state}
                    {c.observed !== undefined
                      ? ` · ${c.matched} matched / ${c.observed} observed`
                      : ''}
                  </span>
                </li>
              ))}
            </ul>
          </details>
        </>
      )}
    </>
  );
}
