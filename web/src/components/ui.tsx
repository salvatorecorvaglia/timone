import { ScopedLink as Link } from './navigation';
import type { ReactNode } from 'react';
import { AlertCircle, ArrowUpRight, Box, Check, Copy, Inbox, LoaderCircle } from 'lucide-react';

import { useEffect, useRef, useState } from 'react';
import type { Health, Resource, Warning } from '../types';
import { resourceURL } from '../lib/api';
export function Status({ value, label }: { value: Health | string; label?: string }) {
  return (
    <span className={`status ${value}`}>
      <span className="status-dot" />
      {label ??
        {
          healthy: 'Healthy',
          warning: 'Warning',
          critical: 'Critical',
          unknown: 'Unknown',
          info: 'Info',
        }[value] ??
        value}
    </span>
  );
}
export function ResourceLink({ resource }: { resource: Resource }) {
  return (
    <Link className="resource-link" to={resourceURL(resource)}>
      <Box size={15} />
      <span>{resource.name}</span>
      <ArrowUpRight size={13} />
    </Link>
  );
}
export function PageTitle({
  eyebrow,
  title,
  description,
  action,
}: {
  eyebrow?: string;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="page-title">
      <div>
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action}
    </div>
  );
}
export function Panel({
  title,
  subtitle,
  action,
  children,
  className = '',
}: {
  title?: string;
  subtitle?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      {title && (
        <div className="panel-header">
          <div>
            <h2>{title}</h2>
            {subtitle && <p>{subtitle}</p>}
          </div>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}
export function Empty({
  title = 'No matching resources',
  description = 'Try changing the search or namespace filters.',
}: {
  title?: string;
  description?: string;
}) {
  return (
    <div className="empty">
      <Inbox size={30} />
      <h3>{title}</h3>
      <p>{description}</p>
    </div>
  );
}
export function Loading() {
  return (
    <div className="loading" role="status">
      <LoaderCircle className="spin" size={21} /> Loading cluster data…
    </div>
  );
}
export function ErrorState({ error, retry }: { error: Error; retry?: () => void }) {
  return (
    <div className="error-state" role="alert">
      <AlertCircle size={22} />
      <div>
        <h3>Unable to retrieve data</h3>
        <p>{error.message}</p>
        {retry && (
          <button className="button" onClick={retry}>
            Try again
          </button>
        )}
      </div>
    </div>
  );
}
export function Warnings({ items }: { items?: Warning[] }) {
  return items?.length ? (
    <details className="data-warning">
      <summary>
        <AlertCircle size={15} />
        {items.length} collection{items.length === 1 ? '' : 's'} unavailable or incomplete
      </summary>
      {items.map((w, i) => (
        <p key={`${w.kind}-${i}`}>
          <strong>{w.kind}:</strong> {w.message}
        </p>
      ))}
    </details>
  ) : null;
}
export function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  return (
    <button
      className="button subtle"
      onClick={() => {
        setError(false);
        setCopied(false);
        clearTimeout(timer.current);
        if (!navigator.clipboard?.writeText) {
          setError(true);
          return;
        }
        void Promise.resolve()
          .then(() => navigator.clipboard.writeText(text))
          .then(() => {
            setCopied(true);
            timer.current = setTimeout(() => setCopied(false), 2000);
          })
          .catch(() => setError(true));
      }}
    >
      {copied ? <Check size={14} /> : <Copy size={14} />}{' '}
      {error ? 'Clipboard unavailable' : copied ? 'Copied' : label}
    </button>
  );
}
