type DesktopResult<T = undefined> = { ok: boolean; error?: string; value?: T };

export type UpdateStatus = 'idle' | 'checking' | 'available' | 'downloading' | 'ready' | 'error';
export type UpdateMode = 'squirrel' | 'self' | 'manual';
export type UpdateReason =
  | 'package-manager'
  | 'system-package'
  | 'unsigned'
  | 'read-only'
  | 'development';

export interface UpdateState {
  status: UpdateStatus;
  currentVersion: string;
  version: string | null;
  percent: number;
  bytesPerSecond: number | null;
  error: string | null;
  mode: UpdateMode;
  canCheck: boolean;
  canInstall: boolean;
  reason: UpdateReason | null;
  packageManager: 'homebrew' | 'scoop' | 'deb' | null;
  command: string | null;
  checkedAt: string | null;
}

interface DesktopBridge {
  configure(): Promise<DesktopResult>;
  reconnect(): Promise<DesktopResult>;
  update?: {
    state(): Promise<DesktopResult<UpdateState>>;
    check(): Promise<DesktopResult<UpdateState>>;
    install(): Promise<DesktopResult<boolean>>;
    openReleasePage(): Promise<DesktopResult<boolean>>;
    onEvent(callback: (state: UpdateState) => void): () => void;
  };
}

declare global {
  interface Window {
    timoneDesktop?: DesktopBridge;
  }
}

export const desktop = window.timoneDesktop;

function unwrap<T>(result: DesktopResult<T> | undefined, fallback: string): T {
  if (!result?.ok) throw new Error(result?.error || fallback);
  return result.value as T;
}

export async function configureDesktop() {
  const result = await desktop?.configure();
  if (!result?.ok) throw new Error(result?.error || 'Could not open connection settings.');
}

export async function reconnectDesktop() {
  const result = await desktop?.reconnect();
  if (!result?.ok) throw new Error(result?.error || 'Could not renew the desktop session.');
  window.location.reload();
}

export async function getUpdateState(): Promise<UpdateState> {
  return unwrap(await desktop?.update?.state(), 'Updates are available only in the desktop app.');
}

export async function checkForUpdate(): Promise<UpdateState> {
  return unwrap(await desktop?.update?.check(), 'Updates are available only in the desktop app.');
}

export async function installUpdate(): Promise<boolean> {
  return unwrap(await desktop?.update?.install(), 'Could not start the update.');
}

export async function openReleasePage(): Promise<boolean> {
  return unwrap(await desktop?.update?.openReleasePage(), 'Could not open the releases page.');
}
