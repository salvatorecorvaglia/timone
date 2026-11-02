import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { request } from '../lib/api';
import { historyRows } from '../lib/history';
import type { Incident, MetricHistory, StorageInvestigation, WorkloadLogs } from '../types';
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

export function saveInvestigation(filename: string, text: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function localTime(value: string) {
  const t = new Date(value);
  if (!Number.isFinite(t.getTime())) return '';
  return new Date(t.getTime() - t.getTimezoneOffset() * 60000).toISOString().slice(0, 19);
}
export function useInvestigationWindow() {
  const [params, setParams] = useSearchParams();
  const [initial] = useState(() => {
    const end = new Date();
    return { since: new Date(end.getTime() - 3600000).toISOString(), until: end.toISOString() };
  });
  const since = params.get('since') ?? initial.since;
  const until = params.get('until') ?? initial.until;
  useEffect(() => {
    if (params.has('since') && params.has('until')) return;
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (!next.has('since')) next.set('since', initial.since);
        if (!next.has('until')) next.set('until', initial.until);
        return next;
      },
      { replace: true },
    );
  }, [params, initial, setParams]);

  const [draftSince, setSince] = useState(localTime(since));
  const [draftUntil, setUntil] = useState(localTime(until));
  useEffect(() => {
    setSince(localTime(since));
    setUntil(localTime(until));
  }, [since, until]);
  const start = new Date(since).getTime();
  const end = new Date(until).getTime();
  const valid =
    Number.isFinite(start) &&
    Number.isFinite(end) &&
    start < end &&
    end - start <= 86400000 &&
    end <= Date.now() + 1000;
  const controls = (
    <form
      className="filterbar"
      onSubmit={(event) => {
        event.preventDefault();
        const a = new Date(draftSince),
          b = new Date(draftUntil);
        if (!Number.isFinite(a.getTime()) || !Number.isFinite(b.getTime())) return;
        const next = new URLSearchParams(params);
        next.set('since', a.toISOString());
        next.set('until', b.toISOString());
        setParams(next);
      }}
    >
      <label>
        From{' '}
        <input
          aria-label="Investigation from"
          required
          type="datetime-local"
          step="1"
          value={draftSince}
          onChange={(e) => setSince(e.target.value)}
        />
      </label>
      <label>
        To{' '}
        <input
          aria-label="Investigation to"
          required
          type="datetime-local"
          step="1"
          value={draftUntil}
          onChange={(e) => setUntil(e.target.value)}
        />
      </label>
      <button className="button" type="submit">
        Apply interval
      </button>
      {!valid && <p role="alert">Select a past interval of up to 24 hours.</p>}
    </form>
  );
  return {
    since,
    until,
    valid,
    controls,
    freezeEnd: () =>
      setParams((previous) => {
        const next = new URLSearchParams(previous);
        next.set('until', new Date().toISOString());
        return next;
      }),
  };
}
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
                          stroke={['#79b8ff', '#66d9a5', '#ffbe69'][i % 3]}
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
function LogResults({ data, search }: { data: WorkloadLogs; search: string }) {
  const lines = data.lines.filter((line) =>
    `${line.pod} ${line.container} ${line.text}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <Panel title="Aggregated container logs" subtitle={`${data.window.start} — ${data.window.end}`}>
      <Warnings items={data.warnings} />
      {data.truncated && (
        <p role="status">Output or source selection was truncated. Narrow the selection.</p>
      )}
      <TableScroll>
        <table aria-label="Log source coverage">
          <thead>
            <tr>
              <th>Pod</th>
              <th>Container</th>
              <th>State</th>
              <th>Coverage</th>
            </tr>
          </thead>
          <tbody>
            {data.sources.map((s) => (
              <tr key={s.pod + s.container}>
                <td>{s.pod}</td>
                <td>{s.container}</td>
                <td>{s.state}</td>
                <td>
                  {s.message} {s.truncated && 'Output truncated'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </TableScroll>
      <button
        className="button"
        onClick={() =>
          saveInvestigation(
            'workload.log',
            lines.map((l) => `${l.timestamp ?? ''} [${l.pod}/${l.container}] ${l.text}`).join('\n'),
            'text/plain',
          )
        }
      >
        Download retrieved logs
      </button>
      <pre className="code-block logs">
        {lines.map((l) => `${l.timestamp ?? ''} [${l.pod}/${l.container}] ${l.text}`).join('\n') ||
          'No matching timestamped lines in this interval.'}
      </pre>
    </Panel>
  );
}
export function WorkloadLogInvestigation(target: Target) {
  const window = useInvestigationWindow();
  const [params, setParams] = useSearchParams();
  const updateLog = (key: string, value: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  const container = params.get('logContainer') ?? '';
  const setContainer = (value: string) => updateLog('logContainer', value);
  const previous = params.get('logPrevious') === 'true';
  const setPrevious = (value: boolean) => updateLog('logPrevious', value ? 'true' : '');
  const requestedTail = params.get('logTail') ?? '100';
  const tail = ['100', '500', '1000'].includes(requestedTail) ? requestedTail : '100';
  const setTail = (value: string) => updateLog('logTail', value);
  const [live, setLive] = useState(false);
  const [search, setSearch] = useState('');
  type Source = { pod: string; uid: string; container: string };
  const [picker, setPicker] = useState(false);
  let selected: Source[] | null = null;
  let validSelection = true;
  const rawSelection = params.get('logSources');
  if (rawSelection !== null) {
    try {
      const parsed = JSON.parse(rawSelection);
      if (
        rawSelection.length > 8192 ||
        !Array.isArray(parsed) ||
        !parsed.length ||
        parsed.length > 12 ||
        !parsed.every(
          (source) =>
            source &&
            typeof source.pod === 'string' &&
            typeof source.uid === 'string' &&
            typeof source.container === 'string',
        )
      )
        throw new Error('Invalid selection');
      selected = parsed;
    } catch {
      validSelection = false;
    }
  }
  const setSelected = (sources: Source[] | null) =>
    updateLog('logSources', sources ? JSON.stringify(sources) : '');
  const [draft, setDraft] = useState<Source[]>(() => selected ?? []);
  const [sourceSearch, setSourceSearch] = useState('');
  const sourceKey = (source: Source) => JSON.stringify([source.pod, source.uid, source.container]);
  const catalogPath = `${base(target.kind, target.namespace, target.name)}/log-sources?${new URLSearchParams({ container })}`;
  const catalog = useQuery({
    queryKey: [catalogPath],
    queryFn: ({ signal }) =>
      request<{ sources: Source[]; warnings: import('../types').Warning[]; truncated: boolean }>(
        catalogPath,
        { signal },
      ),
    enabled: picker,
    retry: false,
  });
  const path = `${base(target.kind, target.namespace, target.name)}/logs?${new URLSearchParams({ since: window.since, ...(live && !previous ? {} : { until: window.until }), container: selected ? '' : container, tail, previous: String(previous), ...(selected ? { sources: JSON.stringify(selected) } : {}) })}`;
  const query = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<WorkloadLogs>(path, { signal }),
    enabled: window.valid && validSelection,
    retry: false,
    refetchInterval: live && !previous ? 5000 : false,
  });
  return (
    <>
      <Panel
        title="Workload logs"
        subtitle="Verified current controller descendants. Up to 12 sources, four concurrent reads, 64 KiB per source and 5000 lines."
      >
        {window.controls}
        {!validSelection && (
          <p role="alert">
            Invalid source selection in URL. Choose sources again or use automatic selection.
          </p>
        )}
        <details onToggle={(event) => setPicker(event.currentTarget.open)}>
          <summary>Select Pod/container sources</summary>
          <p>
            {selected
              ? `${selected.length} explicit sources selected`
              : 'Automatic selection: first 12 sources'}
            . Selections are bound to current Pod UIDs.
          </p>
          <input
            aria-label="Filter source catalog"
            placeholder="Find a Pod or container"
            value={sourceSearch}
            onChange={(e) => setSourceSearch(e.target.value)}
          />
          <button
            className="button"
            disabled={catalog.isFetching}
            onClick={() => void catalog.refetch()}
          >
            Refresh source list
          </button>
          {catalog.isFetching && <Loading />}
          {catalog.error && <ErrorState error={catalog.error} />}
          <Warnings items={catalog.data?.warnings} />
          {catalog.data?.truncated && <p>Source catalog truncated. Narrow the container filter.</p>}
          <ul>
            {catalog.data?.sources
              .filter((source) =>
                `${source.pod} ${source.container}`
                  .toLowerCase()
                  .includes(sourceSearch.toLowerCase()),
              )
              .map((source) => {
                const checked = draft.some((entry) => sourceKey(entry) === sourceKey(source));
                return (
                  <li key={sourceKey(source)}>
                    <label>
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={!checked && draft.length >= 12}
                        onChange={() =>
                          setDraft(
                            checked
                              ? draft.filter((entry) => sourceKey(entry) !== sourceKey(source))
                              : [...draft, source],
                          )
                        }
                      />
                      {source.pod}/{source.container}
                    </label>
                  </li>
                );
              })}
          </ul>
          <p>{draft.length}/12 sources chosen for the next retrieval.</p>
          {draft.some(
            (source) =>
              !catalog.data?.sources.some((entry) => sourceKey(entry) === sourceKey(source)),
          ) && (
            <p role="status">
              Some chosen sources are absent from this catalog. Clear the selection or adjust the
              filter before applying.
            </p>
          )}
          <button
            className="button"
            disabled={
              !draft.length ||
              draft.some(
                (source) =>
                  !catalog.data?.sources.some((entry) => sourceKey(entry) === sourceKey(source)),
              )
            }
            onClick={() => setSelected([...draft])}
          >
            Apply selected sources
          </button>
          <button
            className="button"
            onClick={() => {
              setSelected(null);
              setDraft([]);
            }}
          >
            Use automatic source selection
          </button>
          <button className="button" onClick={() => setDraft([])}>
            Clear chosen sources
          </button>
        </details>
        <label>
          Container{' '}
          <input
            aria-label="Workload container"
            maxLength={63}
            value={container}
            onChange={(e) => setContainer(e.target.value)}
            placeholder="All containers"
          />
        </label>
        <label>
          <input
            type="checkbox"
            checked={previous}
            onChange={(e) => setPrevious(e.target.checked)}
          />{' '}
          Previous instances
        </label>
        <select
          aria-label="Lines per source"
          value={tail}
          onChange={(e) => setTail(e.target.value)}
        >
          {['100', '500', '1000'].map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
        <label>
          <input
            type="checkbox"
            checked={live}
            disabled={previous}
            onChange={(e) => {
              if (!e.target.checked) window.freezeEnd();
              setLive(e.target.checked);
            }}
          />{' '}
          Live refresh (5 seconds; moving end time)
        </label>
        <input
          aria-label="Search aggregated logs"
          placeholder="Filter retrieved lines"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button
          className="button"
          disabled={!window.valid || !validSelection || query.isFetching}
          onClick={() => void query.refetch()}
        >
          Reload workload logs
        </button>
        <p className="footnote">
          Application logs may contain sensitive information. Pausing stops future refreshes. Pod
          membership is re-read each time; missing sources remain visible.
        </p>
      </Panel>
      {query.isPending && window.valid && <Loading />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && <LogResults data={query.data} search={search} />}
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
