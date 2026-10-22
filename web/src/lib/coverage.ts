import type { Health, Overview } from '../types';

export function collectionHealth(
  kind: string,
  overview: Pick<Overview, 'counts' | 'warnings'>,
  observedHealth: Health,
): Health {
  return overview.counts[kind] === undefined ||
    overview.warnings.some((warning) => warning.kind === kind)
    ? 'unknown'
    : observedHealth;
}
