import { ScopedLink as Link } from '../components/navigation';

import type { Allocation, LimitBudget } from '../types';
import { memory } from '../lib/api';
import { Empty, Panel } from '../components/ui';

function Capacity({
  requested,
  available,
  format,
}: {
  requested: number | null;
  available: number | null;
  format: (v: number) => string;
}) {
  return (
    <>
      <span className="mono">
        {requested == null ? 'Unavailable' : format(requested)} /{' '}
        {available == null ? 'Unavailable' : format(available)}
      </span>
      {requested != null && available != null && available > 0 && (
        <small className="table-subtitle">
          {((requested / available) * 100).toFixed(1)}% requested
        </small>
      )}
    </>
  );
}

function LimitCell({
  budget,
  available,
  format,
}: {
  budget?: LimitBudget;
  available: number | null;
  format: (v: number) => string;
}) {
  if (!budget || budget.state === 'unavailable') return <span className="muted">Unavailable</span>;
  return (
    <>
      <span className="mono">
        {budget.state === 'finite' && budget.total != null
          ? format(budget.total)
          : budget.state === 'incomplete'
            ? 'Incomplete limit coverage'
            : 'Unknown limit total'}
      </span>
      {budget.state === 'finite' && budget.total != null && available != null && available > 0 && (
        <small className={`table-subtitle ${budget.total > available ? 'warning' : ''}`}>
          {((budget.total / available) * 100).toFixed(1)}% of allocatable
          {budget.total > available && ' · Over allocatable'}
        </small>
      )}
      {budget.state === 'finite' && (available == null || available <= 0) && (
        <small className="table-subtitle">Allocatable comparison unavailable</small>
      )}
      {budget.state !== 'finite' &&
        budget.knownSubtotal != null &&
        (budget.finitePods ?? 0) > 0 && (
          <small className="table-subtitle">
            {format(budget.knownSubtotal)} from fully bounded Pods only
          </small>
        )}
      <small className="table-subtitle">
        {budget.finitePods ?? 'Unknown'} finite · {budget.missingPods ?? 'Unknown'} missing/zero ·{' '}
        {budget.invalidPods ?? 'Unknown'} invalid
      </small>
    </>
  );
}

export function NodeBudgets({ allocations }: { allocations?: Allocation[] }) {
  return (
    <Panel
      title="Configured requests, limits & capacity"
      subtitle="Current non-terminal Pod specs, grouped by assigned node. Independent of measured usage; unscheduled Pods are separate."
    >
      <div className="table-scroll">
        <table aria-label="Configured node resource budgets">
          <thead>
            <tr>
              <th>Node</th>
              <th>Active Pods</th>
              <th>CPU requests / allocatable</th>
              <th>Memory requests / allocatable</th>
              <th>CPU limit budget</th>
              <th>Memory limit budget</th>
              <th>Coverage</th>
            </tr>
          </thead>
          <tbody>
            {allocations?.map((a) => (
              <tr key={a.node}>
                <td>
                  {a.node ? (
                    <Link
                      className="resource-link"
                      to={`/resources/Node/_/${encodeURIComponent(a.node)}`}
                    >
                      {a.node}
                    </Link>
                  ) : (
                    'Unscheduled'
                  )}
                </td>
                <td>{a.pods ?? 'Unavailable'}</td>
                <td>
                  <Capacity
                    requested={a.cpuRequest}
                    available={a.cpuAllocatable}
                    format={(v) => `${v.toFixed(3)} cores`}
                  />
                </td>
                <td>
                  <Capacity
                    requested={a.memoryRequest}
                    available={a.memoryAllocatable}
                    format={memory}
                  />
                </td>
                <td>
                  <LimitCell
                    budget={a.cpuLimits}
                    available={a.cpuAllocatable}
                    format={(v) => `${v.toFixed(3)} cores`}
                  />
                </td>
                <td>
                  <LimitCell
                    budget={a.memoryLimits}
                    available={a.memoryAllocatable}
                    format={memory}
                  />
                </td>
                <td>{a.scope}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {!allocations?.length && (
          <Empty
            title="Allocation data unavailable"
            description="Pod and Node read permissions are required to calculate configured demand and allocatable capacity."
          />
        )}
      </div>
      <div className="panel-footnote">
        Finite totals require positive limits for every Pod in scope. Subtotals exclude Pods with
        missing or invalid limits. Configured limits above allocatable do not establish measured
        pressure or predict scheduling.
      </div>
    </Panel>
  );
}
