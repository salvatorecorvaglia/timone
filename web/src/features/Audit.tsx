import { ScopedLink as Link } from '../components/navigation';
import { EffectiveAuthorization } from './EffectiveAuthorization';

import { AlertTriangle, Check, Info, ShieldCheck } from 'lucide-react';
import { useAPI } from '../lib/api';
import type { Finding, Warning } from '../types';
import {
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Status,
  Warnings,
} from '../components/ui';
import { Filters } from './Filters';
import { useFilters } from '../lib/filters';

export function Audit({ namespace }: { namespace: string }) {
  const data = useAPI<{
    items: Finding[];
    warnings: Warning[];
    performed: string[];
    unavailable: string[];
    disclaimer: string;
  }>(`/api/v1/audit?namespace=${encodeURIComponent(namespace)}`);
  const { search, status, update } = useFilters();
  const items =
    data.data?.items.filter(
      (f) =>
        (!status || f.severity === status) &&
        `${f.rule} ${f.resource.name} ${f.evidence}`.toLowerCase().includes(search.toLowerCase()),
    ) ?? [];
  return (
    <>
      <PageTitle
        eyebrow="CONFIGURATION, WITH CONTEXT"
        title="Security & Audit"
        description="Practical configuration checks with evidence and recommendations, scoped to readable resources."
        action={
          <div className="grant-actions">
            <span className="tag">
              <ShieldCheck size={13} />
              Read-only checks
            </span>
            <Link
              className="button"
              to={`/resources?kind=ServiceAccount${namespace ? `&namespace=${encodeURIComponent(namespace)}` : ''}`}
            >
              Browse ServiceAccounts
            </Link>
          </div>
        }
      />
      <EffectiveAuthorization namespace={namespace} />
      <Warnings items={data.data?.warnings} />
      {data.data && (
        <div className="audit-summary">
          <div>
            <ShieldCheck size={22} />
            <strong>{data.data.performed.length}</strong>
            <span>check families performed</span>
          </div>
          <div>
            <AlertTriangle size={22} />
            <strong>{data.data.items.length}</strong>
            <span>configuration findings</span>
          </div>
          <div>
            <Info size={22} />
            <strong>{data.data.unavailable.length}</strong>
            <span>checks unavailable</span>
          </div>
        </div>
      )}
      <Filters
        search={search}
        status={status}
        onSearch={(v) => update('search', v)}
        onStatus={(v) => update('status', v)}
        placeholder="Search findings, rules, or resources…"
        info
      />
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} />
      ) : (
        <>
          <Panel
            title="Configuration findings"
            action={<span className="count-label">{items.length}</span>}
          >
            <div className="table-scroll">
              <table className="audit-table">
                <thead>
                  <tr>
                    <th>Severity</th>
                    <th>Rule / resource</th>
                    <th>Evidence</th>
                    <th>Recommendation</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((f) => (
                    <tr key={f.id}>
                      <td>
                        <Status value={f.severity} />
                      </td>
                      <td>
                        <strong>{f.rule.replaceAll('-', ' ')}</strong>
                        <ResourceLink resource={f.resource} />
                      </td>
                      <td>{f.evidence}</td>
                      <td>{f.recommendation}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!items.length && <Empty title="No matching findings" />}
            </div>
          </Panel>
          <Panel title="Check coverage" className="coverage-panel">
            <div>
              <h3>Performed on readable resources</h3>
              <div className="check-chips">
                {data.data.performed.map((c) => (
                  <span key={c}>
                    <Check size={12} />
                    {c}
                  </span>
                ))}
              </div>
            </div>
            <div>
              <h3>Unavailable in this version</h3>
              <ul>
                {data.data.unavailable.map((c) => (
                  <li key={c}>{c}</li>
                ))}
              </ul>
            </div>
          </Panel>
          <p className="footnote">{data.data.disclaimer}</p>
        </>
      )}
    </>
  );
}
