import type { MetricHistory as History } from '../types';
export function historyRows(history: History, metric: string) {
  const start = Date.parse(history.start) / 1000,
    end = Date.parse(history.end) / 1000;
  if (
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    !Number.isFinite(history.stepSeconds) ||
    history.stepSeconds < 60 ||
    end < start ||
    (end - start) / history.stepSeconds > 240
  )
    return [];
  const series = history.series.filter((s) => s.metric === metric);
  const rows = new Map<number, Record<string, number | null>>();
  for (
    let timestamp = Date.parse(history.start) / 1000;
    timestamp <= Date.parse(history.end) / 1000;
    timestamp += history.stepSeconds
  ) {
    const row: Record<string, number | null> = { timestamp };
    series.forEach((_, i) => {
      row[`series${i}`] = null;
    });
    rows.set(timestamp, row);
  }
  series.forEach((s, i) => {
    s.points.forEach((point) => {
      const row = rows.get(point.timestamp);
      if (row)
        row[`series${i}`] =
          point.value === null ? null : metric === 'memory' ? point.value / 2 ** 20 : point.value;
    });
  });
  return [...rows.values()];
}
