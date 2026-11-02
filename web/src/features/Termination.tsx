import { useState } from 'react';
import { Empty, ErrorState, Loading, Panel, ResourceLink, TableScroll } from '../components/ui';
import type { KEvent, Resource, Warning } from '../types';
import { CheckList, Coverage, useAssessment, type Check } from './Operations';
import { saveInvestigation } from './Investigation';
import { EventCard } from './EventCard';

interface Deletion {
  state: string;
  timestamp: string;
  secondsPastTimestamp: number | null;
  secondsUntilTimestamp: number | null;
  deletionGraceSeconds: number | null;
  configuredGraceSeconds: number | null;
  metadataFinalizers: string[];
  namespaceFinalizers: string[];
  checks: Check[];
}
interface Termination {
  uid: string;
  resource: Resource;
  observedAt: string;
  state: string;
  deletion: Deletion;
  relations: {
    uid?: string;
    resource?: Resource;
    kind: string;
    namespace: string;
    name: string;
    relation: string;
    state: string;
    deletion?: Deletion;
    checks: Check[];
  }[];
  collections: { kind: string; observed: number | null; complete: boolean }[];
  checks: Check[];
  events: KEvent[];
  warnings: Warning[];
  limitations: string[];
}
function Finalizers({ data }: { data: Deletion }) {
  return (
    <>
      <p>metadata.finalizers: {data.metadataFinalizers.join(', ') || '(none observed)'}</p>
      {!!data.namespaceFinalizers.length && (
        <p>spec.finalizers: {data.namespaceFinalizers.join(', ')}</p>
      )}
      <CheckList checks={data.checks} />
    </>
  );
}
export function TerminationDetails({
  kind,
  namespace,
  name,
}: {
  kind: string;
  namespace: string;
  name: string;
}) {
  const [deletingOnly, setDeletingOnly] = useState(false);
  const query = useAssessment<Termination>(
    `/api/v1/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace || '_')}/${encodeURIComponent(name)}/termination`,
  );
  return (
    <Panel
      title="Deletion diagnostics"
      subtitle="Observed finalization requirements and related evidence. This view performs no delete, force-delete or finalizer removal."
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh deletion analysis
      </button>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && !query.isFetching && (
        <>
          <Coverage data={query.data} />
          <button
            className="button"
            onClick={() =>
              saveInvestigation(
                'deletion-diagnostics.json',
                JSON.stringify(query.data, null, 2),
                'application/json',
              )
            }
          >
            Export deletion analysis
          </button>
          <dl className="property-list">
            <dt>Observed resource UID</dt>
            <dd className="mono">{query.data.uid || 'Not reported'}</dd>
            <dt>Deletion timestamp</dt>
            <dd>{query.data.deletion.timestamp || 'Not marked for deletion'}</dd>
            <dt>Time past deletion timestamp</dt>
            <dd>
              {query.data.deletion.secondsPastTimestamp === null
                ? 'Not applicable / unknown'
                : `${query.data.deletion.secondsPastTimestamp.toFixed(0)} seconds`}
            </dd>
            <dt>Time until deletion timestamp</dt>
            <dd>
              {query.data.deletion.secondsUntilTimestamp === null
                ? 'Not applicable / unknown'
                : `${query.data.deletion.secondsUntilTimestamp.toFixed(0)} seconds`}
            </dd>
            <dt>Reported deletion grace period</dt>
            <dd>{query.data.deletion.deletionGraceSeconds ?? 'Not reported'} seconds</dd>
            {kind === 'Pod' && (
              <>
                <dt>Configured Pod grace period</dt>
                <dd>{query.data.deletion.configuredGraceSeconds ?? 'Not reported'} seconds</dd>
              </>
            )}
          </dl>
          <p className="footnote">
            The timestamp may be a graceful-deletion deadline. Time past it is not the duration
            since the delete request; the grace period is not added again. Duration alone does not
            establish that deletion is stuck.
          </p>
          <h3>Finalization requirements</h3>
          <Finalizers data={query.data.deletion} />
          <h3>Related identities and declared dependencies</h3>
          <label>
            <input
              type="checkbox"
              checked={deletingOnly}
              onChange={(e) => setDeletingOnly(e.target.checked)}
            />{' '}
            Show only related resources marked for deletion
          </label>
          <TableScroll>
            <table aria-label="Deletion dependency evidence">
              <thead>
                <tr>
                  <th>Resource / relationship</th>
                  <th>Identity evidence</th>
                  <th>Deletion / finalizers</th>
                  <th>Observed checks</th>
                </tr>
              </thead>
              <tbody>
                {query.data.relations
                  .filter((row) => !deletingOnly || !!row.deletion?.timestamp)
                  .map((row, i) => (
                    <tr key={`${row.kind}/${row.namespace}/${row.name}/${row.relation}/${i}`}>
                      <td>
                        {row.resource ? (
                          <ResourceLink resource={row.resource} />
                        ) : (
                          `${row.kind}/${row.namespace}/${row.name}`
                        )}
                        <p>{row.relation}</p>
                      </td>
                      <td>
                        {row.state}
                        {row.uid && <p className="mono">{row.uid}</p>}
                      </td>
                      <td>
                        {row.deletion ? (
                          <>
                            <p>
                              {row.deletion.state} ·{' '}
                              {row.deletion.timestamp || 'No deletion timestamp'}
                            </p>
                            <Finalizers data={row.deletion} />
                          </>
                        ) : (
                          'Unverified identity'
                        )}
                      </td>
                      <td>
                        <CheckList checks={row.checks} />
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </TableScroll>
          {!query.data.relations.length && (
            <Empty
              title="No related identities observed"
              description="This does not establish absence of dependencies or completion of external cleanup."
            />
          )}
          {!!query.data.collections.length && (
            <>
              <h3>Inventory coverage</h3>
              <p>
                Counts cover the supported bounded lists only. Complete for one listed kind does not
                establish a complete namespace inventory.
              </p>
              <TableScroll>
                <table aria-label="Deletion inventory coverage">
                  <thead>
                    <tr>
                      <th>Kind</th>
                      <th>Observed in bounded list</th>
                      <th>Collection coverage</th>
                    </tr>
                  </thead>
                  <tbody>
                    {query.data.collections.map((collection) => (
                      <tr key={collection.kind}>
                        <td>{collection.kind}</td>
                        <td>{collection.observed ?? 'Not scanned'}</td>
                        <td>
                          {collection.complete
                            ? 'Complete for this listed kind'
                            : 'Incomplete / unavailable'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </TableScroll>
            </>
          )}
          <h3>Retained related events</h3>
          {query.data.events.map((event) => (
            <EventCard key={event.id} event={event} />
          ))}
          {!query.data.events.length && (
            <p>No related events were retained in the readable scope.</p>
          )}
        </>
      )}
    </Panel>
  );
}
