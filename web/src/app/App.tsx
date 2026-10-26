import { Sidebar } from './Sidebar';
import {
  ArrowUpRight,
  Bell,
  Box,
  ChevronDown,
  FlaskConical,
  Layers3,
  Menu,
  Search,
  ShieldCheck,
  Terminal,
} from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { useLocation, useSearchParams } from 'react-router-dom';
import { ScopedLink as Link } from '../components/navigation';
import { UpdateBanner } from '../components/UpdateBanner';
import { ErrorState } from '../components/ui';
import { WorkspaceSwitcher } from '../features/WorkspaceSwitcher';
import { useAPI } from '../lib/api';
import { useNamespaces } from '../lib/namespaces';
import { useLiveUpdates } from '../lib/useLiveUpdates';
import { useMobileNavigation } from '../lib/useMobileNavigation';
import type { Cluster, Connection, Overview as OverviewType } from '../types';
import { navigation } from './MainNavigation';
import { SessionGate } from './SessionGate';
import { WorkspaceRoutes } from './WorkspaceRoutes';
import { GlobalSearchDialog } from './GlobalSearchDialog';

export function App() {
  return (
    <SessionGate>
      <Shell />
    </SessionGate>
  );
}
function Shell() {
  const cluster = useAPI<Cluster>('/api/v1/cluster');
  const namespaces = useNamespaces();
  const apiConnection = useAPI<Connection>('/api/v1/connection');
  const [params, setParams] = useSearchParams();
  const ns = params.get('namespace') ?? '';
  const overview = useAPI<OverviewType>(
    `/api/v1/overview?namespace=${encodeURIComponent(ns || cluster.data?.scope || '')}`,
  );
  const connection = useLiveUpdates();
  const [mobile, setMobile] = useState(false);
  const closeMobile = useCallback(() => setMobile(false), []);
  const mobileNavigation = useMobileNavigation(mobile, closeMobile);
  const [clusterOpen, setClusterOpen] = useState(false);
  const searchOpen = params.get('gsearch') === '1';
  const setSearchOpen = useCallback(
    (open: boolean) =>
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (open) next.set('gsearch', '1');
          else next.delete('gsearch');
          return next;
        },
        { replace: true },
      ),
    [setParams],
  );
  const location = useLocation();
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setSearchOpen(true);
      }
      if (e.key === 'Escape') {
        setSearchOpen(false);
        setMobile(false);
        setClusterOpen(false);
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [setSearchOpen]);
  const title =
    navigation
      .flatMap((g) => g.items)
      .find((i) =>
        i.path === '/' ? location.pathname === '/' : location.pathname.startsWith(i.path),
      )?.name ?? 'Settings';
  const issues =
    overview.data &&
    !overview.isError &&
    overview.data.warnings.length === 0 &&
    !['partial', 'unavailable'].includes(overview.data.customDiagnostics?.state)
      ? overview.data.issues.length
      : null;
  const scoped = cluster.data?.scope;
  const actualNS = ns || scoped || '';
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      {mobile && <div className="sidebar-scrim" onClick={() => setMobile(false)} />}
      <Sidebar
        sidebar={mobileNavigation.sidebar}
        narrow={mobileNavigation.narrow}
        mobile={mobile}
        closeMobile={closeMobile}
        namespace={ns}
        issues={issues}
      />
      <div className="workspace" inert={mobileNavigation.narrow && mobile}>
        <header className="topbar">
          <button
            className="icon-button mobile-menu"
            aria-label="Open navigation"
            ref={mobileNavigation.trigger}
            aria-controls="main-navigation"
            aria-expanded={mobile}
            onClick={() => setMobile(!mobile)}
          >
            <Menu size={19} />
          </button>
          <div className="cluster-switch-wrap">
            <button
              className="cluster-switch"
              aria-expanded={clusterOpen}
              onClick={() => setClusterOpen(!clusterOpen)}
            >
              <span className="cluster-icon">
                <Box size={16} />
              </span>
              <strong>{cluster.data?.name ?? 'Cluster'}</strong>
              <span className="tag">
                {cluster.data ? (cluster.data.mode === 'demo' ? 'DEMO' : 'REAL') : '…'}
              </span>
              <ChevronDown size={14} />
            </button>
            {clusterOpen && (
              <div className="cluster-popover">
                <span className="eyebrow">ACTIVE CLUSTER</span>
                <strong>{cluster.data?.name}</strong>
                <p>
                  {cluster.data?.mode === 'demo'
                    ? 'An isolated, simulated environment.'
                    : 'Uses the server’s configured credentials. API reachability is reported separately.'}
                </p>
                <WorkspaceSwitcher close={() => setClusterOpen(false)} />
              </div>
            )}
          </div>
          <span className="header-divider" />
          <span className="header-location">{title}</span>
          <button
            className="global-search"
            aria-label="Find anything in your cluster… ⌘ K"
            aria-keyshortcuts="Control+k Meta+k"
            onClick={() => setSearchOpen(true)}
          >
            <Search size={15} />
            <span>Find anything in your cluster…</span>
            <kbd>⌘ K</kbd>
          </button>
          <Link
            to="/settings"
            className={`api-connection ${apiConnection.data?.state ?? 'unknown'}`}
            aria-label={`${apiConnection.isError ? 'API unavailable' : apiConnection.data?.state === 'simulated' ? 'Demo isolated' : `API ${apiConnection.data?.state ?? 'checking'}`} connection settings`}
            title={
              apiConnection.error?.message ??
              apiConnection.data?.message ??
              'Checking Kubernetes API reachability'
            }
          >
            <span className="status-dot" />
            {apiConnection.isError
              ? 'API unavailable'
              : apiConnection.data?.state === 'simulated'
                ? 'Demo isolated'
                : `API ${apiConnection.data?.state ?? 'checking'}`}
          </Link>
          <span className={`connection ${connection === 'Updates live' ? 'connected' : ''}`}>
            <span className="status-dot" />
            {connection}
          </span>
          <Link className="icon-button" aria-label="View cluster issues" to="/issues">
            <Bell size={18} />
            {issues !== null && issues > 0 && <i />}
            {issues === null && (
              <span className="sr-only">Issue count unavailable or incomplete</span>
            )}
          </Link>
          <Link to="/settings" className="user-avatar" aria-label="OP workspace settings">
            <span aria-hidden="true">OP</span>
          </Link>
        </header>
        <section
          aria-label="Workspace connection"
          className={`mode-banner ${cluster.data?.mode === 'demo' ? 'demo' : ''}`}
        >
          {!cluster.data ? (
            <strong>Loading connection details…</strong>
          ) : cluster.data.mode === 'demo' ? (
            <>
              <FlaskConical size={14} />
              <strong>Demo environment</strong>
              <span>You’re exploring simulated Kubernetes data. No real cluster is connected.</span>
            </>
          ) : (
            <>
              <ShieldCheck size={14} />
              <strong>Read-only connection</strong>
              <span>All reads use the configured Kubernetes identity and RBAC permissions.</span>
            </>
          )}
          <Link to="/settings">
            Connection details
            <ArrowUpRight size={13} />
          </Link>
        </section>
        <UpdateBanner />
        <nav aria-label="Workspace context" className="contextbar">
          <div>
            <span>Workspace</span>
            <span className="breadcrumb-slash">/</span>
            <strong>{title}</strong>
            {location.pathname.startsWith('/resources/') && (
              <>
                <span className="breadcrumb-slash">/</span>
                <span>Resource details</span>
              </>
            )}
          </div>
          <label className="namespace-control">
            <Layers3 size={14} />
            <select
              aria-label="Namespace"
              value={actualNS}
              onChange={(e) => {
                const next = new URLSearchParams(params);
                if (e.target.value) next.set('namespace', e.target.value);
                else next.delete('namespace');
                next.delete('offset');
                next.delete('goffset');
                setParams(next);
              }}
              disabled={!!scoped}
            >
              {!scoped && <option value="">All namespaces</option>}
              {!scoped && actualNS && !namespaces.data?.items.some((r) => r.name === actualNS) && (
                <option value={actualNS}>{actualNS}</option>
              )}
              {(scoped ? [scoped] : (namespaces.data?.items.map((r) => r.name) ?? [])).map((n) => (
                <option key={n}>{n}</option>
              ))}
            </select>
            {namespaces.isError && <span role="status">Namespace list unavailable</span>}
            {namespaces.data?.truncated && <span role="status">Namespace list incomplete</span>}
          </label>
        </nav>
        <main id="main" className="main-content" tabIndex={-1}>
          {cluster.isError ? (
            <ErrorState error={cluster.error} />
          ) : (
            <WorkspaceRoutes namespace={actualNS} cluster={cluster.data} />
          )}
        </main>
        <footer className="workspace-footer">
          <span>
            <Terminal size={12} />
            Built for the people behind the cluster.
          </span>
          <span>
            Read-only by design <span className="footer-dot">·</span> No telemetry
          </span>
        </footer>
      </div>
      <GlobalSearchDialog
        namespace={actualNS}
        cluster={cluster.data}
        open={searchOpen}
        onOpenChange={setSearchOpen}
      />
    </div>
  );
}
