import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { resourceURL } from '../lib/api';
import {
  maxSavedEntries,
  parseSavedEntries,
  preferenceProfile,
  savedURL,
  type SavedEntry,
} from '../lib/saved';
import type { Cluster, Resource } from '../types';
import { Empty, PageTitle, Panel } from '../components/ui';

type PersonalWorkspace = {
  enabled: boolean;
  items: SavedEntry[];
  message: string;
  save: (name: string, href: string, type: SavedEntry['type']) => void;
  remove: (id: string) => void;
  rename: (id: string, name: string) => void;
};
const WorkspaceContext = createContext<PersonalWorkspace>({
  enabled: false,
  items: [],
  message: 'Cluster identity unavailable.',
  save: () => {},
  remove: () => {},
  rename: () => {},
});
export function SavedWorkspaceProvider({
  cluster,
  children,
}: {
  cluster?: Cluster;
  children: ReactNode;
}) {
  const key = cluster?.key ? preferenceProfile(cluster.key, cluster.scope) : '';
  return <PreferenceStore storageKey={key}>{children}</PreferenceStore>;
}
function PreferenceStore({ storageKey, children }: { storageKey: string; children: ReactNode }) {
  const initial = useMemo(() => {
    if (!storageKey) return { items: [] as SavedEntry[], message: 'Cluster identity unavailable.' };
    try {
      const raw = localStorage.getItem(storageKey);
      return { items: raw ? parseSavedEntries(raw) : [], message: '' };
    } catch {
      return {
        items: [] as SavedEntry[],
        message:
          'Stored preferences unavailable or invalid. Changes remain usable in this session.',
      };
    }
  }, [storageKey]);
  const [stored, setStored] = useState({ storageKey, ...initial });
  const active = stored.storageKey === storageKey ? stored : { storageKey, ...initial };
  const items = active.items;
  const message = active.message;
  useEffect(() => {
    setStored({ storageKey, ...initial });
  }, [storageKey, initial]);
  useEffect(() => {
    const update = (event: StorageEvent) => {
      if (event.key !== storageKey) return;
      try {
        setStored({
          storageKey,
          items: event.newValue ? parseSavedEntries(event.newValue) : [],
          message: '',
        });
      } catch {
        setStored((previous) => ({
          ...previous,
          message: 'A saved preference update was invalid and was ignored.',
        }));
      }
    };
    window.addEventListener('storage', update);
    return () => window.removeEventListener('storage', update);
  }, [storageKey]);
  const setMessage = (message: string) => setStored({ storageKey, items, message });
  const commit = (next: SavedEntry[]) => {
    const raw = JSON.stringify({ schemaVersion: 1, items: next });
    if (raw.length > 256 * 1024) {
      setMessage('Saved preferences exceed the storage limit. Remove entries first.');
      return;
    }
    let message = 'Saved in this browser.';
    try {
      localStorage.setItem(storageKey, raw);
    } catch {
      message = 'Saved for this session only; browser storage is unavailable.';
    }
    setStored({ storageKey, items: next, message });
  };
  const value: PersonalWorkspace = {
    enabled: !!storageKey,
    items,
    message,
    save: (name, href, type) => {
      if (!storageKey) return;
      if (!name.trim() || name.length > 80) {
        setMessage('Choose a name of 1–80 characters.');
        return;
      }
      try {
        const url = savedURL(href);
        const existing = items.find((item) => item.href === url && item.type === type);
        if (!existing && items.length >= maxSavedEntries) {
          setMessage('Up to 100 saved entries are supported. Remove an entry first.');
          return;
        }
        const entry: SavedEntry = {
          id: existing?.id ?? crypto.randomUUID(),
          type,
          name: name.trim(),
          href: url,
          createdAt: existing?.createdAt ?? new Date().toISOString(),
        };
        commit(
          existing
            ? items.map((item) => (item.id === existing.id ? entry : item))
            : [...items, entry],
        );
      } catch {
        setMessage('This destination cannot be saved.');
      }
    },
    remove: (id) => {
      if (storageKey) commit(items.filter((item) => item.id !== id));
    },
    rename: (id, name) => {
      if (!storageKey) return;
      if (!name.trim() || name.length > 80) {
        setMessage('Choose a name of 1–80 characters.');
        return;
      }
      commit(items.map((item) => (item.id === id ? { ...item, name: name.trim() } : item)));
    },
  };
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}
export function SaveCurrentView() {
  const personal = useContext(WorkspaceContext);
  const location = useLocation();
  const [name, setName] = useState('');
  return (
    <>
      <details className="diagnostic-policy">
        <summary>Save this view</summary>
        <form
          className="filterbar"
          onSubmit={(event) => {
            event.preventDefault();
            personal.save(name, location.pathname + location.search, 'view');
          }}
        >
          <label>
            View name{' '}
            <input
              aria-label="Saved view name"
              maxLength={80}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <button className="button" disabled={!personal.enabled}>
            Save view
          </button>
          <Link className="button" to="/saved">
            Open saved workspace
          </Link>
        </form>
      </details>
      {location.pathname !== '/saved' && personal.message && (
        <p role="status">{personal.message}</p>
      )}
    </>
  );
}
export function FavoriteResource({ resource }: { resource: Resource }) {
  const personal = useContext(WorkspaceContext);
  const target = new URL(resourceURL(resource), 'https://timone.invalid');
  if (resource.namespace) target.searchParams.set('namespace', resource.namespace);
  const href = savedURL(target.pathname + target.search);
  const existing = personal.items.find((entry) => entry.type === 'favorite' && entry.href === href);
  return (
    <button
      className="button"
      disabled={!personal.enabled}
      aria-pressed={!!existing}
      onClick={() =>
        existing
          ? personal.remove(existing.id)
          : personal.save(`${resource.kind}/${resource.name}`.slice(0, 80), href, 'favorite')
      }
    >
      {existing ? 'Remove favorite' : 'Favorite resource'}
    </button>
  );
}
function SavedRow({ entry }: { entry: SavedEntry }) {
  const personal = useContext(WorkspaceContext);
  const [name, setName] = useState(entry.name);
  useEffect(() => setName(entry.name), [entry.name]);
  return (
    <li>
      <Link className="resource-link" to={entry.href}>
        {entry.name}
      </Link>
      <form
        className="filterbar"
        onSubmit={(e) => {
          e.preventDefault();
          personal.rename(entry.id, name);
        }}
      >
        <input
          aria-label={`Rename ${entry.name}`}
          maxLength={80}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button className="button">Rename</button>
        <button
          className="button"
          type="button"
          onClick={() => personal.remove(entry.id)}
          aria-label={`Remove ${entry.name}`}
        >
          Remove
        </button>
      </form>
    </li>
  );
}
export function SavedWorkspace() {
  const personal = useContext(WorkspaceContext);
  return (
    <>
      <PageTitle
        title="Saved workspace"
        description="Named views and favorite resources for this cluster and configured namespace scope. Stored only in this browser."
      />
      <p className="footnote">
        Saved links do not preserve resource data or grant access. Resources and Pod identities may
        change; opening a link uses current permissions.
      </p>
      {personal.message && <p role="status">{personal.message}</p>}
      {(['view', 'favorite'] as const).map((type) => (
        <Panel key={type} title={type === 'view' ? 'Saved views' : 'Favorite resources'}>
          <ul>
            {personal.items
              .filter((entry) => entry.type === type)
              .map((entry) => (
                <SavedRow key={entry.id} entry={entry} />
              ))}
          </ul>
          {!personal.items.some((entry) => entry.type === type) && (
            <Empty title={type === 'view' ? 'No saved views' : 'No favorite resources'} />
          )}
        </Panel>
      ))}
    </>
  );
}
