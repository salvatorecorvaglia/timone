import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  checkForUpdate,
  desktop,
  getUpdateState,
  installUpdate,
  openReleasePage,
  type UpdateState,
} from './desktop';

interface UpdaterValue {
  /** False in a browser tab: the bridge exists only in the desktop shell. */
  supported: boolean;
  state: UpdateState | null;
  checking: boolean;
  dismissed: boolean;
  check: () => Promise<void>;
  install: () => void;
  openRelease: () => void;
  dismiss: () => void;
}

const EMPTY: UpdaterValue = {
  supported: false,
  state: null,
  checking: false,
  dismissed: false,
  check: async () => {},
  install: () => {},
  openRelease: () => {},
  dismiss: () => {},
};

const UpdaterContext = createContext<UpdaterValue>(EMPTY);

/**
 * One subscription for the whole app.
 *
 * The update banner and the Settings panel both read this, so a download that
 * starts in one is reflected in the other, and there is exactly one listener
 * on the preload bridge. Main keeps the authoritative state; `getUpdateState`
 * catches up on anything emitted before this React tree mounted (the startup
 * check can finish while the welcome page is still showing).
 */
export function UpdaterProvider({ children }: { children: ReactNode }) {
  const supported = Boolean(desktop?.update);
  const [state, setState] = useState<UpdateState | null>(null);
  const [checking, setChecking] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const sawEvent = useRef(false);
  const eventSeq = useRef(0);

  useEffect(() => {
    if (!supported) return;
    let active = true;
    sawEvent.current = false;
    void getUpdateState()
      .then((next) => {
        // An event can arrive while the snapshot request is in flight; it is
        // newer than the snapshot, so do not let the snapshot overwrite it.
        if (active && !sawEvent.current) setState(next);
      })
      .catch(() => {
        // The shell may be between pages; a later event or remount catches up.
      });
    const unsubscribe = desktop?.update?.onEvent((next) => {
      eventSeq.current += 1;
      sawEvent.current = true;
      if (active) {
        setState(next);
        if (next.status !== 'checking') setChecking(false);
      }
    });
    return () => {
      active = false;
      unsubscribe?.();
    };
  }, [supported]);

  // A new version, or an update becoming installable, deserves attention even
  // if the user hid the banner earlier.
  // biome-ignore lint/correctness/useExhaustiveDependencies: the effect intentionally reacts to update state changes.
  useEffect(() => {
    setDismissed(false);
  }, [state?.version, state?.status]);

  const check = useCallback(async () => {
    if (!supported) return;
    setChecking(true);
    const before = eventSeq.current;
    try {
      const next = await checkForUpdate();
      // Events are newer than the snapshot the invoke resolves with; do not
      // overwrite an update-available/progress event that beat the response.
      if (eventSeq.current === before) setState(next);
    } catch (error) {
      if (eventSeq.current === before) {
        const message = error instanceof Error ? error.message : String(error);
        setState((previous) =>
          previous ? { ...previous, status: 'error', error: message } : previous,
        );
      }
    } finally {
      setChecking(false);
    }
  }, [supported]);

  const install = useCallback(() => {
    if (!supported) return;
    void installUpdate().catch((error: unknown) => {
      const message = error instanceof Error ? error.message : String(error);
      setState((previous) =>
        previous ? { ...previous, status: 'error', error: message } : previous,
      );
    });
  }, [supported]);

  const openRelease = useCallback(() => {
    if (!supported) return;
    void openReleasePage().catch(() => {});
  }, [supported]);

  const dismiss = useCallback(() => setDismissed(true), []);

  const value = useMemo(
    () => ({ supported, state, checking, dismissed, check, install, openRelease, dismiss }),
    [supported, state, checking, dismissed, check, install, openRelease, dismiss],
  );

  return <UpdaterContext.Provider value={value}>{children}</UpdaterContext.Provider>;
}

export function useUpdater(): UpdaterValue {
  return useContext(UpdaterContext);
}
