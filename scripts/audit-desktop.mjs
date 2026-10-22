// Keep the one unpatched build-only advisory visible without hiding new findings.
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

export function reviewReport(report) {
  if (report.error || !report.metadata || !report.advisories) throw new Error('Desktop dependency audit failed');
  return Object.values(report.advisories).map(advisory => ({
    advisory,
    tracked: advisory.github_advisory_id === 'GHSA-hp3w-g68c-fv3c' &&
      advisory.module_name === 'sprintf-js' && advisory.severity === 'moderate' &&
      !advisory.patched_versions && advisory.findings?.length > 0 &&
      advisory.findings.every(f => f.dev === true && f.version === '1.1.3' &&
        f.paths?.length > 0 && f.paths.every(p => p.includes('>electron-builder>'))),
  }));
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const result = spawnSync('pnpm', ['--dir', 'desktop', 'audit', '--json'], { encoding: 'utf8', shell: process.platform === 'win32' });
  if (result.error) throw new Error('Desktop dependency audit could not start');
  const findings = reviewReport(JSON.parse(result.stdout));
  for (const { advisory, tracked } of findings)
    console.log(`${tracked ? 'Tracked unpatched build dependency' : 'Unreviewed advisory'}: ${advisory.github_advisory_id} ${advisory.url}`);
  process.exitCode = findings.some(f => !f.tracked) ? 1 : 0;
}
