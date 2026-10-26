import {
  AlertTriangle,
  Download,
  ExternalLink,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
} from 'lucide-react';
import { CopyButton, Panel, Status } from '../components/ui';
import { ago } from '../lib/api';
import type { UpdateState } from '../lib/desktop';
import { useUpdater } from '../lib/updater';
import { VERSION } from '../lib/version';

function statusLabel(state: UpdateState | null, checking: boolean) {
  if (checking || state?.status === 'checking') return 'Checking…';
  switch (state?.status) {
    case 'available':
      return state.version ? `Update ${state.version} available` : 'Update available';
    case 'downloading':
      return `Downloading ${Math.round(state.percent)}%`;
    case 'ready':
      return 'Ready to install';
    case 'error':
      return 'Update failed';
    default:
      return state?.checkedAt ? 'Up to date' : 'Not checked yet';
  }
}

function statusTone(state: UpdateState | null, checking: boolean) {
  if (checking || state?.status === 'checking') return 'info';
  if (state?.status === 'error') return 'critical';
  if (state?.status === 'ready') return 'healthy';
  if (state?.status === 'available' || state?.status === 'downloading') return 'warning';
  return state?.checkedAt ? 'healthy' : 'unknown';
}

function deliveryLabel(state: UpdateState | null) {
  if (!state) return 'Checking…';
  if (state.reason === 'development') return 'Unavailable in development';
  if (state.packageManager === 'homebrew') return 'Homebrew (manual)';
  if (state.packageManager === 'scoop') return 'Scoop (manual)';
  if (state.packageManager === 'deb') return 'System package (manual)';
  if (state.mode === 'self') return 'In-place macOS update';
  if (state.mode === 'squirrel') return 'Automatic';
  return 'Manual download';
}

function manualNote(state: UpdateState) {
  if (state.reason === 'package-manager') {
    return 'This copy belongs to a package manager. Updating it here would be overwritten on the next upgrade, so use the command below instead.';
  }
  if (state.reason === 'system-package') {
    return 'This copy was installed by your system package manager. Update it there so the package database stays consistent.';
  }
  if (state.reason === 'development') {
    return 'The updater runs only in a packaged application.';
  }
  return 'This build cannot replace its own bundle. Download the latest release from GitHub and install it as usual.';
}

export function ApplicationUpdates() {
  const { state, checking, check, install, openRelease } = useUpdater();
  const busy = checking || state?.status === 'checking' || state?.status === 'downloading';
  const checkingNow = checking || state?.status === 'checking';
  const showDownload = state?.status === 'available' && state.canInstall;
  const showRestart = state?.status === 'ready' && state.canInstall;
  const manualUpdate =
    state?.mode === 'manual' && (state.status === 'available' || state.status === 'error');

  return (
    <Panel
      className="span-full"
      title="Application updates"
      subtitle="Stable releases from GitHub"
      action={
        <button
          className="button"
          onClick={() => void check()}
          disabled={busy || state?.status === 'ready' || state?.canCheck === false}
        >
          {busy ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}
          {checkingNow
            ? 'Checking…'
            : state?.status === 'downloading'
              ? 'Downloading…'
              : 'Check for updates'}
        </button>
      }
    >
      <dl className="property-list">
        <dt>Installed version</dt>
        <dd>{state?.currentVersion || VERSION}</dd>
        <dt>Status</dt>
        <dd>
          <Status value={statusTone(state, checkingNow)} label={statusLabel(state, checkingNow)} />
        </dd>
        <dt>Delivery</dt>
        <dd>{deliveryLabel(state)}</dd>
        {state?.checkedAt && (
          <>
            <dt>Last checked</dt>
            <dd>{ago(state.checkedAt)}</dd>
          </>
        )}
      </dl>

      {state?.status === 'downloading' && (
        <div className="update-progress">
          <progress max={100} value={state.percent} aria-label="Update download progress" />
          <span>
            {Math.round(state.percent)}%
            {state.bytesPerSecond
              ? ` · ${(state.bytesPerSecond / (1024 * 1024)).toFixed(1)} MiB/s`
              : ''}
          </span>
        </div>
      )}

      {manualUpdate && state.command && (
        <div className="update-command">
          <code>{state.command}</code>
          <CopyButton text={state.command} label="Copy command" />
        </div>
      )}

      {manualUpdate && state && <div className="integration-note">{manualNote(state)}</div>}

      {state?.error && (
        <div className="update-error" role="alert">
          <AlertTriangle size={14} />
          {state.error}
        </div>
      )}

      <div className="update-actions">
        {showDownload && (
          <button className="button primary" onClick={install}>
            <Download size={14} />
            Download update
          </button>
        )}
        {showRestart && (
          <button className="button primary" onClick={install}>
            <RotateCcw size={14} />
            Restart and install
          </button>
        )}
        {manualUpdate && (
          <button className="button primary" onClick={openRelease}>
            <ExternalLink size={14} />
            Open GitHub release
          </button>
        )}
        {state?.status === 'error' && (
          <button className="button" onClick={() => void check()}>
            <RefreshCw size={14} />
            Try again
          </button>
        )}
        {state?.status === 'error' && !manualUpdate && (
          <button className="button" onClick={openRelease}>
            <ExternalLink size={14} />
            Open GitHub release
          </button>
        )}
      </div>
    </Panel>
  );
}
