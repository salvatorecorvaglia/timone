import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { request } from '../lib/api';
import { compareSnapshots, parseSnapshot, type ResourceSnapshot } from '../lib/snapshots';
import { saveInvestigation } from '../lib/download';
import { ErrorState, PageTitle, Panel, TableScroll, Warnings } from '../components/ui';

export function Snapshots({ namespace }: { namespace: string }) {
  const [params] = useSearchParams();
  const [kind, setKind] = useState(params.get('kind') ?? 'Deployment');
  const [scope, setScope] = useState(params.get('resourceNamespace') ?? namespace);
  const [name, setName] = useState(params.get('name') ?? '');
  const [current, setCurrent] = useState<ResourceSnapshot>();
  const [before, setBefore] = useState<ResourceSnapshot>();
  const [after, setAfter] = useState<ResourceSnapshot>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error>();
  useEffect(() => {
    setKind(params.get('kind') ?? 'Deployment');
    setScope(params.get('resourceNamespace') ?? namespace);
    setName(params.get('name') ?? '');
  }, [params, namespace]);
  const imports = useRef({ before: 0, after: 0 });
  const { comparison, comparisonError } = useMemo(() => {
    try {
      return { comparison: before && after ? compareSnapshots(before, after) : undefined };
    } catch (e) {
      return { comparisonError: e instanceof Error ? e : new Error('Comparison failed') };
    }
  }, [before, after]);
  const load = async (file: File | undefined, side: 'before' | 'after') => {
    if (!file) return;
    const generation = ++imports.current[side];
    setError(undefined);
    try {
      if (file.size > 2 * 1024 * 1024) throw new Error('Snapshot exceeds 2 MiB.');
      const parsed = parseSnapshot(await file.text());
      if (generation !== imports.current[side]) return;
      (side === 'before' ? setBefore : setAfter)(parsed);
    } catch (e) {
      if (generation !== imports.current[side]) return;
      (side === 'before' ? setBefore : setAfter)(undefined);
      setError(e instanceof Error ? e : new Error('Cannot read snapshot'));
    }
  };
  return (
    <>
      <PageTitle
        title="Snapshots & comparison"
        description="Capture selected configuration and conditions. Compare downloaded files locally, including while the cluster is unavailable."
      />
      <Panel title="Capture current resource and descendants">
        <form
          className="filterbar"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError(undefined);
            setCurrent(undefined);
            try {
              const data = await request<ResourceSnapshot>(
                `/api/v1/resources/${encodeURIComponent(kind)}/${encodeURIComponent(scope || '_')}/${encodeURIComponent(name)}/snapshot`,
              );
              setCurrent(parseSnapshot(JSON.stringify(data)));
            } catch (e) {
              setError(e instanceof Error ? e : new Error('Capture failed'));
            } finally {
              setBusy(false);
            }
          }}
        >
          <label>
            Kind{' '}
            <input
              aria-label="Snapshot resource kind"
              required
              value={kind}
              onChange={(e) => setKind(e.target.value)}
            />
          </label>
          <label>
            Namespace{' '}
            <input
              aria-label="Snapshot namespace"
              value={scope}
              onChange={(e) => setScope(e.target.value)}
            />
          </label>
          <label>
            Name{' '}
            <input
              aria-label="Snapshot resource name"
              required
              maxLength={253}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <button className="button" disabled={busy}>
            Capture snapshot
          </button>
        </form>
        <p className="footnote">
          Secret/ConfigMap payloads, commands, environment values and probe paths are excluded.
          Files remain on your device; imported snapshots are never sent to the server.
        </p>
        {error && <ErrorState error={error} />}
        {current && (
          <>
            <p>
              {current.root} · {current.capturedAt} · {current.resources.length} resources ·{' '}
              {current.mode}
            </p>
            <Warnings items={current.warnings} />
            {current.limitations.map((text) => (
              <p className="footnote" key={text}>
                {text}
              </p>
            ))}
            <button
              className="button"
              onClick={() =>
                saveInvestigation(
                  'snapshot.json',
                  JSON.stringify(current, null, 2),
                  'application/json',
                )
              }
            >
              Download snapshot
            </button>
            <button className="button" onClick={() => setBefore(current)}>
              Use as before
            </button>
            <button className="button" onClick={() => setAfter(current)}>
              Use as after
            </button>
          </>
        )}
      </Panel>
      <Panel title="Offline comparison">
        <label>
          Before snapshot{' '}
          <input
            aria-label="Before snapshot file"
            type="file"
            accept=".json,application/json"
            onChange={(e) => void load(e.target.files?.[0], 'before')}
          />
        </label>
        <label>
          After snapshot{' '}
          <input
            aria-label="After snapshot file"
            type="file"
            accept=".json,application/json"
            onChange={(e) => void load(e.target.files?.[0], 'after')}
          />
        </label>
        {before && (
          <p>
            Before: {before.cluster} · {before.root} · {before.capturedAt}
          </p>
        )}
        {after && (
          <p>
            After: {after.cluster} · {after.root} · {after.capturedAt}
          </p>
        )}
        {!!(before?.warnings.length || after?.warnings.length) && (
          <p role="status">
            At least one capture has incomplete coverage. “Not observed” does not establish addition
            or deletion.
          </p>
        )}
        {before &&
          after &&
          (before.clusterKey !== after.clusterKey || before.root !== after.root) && (
            <p role="status">
              These captures have different cluster or root identities. Compare matching resource
              identities with care.
            </p>
          )}
        {comparisonError && <ErrorState error={comparisonError} />}
        {comparison && (
          <>
            <p>
              {comparison.rows.length} differences shown
              {comparison.omitted > 0 && ` · ${comparison.omitted} additional differences omitted`}
            </p>
            <TableScroll>
              <table aria-label="Snapshot differences">
                <thead>
                  <tr>
                    <th>Resource</th>
                    <th>Field</th>
                    <th>Before</th>
                    <th>After</th>
                  </tr>
                </thead>
                <tbody>
                  {comparison.rows.map((r) => (
                    <tr key={r.resource + r.path}>
                      <td>{r.resource}</td>
                      <td>{r.path}</td>
                      <td>
                        <pre>{r.before}</pre>
                      </td>
                      <td>
                        <pre>{r.after}</pre>
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
                  'snapshot-comparison.json',
                  JSON.stringify(
                    {
                      schemaVersion: 1,
                      before: before?.capturedAt,
                      after: after?.capturedAt,
                      ...comparison,
                    },
                    null,
                    2,
                  ),
                  'application/json',
                )
              }
            >
              Download comparison
            </button>
          </>
        )}
      </Panel>
    </>
  );
}
