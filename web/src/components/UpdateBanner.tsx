import { AlertTriangle, Download, ExternalLink, RefreshCw, RotateCcw, X } from 'lucide-react';
import type { UpdateState } from '../lib/desktop';
import { useUpdater } from '../lib/updater';

function title(state: UpdateState) {
  switch (state.status) {
    case 'available':
      return `Timone ${state.version ?? 'update'} is available`;
    case 'downloading':
      return `Downloading Timone ${state.version ?? 'update'}`;
    case 'ready':
      return `Timone ${state.version ?? 'update'} is ready`;
    default:
      return 'Update failed';
  }
}

function description(state: UpdateState) {
  if (state.status === 'error') return state.error;
  if (state.status === 'downloading') {
    return `${Math.round(state.percent)}%${
      state.bytesPerSecond ? ` · ${(state.bytesPerSecond / (1024 * 1024)).toFixed(1)} MiB/s` : ''
    }`;
  }
  if (state.status === 'ready') return 'Restart Timone to finish installing it.';
  if (state.mode === 'manual') {
    if (state.reason === 'package-manager') {
      return `This copy is managed by ${state.packageManager === 'homebrew' ? 'Homebrew' : 'Scoop'}. Update it outside Timone.`;
    }
    if (state.reason === 'system-package') {
      return 'This copy is managed by your system package manager. Update it there.';
    }
    return 'Download the latest release from GitHub.';
  }
  return 'Download it now; it installs when you restart.';
}

/** Compact, app-wide notice. The full controls live in Settings. */
export function UpdateBanner() {
  const { state, dismissed, check, install, openRelease, dismiss } = useUpdater();

  if (!state || dismissed) return null;
  if (!['available', 'downloading', 'ready', 'error'].includes(state.status)) return null;

  const manual = state.mode === 'manual';
  const canInstall = state.canInstall;

  return (
    <section
      className={`update-banner ${state.status}`}
      role={state.status === 'error' ? 'alert' : 'status'}
    >
      <div className="update-banner-icon">
        {state.status === 'error' ? <AlertTriangle size={15} /> : <Download size={15} />}
      </div>
      <div className="update-banner-copy">
        <strong>{title(state)}</strong>
        <span>{description(state)}</span>
        {state.status === 'downloading' && (
          <progress max={100} value={state.percent} aria-label="Update download progress" />
        )}
        {manual && state.command && ['available', 'error'].includes(state.status) && (
          <code>{state.command}</code>
        )}
      </div>
      <div className="update-banner-actions">
        {state.status === 'available' && canInstall && (
          <button className="button primary" onClick={install}>
            <Download size={13} />
            Download
          </button>
        )}
        {state.status === 'available' && manual && (
          <button className="button primary" onClick={openRelease}>
            <ExternalLink size={13} />
            Open GitHub
          </button>
        )}
        {state.status === 'ready' && canInstall && (
          <button className="button primary" onClick={install}>
            <RotateCcw size={13} />
            Restart and install
          </button>
        )}
        {state.status === 'error' && (
          <button className="button" onClick={() => void check()}>
            <RefreshCw size={13} />
            Try again
          </button>
        )}
        {state.status === 'error' && (
          <button className="button" onClick={openRelease}>
            <ExternalLink size={13} />
            GitHub
          </button>
        )}
      </div>
      <button className="update-banner-close" aria-label="Dismiss update notice" onClick={dismiss}>
        <X size={14} />
      </button>
    </section>
  );
}
