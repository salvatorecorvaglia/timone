import { expect, it } from 'vitest';
import { supportsResourceTab } from './resourceCapabilities';

it('limits specialized investigations to supported resource kinds', () => {
  for (const [kind, tab] of [
    ['Pod', 'logs'],
    ['Service', 'network'],
    ['Node', 'maintenance'],
    ['PersistentVolume', 'storage'],
    ['ServiceAccount', 'grants'],
  ]) {
    expect(supportsResourceTab(kind, tab)).toBe(true);
    expect(supportsResourceTab(kind, tab, true)).toBe(false);
  }
  expect(supportsResourceTab('CronJob', 'logs')).toBe(false);
  expect(supportsResourceTab('Pod', 'rollout')).toBe(false);
  expect(supportsResourceTab('Node', 'toString')).toBe(false);
  expect(supportsResourceTab('Custom', 'manifest', true)).toBe(true);
});
