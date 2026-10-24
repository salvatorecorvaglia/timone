import * as Dialog from '@radix-ui/react-dialog';
import {
  ArrowUpRight,
  Bell,
  Box,
  ChevronDown,
  ChevronsUpDown,
  CircleHelp,
  Command,
  FlaskConical,
  Layers3,
  Menu,
  Search,
  Settings2,
  ShieldCheck,
  Terminal,
  X,
} from 'lucide-react';
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import {
  NavLink,
  Route,
  Routes,
  useLocation,
  useNavigate,
  useSearchParams,
} from 'react-router-dom';
import { Logo } from '../components/Logo';
import { ScopedLink as Link } from '../components/navigation';
import { PageBoundary } from '../components/PageBoundary';
import { Empty, ErrorState, Loading } from '../components/ui';
import { WorkspaceSwitcher } from '../features/WorkspaceSwitcher';
import { resourceURL, useAPI } from '../lib/api';
import { useNamespaces } from '../lib/namespaces';
import { withNamespace } from '../lib/navigation';
import { useLiveUpdates } from '../lib/useLiveUpdates';
import { useMobileNavigation } from '../lib/useMobileNavigation';
import { VERSION } from '../lib/version';
import type { Cluster, Connection, Overview as OverviewType } from '../types';
import { MainNavigation, navigation } from './MainNavigation';
import { SessionGate } from './SessionGate';

const Overview = lazy(() => import('../features/Overview').then((m) => ({ default: m.Overview })));
const Resources = lazy(() =>
  import('../features/Resources').then((m) => ({ default: m.Resources })),
);
const ResourceDetail = lazy(() =>
  import('../features/ResourceDetail').then((m) => ({ default: m.ResourceDetail })),
);
const CustomResources = lazy(() =>
  import('../features/CustomResources').then((m) => ({ default: m.CustomResources })),
);
const GlobalResults = lazy(() =>
  import('../features/GlobalSearch').then((m) => ({ default: m.GlobalResults })),
);
const Issues = lazy(() => import('../features/Issues').then((m) => ({ default: m.Issues })));
const Timeline = lazy(() => import('../features/Timeline').then((m) => ({ default: m.Timeline })));
const Audit = lazy(() => import('../features/Audit').then((m) => ({ default: m.Audit })));
const Integrations = lazy(() =>
  import('../features/Integrations').then((m) => ({ default: m.Integrations })),
);
const MetricsPage = lazy(() =>
  import('../features/MetricsPage').then((m) => ({ default: m.MetricsPage })),
);
const Settings = lazy(() => import('../features/Settings').then((m) => ({ default: m.Settings })));
const Applications = lazy(() =>
  import('../features/Applications').then((m) => ({ default: m.Applications })),
);
const HelmRelease = lazy(() =>
  import('../features/Helm').then((m) => ({ default: m.HelmRelease })),
);
const Topology = lazy(() => import('../features/Topology').then((m) => ({ default: m.Topology })));
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
  const search = params.get('gq') ?? '';
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
  const setSearch = (value: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('gq', value);
        next.delete('goffset');
        return next;
      },
      { replace: true },
    );
  const navigate = useNavigate();
  const location = useLocation();
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setSearchOpen(true);
        setTimeout(() => input.current?.focus(), 0);
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
      <aside
        id="main-navigation"
        ref={mobileNavigation.sidebar}
        inert={mobileNavigation.narrow && !mobile}
        className={`sidebar ${mobile ? 'open' : ''}`}
      >
        <Link to="/" className="brand">
          <Logo />
          Timone<span className="version">{VERSION}</span>
        </Link>
        <button className="button mobile-menu" onClick={closeMobile}>
          Close navigation
        </button>
        <div className="workspace-label">
          <span className="workspace-avatar">T</span>
          <div>
            Timone workspace<small>Cluster operations</small>
          </div>
          <ChevronsUpDown size={14} />
        </div>
        <MainNavigation namespace={ns} issues={issues} onNavigate={() => setMobile(false)} />
        <div className="sidebar-bottom">
          <NavLink to="/settings" onClick={() => setMobile(false)}>
            <Settings2 size={17} />
            Settings
          </NavLink>
          <a
            href="https://github.com/kubernetes/website/tree/main/content/en/docs"
            target="_blank"
            rel="noreferrer"
          >
            <CircleHelp size={17} />
            Kubernetes docs
            <ArrowUpRight size={13} />
          </a>
          <div className="sidebar-footer">
            <span className="status-dot healthy" />
            Timone v{VERSION}
            <span>READ ONLY</span>
          </div>
        </div>
      </aside>
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
            <PageBoundary key={location.pathname}>
              <Suspense fallback={<Loading />}>
                <Routes>
                  <Route path="/" element={<Overview namespace={actualNS} />} />
                  <Route path="/resources" element={<Resources namespace={actualNS} />} />
                  <Route
                    path="/custom-resources"
                    element={<CustomResources namespace={actualNS} />}
                  />
                  <Route
                    path="/custom-resources/:definition/:namespace/:name"
                    element={<ResourceDetail custom />}
                  />
                  <Route path="/applications" element={<Applications namespace={actualNS} />} />
                  <Route path="/resources/:kind/:namespace/:name" element={<ResourceDetail />} />
                  <Route path="/issues" element={<Issues namespace={actualNS} />} />
                  <Route path="/issues/:id" element={<Issues namespace={actualNS} />} />
                  <Route path="/events" element={<Timeline namespace={actualNS} />} />
                  <Route path="/topology" element={<Topology namespace={actualNS} />} />
                  <Route path="/metrics" element={<MetricsPage namespace={actualNS} />} />
                  <Route path="/audit" element={<Audit namespace={actualNS} />} />
                  <Route path="/integrations" element={<Integrations namespace={actualNS} />} />
                  <Route
                    path="/helm"
                    element={<Integrations namespace={actualNS} filter="Helm" />}
                  />
                  <Route path="/helm/:namespace/:name" element={<HelmRelease />} />
                  <Route
                    path="/gitops"
                    element={<Integrations namespace={actualNS} filter="GitOps" />}
                  />
                  <Route path="/settings" element={<Settings cluster={cluster.data} />} />
                  <Route
                    path="*"
                    element={
                      <Empty
                        title="Page not found"
                        description="Choose a view from the navigation to continue."
                      />
                    }
                  />
                </Routes>
              </Suspense>
            </PageBoundary>
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
      <Dialog.Root open={searchOpen} onOpenChange={setSearchOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="search-backdrop" />
          <Dialog.Content className="search-dialog" aria-describedby={undefined}>
            <Dialog.Title className="sr-only">Global resource search</Dialog.Title>
            <div className="search-input">
              <Search size={20} />
              <input
                ref={input}
                autoFocus
                placeholder="Search resources across your cluster…"
                aria-label="Search all resources"
                maxLength={200}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <button
                className="icon-button"
                aria-label="Close search"
                onClick={() => setSearchOpen(false)}
              >
                <X size={18} />
              </button>
            </div>
            <PageBoundary>
              <Suspense fallback={<Loading />}>
                <GlobalResults
                  search={search}
                  namespace={actualNS}
                  kinds={cluster.data?.kinds ?? []}
                  onSelect={(r) => {
                    navigate(withNamespace(resourceURL(r), actualNS));
                  }}
                />
              </Suspense>
            </PageBoundary>
            <div className="search-footer">
              <Command size={12} /> Resources you have permission to read <kbd>esc to close</kbd>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
