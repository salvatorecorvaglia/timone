import { createRequire } from 'node:module';
const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { test, expect } = require('@playwright/test');

let productCookies;
test.beforeAll(async ({browser})=>{
 const context=await browser.newContext({baseURL:'http://127.0.0.1:18080'});
 const response=await context.request.post('/api/v1/session',{data:{token:process.env.TIMONE_BROWSER_TOKEN},headers:{Origin:'http://127.0.0.1:18080'}});
 expect(response.status()).toBe(200);productCookies=await context.cookies();await context.close();
});
test.beforeEach(async ({ page, context }, testInfo) => {
 const interactive=testInfo.title.includes('authenticates with') || testInfo.title.includes('revokes the browser session');
 if(!interactive) await context.addCookies(productCookies);
 await page.goto('/');
 if(interactive){await page.getByLabel('Access token', { exact: true }).fill(process.env.TIMONE_BROWSER_TOKEN);await page.getByRole('button', { name: 'Open workspace' }).click();}
 await expect(page.getByRole('heading', { name: 'Cluster overview' })).toBeVisible();
});
test('authenticates with an opaque session and navigates real resource details', async ({ page, context }) => {
  await expect(page.getByText('Demo environment')).toBeVisible();
  const cookies = await context.cookies();
  const session = cookies.find(c => c.name === 'timone_session');
  expect(session?.httpOnly).toBe(true);
  expect(session?.value).not.toBe(process.env.TIMONE_BROWSER_TOKEN);
  await page.goto('/resources?kind=Pod&search=payments');
  await expect(page.getByRole('link', { name: 'payments-7c8d9f-k9x2p', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'payments-7c8d9f-k9x2p', exact: true }).click();
  await page.getByRole('tab', { name: 'Manifest' }).click();
  await expect(page.getByLabel('Resource YAML')).toContainText('kind: Pod');
});

test('keeps the workspace usable when browser preference storage is denied', async ({ page }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => { throw new DOMException('Storage denied', 'SecurityError'); };
    Storage.prototype.setItem = () => { throw new DOMException('Storage denied', 'SecurityError'); };
  });
  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Light', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.goto('/resources');
  await page.getByRole('button', { name: 'Density', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Density', exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect(errors).toEqual([]);
});

test('reports a failed sign-out without claiming the session was revoked', async ({ page }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/settings');
  await page.route('**/api/v1/session', route => route.request().method() === 'DELETE' ? route.abort('failed') : route.continue());
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Your session may still be active');
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeEnabled();
  expect(errors).toEqual([]);
});

test('sends topology group selection to the backend before applying graph limits', async ({ page }) => {
  await page.goto('/topology?namespace=production');
  const request = page.waitForRequest(request => {
    const url = new URL(request.url());
    return url.pathname === '/api/v1/topology' && url.searchParams.get('group') === 'workloads';
  });
  await page.getByLabel('Topology resource types').selectOption('workloads');
  await request;
  await expect(page.locator('.react-flow__node').first()).toBeVisible();
  await expect(page.locator('.react-flow__node').filter({ hasText: 'ConfigMap' })).toHaveCount(0);
});
test('keeps event filters after reload and compares retained Helm revisions', async ({ page }) => {
  await page.goto('/events');
  await page.getByLabel('Event resource kind').selectOption('Job');
  await expect(page.getByText('Job completed successfully')).toBeVisible();
  await page.reload();
  await expect(page.getByLabel('Event resource kind')).toHaveValue('Job');
  await page.goto('/helm/production/checkout-api?from=1&to=3');
  await expect(page.getByRole('heading', { name: 'Retained revisions' })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Revision metadata comparison' })).toContainText('2.3.0');
  await expect(page.locator('.helm-inventory').first()).toContainText('Service');
});
test('renders an interactive topology and keyboard search', async ({ page }) => {
  await page.goto('/topology');
  await expect(page.locator('.react-flow__node').first()).toBeVisible();
  const viewport = page.locator('.react-flow__viewport');
  const before = await viewport.getAttribute('style');
  await page.getByRole('button', { name: /zoom in/i }).click();
  await expect(viewport).not.toHaveAttribute('style', before);
  await page.keyboard.press('ControlOrMeta+k');
  await expect(page.getByRole('dialog', { name: 'Global resource search' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog', { name: 'Global resource search' })).not.toBeVisible();
});
test('supports mobile navigation and revokes the browser session on logout', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByLabel('Access token', { exact: true })).toBeVisible();
});

test('explores custom types and shows a safe projection with related events', async ({ page }) => {
  await page.goto('/custom-resources?definition=databases.platform.timone.io&namespace=production');
  await page.getByLabel('Search custom resources').fill('orders');
  await page.getByRole('link', { name: 'orders-db', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'orders-db' })).toBeVisible();
  await page.getByRole('tab', { name: 'Observed YAML' }).click();
  await expect(page.getByLabel('Resource YAML')).toContainText('kind: Database');
  await expect(page.getByLabel('Resource YAML')).not.toContainText('SIMULATED-OMITTED-CUSTOM-SECRET');
  await page.getByRole('tab', { name: 'Events (1)' }).click();
  await expect(page.getByText('BackupStorageUnavailable', { exact: true })).toBeVisible();
});

test('renders simulated resource history and keeps the investigation URL', async ({ page }) => {
  await page.goto('/metrics?historyKind=Node&historyName=harbor-worker-01&historyRange=1h');
  await expect(page.getByText('Node/harbor-worker-01', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Historical CPU chart').locator('svg').first()).toBeVisible();
  await page.getByLabel('History time range').selectOption('6h');
  await page.getByRole('button', { name: 'View history' }).click();
  await expect(page).toHaveURL(/historyRange=6h/);
  await page.getByText('View sample table', { exact: true }).click();
  await expect(page.getByRole('table', { name: 'Historical metric samples' })).toBeVisible();
});

test('inspects Gateway admission and separates controller reports from configuration', async ({ page }) => {
  await page.goto('/topology?group=networking');
  await page.getByRole('button', {name:'Inspect public-gateway to preview-route spec.parentRefs[0] Evidence'}).click();
  await expect(page.getByRole('heading', {name:'Reference evidence'})).toBeVisible();
  await expect(page.getByText('Accepted: False · ResolvedRefs: False')).toBeVisible();
  await expect(page.getByText(/default\/Same policy excludes/)).toBeVisible();
  await page.getByRole('link', {name:'preview-route', exact:true}).click();
  await expect(page).toHaveURL('/resources/HTTPRoute/staging/preview-route');
});

test('searches enabled custom resources with health filters and opens exact details', async ({ page }) => {
  await page.keyboard.press('ControlOrMeta+k');
  await page.getByLabel('Search all resources', { exact: true }).fill('orders-db');
  await page.getByLabel('Global search resource type').selectOption('custom:databases.platform.timone.io');
  await page.getByLabel('Global search health').selectOption('warning');
  await expect(page.getByRole('status').filter({hasText:'1 matches in observed collections'})).toBeVisible();
  await page.getByRole('button', {name:'Inspect Database production/orders-db (databases.platform.timone.io)', exact:true}).click();
  await expect(page).toHaveURL(/\/custom-resources\/databases\.platform\.timone\.io\/production\/orders-db\?version=v1$/);
  await expect(page.getByRole('heading', {name:'orders-db'})).toBeVisible();
});

test('investigates ServiceAccount bindings without treating unresolved roles as denial', async ({ page }) => {
  await page.goto('/resources/ServiceAccount/production/default?tab=grants&grantBinding=RoleBinding%3Aproduction%3Acommerce-config-reader');
  await expect(page.getByRole('tab', {name:'RBAC grants'})).toHaveAttribute('aria-selected','true');
  await expect(page.getByRole('table', {name:'Observed RBAC policy rules'})).toContainText('storefront-config');
  await page.getByRole('button', {name:'Inspect RBAC binding legacy-reader'}).click();
  await expect(page).toHaveURL(/grantBinding=RoleBinding%3Aproduction%3Alegacy-reader/);
  await expect(page.getByText('Rules unavailable', {exact:true})).toBeVisible();
  await expect(page.getByText('No empty permission set is inferred from missing or invalid role evidence.')).toBeVisible();
});


test('exposes the detection policy and keeps it accessible from an investigation', async ({ page }) => {
  await page.goto('/issues/issue-Deployment%3Aproduction%3Acheckout-api');
  const summary = page.getByText('Active detection policy', { exact: true });
  await expect(summary).toBeVisible();
  await summary.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByText('300 seconds', { exact: true })).toBeVisible();
  await expect(page.getByText('900 seconds', { exact: true })).toBeVisible();
  await expect(page.getByText('At least 5', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: /Observed evidence/ })).toBeVisible();
  await expect(page.getByText(/Restart counts are cumulative across Pod containers, not a rate/)).toBeVisible();
});


test('opens a custom controller issue and follows its exact resource identity', async ({ page }) => {
  await page.goto('/issues/issue-databases.platform.timone.io%3Aproduction%3Aorders-db');
  await expect(page.getByRole('heading', { name: /Observed evidence/ })).toBeVisible();
  const coverage = page.getByText('Custom resource coverage · available', { exact: true });
  await coverage.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByText(/Stale, missing or ambiguous conditions cannot establish current readiness/)).toBeVisible();
  await page.getByRole('link', { name: 'Inspect resource' }).click();
  await expect(page).toHaveURL('/custom-resources/databases.platform.timone.io/production/orders-db?version=v1');
  await expect(page.getByRole('heading', { name: 'orders-db', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Observed YAML' }).click();
  await expect(page.getByLabel('Resource YAML')).not.toContainText('SIMULATED-OMITTED-CUSTOM-SECRET');
});


test('distinguishes missing node limits from finite budgets in the simulated cluster', async ({ page }) => {
  await page.goto('/metrics?namespace=production');
  const budgets = page.getByRole('table', { name: 'Configured node resource budgets' });
  await expect(budgets).toBeVisible();
  await expect(budgets.getByRole('columnheader', { name: 'CPU limit budget' })).toBeVisible();
  const partial = budgets.getByRole('row').filter({ has: page.getByRole('link', { name: 'harbor-worker-01', exact: true }) });
  await expect(partial).toContainText('Incomplete limit coverage');
  await expect(partial).toContainText('2 missing/zero');
  const finite = budgets.getByRole('row').filter({ has: page.getByRole('link', { name: 'harbor-worker-02', exact: true }) });
  await expect(finite).toContainText('% of allocatable');
  await finite.getByRole('link', { name: 'harbor-worker-02', exact: true }).click();
  await expect(page).toHaveURL('/resources/Node/_/harbor-worker-02?namespace=production');
});


test('persists global search filters and opens a correlated application', async ({ page }) => {
  await page.keyboard.press('ControlOrMeta+k');
  await page.getByLabel('Search all resources', {exact:true}).fill('checkout');
  await page.getByLabel('Global search health').selectOption('critical');
  await expect(page).toHaveURL(/gq=checkout/);
  await page.reload();
  await expect(page.getByLabel('Search all resources', {exact:true})).toHaveValue('checkout');
  await expect(page.getByLabel('Global search health')).toHaveValue('critical');
  await page.keyboard.press('Escape');
  await page.goto('/applications?namespace=production');
  await page.getByRole('table', {name:'Applications',exact:true}).getByRole('button',{name:'checkout-api',exact:true}).click();
  await expect(page.getByRole('heading',{name:'checkout-api · investigation',exact:true})).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading',{name:'checkout-api · investigation',exact:true})).toBeVisible();
  await expect(page.getByRole('link',{name:'checkout-api · Synced',exact:true})).toBeVisible();
});

test('shows sanitized configuration differences and observed history', async ({ page }) => {
  await page.goto('/helm/production/checkout-api?from=2&to=3');
  await expect(page.getByRole('table',{name:'Sanitized Helm configuration differences',exact:true})).toContainText('/spec/replicas');
  await expect(page.locator('body')).not.toContainText('DEMO-PRIVATE-HELM-VALUE');
  await page.goto('/events');
  await page.getByText('Observed resource changes',{exact:true}).click();
  await expect(page.getByText(/Observed by this process only/)).toBeVisible();
});

test('captures responsive operational screens and supports keyboard dialog focus', async ({ page }, testInfo) => {
  await expect(page.locator('.recharts-pie-sector').first()).toBeVisible();
  await page.screenshot({path:testInfo.outputPath('overview-desktop.png'),fullPage:true,animations:'disabled'});
  await page.keyboard.press('ControlOrMeta+k');
  const search=page.getByLabel('Search all resources',{exact:true});
  await expect(search).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog',{name:'Global resource search'})).not.toBeVisible();
  await page.setViewportSize({width:390,height:844});
  await page.screenshot({path:testInfo.outputPath('overview-mobile.png'),fullPage:true,animations:'disabled'});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('keeps resource identity visible while scrolling mobile tables', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/resources');
  const viewport = page.getByRole('group', { name: 'Scrollable table' });
  await expect(viewport).toBeVisible();
  await expect(page.getByText('Scroll horizontally to see more columns.')).toBeVisible();
  const firstCell = viewport.locator('tbody tr').first().locator('td').first();
  const before = await firstCell.boundingBox();
  await viewport.evaluate(el => { el.scrollLeft = el.scrollWidth; });
  await expect.poll(() => viewport.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
  const after = await firstCell.boundingBox();
  expect(Math.abs(after.x - before.x)).toBeLessThan(2);
  await viewport.focus();
  await expect(viewport).toBeFocused();
});

test('renders CRD server columns, wide mode and versioned OpenAPI schema', async ({page})=>{
 await page.goto('/custom-resources?definition=databases.platform.timone.io&namespace=production');
 const table=page.getByRole('table',{name:'Custom resources',exact:true});
 await expect(table.getByRole('columnheader',{name:'Ready',exact:true})).toBeVisible();
 await expect(table.getByRole('columnheader',{name:'Generation',exact:true})).toHaveCount(0);
 await page.getByLabel('Wide printer columns').check();
 await expect(table.getByRole('columnheader',{name:'Generation',exact:true})).toBeVisible();
 await page.getByText('OpenAPI v3 schema · v1',{exact:true}).click();
 await expect(page.getByLabel('OpenAPI v3 schema')).toContainText('"minimum": 1');
 await expect(page.getByLabel('OpenAPI v3 schema')).not.toContainText('SIMULATED-OMITTED-CUSTOM-SECRET');
 await page.getByLabel('Custom resource version').selectOption('v1beta1');
 await expect(page.getByText('OpenAPI v3 schema · v1beta1',{exact:true})).toBeVisible();
 await expect(table.getByRole('link',{name:'orders-db',exact:true})).toBeVisible();
});

test('checks live-review UI with clearly simulated allow and deny decisions',async({page})=>{
 await page.goto('/audit?namespace=production');
 await page.getByRole('button',{name:'Check effective access',exact:true}).click();
 await expect(page.getByText('Reviewed: get Pod production/*',{exact:true})).toBeVisible();
 await expect(page.getByText('SIMULATED authorization decision; no Kubernetes review was sent.',{exact:true})).toBeVisible();
 await page.getByLabel('Authorization resource',{exact:true}).selectOption('Secret');
 await page.getByRole('button',{name:'Check effective access',exact:true}).click();
 await expect(page.getByText('Reviewed: get Secret production/*',{exact:true})).toBeVisible();
 await expect(page.getByText('denied',{exact:true})).toBeVisible();
});

test('compares safe values and computes desired versus live GitOps drift',async({page},testInfo)=>{
 await page.goto('/helm/production/checkout-api?from=1&to=3');
 await expect(page.getByRole('table',{name:'Helm values differences',exact:true})).toContainText('/replicaCount');
 await expect(page.locator('body')).not.toContainText('DEMO-PRIVATE-VALUES');
 await page.goto('/gitops');
 await page.getByRole('button',{name:'Compare live drift for checkout-api',exact:true}).click();
 await expect(page.getByRole('table',{name:'Desired versus live drift',exact:true})).toContainText('/spec/replicas');
 await expect(page.getByText('drift-detected',{exact:true})).toBeVisible();
 await page.screenshot({path:testInfo.outputPath('gitops-drift.png'),fullPage:true,animations:'disabled'});
 await page.goto('/');await page.locator('.cluster-switch').click();
 await expect(page.getByText(/Demo and shared deployments keep a fixed workspace/)).toBeVisible();
});

test('keeps closed mobile navigation out of the tab order and traps open navigation', async ({page}) => {
 await page.setViewportSize({width:390,height:844});
 await page.goto('/');
 await expect(page.getByRole('heading',{name:'Cluster overview'})).toBeVisible();
 await page.keyboard.press('Tab');
 await expect(page.getByRole('link',{name:'Skip to content'})).toBeFocused();
 await page.keyboard.press('Tab');
 await expect(page.getByRole('button',{name:'Open navigation'})).toBeFocused();
 await page.keyboard.press('Enter');
 for(let i=0;i<20;i++) {
  expect(await page.locator('#main-navigation').evaluate(el=>el.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Tab');
 }
 await page.keyboard.press('Escape');
 await expect(page.getByRole('button',{name:'Open navigation'})).toBeFocused();
 await expect(page.locator('#main-navigation')).toHaveAttribute('inert','');
});

test('correlates incident evidence and exports without application logs by default', async ({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api?tab=incident');
 await expect(page.getByRole('heading',{name:'Current conditions and problems'})).toBeVisible();
 await expect(page.getByRole('heading',{name:'Timeline in selected interval'})).toBeVisible();
 await expect(page.getByRole('checkbox',{name:'Include application logs in investigation and exports'})).not.toBeChecked();
 const downloadPromise=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export incident JSON'}).click();
 const download=await downloadPromise;
 expect(download.suggestedFilename()).toBe('incident.json');
 const stream=await download.createReadStream();const chunks=[];for await(const chunk of stream)chunks.push(chunk);
 const bundle=JSON.parse(Buffer.concat(chunks).toString('utf8'));
 expect(bundle.schemaVersion).toBe(1);expect(bundle.logs).toBeUndefined();expect(bundle.cluster).toBe('harbor-demo');
 const interval=new URL(page.url()).searchParams;
 expect(interval.get('since')).toBeTruthy();expect(interval.get('until')).toBeTruthy();
 await page.getByRole('tab',{name:'Workload logs'}).click();
 await expect(page.getByRole('table',{name:'Log source coverage'})).toBeVisible();
 const after=new URL(page.url()).searchParams;
 expect(after.get('since')).toBe(interval.get('since'));expect(after.get('until')).toBe(interval.get('until'));
});

test('explores workload log origins and exact storage bindings', async ({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api?tab=logs');
 const sources=page.getByRole('table',{name:'Log source coverage'});
 await expect(sources).toBeVisible();await expect(sources).toContainText('checkout-api');
 await page.getByRole('checkbox',{name:'Previous instances'}).check();
 await expect(page.getByRole('checkbox',{name:/Live refresh/})).toBeDisabled();
 await page.goto('/resources/StatefulSet/production/redis?tab=storage');
 await expect(page.getByRole('heading',{name:'Storage investigation'})).toBeVisible();
 await expect(page.getByRole('link',{name:'redis-data',exact:true})).toBeVisible();
 await expect(page.getByRole('link',{name:'redis-pv',exact:true})).toBeVisible();
 await expect(page.getByRole('link',{name:'demo-csi',exact:true})).toBeVisible();
 await expect(page.getByRole('link',{name:'redis-attachment',exact:true})).toBeVisible();
});

test('expands resource-centered topology and finds a bounded declared path',async({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api');
 await page.getByRole('link',{name:'Explore dependencies'}).click();
 await expect(page.getByRole('heading',{name:'Exploring Deployment/checkout-api'})).toBeVisible();
 await page.getByRole('button',{name:'Expand one level'}).click();
 await expect(page.getByText('Depth 2 · Declared relationships only.',{exact:false})).toBeVisible();
 await expect(page.getByRole('combobox',{name:'Find path to resource'}).locator('option[value="Pod:production:checkout-api-7c8d9f-m4n7q"]')).toHaveCount(1);
 await page.getByRole('combobox',{name:'Find path to resource'}).selectOption('Pod:production:checkout-api-7c8d9f-m4n7q');
 await expect(page.getByText('Observed path:',{exact:false})).toContainText('Deployment:production:checkout-api');
 await expect(page.getByText('Observed path:',{exact:false})).toContainText('Pod:production:checkout-api-7c8d9f-m4n7q');
});

test('captures a sanitized snapshot and compares imported files offline',async({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api');
 await page.getByRole('link',{name:'Capture / compare snapshots'}).click();
 await page.getByRole('button',{name:'Capture snapshot',exact:true}).click();
 const downloading=page.waitForEvent('download');
 await page.getByRole('button',{name:'Download snapshot',exact:true}).click();
 const file=await downloading;const stream=await file.createReadStream();const chunks=[];for await(const chunk of stream)chunks.push(chunk);
 const before=JSON.parse(Buffer.concat(chunks).toString('utf8'));expect(before.schemaVersion).toBe(1);
 const after=structuredClone(before);after.resources[0].configuration.replicas=99;
 await page.route('**/api/v1/resources/**',route=>route.abort());
 await page.getByLabel('Before snapshot file').setInputFiles({name:'before.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(before))});
 await page.getByLabel('After snapshot file').setInputFiles({name:'after.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(after))});
 await expect(page.getByRole('table',{name:'Snapshot differences'})).toContainText('/configuration/replicas');
 await expect(page.getByRole('table',{name:'Snapshot differences'})).toContainText('99');
});

test('shows persistent collector status and sampled coverage',async({page})=>{
 await page.goto('/history');
 await expect(page.getByRole('heading',{name:'Persistent observation history'})).toBeVisible();
 const status=await (await page.request.get('/api/v1/history/status')).json();
 if(!status.enabled){await expect(page.getByText('Persistent history is disabled',{exact:true})).toBeVisible();return;}
 await expect.poll(async()=> (await (await page.request.get('/api/v1/history/status')).json()).state).toBe('sampling');
 await page.getByRole('button',{name:'Refresh retained history'}).click();
 await expect(page.getByRole('heading',{name:'Sampling coverage'})).toBeVisible();
 await expect(page.getByText(/retained samples in selected interval/)).toBeVisible();
});

test('persists named views and favorites across browser reloads',async({page})=>{
 await page.goto('/resources?kind=Pod&namespace=production&status=critical');
 await page.getByText('Save this view',{exact:true}).click();
 await page.getByLabel('Saved view name').fill('Production failures');
 await page.getByRole('button',{name:'Save view',exact:true}).click();
 await expect(page.getByText('Saved in this browser.',{exact:true})).toBeVisible();
 await page.goto('/resources/Deployment/production/checkout-api');
 await page.getByRole('button',{name:'Favorite resource',exact:true}).click();
 await page.reload();
 await expect(page.getByRole('button',{name:'Remove favorite',exact:true})).toHaveAttribute('aria-pressed','true');
 await page.goto('/saved');
 await expect(page.getByRole('link',{name:'Deployment/checkout-api',exact:true})).toBeVisible();
 await page.getByRole('link',{name:'Production failures',exact:true}).click();
 await expect(page).toHaveURL(/status=critical/);
 await page.goto('/saved');
 await page.getByRole('button',{name:'Remove Deployment/checkout-api',exact:true}).click();
 await page.reload();
 await expect(page.getByRole('link',{name:'Deployment/checkout-api',exact:true})).toHaveCount(0);
});

test('reads only explicitly selected workload log sources and preserves selection in URL',async({page})=>{
 const response=await page.request.get('/api/v1/resources/Deployment/production/checkout-api/log-sources');
 const catalog=await response.json();const source=catalog.sources.at(-1);
 await page.goto('/resources/Deployment/production/checkout-api?tab=logs');
 await page.getByText('Select Pod/container sources',{exact:true}).click();
 await page.getByRole('checkbox',{name:source.pod+'/'+source.container,exact:true}).check();
 const loaded=page.waitForResponse(r=>r.url().includes('/logs?')&&new URL(r.url()).searchParams.has('sources'));
 await page.getByRole('button',{name:'Apply selected sources',exact:true}).click();
 const result=await (await loaded).json();
 expect(result.sources).toHaveLength(1);expect(result.sources[0].uid).toBe(source.uid);
 expect(new URL(page.url()).searchParams.get('logSources')).toContain(source.uid);
 await page.reload();
 await expect(page.getByRole('table',{name:'Log source coverage'}).locator('tbody tr')).toHaveCount(1);
});

test('links matching operational runbooks to stable issue rules',async({page})=>{
 await page.goto('/issues/issue-Deployment%3Aproduction%3Acheckout-api?namespace=production');
 await expect(page.getByRole('heading',{name:'Diagnostic rules & runbooks'})).toBeVisible();
 await expect(page.getByText('container-failure',{exact:false})).toBeVisible();
 const link=page.getByRole('link',{name:'Crash investigation runbook',exact:true});
 if(process.env.TIMONE_RUNBOOKS_FILE){
  await expect(link).toHaveAttribute('href','https://docs.example/runbooks/crashes');
  await expect(link).toHaveAttribute('rel','noopener noreferrer');
 }
});

test('explains scheduling constraints for a Pending Pod without claiming eligibility',async({page})=>{
 await page.goto('/resources/Pod/production/notification-worker-7c8d9f-m4n7q');
 await page.getByRole('tab',{name:'Scheduling',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Scheduling constraints'})).toBeVisible();
 await expect(page.getByRole('table',{name:'Node scheduling constraints'})).toContainText('harbor-worker');
 await expect(page.getByText('Passing these checks does not establish',{exact:false})).toBeVisible();
 const download=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export scheduling analysis',exact:true}).click();
 expect((await download).suggestedFilename()).toBe('scheduling-assessment.json');
});

test('compares rollout generations, revision identities and current Pod images',async({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api');
 await page.getByRole('tab',{name:'Rollout',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Rollout investigation'})).toBeVisible();
 await expect(page.getByRole('table',{name:'Rollout revisions'})).toContainText('checkout-api-7c8d9f');
 await expect(page.getByRole('table',{name:'Rollout Pods'})).toContainText('checkout-api');
 await expect(page.getByText('Reported target revision',{exact:true})).toBeVisible();
 await expect(page.getByRole('table',{name:'Rollout Pods'}).getByRole('link',{name:'Logs',exact:true}).first()).toBeVisible();
});

test('shows node maintenance evidence with no eviction controls',async({page})=>{
 await page.goto('/resources/Node/_/harbor-worker-01');
 await page.getByRole('tab',{name:'Maintenance',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Node maintenance assessment'})).toBeVisible();
 await expect(page.getByRole('table',{name:'Node maintenance Pod checks'})).toContainText('redis-0');
 await expect(page.getByRole('table',{name:'Shared node disruption budgets'})).toBeVisible();
 await expect(page.getByRole('button',{name:/^(Drain|Cordon|Evict)/})).toHaveCount(0);
});

test('traces published network endpoints and evaluates an explicit source Pod',async({page})=>{
 await page.goto('/resources/Service/production/checkout-api');
 await page.getByRole('tab',{name:'Network paths',exact:true}).click();
 await expect(page.getByRole('table',{name:'Published network endpoints'})).toContainText('checkout-api-7c8d9f-k9x2p');
 await page.getByLabel('Source Pod',{exact:true}).fill('payments-7c8d9f-k9x2p');
 await page.getByRole('button',{name:'Analyze source Pod',exact:true}).click();
 await expect(page).toHaveURL(/sourcePod=payments/);
 await expect(page.getByText('Policy source:',{exact:false})).toContainText('payments');
 await page.reload();
 await expect(page.getByLabel('Source Pod',{exact:true})).toHaveValue('payments-7c8d9f-k9x2p');
 const downloading=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export network analysis',exact:true}).click();
 const stream=await (await downloading).createReadStream();const chunks=[];for await(const chunk of stream)chunks.push(chunk);
 const analysis=JSON.parse(Buffer.concat(chunks).toString('utf8'));
 expect(analysis.state).toBe('configuration-only');expect(analysis.source.name).toBe('payments-7c8d9f-k9x2p');
 expect(analysis.paths[0].endpoints[0].pod.name).toBe('checkout-api-7c8d9f-k9x2p');
});

test('inspects retained CronJob executions without inferring missed schedules',async({page})=>{
 await page.goto('/resources/CronJob/platform/database-backup');
 await page.getByRole('tab',{name:'Executions',exact:true}).click();
 await expect(page.getByRole('table',{name:'Retained Job executions'})).toContainText('database-backup-29183040');
 await expect(page.getByText('0 2 * * * · Europe/Rome',{exact:true})).toBeVisible();
 await page.getByRole('table',{name:'Retained Job executions'}).getByRole('link',{name:'database-backup-29183040',exact:true}).click();
 await page.getByRole('tab',{name:'Executions',exact:true}).click();
 await expect(page.getByRole('table',{name:'Retained Job executions'})).toContainText('Complete=True');
 const download=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export execution analysis',exact:true}).click();
 expect((await download).suggestedFilename()).toBe('batch-analysis.json');
});

test('compares sampled resource efficiency and exports explicit coverage',async({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api');
 await page.getByRole('tab',{name:'Resource efficiency',exact:true}).click();
 await expect(page.getByRole('table',{name:'Container resource efficiency'})).toContainText('P95:');
 await expect(page.getByText(/^Source: simulated · Window:/)).toBeVisible();
 await page.getByLabel('Efficiency observation window',{exact:true}).selectOption('1h');
 await expect(page).toHaveURL(/efficiencyRange=1h/);
 const downloading=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export efficiency analysis',exact:true}).click();
 const stream=await (await downloading).createReadStream();const chunks=[];for await(const chunk of stream)chunks.push(chunk);
 const analysis=JSON.parse(Buffer.concat(chunks).toString('utf8'));
 expect(analysis.range).toBe('1h');expect(analysis.source).toBe('simulated');
 expect(analysis.containers.length).toBeGreaterThan(0);
 const usage=analysis.containers[0].usage[0];
 expect(usage.samples).toBeGreaterThan(0);expect(usage.coverage).toBeLessThanOrEqual(1);expect(usage.p95).toBeGreaterThan(0);
 await page.reload();await expect(page.getByLabel('Efficiency observation window',{exact:true})).toHaveValue('1h');
});

test('inspects configuration references without exporting Secret payloads',async({page})=>{
 await page.goto('/resources/Deployment/production/checkout-api');
 await page.getByRole('tab',{name:'Configuration dependencies',exact:true}).click();
 const table=page.getByRole('table',{name:'Configuration dependency references'});
 await expect(table).toContainText('checkout-db');await expect(table).toContainText('key-unverified');
 await expect(table).toContainText('desired-template');await expect(table).toContainText('current-pod');
 const downloading=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export configuration analysis',exact:true}).click();
 const stream=await (await downloading).createReadStream();const chunks=[];for await(const chunk of stream)chunks.push(chunk);
 const analysis=JSON.parse(Buffer.concat(chunks).toString('utf8'));
 expect(analysis.references.length).toBeGreaterThan(0);
 for(const ref of analysis.references){expect(ref).not.toHaveProperty('data');expect(ref.resource).not.toHaveProperty('object');expect(ref.consumer).not.toHaveProperty('object');}
});

test('investigates Pod deletion requirements and exports observed identity',async({page})=>{
 await page.goto('/resources/Pod/production/redis-0');
 await page.getByRole('tab',{name:'Deletion diagnostics',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Deletion diagnostics',exact:true})).toBeVisible();
 const table=page.getByRole('table',{name:'Deletion dependency evidence'});
 await expect(table).toContainText('assigned-node');await expect(table).toContainText('uid-verified');
 await expect(page.getByText('metadata.finalizers: demo.timone.io/cleanup',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:/^(Force delete|Delete|Remove finalizer|Finalize)/})).toHaveCount(0);
 const downloading=page.waitForEvent('download');
 await page.getByRole('button',{name:'Export deletion analysis',exact:true}).click();
 const stream=await (await downloading).createReadStream();const chunks=[];for await(const chunk of stream)chunks.push(chunk);
 const analysis=JSON.parse(Buffer.concat(chunks).toString('utf8'));
 expect(analysis.resource.name).toBe('redis-0');expect(analysis.uid).toBe('Pod:production:redis-0');
 expect(analysis.deletion.metadataFinalizers).toContain('demo.timone.io/cleanup');
 expect(analysis.deletion.secondsPastTimestamp).toBeGreaterThan(0);
 await page.getByRole('checkbox',{name:'Show only related resources marked for deletion',exact:true}).check();
 await expect(table).not.toContainText('assigned-node');
});

test('follows PVC deletion evidence through its verified binding',async({page})=>{
 await page.goto('/resources/PersistentVolumeClaim/production/redis-data?tab=termination');
 const table=page.getByRole('table',{name:'Deletion dependency evidence'});
 await expect(table).toContainText('redis-0');await expect(table).toContainText('bound-volume');
 await expect(table).toContainText('redis-attachment');
 await expect(page.getByText('metadata.finalizers: kubernetes.io/pvc-protection',{exact:true})).toBeVisible();
 await expect(table).toContainText('does not establish detach progress');
});

test('shows namespace finalization reports and bounded inventory coverage',async({page})=>{
 await page.goto('/resources/Namespace/_/staging?tab=termination');
 await expect(page.getByText('spec.finalizers: kubernetes',{exact:true})).toBeVisible();
 await expect(page.getByRole('table',{name:'Deletion inventory coverage'})).toBeVisible();
 await expect(page.getByRole('table',{name:'Deletion dependency evidence'})).toContainText('catalog-preview');
 await expect(page.getByText('Supported-kind inventory is not API discovery;',{exact:false})).toBeVisible();
});
