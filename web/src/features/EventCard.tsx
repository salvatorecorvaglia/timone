import { ScopedLink as Link } from '../components/navigation';
import { ago } from '../lib/api';
import type { KEvent } from '../types';
import { Status } from '../components/ui';

export function EventCard({ event: e, resourceHref }: { event: KEvent; resourceHref?: string }) {
  return (
    <article className={`event-card ${e.type === 'Warning' ? 'warning' : ''}`}>
      <div className="event-card-top">
        <Status value={e.type === 'Warning' ? 'warning' : 'healthy'} label={e.type} />
        <strong>{e.reason}</strong>
        <time title={`First: ${e.firstObserved || 'unknown'} · Last: ${e.timestamp}`}>
          {ago(e.timestamp)}
        </time>
      </div>
      <p>{e.message}</p>
      <div className="event-card-bottom">
        <Link
          to={
            resourceHref ??
            (e.customDefinition
              ? `/custom-resources/${encodeURIComponent(e.customDefinition)}/${encodeURIComponent((e.resourceNamespace ?? e.namespace) || '_')}/${encodeURIComponent(e.resource)}`
              : `/resources/${encodeURIComponent(e.kind)}/${encodeURIComponent((e.resourceNamespace ?? e.namespace) || '_')}/${encodeURIComponent(e.resource)}`)
          }
        >
          {e.kind}/{e.resource}
        </Link>
        <span>
          {e.namespace || 'Cluster scoped'} · {e.count} occurrence{e.count === 1 ? '' : 's'}
        </span>
      </div>
    </article>
  );
}
