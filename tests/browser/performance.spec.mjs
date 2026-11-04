import { createRequire } from 'node:module';
const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { test, expect } = require('@playwright/test');

test('profiles overview reads across three concurrent tabs', async ({ browser }, testInfo) => {
  test.setTimeout(60000);
  const context = await browser.newContext({ baseURL: 'http://127.0.0.1:18080' });
  try {
    const login = await context.request.post('/api/v1/session', {
      data: { token: process.env.TIMONE_BROWSER_TOKEN },
      headers: { Origin: 'http://127.0.0.1:18080' },
    });
    expect(login.status()).toBe(200);
    const samples = [];
    const pages = await Promise.all([context.newPage(), context.newPage(), context.newPage()]);
    for (const [tab, page] of pages.entries()) {
      page.on('response', response => {
        if (!new URL(response.url()).pathname.endsWith('/overview')) return;
        samples.push({ tab, status: response.status(), time: Date.now(),
          durationMs: response.request().timing().responseStart });
      });
    }
    const started = Date.now();
    await Promise.all(pages.map((page, i) => page.goto(['/', '/resources', '/settings'][i])));
    await Promise.all(pages.map(page => expect(page.locator('main h1')).toBeVisible()));
    // Observe one complete 30-second fallback cycle, including initial SSE invalidation.
    await pages[0].waitForTimeout(32000);
    await testInfo.attach('overview-profile', {
      body: JSON.stringify({ durationMs: Date.now() - started, tabs: 3, samples }, null, 2),
      contentType: 'application/json',
    });
    expect(samples.length).toBeGreaterThanOrEqual(3);
    expect(samples.every(sample => sample.status === 200)).toBe(true);
    // Each tab may issue initial, initial-SSE, three demo invalidations
    // (every ten seconds), and one periodic fallback read.
    for (let tab = 0; tab < 3; tab++) {
      expect(samples.filter(sample => sample.tab === tab).length).toBeLessThanOrEqual(6);
    }
  } finally {
    await context.close();
  }
});
