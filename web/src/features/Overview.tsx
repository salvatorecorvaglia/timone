import {
  ArrowDownRight,
  ArrowRight,
  ArrowUpRight,
  Box,
  CheckCircle2,
  Clock3,
  Layers3,
  RefreshCw,
  Server,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { ScopedLink as Link } from '../components/navigation';
import {
  TableScroll,
  Empty,
  ErrorState,
  Loading,
  PageTitle,
  Panel,
  ResourceLink,
  Status,
  Warnings,
} from '../components/ui';
import { collectionHealth } from '../lib/coverage';
import { ago, useAPI } from '../lib/api';
import type { Metrics, Overview as OverviewData } from '../types';

const palette = [
  'var(--healthy)',
  'var(--warning)',
  'var(--critical)',
  'var(--blue)',
  'var(--muted)',
];
export function Overview({ namespace }: { namespace: string }) {
  const query = useAPI<OverviewData>(`/api/v1/overview?namespace=${encodeURIComponent(namespace)}`);
  const metrics = useAPI<Metrics>(`/api/v1/metrics?namespace=${encodeURIComponent(namespace)}`);
  if (query.isPending) return <Loading />;
  if (query.error) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const d = query.data;
  const critical = d.issues.filter((i) => i.severity === 'critical').length;
  const readyNodes = d.nodes.filter((n) => n.health === 'healthy').length;
  const podData = Object.entries(d.podStates)
    .sort(([a], [b]) => (a === 'Running' ? -1 : b === 'Running' ? 1 : a.localeCompare(b)))
    .map(([name, value]) => ({ name, value }));
  const running = d.podStates.Running ?? 0;
  const cpu =
    metrics.data?.items
      .filter((m) => m.kind === 'Node')
      .map((m) => ({
        name: m.name.replace('harbor-', ''),
        cpu: Number(m.cpu.toFixed(2)),
        capacity: m.cpuCapacity,
      })) ?? [];
  const suffix = namespace ? `&namespace=${encodeURIComponent(namespace)}` : '';
  return (
    <>
      <PageTitle
        eyebrow="YOUR CLUSTER, AT A GLANCE"
        title="Cluster overview"
        description="A clear picture of what’s running, what’s changing, and what needs you."
        action={
          <button
            className="button"
            disabled={query.isFetching}
            onClick={() => {
              void query.refetch();
              void metrics.refetch();
            }}
          >
            <RefreshCw size={14} className={query.isFetching ? 'spin' : ''} />
            Refresh<span className="keyboard-hint">↻</span>
          </button>
        }
      />
      <div className="overview-meta">
        <span>
          <span className="status-dot healthy" /> {d.cluster}
        </span>
        <span>
          <Clock3 size={12} />
          Updated {ago(d.observedAt)}
        </span>
        <span className="tag">{d.scope || 'Cluster-wide'}</span>
      </div>
      <Link to="/issues" className="cluster-health-label">
        <Status value={d.health} label={`Cluster health: ${d.health}`} />
      </Link>
      <Warnings items={d.warnings} />
      {d.customDiagnostics && d.customDiagnostics.state !== 'disabled' && (
        <Link
          to={`/issues?${new URLSearchParams(namespace ? { namespace } : {})}`}
          className="panel-footnote"
        >
          Custom diagnostics: {d.customDiagnostics.state} · {d.customDiagnostics.resourcesObserved}{' '}
          resources observed · {d.customDiagnostics.readinessUnknown} with unverified readiness.
          Inspect coverage →
        </Link>
      )}
      {d.issues.length > 0 ? (
        <Link
          to={`/issues?namespace=${encodeURIComponent(namespace)}`}
          className="attention-banner"
        >
          <div className="attention-icon">
            <TriangleAlert size={21} />
          </div>
          <div>
            <strong>{d.issues.length} issues need your attention</strong>
            <p>
              {critical} critical · {d.issues.length - critical} warning
              {d.issues.length - critical === 1 ? '' : 's'} across your visible workloads
            </p>
          </div>
          <span>
            Investigate issues
            <ArrowRight size={16} />
          </span>
        </Link>
      ) : (
        <div className={d.health === 'healthy' ? 'healthy-banner' : 'manifest-notice'}>
          {d.health === 'healthy' ? <CheckCircle2 size={20} /> : <ShieldCheck size={20} />}
          <span>
            {d.warnings.length
              ? 'No issues detected in readable resources. Collection coverage is incomplete.'
              : d.health === 'healthy'
                ? 'Your visible cluster resources look healthy.'
                : `No issues detected. Current resource health: ${d.health}.`}
          </span>
        </div>
      )}
      <div className="stat-grid">
        <Stat
          icon={Server}
          label="Nodes"
          value={d.counts.Node ?? '—'}
          detail={d.counts.Node === undefined ? 'Collection unavailable' : `${readyNodes} ready`}
          health={collectionHealth('Node', d, readyNodes === d.counts.Node ? 'healthy' : 'warning')}
          href={`/resources?kind=Node${suffix}`}
        />
        <Stat
          icon={Box}
          label="Pods"
          value={d.counts.Pod ?? '—'}
          detail={d.counts.Pod === undefined ? 'Collection unavailable' : `${running} running`}
          health={collectionHealth('Pod', d, running === d.counts.Pod ? 'healthy' : 'warning')}
          href={`/resources?kind=Pod${suffix}`}
          secondary={`${(d.counts.Pod ?? 0) - running} not running`}
        />
        <Stat
          icon={Layers3}
          label="Deployments"
          value={d.counts.Deployment ?? '—'}
          detail={
            d.counts.Deployment === undefined
              ? 'Collection unavailable'
              : `${d.availableReplicas}/${d.desiredReplicas} replicas available`
          }
          health={collectionHealth(
            'Deployment',
            d,
            d.availableReplicas === d.desiredReplicas ? 'healthy' : 'warning',
          )}
          href={`/applications?namespace=${encodeURIComponent(namespace)}`}
        />
        <Stat
          icon={Layers3}
          label="Namespaces"
          value={d.counts.Namespace ?? '—'}
          detail={
            d.counts.Namespace === undefined
              ? 'Collection unavailable'
              : 'Visible to this connection'
          }
          health="unknown"
          href={`/resources?kind=Namespace${suffix}`}
        />
      </div>
      <div className="overview-chart-grid">
        <Panel
          title="Resource utilization"
          subtitle={
            metrics.data?.source === 'simulated'
              ? 'Current node CPU · simulated samples'
              : 'Current node CPU · Metrics Server'
          }
          action={
            <Link className="text-link" to="/metrics">
              Explore metrics
              <ArrowUpRight size={14} />
            </Link>
          }
        >
          <div className="chart-summary">
            <div>
              <span className="chart-key" />
              CPU usage
              <strong>
                {cpu.length
                  ? `${cpu.reduce((a, m) => a + m.cpu, 0).toFixed(2)} cores`
                  : 'Unavailable'}
              </strong>
            </div>
            <span className="chart-caption">Current samples · no history</span>
          </div>
          {metrics.error ? (
            <ErrorState error={metrics.error} />
          ) : metrics.data && !metrics.data.available ? (
            <Empty title="Metrics Server unavailable" description={metrics.data.message} />
          ) : cpu.length ? (
            <div className="cpu-chart">
              <ResponsiveContainer width="100%" height={174}>
                <BarChart
                  data={cpu}
                  margin={{ left: -27, right: 24, top: 12, bottom: 0 }}
                  barSize={34}
                >
                  <CartesianGrid stroke="var(--border)" vertical={false} />
                  <XAxis
                    dataKey="name"
                    axisLine={false}
                    tickLine={false}
                    tick={{ fill: 'var(--muted)', fontSize: 11 }}
                  />
                  <YAxis
                    axisLine={false}
                    tickLine={false}
                    tick={{ fill: 'var(--muted)', fontSize: 10 }}
                    unit="c"
                  />
                  <Tooltip
                    contentStyle={{
                      background: 'var(--raised)',
                      border: '1px solid var(--border)',
                      borderRadius: 8,
                      color: 'var(--text)',
                    }}
                    cursor={{ fill: '#ffffff06' }}
                  />
                  <Bar
                    isAnimationActive={false}
                    dataKey="cpu"
                    name="CPU cores"
                    fill="var(--healthy)"
                    radius={[4, 4, 0, 0]}
                  />
                </BarChart>
              </ResponsiveContainer>
            </div>
          ) : metrics.isPending ? (
            <Loading />
          ) : (
            <Empty
              title="No node samples available"
              description="The current metrics response contains no readable node samples."
            />
          )}
          <div className="panel-footnote">
            <span className="status-dot healthy" />
            {metrics.data?.available
              ? 'Samples provided by ' + metrics.data.source
              : 'Waiting for metrics integration'}
            <Link to="/integrations">
              Integration status
              <ArrowRight size={12} />
            </Link>
          </div>
        </Panel>
        <Panel
          title="Pod health"
          subtitle="A closer look at your containers"
          action={
            <Link to={`/resources?kind=Pod${suffix}`} className="text-link">
              View pods
              <ArrowUpRight size={14} />
            </Link>
          }
        >
          <div className="pod-health">
            <div className="donut">
              <ResponsiveContainer width="100%" height={172}>
                <PieChart>
                  <Pie
                    isAnimationActive={false}
                    data={podData}
                    dataKey="value"
                    innerRadius={57}
                    outerRadius={73}
                    paddingAngle={3}
                    stroke="none"
                    startAngle={90}
                    endAngle={-270}
                  >
                    {podData.map((d, i) => (
                      <Cell key={d.name} fill={palette[i % palette.length]} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{
                      background: 'var(--raised)',
                      border: '1px solid var(--border)',
                      borderRadius: 8,
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
              <div className="donut-center">
                <strong>{d.counts.Pod ?? '—'}</strong>
                <span>total pods</span>
              </div>
            </div>
            <div className="pod-legend">
              {podData.map((p, i) => (
                <Link
                  to={`/resources?kind=Pod&phase=${encodeURIComponent(p.name)}${suffix}`}
                  key={p.name}
                >
                  <i style={{ background: palette[i % palette.length] }} />
                  <span>{p.name}</span>
                  <strong>{p.value}</strong>
                </Link>
              ))}
            </div>
          </div>
          <div className="panel-footnote">
            <ShieldCheck size={13} />
            {running} of {d.counts.Pod ?? 0} pods are running
            <Link to="/issues">
              View diagnostics
              <ArrowRight size={12} />
            </Link>
          </div>
        </Panel>
      </div>
      <div className="overview-lower-grid">
        <Panel
          title="Workloads"
          subtitle="Availability across your applications"
          action={
            <Link
              to={`/applications?namespace=${encodeURIComponent(namespace)}`}
              className="text-link"
            >
              View all
              <ArrowUpRight size={14} />
            </Link>
          }
        >
          <TableScroll>
            <table>
              <thead>
                <tr>
                  <th>Workload</th>
                  <th>Namespace</th>
                  <th>Replicas</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {d.workloads.slice(0, 6).map((r) => (
                  <tr key={r.id}>
                    <td>
                      <ResourceLink resource={r} />
                      <small className="table-subtitle">{r.kind}</small>
                    </td>
                    <td>
                      <span className="namespace-tag">{r.namespace}</span>
                    </td>
                    <td className="mono">{r.ready || '—'}</td>
                    <td>
                      <Status value={r.health} label={r.status} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!d.workloads.length && <Empty />}
          </TableScroll>
          <div className="panel-footnote">
            {d.workloads.length} visible workloads<span>Deployment · StatefulSet · DaemonSet</span>
          </div>
        </Panel>
        <Panel
          title="Needs attention"
          subtitle="Observed problems, with the evidence attached"
          action={<span className="count-label">{d.issues.length}</span>}
        >
          <div className="attention-list">
            {d.issues.slice(0, 3).map((i) => (
              <Link
                to={`/issues/${encodeURIComponent(i.id)}?namespace=${encodeURIComponent(namespace)}`}
                key={i.id}
              >
                <span className={`issue-mark ${i.severity}`}>
                  <TriangleAlert size={16} />
                </span>
                <div>
                  <strong>{i.title}</strong>
                  <span>{i.resource.name}</span>
                  <small>
                    {i.namespace} <span>·</span> {i.evidence.length} observed signal
                    {i.evidence.length === 1 ? '' : 's'}
                  </small>
                </div>
                <ArrowUpRight size={14} />
              </Link>
            ))}
            {!d.issues.length && (
              <Empty
                title="No detected issues"
                description="No diagnostic rules matched the readable resources."
              />
            )}
          </div>
          <Link className="panel-link-footer" to="/issues">
            Open issue center
            <ArrowRight size={15} />
          </Link>
        </Panel>
      </div>
      <Panel
        title="Recent activity"
        subtitle="Events currently retained by Kubernetes"
        action={
          <Link to="/events" className="text-link">
            Open timeline
            <ArrowUpRight size={14} />
          </Link>
        }
      >
        <div className="activity-list">
          {d.events.slice(0, 4).map((e) => (
            <Link to={`/resources/${e.kind}/${e.namespace || '_'}/${e.resource}`} key={e.id}>
              <span className={`event-icon ${e.type === 'Warning' ? 'warning' : 'healthy'}`}>
                {e.type === 'Warning' ? <TriangleAlert size={15} /> : <CheckCircle2 size={15} />}
              </span>
              <div>
                <strong>
                  {e.reason}
                  <span>
                    {e.kind}/{e.resource}
                  </span>
                </strong>
                <p>{e.message}</p>
              </div>
              <span className="event-count">{e.count > 1 ? `×${e.count}` : ''}</span>
              <time>{ago(e.timestamp)}</time>
            </Link>
          ))}
          {!d.events.length && (
            <Empty
              title="No events available"
              description="Kubernetes may have expired previous events, or event access may be restricted."
            />
          )}
        </div>
      </Panel>
      <div className="resource-strip">
        <span>Also in your cluster</span>
        {['StatefulSet', 'DaemonSet', 'Job', 'CronJob', 'Service', 'PersistentVolumeClaim'].map(
          (k) => (
            <Link key={k} to={`/resources?kind=${k}${suffix}`}>
              {k}
              <strong>{d.counts[k] ?? '—'}</strong>
              <ArrowUpRight size={11} />
            </Link>
          ),
        )}
      </div>
    </>
  );
}
function Stat({
  icon: Icon,
  label,
  value,
  detail,
  health,
  href,
  secondary,
}: {
  icon: typeof Server;
  label: string;
  value: number | string;
  detail: string;
  health: string;
  href: string;
  secondary?: string;
}) {
  return (
    <Link className="stat-card" to={href}>
      <div className="stat-card-label">
        <Icon size={16} />
        {label}
        <ArrowUpRight size={14} />
      </div>
      <div className="stat-value">
        {value}
        {secondary && (
          <span className="stat-secondary">
            <ArrowDownRight size={13} />
            {secondary}
          </span>
        )}
      </div>
      <div className={`stat-detail ${health}`}>
        <span className="status-dot" />
        {detail}
      </div>
    </Link>
  );
}
