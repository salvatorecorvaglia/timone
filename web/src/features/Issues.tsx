import { ScopedLink as Link } from '../components/navigation';
import { useNavigate, useParams } from 'react-router-dom';
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Check,
  Clock3,
  Info,
  ShieldCheck,
  Terminal,
  X,
} from 'lucide-react';
import { ago, resourceURL, useAPI } from '../lib/api';
import type { DiagnosticPolicy, CustomDiagnosticCoverage, Issue, Warning } from '../types';
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
import { EventCard } from './EventCard';
import { Filters } from './Filters';
import { useFilters } from '../lib/filters';

export function Issues({ namespace }: { namespace: string }) {
  const data = useAPI<{
    items: Issue[];
    warnings: Warning[];
    diagnosticPolicy: DiagnosticPolicy;
    customDiagnostics: CustomDiagnosticCoverage;
  }>(`/api/v1/issues?namespace=${encodeURIComponent(namespace)}`);
  const { id } = useParams();
  const navigate = useNavigate();
  const { p, search, status, update } = useFilters();
  const selected = data.data?.items.find((i) => i.id === id);
  const items =
    data.data?.items.filter(
      (i) =>
        (!status || i.severity === status) &&
        `${i.title} ${i.resource.kind} ${i.resource.definition ?? ''} ${i.resource.name} ${i.namespace}`
          .toLowerCase()
          .includes(search.toLowerCase()),
    ) ?? [];
  return (
    <>
      <PageTitle
        eyebrow="FROM SIGNAL TO UNDERSTANDING"
        title="Issues"
        description="Observed failures, correlated evidence, and a clear path to investigation."
        action={
          <span className="tag">
            <ShieldCheck size={13} />
            Deterministic diagnostics
          </span>
        }
      />
      {data.data && (
        <div className="issue-summary">
          <span className="critical">
            <i />
            {data.data.items.filter((i) => i.severity === 'critical').length} critical
          </span>
          <span className="warning">
            <i />
            {data.data.items.filter((i) => i.severity === 'warning').length} warnings
          </span>
          <span className="muted">Grouped by observed resource ownership</span>
        </div>
      )}
      {data.data?.diagnosticPolicy && (
        <details className="diagnostic-policy">
          <summary>Active detection policy</summary>
          <dl>
            <div>
              <dt>Startup & readiness grace</dt>
              <dd>{data.data.diagnosticPolicy.startupGraceSeconds} seconds</dd>
            </div>
            <div>
              <dt>Recent warning events</dt>
              <dd>{data.data.diagnosticPolicy.warningEventWindowSeconds} seconds</dd>
            </div>
            <div>
              <dt>Cumulative Pod restarts</dt>
              <dd>At least {data.data.diagnosticPolicy.restartThreshold}</dd>
            </div>
          </dl>
          <p className="muted">
            Administrator settings apply across the dashboard, Issues and AI queries. Explicit
            crash, image-pull and controller failure signals are evaluated immediately. Changing
            these thresholds requires restarting Timone.
          </p>
        </details>
      )}
      {data.data?.customDiagnostics && (
        <details className="diagnostic-policy">
          <summary>Custom resource coverage · {data.data.customDiagnostics.state}</summary>
          <p className="muted">{data.data.customDiagnostics.message}</p>
          <dl>
            <div>
              <dt>Types inspected</dt>
              <dd>{data.data.customDiagnostics.typesInspected}</dd>
            </div>
            <div>
              <dt>Resources observed</dt>
              <dd>{data.data.customDiagnostics.resourcesObserved}</dd>
            </div>
            <div>
              <dt>Readiness unverified</dt>
              <dd>{data.data.customDiagnostics.readinessUnknown}</dd>
            </div>
            <div>
              <dt>Issue candidates omitted</dt>
              <dd>{data.data.customDiagnostics.candidatesOmitted}</dd>
            </div>
          </dl>
          <p className="muted">
            Limited to administrator-enabled custom APIs and existing read permissions. Stale,
            missing or ambiguous conditions cannot establish current readiness. Ready=False uses the
            active grace period. Recent UID-matched warnings can create separate evidence. At most{' '}
            {data.data.customDiagnostics.candidateLimit} custom issue candidates are retained.
          </p>
        </details>
      )}
      <Warnings items={data.data?.warnings} />
      <Filters
        search={search}
        status={status}
        onSearch={(v) => update('search', v)}
        onStatus={(v) => update('status', v)}
        placeholder="Search issues or affected resources…"
      />
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <ErrorState error={data.error} />
      ) : (
        <div className={`issues-layout ${id ? 'has-detail' : ''}`}>
          <div className="issue-cards">
            {items.map((i) => (
              <Link
                className={`issue-card ${selected?.id === i.id ? 'selected' : ''}`}
                to={`/issues/${encodeURIComponent(i.id)}?${p}`}
                key={i.id}
              >
                <div className="issue-card-heading">
                  <Status value={i.severity} />
                  <span>{i.namespace || 'Cluster scoped'}</span>
                  <ArrowUpRight size={15} />
                </div>
                <h2>{i.title}</h2>
                <div className="issue-resource">
                  {i.resource.kind}
                  <span>/</span>
                  <strong>{i.resource.name}</strong>
                </div>
                {i.resource.definition && <p className="mono muted">{i.resource.definition}</p>}
                <p>{i.evidence[0]}</p>
                <div className="issue-card-footer">
                  <span>
                    <Activity size={12} />
                    {i.evidence.length} observed signals · {i.events.length} related events
                  </span>
                  <span>{ago(i.lastObserved)}</span>
                </div>
              </Link>
            ))}
            {!items.length && (
              <Panel>
                <Empty
                  title="No matching issues"
                  description="No diagnostics match your filters. Collection warnings above describe any missing coverage."
                />
              </Panel>
            )}
          </div>
          {id && (
            <Panel
              className="issue-detail"
              title={selected ? 'Investigation' : 'Issue unavailable'}
              action={
                <button
                  aria-label="Close investigation"
                  className="icon-button"
                  onClick={() => navigate(`/issues?${p}`)}
                >
                  <X size={16} />
                </button>
              }
            >
              {selected ? (
                <>
                  <div className="investigation-header">
                    <Status value={selected.severity} />
                    <h2>{selected.title}</h2>
                    <ResourceLink resource={selected.resource} />
                  </div>
                  <div className="investigation-section">
                    <h3>
                      <Check size={15} />
                      Observed evidence
                    </h3>
                    <ul>
                      {selected.evidence.map((e) => (
                        <li key={e}>{e}</li>
                      ))}
                    </ul>
                  </div>
                  <div className="investigation-section">
                    <h3>
                      <Info size={15} />
                      Potential causes · hypotheses
                    </h3>
                    {selected.causes.map((c) => (
                      <p key={c}>{c}</p>
                    ))}
                  </div>
                  {!!selected.recentChanges?.length && (
                    <div className="investigation-section">
                      <h3>Recent observed changes</h3>
                      <ul>
                        {selected.recentChanges.map((c, i) => (
                          <li key={i}>{c}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  <div className="investigation-section">
                    <h3>
                      <Terminal size={15} />
                      Recommended next steps
                    </h3>
                    <ol>
                      {selected.recommendations.map((r) => (
                        <li key={r}>{r}</li>
                      ))}
                    </ol>
                  </div>
                  <div className="investigation-section">
                    <h3>
                      <Clock3 size={15} />
                      Related events
                    </h3>
                    {selected.events.map((e) => (
                      <EventCard key={e.id} event={e} />
                    ))}
                    {!selected.events.length && (
                      <p className="muted">No related events currently retained.</p>
                    )}
                  </div>
                  <div className="investigation-footer">
                    <Link className="button primary" to={resourceURL(selected.resource)}>
                      Inspect resource
                      <ArrowRight size={14} />
                    </Link>
                  </div>
                </>
              ) : (
                <Empty
                  title="This issue is no longer observed"
                  description="It may have resolved, moved outside the selected namespace, or become unavailable."
                />
              )}
            </Panel>
          )}
        </div>
      )}
      <p className="footnote">
        Diagnostics describe observed state. Potential causes are investigation hypotheses; they are
        not confirmed root causes. Grace periods apply to observed timestamps and cannot prove how
        long a failure has persisted. Restart counts are cumulative across Pod containers, not a
        rate. Collection warnings identify missing coverage.
      </p>
    </>
  );
}
