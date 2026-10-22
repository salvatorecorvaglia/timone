type DesktopResult = { ok: boolean; error?: string };

declare global {
  interface Window {
    timoneDesktop?: {
      configure(): Promise<DesktopResult>;
      reconnect(): Promise<DesktopResult>;
    };
  }
}

export const desktop = window.timoneDesktop;

export async function configureDesktop() {
  const result = await desktop?.configure();
  if (!result?.ok) throw new Error(result?.error || 'Could not open connection settings.');
}

export async function reconnectDesktop() {
  const result = await desktop?.reconnect();
  if (!result?.ok) throw new Error(result?.error || 'Could not renew the desktop session.');
  window.location.reload();
}
