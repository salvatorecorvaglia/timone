import { VERSION } from '../lib/version';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowRight, Check, Info, LockKeyhole, Moon, Sun } from 'lucide-react';
import { ago, request, useAPI } from '../lib/api';
import { readPreference, writePreference } from '../lib/preferences';
import type { Cluster, AccessSession, Connection } from '../types';
import {
  TableScroll,
  CopyButton,
  ErrorState,
  PageTitle,
  Panel,
  Status,
  Warnings,
} from '../components/ui';
import { desktop, configureDesktop } from '../lib/desktop';

export function Settings({ cluster }: { cluster?: Cluster }) {
  const session = useAPI<AccessSession>('/api/v1/session');
  const connection = useAPI<Connection>('/api/v1/connection');
  const [theme, setTheme] = useState(() => readPreference('timone-theme', 'dark'));
  const [signingOut, setSigningOut] = useState(false);
  const [signOutError, setSignOutError] = useState<Error | null>(null);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    writePreference('timone-theme', theme);
  }, [theme]);
  const navigate = useNavigate();
  const mcp = JSON.stringify(
    {
      mcpServers: {
        timone: {
          url: `${window.location.origin}/mcp`,
          ...(session.data?.mode === 'proxy'
            ? {}
            : // biome-ignore lint/suspicious/noTemplateCurlyInString: Client configuration expands this environment variable.
              { headers: { Authorization: 'Bearer ${TIMONE_TOKEN}' } }),
        },
      },
    },
    null,
    2,
  );
  return (
    <>
      <PageTitle
        eyebrow="YOUR WORKSPACE"
        title="Settings"
        description="Connection details, interface preferences, and read-only assistant access."
      />
      <div className="settings-grid">
        <Panel title="Cluster connection">
          <dl className="property-list">
            <dt>Cluster</dt>
            <dd>{cluster?.name}</dd>
            <dt>Mode</dt>
            <dd>
              <span className="tag">
                {cluster?.mode === 'demo' || connection.data?.mode === 'demo'
                  ? 'SIMULATED DEMO'
                  : cluster?.mode === 'cluster'
                    ? 'REAL KUBERNETES'
                    : 'CHECKING CONNECTION'}
              </span>
            </dd>
            <dt>API status</dt>
            <dd>
              {connection.data ? (
                <Status
                  value={
                    connection.data.state === 'connected'
                      ? 'healthy'
                      : connection.data.state === 'simulated'
                        ? 'info'
                        : 'warning'
                  }
                  label={connection.data.state}
                />
              ) : connection.isError ? (
                'Unavailable'
              ) : (
                'Checking…'
              )}
            </dd>
            <dt>Kubernetes version</dt>
            <dd>
              {connection.data?.serverVersion ??
                (cluster?.mode === 'demo' ? 'Simulated environment' : 'Not reported')}
            </dd>
            <dt>Authentication</dt>
            <dd>{connection.data?.auth ?? 'Not reported'}</dd>
            <dt>Last successful read</dt>
            <dd>
              {connection.data?.lastSuccessfulRead
                ? ago(connection.data.lastSuccessfulRead)
                : 'Not observed'}
            </dd>
            <dt>Namespace scope</dt>
            <dd>{cluster?.scope || 'All authorized namespaces'}</dd>
            <dt>Access</dt>
            <dd>
              <LockKeyhole size={13} />
              Read only
            </dd>
            <dt>Data storage</dt>
            <dd>In-memory, no persistent database</dd>
            <dt>External transmission</dt>
            <dd>No telemetry or third-party data forwarding</dd>
          </dl>
          {connection.isError && <ErrorState error={connection.error} />}
          <Warnings items={connection.data?.retentionWarnings} />
          {connection.data && (
            <div className="integration-note">
              {connection.data.message}
              <br />
              {connection.data.discovery}
            </div>
          )}
          <div className="integration-note">
            {desktop
              ? 'Choose a kubeconfig and context from Change connection. Local workspaces can also switch contexts from the cluster menu.'
              : 'Select a cluster at server startup with TIMONE_MODE, KUBECONFIG, TIMONE_CONTEXT, and TIMONE_NAMESPACE. Local loopback workspaces can switch contexts from the cluster menu. Shared deployments keep a fixed workspace.'}
          </div>
          <div className="integration-note">
            Workspace identity: {session.data?.identity?.name || 'Not reported'} ·{' '}
            {desktop
              ? 'Private desktop session. Kubernetes access uses your selected kubeconfig.'
              : session.data?.mode === 'proxy'
                ? 'Authenticated by your identity proxy. Kubernetes access uses the server’s configured identity.'
                : 'Administrator token access. Browser sessions expire after eight hours and can be revoked.'}
          </div>
          {signOutError && <ErrorState error={signOutError} />}
          {desktop ? (
            <button
              className="button settings-signout"
              disabled={signingOut}
              onClick={() => {
                setSigningOut(true);
                void configureDesktop()
                  .catch(setSignOutError)
                  .finally(() => setSigningOut(false));
              }}
            >
              Change connection
            </button>
          ) : session.data?.mode === 'proxy' ? (
            <p className="integration-note">
              Sign out through your organization’s identity provider.
            </p>
          ) : session.data?.authRequired ? (
            <button
              className="button settings-signout"
              disabled={signingOut}
              onClick={() => {
                setSigningOut(true);
                setSignOutError(null);
                void request('/api/v1/session', { method: 'DELETE' })
                  .then(() => window.location.reload())
                  .catch(() =>
                    setSignOutError(
                      new Error('Sign out failed. Your session may still be active. Please retry.'),
                    ),
                  )
                  .finally(() => setSigningOut(false));
              }}
            >
              {signingOut ? 'Signing out…' : 'Sign out'}
            </button>
          ) : (
            <p className="integration-note">Local anonymous workspace</p>
          )}
        </Panel>
        <Panel title="Appearance">
          <div className="settings-description">
            <p>Choose the interface theme for this browser.</p>
            <div className="theme-options">
              <button
                className={theme === 'dark' ? 'selected' : ''}
                onClick={() => setTheme('dark')}
              >
                <Moon size={22} />
                <span>Dark</span>
                {theme === 'dark' && <Check size={14} />}
              </button>
              <button
                className={theme === 'light' ? 'selected' : ''}
                onClick={() => setTheme('light')}
              >
                <Sun size={22} />
                <span>Light</span>
                {theme === 'light' && <Check size={14} />}
              </button>
            </div>
            <p className="muted">
              Table density can be adjusted in Resources. Preferences stay in this browser.
            </p>
          </div>
          <div className="detail-section">
            <h3>About Timone</h3>
            <p className="muted">Version {VERSION} · A foundation for Kubernetes clarity.</p>
            <button className="text-link" onClick={() => navigate('/integrations')}>
              Inspect available integrations
              <ArrowRight size={13} />
            </button>
          </div>
        </Panel>
        {connection.data && (
          <Panel
            title="Kubernetes observation"
            subtitle="Shared client-go informers and collection coverage"
            className="span-full"
            action={
              <button className="button" onClick={() => void connection.refetch()}>
                Check connection
              </button>
            }
          >
            <Warnings items={connection.data.collectionFailures} />
            {connection.data.streams.length ? (
              <TableScroll>
                <table>
                  <thead>
                    <tr>
                      <th>Resource kind</th>
                      <th>Namespace</th>
                      <th>Observer state</th>
                      <th>Last change</th>
                      <th>Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {connection.data.streams.map((s) => (
                      <tr key={`${s.kind}:${s.namespace}`}>
                        <td>{s.kind}</td>
                        <td>{s.namespace || 'Cluster-wide'}</td>
                        <td>
                          <Status
                            value={
                              s.state === 'watching'
                                ? 'healthy'
                                : s.state === 'syncing'
                                  ? 'unknown'
                                  : 'warning'
                            }
                            label={s.state}
                          />
                        </td>
                        <td>{s.lastSync ? ago(s.lastSync) : 'Not observed'}</td>
                        <td>
                          {s.message ||
                            (s.state === 'watching'
                              ? 'Active shared observer'
                              : s.state === 'syncing'
                                ? 'Initial synchronization'
                                : 'Observer stopped; current reads use polling')}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </TableScroll>
            ) : (
              <div className="integration-note">
                {connection.data.mode === 'demo'
                  ? 'Demo refreshes are simulated. No Kubernetes informer is started.'
                  : 'Observers start after a resource collection is read. Collections without an observer use bounded polling.'}
              </div>
            )}
          </Panel>
        )}
        <Panel
          title="MCP assistant access"
          subtitle="Official MCP Go SDK · Streamable HTTP · 23 read-only tools"
          className="span-full"
          action={desktop ? undefined : <CopyButton text={mcp} label="Copy configuration" />}
        >
          <div className="manifest-notice">
            <Info size={15} />
            {desktop
              ? 'For external MCP clients, run the Timone CLI or an in-cluster deployment with a dedicated access token. The desktop session stays inside this application.'
              : session.data?.mode === 'proxy'
                ? 'Clients must authenticate through the same configured identity proxy. The proxy injects its private upstream credential; never distribute that credential to clients.'
                : 'Clients must support Streamable HTTP and administrator-provided bearer authentication. Local unauthenticated mode can omit the headers field. Environment substitution depends on the client.'}
          </div>
          {!desktop && (
            <pre className="code-block" tabIndex={0} aria-label="MCP client configuration">
              <code>{mcp}</code>
            </pre>
          )}
          <div className="integration-note">
            MCP uses the same provider, resource redaction, namespace scope, and permissions as this
            UI. It provides no shell, filesystem, or write tools.
          </div>
        </Panel>
      </div>
    </>
  );
}
