import { VERSION } from '../lib/version';
import { useEffect, useState, type ReactNode } from 'react';
import { ArrowUpRight } from 'lucide-react';
import { useSession } from '../lib/useSession';
import { readPreference } from '../lib/preferences';
import { request } from '../lib/api';
import { ErrorState, Loading } from '../components/ui';
import { Logo } from '../components/Logo';
import { desktop, reconnectDesktop, configureDesktop } from '../lib/desktop';

export function SessionGate({ children }: { children: ReactNode }) {
  useEffect(() => {
    document.documentElement.dataset.theme = readPreference('timone-theme', 'dark');
  }, []);
  const { authMode, authorized, error, authorize } = useSession();
  if (error) return <ErrorState error={error} retry={() => window.location.reload()} />;
  if (authorized === null) return <Loading />;
  if (!authorized && authMode === 'proxy')
    return (
      <main className="login-screen">
        <ErrorState
          error={
            new Error(
              'Sign in through your organization’s configured identity proxy, then reload Timone.',
            )
          }
          retry={() => window.location.reload()}
        />
      </main>
    );
  if (!authorized && desktop) return <DesktopLogin />;
  if (!authorized) return <Login onSuccess={authorize} />;
  return children;
}
function DesktopLogin() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const run = (action: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    void action()
      .catch(setError)
      .finally(() => setBusy(false));
  };
  return (
    <main className="login-screen">
      <div className="login-card">
        <div className="brand">
          <Logo />
          Timone
        </div>
        <h1>Resume your workspace.</h1>
        <p>Your desktop session has expired. Reconnect to continue.</p>
        {error && <ErrorState error={error} />}
        <button className="button primary" disabled={busy} onClick={() => run(reconnectDesktop)}>
          {busy ? 'Connecting…' : 'Reconnect'}
        </button>
        <button className="button" disabled={busy} onClick={() => run(configureDesktop)}>
          Change connection
        </button>
      </div>
    </main>
  );
}
function Login({ onSuccess }: { onSuccess: () => void }) {
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <main className="login-screen">
      <div className="login-card">
        <div className="brand">
          <Logo />
          Timone<span className="version">{VERSION}</span>
        </div>
        <h1>Your cluster, in focus.</h1>
        <p>Enter the access token configured by your Timone administrator.</p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setBusy(true);
            void request('/api/v1/session', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ token }),
            })
              .then(onSuccess)
              .catch((e) => setError(e.message))
              .finally(() => setBusy(false));
          }}
        >
          <label htmlFor="token">Access token</label>
          <input
            id="token"
            autoComplete="current-password"
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            required
            minLength={32}
          />
          {error && (
            <p role="alert" className="text-critical">
              {error}
            </p>
          )}
          <button className="button primary" disabled={busy}>
            {busy ? 'Connecting…' : 'Open workspace'}
            <ArrowUpRight size={16} />
          </button>
        </form>
        <small>Read-only cluster access · Data stays in your environment</small>
      </div>
    </main>
  );
}
