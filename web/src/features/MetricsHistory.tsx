import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { request, useAPI, memory } from '../lib/api';
import { historyRows } from '../lib/history';
import type { HistoryCapability, MetricHistory as History, HistorySeries } from '../types';
import { Empty, ErrorState, Loading, Panel, Warnings } from '../components/ui';

const colors = [
  'var(--healthy)',
  'var(--blue)',
  'var(--warning)',
  'var(--critical)',
  'var(--chart-purple)',
  'var(--chart-cyan)',
];
export function MetricsHistory({ namespace }: { namespace: string }) {
  const [params] = useSearchParams();
  const key = [
    namespace,
    ...['historyKind', 'historyName', 'historyNamespace', 'historyContainer', 'historyRange'].map(
      (name) => params.get(name) ?? '',
    ),
  ].join(':');
  return <HistoryInvestigation key={key} namespace={namespace} />;
}
function HistoryInvestigation({ namespace }: { namespace: string }) {
  const status = useAPI<HistoryCapability>('/api/v1/metrics/history/status');
  const [params, setParams] = useSearchParams();
  const [kind, setKind] = useState(params.get('historyKind') ?? 'Pod');
  const [name, setName] = useState(params.get('historyName') ?? '');
  const [targetNamespace, setNamespace] = useState(params.get('historyNamespace') ?? namespace);
  const [container, setContainer] = useState(params.get('historyContainer') ?? '');
  const [range, setRange] = useState(params.get('historyRange') ?? '1h');
  const [samples, setSamples] = useState(false);
  const selectedName = params.get('historyName') ?? '';
  const selectedKind = params.get('historyKind') ?? 'Pod';
  const selectedNamespace =
    selectedKind === 'Pod' ? namespace || params.get('historyNamespace') || '' : '';
  const q = new URLSearchParams({
    kind: selectedKind,
    name: selectedName,
    namespace: selectedNamespace,
    container: params.get('historyContainer') ?? '',
    range: params.get('historyRange') ?? '1h',
  });
  const path = `/api/v1/metrics/history?${q}`;
  const history = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<History>(path, { signal }),
    enabled: !!selectedName && status.data?.state !== 'disabled' && !!status.data,
    refetchInterval: 60000,
    staleTime: 30000,
    retry: false,
  });
  const result = history.data;
  return (
    <Panel
      title="Historical CPU & memory"
      subtitle="Bounded resource history. Current measurements and scheduling demand remain independent."
    >
      {status.isPending ? (
        <Loading />
      ) : status.error ? (
        <ErrorState error={status.error} retry={() => void status.refetch()} />
      ) : status.data.state === 'disabled' ? (
        <Empty title="Historical metrics not configured" description={status.data.message} />
      ) : (
        <>
          <p className="history-source">{status.data.message}</p>
          <form
            className="history-controls"
            onSubmit={(event) => {
              event.preventDefault();
              const next = new URLSearchParams(params);
              next.set('historyKind', kind);
              next.set('historyName', name.trim());
              next.set('historyRange', range);
              if (kind === 'Pod') {
                next.set('historyNamespace', namespace || targetNamespace.trim());
                if (container.trim()) next.set('historyContainer', container.trim());
                else next.delete('historyContainer');
              } else {
                next.delete('historyNamespace');
                next.delete('historyContainer');
              }
              setParams(next);
            }}
          >
            <label>
              Resource type
              <select
                aria-label="History resource type"
                value={kind}
                onChange={(e) => setKind(e.target.value)}
              >
                <option>Pod</option>
                <option>Node</option>
              </select>
            </label>
            <label>
              Resource name
              <input
                aria-label="History resource name"
                value={name}
                required
                maxLength={253}
                placeholder={kind === 'Pod' ? 'checkout-api-…' : 'worker-01'}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            {kind === 'Pod' && (
              <>
                <label>
                  Namespace
                  <input
                    aria-label="History namespace"
                    value={namespace || targetNamespace}
                    disabled={!!namespace}
                    required
                    maxLength={63}
                    onChange={(e) => setNamespace(e.target.value)}
                  />
                </label>
                <label>
                  Container
                  <input
                    aria-label="History container"
                    placeholder="All current containers"
                    value={container}
                    maxLength={63}
                    onChange={(e) => setContainer(e.target.value)}
                  />
                </label>
              </>
            )}
            <label>
              Window
              <select
                aria-label="History time range"
                value={range}
                onChange={(e) => setRange(e.target.value)}
              >
                <option value="1h">Last hour</option>
                <option value="6h">Last 6 hours</option>
                <option value="24h">Last 24 hours</option>
              </select>
            </label>
            <button className="button" type="submit">
              View history
            </button>
          </form>
          {!selectedName ? (
            <Empty
              title="Choose a resource to investigate"
              description="Enter a current Pod or Node. Kubernetes read permission is checked before querying its history."
            />
          ) : history.isPending ? (
            <Loading />
          ) : history.error ? (
            <ErrorState error={history.error} retry={() => void history.refetch()} />
          ) : (
            result && (
              <>
                <div className="history-summary">
                  <strong>
                    {result.resource.kind}/{result.resource.name}
                  </strong>
                  <span className="tag">
                    {result.mode === 'demo' ? 'SIMULATED' : result.source} · {result.state}
                  </span>
                  <button className="button subtle" onClick={() => void history.refetch()}>
                    Refresh history
                  </button>
                </div>
                <p className="history-source">{result.message}</p>
                <p className="footnote">
                  {new Date(result.start).toLocaleString()} –{' '}
                  {new Date(result.end).toLocaleString()} · {result.stepSeconds}s resolution · Gaps
                  mean unavailable samples.
                </p>
                <Warnings items={result.warnings} />
                {result.identity && <p className="history-source">{result.identity}</p>}
                {!result.series.some((s) => s.points.some((p) => p.value !== null)) ? (
                  <Empty
                    title="No usable historical samples"
                    description="Check the integration warnings, required scrape labels, resource age and Prometheus retention. Missing data is not zero usage."
                  />
                ) : (
                  <>
                    <div className="history-charts">
                      {['cpu', 'memory'].map((metric) => (
                        <HistoryChart key={metric} metric={metric} history={result} />
                      ))}
                    </div>
                    <details
                      className="history-samples"
                      onToggle={(e) => setSamples(e.currentTarget.open)}
                    >
                      <summary>View sample table</summary>
                      {samples && <SampleTable series={result.series} />}
                    </details>
                  </>
                )}
              </>
            )
          )}
        </>
      )}
    </Panel>
  );
}
function HistoryChart({ metric, history }: { metric: string; history: History }) {
  const series = history.series.filter((s) => s.metric === metric);
  const available = series.some((s) => s.points.some((p) => p.value !== null));
  return (
    <section
      className="history-chart"
      aria-label={metric === 'cpu' ? 'Historical CPU chart' : 'Historical memory chart'}
    >
      <h3>{metric === 'cpu' ? 'CPU · cores' : 'Memory · MiB'}</h3>
      {!available ? (
        <Empty
          title={`${metric === 'cpu' ? 'CPU' : 'Memory'} history unavailable`}
          description="No valid samples were returned for this metric."
        />
      ) : (
        <ResponsiveContainer width="100%" height={230} minWidth={0}>
          <LineChart
            data={historyRows(history, metric)}
            margin={{ left: 0, right: 20, top: 12, bottom: 0 }}
            accessibilityLayer
          >
            <CartesianGrid stroke="var(--border)" vertical={false} />
            <XAxis
              dataKey="timestamp"
              type="number"
              domain={['dataMin', 'dataMax']}
              tickFormatter={(value) =>
                new Date(Number(value) * 1000).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                })
              }
              tick={{ fill: 'var(--muted)', fontSize: 10 }}
            />
            <YAxis domain={[0, 'auto']} tick={{ fill: 'var(--muted)', fontSize: 10 }} width={55} />
            <Tooltip
              labelFormatter={(value) => new Date(Number(value) * 1000).toLocaleString()}
              contentStyle={{
                background: 'var(--panel)',
                border: '1px solid var(--border)',
                color: 'var(--text)',
              }}
            />
            <Legend wrapperStyle={{ fontSize: 11 }} />
            {series.map((s, i) => (
              <Line
                key={s.container || 'node'}
                type="linear"
                dataKey={`series${i}`}
                name={s.container || history.resource.name}
                stroke={colors[i % colors.length]}
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      )}
    </section>
  );
}
function SampleTable({ series }: { series: HistorySeries[] }) {
  const rows = series.flatMap((s) =>
    s.points.map((p) => ({ metric: s.metric, container: s.container, unit: s.unit, ...p })),
  );
  return (
    <>
      <p className="footnote">
        Showing up to 200 of {rows.length} samples. Full bounded data is available through the API.
      </p>
      <div className="table-scroll">
        <table aria-label="Historical metric samples">
          <thead>
            <tr>
              <th>Time</th>
              <th>Metric</th>
              <th>Container</th>
              <th>Value</th>
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, 200).map((r, i) => (
              <tr key={i}>
                <td>{new Date(r.timestamp * 1000).toLocaleString()}</td>
                <td>{r.metric}</td>
                <td>{r.container || 'Node'}</td>
                <td>
                  {r.value === null
                    ? 'Unavailable'
                    : r.unit === 'bytes'
                      ? memory(r.value)
                      : `${r.value.toFixed(3)} cores`}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
