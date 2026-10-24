import { ScopedLink as Link } from '../components/navigation';

import { Activity, ChevronLeft, ChevronRight, Clock3, Search } from 'lucide-react';
import { ago, useAPI } from '../lib/api';
import type { Cluster, KEvent, Warning } from '../types';
import {
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Warnings,
} from '../components/ui';
import { EventCard } from './EventCard';
import { useFilters } from '../lib/filters';
import { pagination } from '../lib/pagination';

function localTime(value: string) {
  const t = new Date(value);
  if (!value || Number.isNaN(t.getTime())) return '';
  return new Date(t.getTime() - t.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

export function Timeline({ namespace }: { namespace: string }) {
  const changes = useAPI<{
    items: {
      id: string;
      resource: import('../types').Resource;
      type: string;
      evidence: string;
      observedAt: string;
    }[];
    warnings: Warning[];
    total: number;
    retention: string;
  }>(`/api/v1/changes?${new URLSearchParams({ namespace, limit: '50' })}`);
  const { p, search, status, update } = useFilters();
  const { offset } = pagination(p);
  const cluster = useAPI<Cluster>('/api/v1/cluster');
  const kind = p.get('kind') ?? '';
  const resource = p.get('resource') ?? '';
  const since = p.get('since') ?? '';
  const until = p.get('until') ?? '';
  const q = new URLSearchParams({
    namespace,
    search,
    status,
    kind,
    resource,
    since,
    until,
    limit: '25',
    offset: String(offset),
  });
  const data = useAPI<{
    items: KEvent[];
    total: number;
    oldest: string;
    newest: string;
    retention: string;
  }>(`/api/v1/events?${q}`);
  return (
    <>
      <PageTitle
        eyebrow="THE STORY BEHIND THE STATE"
        title="Events & Timeline"
        description="Follow Kubernetes events in order, with repetitive signals grouped by occurrence count."
        action={
          <span className="tag">
            <Activity size={13} />
            Live updates
          </span>
        }
      />
      <details className="diagnostic-policy">
        <summary>Observed resource changes</summary>
        <p className="muted">{changes.data?.retention}</p>
        {changes.isPending ? (
          <Loading />
        ) : changes.error ? (
          <ErrorState error={changes.error} />
        ) : (
          <>
            <Warnings items={changes.data?.warnings} />
            <p className="muted">
              Showing the latest {changes.data?.items.length ?? 0} of {changes.data?.total ?? 0}{' '}
              observed changes in this namespace scope.
            </p>
            <ul>
              {changes.data?.items.map((c) => (
                <li key={c.id}>
                  <ResourceLink resource={c.resource} /> · {c.type} · {ago(c.observedAt)}
                  <p className="muted">{c.evidence}</p>
                </li>
              ))}
            </ul>
            {!changes.data?.items.length && (
              <p>No authorized resource observations available yet.</p>
            )}
          </>
        )}
      </details>
      <div className="filterbar">
        <div className="search-field">
          <Search size={15} />
          <input
            aria-label="Search events"
            placeholder="Search messages, reasons, or resource names…"
            value={search}
            maxLength={200}
            onChange={(e) => update('search', e.target.value)}
          />
        </div>
        <select
          aria-label="Event type"
          value={status}
          onChange={(e) => update('status', e.target.value)}
        >
          <option value="">All event types</option>
          <option value="warning">Warning</option>
          <option value="healthy">Normal</option>
        </select>
      </div>
      <div className="filterbar timeline-filters">
        <select
          aria-label="Event resource kind"
          value={kind}
          onChange={(e) => update('kind', e.target.value)}
        >
          <option value="">All resource kinds</option>
          {cluster.data?.kinds.map((k) => (
            <option key={k.name} value={k.name}>
              {k.name}
            </option>
          ))}
        </select>
        <input
          className="filter-input"
          aria-label="Exact event resource name"
          placeholder="Exact resource name…"
          maxLength={253}
          value={resource}
          onChange={(e) => update('resource', e.target.value)}
        />
        <label className="time-filter">
          From{' '}
          <input
            type="datetime-local"
            aria-label="Events from"
            value={localTime(since)}
            onChange={(e) =>
              update('since', e.target.value ? new Date(e.target.value).toISOString() : '')
            }
          />
        </label>
        <label className="time-filter">
          To{' '}
          <input
            type="datetime-local"
            aria-label="Events until"
            value={localTime(until)}
            onChange={(e) =>
              update('until', e.target.value ? new Date(e.target.value).toISOString() : '')
            }
          />
        </label>
        {(kind || resource || since || until || search || status) && (
          <Link
            className="button"
            to={namespace ? `?namespace=${encodeURIComponent(namespace)}` : '?'}
          >
            Clear filters
          </Link>
        )}
      </div>
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} />
      ) : (
        <>
          <div className="timeline-range">
            <Clock3 size={14} />
            <span>
              Matching occurrence range:{' '}
              {data.data.oldest ? new Date(data.data.oldest).toLocaleString() : 'No events'}
              {data.data.newest && <> — {new Date(data.data.newest).toLocaleString()}</>}
            </span>
            <strong>{data.data.total} event groups</strong>
          </div>
          <Panel>
            <div className="timeline">
              {data.data.items.map((e) => (
                <div className="timeline-entry" key={e.id}>
                  <div className="timeline-time">
                    <span>
                      {new Date(e.timestamp).toLocaleTimeString([], {
                        hour: '2-digit',
                        minute: '2-digit',
                      })}
                    </span>
                    <small>{ago(e.timestamp)}</small>
                  </div>
                  <div className={`timeline-dot ${e.type === 'Warning' ? 'warning' : 'healthy'}`} />
                  <EventCard event={e} />
                </div>
              ))}
              {!data.data.items.length && (
                <Empty
                  title="No events match this view"
                  description="Change the filters or check event permissions and Kubernetes retention."
                />
              )}
            </div>
            <div className="pagination">
              <span>{data.data.total} event groups · newest first</span>
              <Link
                className={`button ${offset === 0 ? 'disabled' : ''}`}
                aria-disabled={offset === 0}
                to={`?${new URLSearchParams({ ...Object.fromEntries(p), offset: String(Math.max(0, offset - 25)) })}`}
              >
                <ChevronLeft size={14} />
                Previous
              </Link>
              <Link
                className={`button ${offset + 25 >= data.data.total ? 'disabled' : ''}`}
                aria-disabled={offset + 25 >= data.data.total}
                to={`?${new URLSearchParams({ ...Object.fromEntries(p), offset: String(offset + 25) })}`}
              >
                Next
                <ChevronRight size={14} />
              </Link>
            </div>
          </Panel>
          <p className="footnote">{data.data.retention} Timone does not persist history.</p>
        </>
      )}
    </>
  );
}
