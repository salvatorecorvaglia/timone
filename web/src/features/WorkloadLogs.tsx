import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useInvestigationWindow } from '../lib/useInvestigationWindow';
import { request } from '../lib/api';
import { saveInvestigation } from '../lib/download';
import type { WorkloadLogs } from '../types';
import { ErrorState, Loading, Panel, TableScroll, Warnings } from '../components/ui';

function base(kind: string, namespace: string, name: string) {
  return `/api/v1/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace || '_')}/${encodeURIComponent(name)}`;
}
type Target = { kind: string; namespace: string; name: string };

export function LogResults({ data, search }: { data: WorkloadLogs; search: string }) {
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
