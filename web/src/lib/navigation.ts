export function withNamespace(to: string, namespace: string): string {
  if (!namespace || (!to.startsWith('/') && !to.startsWith('?')) || to.startsWith('//')) return to;
  const [target, hash] = to.split('#', 2);
  const mark = target.indexOf('?');
  const path = mark < 0 ? target : target.slice(0, mark);
  const query = new URLSearchParams(mark < 0 ? '' : target.slice(mark + 1));
  if (!query.has('namespace')) query.set('namespace', namespace);
  return `${path}?${query}${hash === undefined ? '' : `#${hash}`}`;
}
