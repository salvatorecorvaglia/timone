import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { Resource, DriftReport } from '../types';
import { request } from '../lib/api';
import { ErrorState, ResourceLink, Warnings } from '../components/ui';
export function GitOpsDrift({ resource }: { resource: Resource }) {
  return <DriftInvestigation key={resource.id} resource={resource} />;
}
function DriftInvestigation({ resource }: { resource: Resource }) {
  const [params, setParams] = useSearchParams();
  const [data, setData] = useState<DriftReport | null>(null);
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  const [error, setError] = useState<Error | null>(null);
  const selected = params.get('drift') === resource.id;
  return (
    <div className="integration-evidence">
      <button
        className="button"
        disabled={busy}
        aria-label={`Compare live drift for ${resource.name}`}
        onClick={() => {
          controller.current?.abort();
          controller.current = new AbortController();
          setBusy(true);
          setError(null);
          setData(null);
          setParams((prev) => {
            const next = new URLSearchParams(prev);
            next.set('drift', resource.id);
            return next;
          });
          void request<DriftReport>(
            `/api/v1/gitops/${encodeURIComponent(resource.kind)}/${encodeURIComponent(resource.namespace)}/${encodeURIComponent(resource.name)}/drift`,
            { signal: controller.current.signal },
          )
            .then(setData)
            .catch(setError)
            .finally(() => setBusy(false));
        }}
      >
        {busy ? 'Comparing target and live state…' : 'Compare live drift'}
      </button>
      {selected && (
        <>
          {error && <ErrorState error={error} />}
          {data && (
            <section aria-label={`Drift for ${resource.name}`}>
              <h3>{data.state}</h3>
              <p>
                {data.mode === 'demo' ? 'SIMULATED · ' : ''}
                {data.source} · Target {data.revision} · {data.checked}/{data.total} resources
                checked
              </p>
              <Warnings items={data.warnings} />
              {data.changesTruncated && (
                <p className="data-warning">
                  Only the first 1,000 differences are shown. Narrow the investigation for complete
                  results.
                </p>
              )}
              <div className="table-scroll">
                <table aria-label="Desired versus live drift">
                  <thead>
                    <tr>
                      <th>Resource</th>
                      <th>Field</th>
                      <th>Change</th>
                      <th>Desired</th>
                      <th>Live</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.changes.map((change, i) => (
                      <tr key={i}>
                        <td>
                          <ResourceLink resource={change.resource} />
                        </td>
                        <td>
                          <code>{change.path}</code>
                        </td>
                        <td>{change.change}</td>
                        <td>
                          <pre>{JSON.stringify(change.desired, null, 2)}</pre>
                        </td>
                        <td>
                          <pre>{JSON.stringify(change.actual, null, 2)}</pre>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {!data.changes.length && (
                <p>
                  No differences observed in the compared visible fields. Coverage warnings and
                  excluded fields still apply.
                </p>
              )}
              <p className="footnote">{data.limitations}</p>
            </section>
          )}
        </>
      )}
    </div>
  );
}
