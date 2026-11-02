export interface SavedEntry {
  id: string;
  type: 'view' | 'favorite';
  name: string;
  href: string;
  createdAt: string;
}
export const maxSavedEntries = 100;
const paths = new Set([
  '/',
  '/resources',
  '/custom-resources',
  '/applications',
  '/issues',
  '/events',
  '/topology',
  '/metrics',
  '/audit',
  '/integrations',
  '/helm',
  '/gitops',
  '/settings',
  '/snapshots',
  '/history',
  '/saved',
]);
const parameters = new Set([
  'namespace',
  'kind',
  'search',
  'status',
  'phase',
  'sort',
  'desc',
  'limit',
  'offset',
  'group',
  'nodes',
  'focusKind',
  'focusNS',
  'focusName',
  'depth',
  'direction',
  'target',
  'definition',
  'version',
  'tab',
  'since',
  'until',
  'resource',
  'historyKind',
  'historyName',
  'historyNamespace',
  'historyContainer',
  'historyRange',
  'logContainer',
  'logPrevious',
  'logTail',
  'logSources',
  'resourceNamespace',
  'name',
  'efficiencyRange',
  'sourcePod',
  'sourceNamespace',
]);
export function savedURL(href: string) {
  if (href.length > 4096 || !href.startsWith('/') || href.startsWith('//'))
    throw new Error('Saved views require a local application URL.');
  const url = new URL(href, 'https://timone.invalid');
  const first = '/' + url.pathname.split('/')[1];
  if (url.origin !== 'https://timone.invalid' || (!paths.has(url.pathname) && !paths.has(first)))
    throw new Error('Unsupported saved destination.');
  const query = new URLSearchParams();
  for (const [key, value] of url.searchParams) if (parameters.has(key)) query.set(key, value);
  query.sort();
  return url.pathname + (query.size ? '?' + query : '');
}
export function parseSavedEntries(text: string): SavedEntry[] {
  if (text.length > 256 * 1024) throw new Error('Saved preferences exceed the storage limit.');
  const data = JSON.parse(text);
  if (
    data?.schemaVersion !== 1 ||
    !Array.isArray(data.items) ||
    data.items.length > maxSavedEntries
  )
    throw new Error('Invalid saved preferences.');
  const seen = new Set<string>();
  return data.items.map((item: SavedEntry) => {
    if (
      !item ||
      typeof item.id !== 'string' ||
      !/^[a-zA-Z0-9-]{1,80}$/.test(item.id) ||
      seen.has(item.id) ||
      !['view', 'favorite'].includes(item.type) ||
      typeof item.name !== 'string' ||
      !item.name.trim() ||
      item.name.length > 80 ||
      typeof item.href !== 'string' ||
      !Number.isFinite(Date.parse(item.createdAt))
    )
      throw new Error('Invalid saved entry.');
    seen.add(item.id);
    return { ...item, href: savedURL(item.href) };
  });
}
export function preferenceProfile(clusterKey: string, scope: string) {
  return `timone-saved-v1:${clusterKey}:${encodeURIComponent(scope)}`;
}
