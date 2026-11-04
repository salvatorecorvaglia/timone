import { LogResults } from './WorkloadLogs';
import { useInvestigationWindow } from '../lib/useInvestigationWindow';
import { chartColors } from '../lib/chartColors';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { request } from '../lib/api';
import { historyRows } from '../lib/history';
import type { Incident, MetricHistory, StorageInvestigation } from '../types';
import {
  Empty,
  ErrorState,
  Loading,
  Panel,
  ResourceLink,
  TableScroll,
  Warnings,
} from '../components/ui';
import { RunbookLinks } from './RunbookLinks';
import { EventCard } from './EventCard';

export { saveInvestigation } from '../lib/download';
import { saveInvestigation } from '../lib/download';

function base(kind: string, namespace: string, name: string) {
  return `/api/v1/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace || '_')}/${encodeURIComponent(name)}`;
}
type Target = { kind: string; namespace: string; name: string };
function HistoryChart({ history }: { history: MetricHistory }) {
  return (
    <Panel
      title={`History · ${history.resource.kind}/${history.resource.name}`}
      subtitle={history.message}
    >
      <Warnings items={history.warnings} />
      {['cpu', 'memory'].map((metric) => {
        const rows = historyRows(history, metric);
        const series = history.series.filter((s) => s.metric === metric);
        return (
          <div key={metric}>
            <h4>{metric === 'cpu' ? 'CPU · cores' : 'Memory · MiB'}</h4>
            {series.length ? (
              <>
                <div
                  style={{ height: 180 }}
                  role="img"
                  aria-label={`${metric} historical samples for ${history.resource.name}`}
                >
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={rows}>
                      <XAxis
                        dataKey="timestamp"
                        tickFormatter={(value: number) =>
                          new Date(value * 1000).toLocaleTimeString()
                        }
                      />
                      <YAxis />
                      <Tooltip />
                      {series.map((s, i) => (
                        <Line
                          key={s.container || metric}
                          dataKey={`series${i}`}
                          name={s.container || metric}
                          stroke={chartColors[i % chartColors.length]}
                          dot={false}
                          connectNulls={false}
                        />
                      ))}
                    </LineChart>
                  </ResponsiveContainer>
                </div>
                <details>
                  <summary>Metric samples</summary>
                  <pre className="code-block">{JSON.stringify(series, null, 2)}</pre>
                </details>
              </>
            ) : (
              <p className="muted">No samples available for this interval.</p>
            )}
          </div>
        );
      })}
    </Panel>
  );
}
export function IncidentInvestigation(target: Target) {
  const window = useInvestigationWindow();
  const [includeLogs, setIncludeLogs] = useState(false);
  const path = `${base(target.kind, target.namespace, target.name)}/incident?${new URLSearchParams({ since: window.since, until: window.until, includeLogs: String(includeLogs) })}`;
  const query = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<Incident>(path, { signal }),
    enabled: window.valid,
    retry: false,
  });
  const data = query.data;
  return (
    <>
      <Panel
        title="Incident investigation"
        subtitle="One interval for events, observed changes, metrics and optional logs. Current conditions are labelled separately."
      >
        {window.controls}
        {['Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(
          target.kind,
        ) && (
          <label>
            <input
              type="checkbox"
              checked={includeLogs}
              onChange={(e) => setIncludeLogs(e.target.checked)}
            />{' '}
            Include application logs in investigation and exports
          </label>
        )}
        <button
          className="button"
          disabled={!window.valid || query.isFetching}
          onClick={() => void query.refetch()}
        >
          Refresh investigation
        </button>
      </Panel>
      {query.isFetching && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {data && !query.isFetching && (
        <>
          <Warnings items={data.warnings} />
          <Panel
            title="Evidence and coverage"
            subtitle={`Collected ${data.collectedAt} · ${data.cluster} · ${data.mode}`}
          >
            <p>
              {data.window.start} — {data.window.end}
            </p>
            {data.limitations.map((text) => (
              <p className="footnote" key={text}>
                {text}
              </p>
            ))}
            <button
              className="button"
              onClick={() =>
                saveInvestigation(
                  'incident.json',
                  JSON.stringify(data, null, 2),
                  'application/json',
                )
              }
            >
              Export incident JSON
            </button>
            <button
              className="button"
              onClick={() =>
                saveInvestigation(
                  'incident.md',
                  [
                    '# Incident investigation',
                    `Cluster: ${data.cluster} · Mode: ${data.mode}`,
                    `Collected: ${data.collectedAt}`,
                    `Interval: ${data.window.start} — ${data.window.end}`,
                    ...data.limitations,
                    '## Current problems',
                    ...data.issues.map((i) => `${i.title}: ${i.evidence.join('; ')}`),
                    '## Observed changes',
                    ...data.changes.map((c) => `${c.observedAt} ${c.resource.name}: ${c.evidence}`),
                    '## Events',
                    ...data.events.map((e) => `${e.timestamp} ${e.reason}: ${e.message}`),
                    '## Evidence bundle (JSON)',
                    '~~~json',
                    JSON.stringify(data, null, 2),
                    '~~~',
                  ].join('\n\n'),
                  'text/markdown',
                )
              }
            >
              Export incident Markdown
            </button>
          </Panel>
          <Panel title="Current conditions and problems">
            {data.issues.map((i) => (
              <div key={i.id}>
                <ResourceLink resource={i.resource} />
                <h4>{i.title}</h4>
                <RunbookLinks issue={i} />
                {i.evidence.map((e) => (
                  <p key={e}>{e}</p>
                ))}
              </div>
            ))}
            {!data.issues.length && (
              <Empty
                title="No current diagnostic signals"
                description="This does not establish past health or complete coverage."
              />
            )}
          </Panel>
          <Panel title="Timeline in selected interval">
            {[
              ...data.events.map((e) => ({
                id: e.id,
                at: e.timestamp,
                node: <EventCard event={e} />,
              })),
              ...data.changes.map((c) => ({
                id: c.id,
                at: c.observedAt,
                node: (
                  <p>
                    <ResourceLink resource={c.resource} /> · {c.type}: {c.evidence}
                  </p>
                ),
              })),
            ]
              .sort((a, b) => a.at.localeCompare(b.at))
              .map((entry) => (
                <div key={entry.id}>
                  <time>{entry.at}</time>
                  {entry.node}
                </div>
              ))}
            {!data.events.length && !data.changes.length && (
              <p>No retained evidence in this interval.</p>
            )}
          </Panel>
          <Panel title="Metric samples in selected interval">
            <TableScroll>
              <table>
                <thead>
                  <tr>
                    <th>Resource</th>
                    <th>Container</th>
                    <th>Sample time</th>
                    <th>CPU cores</th>
                    <th>Memory bytes</th>
                  </tr>
                </thead>
                <tbody>
                  {data.metrics.map((m, i) => (
                    <tr key={i}>
                      <td>
                        {m.kind}/{m.name}
                      </td>
                      <td>{m.container}</td>
                      <td>{m.timestamp}</td>
                      <td>{m.cpu}</td>
                      <td>{m.memory}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableScroll>
            {!data.metrics.length && <p>No current samples fall in this interval.</p>}
          </Panel>
          {data.history.map((h) => (
            <HistoryChart key={h.resource.id} history={h} />
          ))}
          {data.logs && <LogResults data={data.logs} search="" />}
        </>
      )}
    </>
  );
}
export function StorageDetails(target: Target) {
  const path = `${base(target.kind, target.namespace, target.name)}/storage`;
  const query = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<StorageInvestigation>(path, { signal }),
    retry: false,
  });
  return (
    <Panel
      title="Storage investigation"
      subtitle="Pod → PVC → PV → StorageClass and volume attachments"
    >
      <button className="button" disabled={query.isFetching} onClick={() => void query.refetch()}>
        Refresh storage
      </button>
      {query.isPending && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && (
        <>
          <Warnings items={query.data.warnings} />
          {query.data.limitations.map((text) => (
            <p className="footnote" key={text}>
              {text}
            </p>
          ))}
          {query.data.items.map((item) => (
            <div key={item.resource.id}>
              <h4>
                <ResourceLink resource={item.resource} /> · {item.resource.status}
              </h4>
              <dl className="property-list">
                {Object.entries(item.facts).map(([key, value]) => (
                  <div key={key}>
                    <dt>{key}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              </dl>
            </div>
          ))}
          <h4>Retained storage and mount events</h4>
          {query.data.events.map((event) => (
            <EventCard key={event.id} event={event} />
          ))}
          <details>
            <summary>Declared storage relationships</summary>
            <ul>
              {query.data.edges.map((edge) => (
                <li key={edge.id}>
                  {edge.source} → {edge.target} · {edge.relation}
                </li>
              ))}
            </ul>
          </details>
          {!query.data.items.length && <Empty title="No readable storage dependencies" />}
        </>
      )}
    </Panel>
  );
}
