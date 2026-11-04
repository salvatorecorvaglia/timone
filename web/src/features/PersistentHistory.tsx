import { useQuery } from '@tanstack/react-query';
import { request, useAPI } from '../lib/api';
import { useInvestigationWindow } from '../lib/useInvestigationWindow';
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
import type { KEvent, Resource, Warning } from '../types';
interface ArchiveStatus {
  maxRecords: number;
  enabled: boolean;
  state: string;
  message: string;
  retentionHours: number;
  maxMiB: number;
  intervalSeconds: number;
  lastSample?: string;
}
interface ArchivedHistory {
  status: ArchiveStatus;
  samples: string[];
  window: { start: string; end: string };
  changes: { id: string; resource: Resource; type: string; evidence: string; observedAt: string }[];
  events: KEvent[];
  warnings: Warning[];
  message: string;
}
export function PersistentHistory({ namespace }: { namespace: string }) {
  const window = useInvestigationWindow();
  const status = useAPI<ArchiveStatus>('/api/v1/history/status');
  const path = `/api/v1/history?${new URLSearchParams({ namespace, since: window.since, until: window.until })}`;
  const query = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<ArchivedHistory>(path, { signal }),
    enabled: !!status.data?.enabled && window.valid,
    retry: false,
  });
  const gaps =
    query.data?.samples.flatMap((stamp, i, all) =>
      i > 0 &&
      Date.parse(stamp) - Date.parse(all[i - 1]) > 2 * (status.data?.intervalSeconds ?? 30) * 1000
        ? [`${all[i - 1]} — ${stamp}`]
        : [],
    ) ?? [];
  return (
    <>
      <PageTitle
        title="Persistent observation history"
        description="Periodic samples continue while no UI is open. Only currently readable identities with matching UIDs are shown."
      />
      <Panel title="Collector status">
        {status.isPending && <Loading />}
        {status.error && <ErrorState error={status.error} />}
        {status.data && (
          <>
            <p>
              {status.data.state}: {status.data.message}
            </p>
            {status.data.enabled ? (
              <p>
                Maximum retention: {status.data.retentionHours} hours · Global sample cap:{' '}
                {status.data.maxRecords} · Global disk budget: {status.data.maxMiB} MiB · Sampling:{' '}
                {status.data.intervalSeconds} seconds · Last sample:{' '}
                {status.data.lastSample ?? 'Not yet observed'}
              </p>
            ) : (
              <Empty
                title="Persistent history is disabled"
                description="Configure TIMONE_HISTORY_DIRECTORY and retention settings on the CLI/server to enable the local archive."
              />
            )}
          </>
        )}
      </Panel>
      {!!status.data?.enabled && (
        <>
          <Panel title="History interval">
            {window.controls}
            <button
              className="button"
              disabled={!window.valid || query.isFetching}
              onClick={() => void query.refetch()}
            >
              Refresh retained history
            </button>
          </Panel>
          {query.isFetching && <Loading />}
          {query.error && <ErrorState error={query.error} />}
          {query.data && !query.isFetching && (
            <>
              <Warnings items={query.data.warnings} />
              <Panel title="Sampling coverage">
                <p>{query.data.message}</p>
                <p>
                  {query.data.samples.length} retained samples in selected interval. Unobserved time
                  before the first and after the last sample is not covered.
                </p>
                {gaps.map((gap) => (
                  <p key={gap}>Sampling gap: {gap}</p>
                ))}
                <details>
                  <summary>Sample timestamps</summary>
                  <ul>
                    {query.data.samples.map((sample) => (
                      <li key={sample}>{sample}</li>
                    ))}
                  </ul>
                </details>
              </Panel>
              <Panel title="Retained observed changes">
                {query.data.changes.map((change) => (
                  <div key={change.id}>
                    <ResourceLink resource={change.resource} />
                    <time> · {change.observedAt}</time>
                    <p>
                      {change.type}: {change.evidence}
                    </p>
                  </div>
                ))}
                {!query.data.changes.length && (
                  <p>No retained changes for currently readable identities.</p>
                )}
              </Panel>
              <Panel title="Retained Kubernetes events">
                {query.data.events.map((event) => (
                  <EventCard key={event.id} event={event} />
                ))}
                {!query.data.events.length && (
                  <p>No retained events for currently readable identities.</p>
                )}
              </Panel>
            </>
          )}
        </>
      )}
    </>
  );
}
