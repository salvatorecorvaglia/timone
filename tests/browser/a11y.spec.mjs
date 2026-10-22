import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { test, expect } = require('@playwright/test');
const axePath = require.resolve('axe-core/axe.min.js');
let auditCookies;
test.beforeAll(async ({ browser }) => {
  // Reuse a real opaque session for this audit, preserving production login
  // throttling while the separate product scenarios exercise interactive login.
  const context = await browser.newContext({ baseURL: 'http://127.0.0.1:18080' });
  const response = await context.request.post('/api/v1/session', { data: { token: process.env.TIMONE_BROWSER_TOKEN }, headers: { Origin: 'http://127.0.0.1:18080' } });
  expect(response.status()).toBe(200);
  auditCookies = await context.cookies();
  await context.close();
});
test.beforeEach(async ({ context }) => { await context.addCookies(auditCookies); });

for (const theme of ['dark', 'light']) {
  for (const viewport of [{ width: 320, height: 800 }, { width: 390, height: 844 }, { width: 820, height: 1180 }, { width: 1440, height: 960 }]) {
    test(`WCAG and keyboard ${theme} ${viewport.width}px`, async ({ page }, testInfo) => {
      test.setTimeout(90000);
      await page.setViewportSize(viewport);
      // Test-only same-origin script delivery preserves the product's strict CSP.
      await page.route('**/__timone_a11y.js', route => route.fulfill({ body: readFileSync(axePath, 'utf8'), contentType: 'application/javascript' }));
      await page.addInitScript(t => localStorage.setItem('timone-theme', t), theme);
      await page.goto('/');
      await expect(page.getByRole('heading', { name: 'Cluster overview' })).toBeVisible();
      const reports = [];
      for (const route of ['/', '/resources', '/applications', '/custom-resources', '/issues', '/events', '/metrics', '/topology', '/audit', '/gitops', '/settings']) {
        await page.goto(route);
        await expect(page.locator('main h1')).toBeVisible();
        await expect(page.locator('.loading')).toHaveCount(0);
        await page.addScriptTag({ url: '/__timone_a11y.js' });
        const result = await page.evaluate(async () => window.axe.run(document, {
          runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'] },
        }));
        // axe cannot infer the background of SVG chart labels. These labels
        // sit on an opaque panel, so assess their computed sRGB colors directly.
        const svgTextContrast = await page.locator('text.recharts-cartesian-axis-tick-value').evaluateAll(nodes => {
          const luminance = color => {
            if (!/^rgb\(/.test(color)) throw new Error(`Unsupported chart color: ${color}`);
            const [r, g, b] = color.match(/[\d.]+/g).map(v => Number(v) / 255).map(v => v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
            return r * 0.2126 + g * 0.7152 + b * 0.0722;
          };
          return nodes.map(node => {
            const fg = luminance(getComputedStyle(node).fill);
            const bg = luminance(getComputedStyle(node.closest('.panel')).backgroundColor);
            return { text: node.textContent, ratio: (Math.max(fg, bg) + 0.05) / (Math.min(fg, bg) + 0.05) };
          });
        });
        if (route === '/') expect(svgTextContrast.length).toBeGreaterThan(0);
        expect(svgTextContrast.filter(label => label.ratio < 4.5)).toEqual([]);
        reports.push({ route, theme, viewport, svgTextContrast, violations: result.violations, incomplete: result.incomplete });
      }
      await testInfo.attach('axe-results', { body: JSON.stringify(reports, null, 2), contentType: 'application/json' });
      const violations = reports.flatMap(r => r.violations.map(v => ({ route: r.route, id: v.id, impact: v.impact, nodes: v.nodes.map(n => ({ target: n.target, summary: n.failureSummary })) })));
      expect(violations.map(v => ({ route: v.route, id: v.id, targets: v.nodes.map(n => n.target) })) ).toEqual([]);

      await page.goto('/');
      await expect(page.getByRole('heading', { name: 'Cluster overview' })).toBeVisible();
      await page.addScriptTag({ url: '/__timone_a11y.js' });
      await page.keyboard.press('Tab');
      await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
      const focusAudit = await page.evaluate(async () => window.axe.run(document.querySelector('.skip-link'), {
        runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] },
      }));
      await testInfo.attach('focused-skip-link-audit', { body: JSON.stringify(focusAudit, null, 2), contentType: 'application/json' });
      expect(focusAudit.violations.map(v => v.id)).toEqual([]);
      await page.keyboard.press('Enter');
      await expect(page.locator('#main')).toBeFocused();
      await page.keyboard.press('ControlOrMeta+k');
      const dialog = page.getByRole('dialog', { name: 'Global resource search' });
      await expect(dialog).toBeVisible();
      await expect(page.getByLabel('Search all resources', { exact: true })).toBeFocused();
      for (let i = 0; i < 12; i++) {
        await page.keyboard.press('Tab');
        expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
      }
      await page.keyboard.press('Shift+Tab');
      expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
      await page.keyboard.press('Escape');
      await expect(dialog).toBeHidden();
      const overflow = await page.locator('body').evaluate(el => ({
        width: el.scrollWidth,
        viewport: window.innerWidth,
        offenders: Array.from(document.querySelectorAll('body *')).filter(node => !node.closest('[inert]') && node.getBoundingClientRect().right > window.innerWidth + 1).map(node => ({tag:node.tagName, class:node.getAttribute('class'), right:node.getBoundingClientRect().right})).slice(0,12),
      }));
      expect(overflow.width, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport);
      await page.screenshot({ path: testInfo.outputPath(`${theme}-${viewport.width}.png`), fullPage: true });
    });
  }
}

test('renders coverage from the actual restricted live-cluster response', async ({ page }) => {
  test.skip(!process.env.TIMONE_LIVE_OVERVIEW, 'Run after real-cluster acceptance with TIMONE_LIVE_OVERVIEW');
  const response = JSON.parse(readFileSync(process.env.TIMONE_LIVE_OVERVIEW, 'utf8'));
  expect(response.mode).toBe('cluster');
  expect(response.warnings.length).toBeGreaterThan(0);
  await page.route('**/api/v1/overview*', route => route.fulfill({ json: response }));
  await page.goto('/');
  const coverage = page.locator('.data-warning');
  await expect(coverage).toContainText('unavailable or incomplete');
  await coverage.locator('summary').click();
  for (const warning of response.warnings) await expect(coverage).toContainText(warning.kind);
  await expect(page.getByRole('heading', { name: 'Cluster overview' })).toBeVisible();
});
