import { useQuery } from '@tanstack/react-query';
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
let workspaceRevision: string | null = null;
export async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const startedRevision = workspaceRevision;
  const response = await fetch(path, { credentials: 'same-origin', ...options });
  const revision = response.headers.get('X-Timone-Workspace');
  if (revision && workspaceRevision && revision !== workspaceRevision) {
    if (startedRevision !== workspaceRevision)
      throw new APIError('Previous workspace response discarded', 409);
    workspaceRevision = revision;
    if (!(path === '/api/v1/workspace' && options?.method === 'POST'))
      window.dispatchEvent(new Event('timone:workspace-changed'));
  } else if (revision) workspaceRevision = revision;
  let body: Awaited<ReturnType<Response['json']>>;
  try {
    body = await response.json();
  } catch {
    throw new APIError(
      'Timone returned an unreadable response. Check the server connection.',
      response.status,
    );
  }
  if (revision && revision !== workspaceRevision)
    throw new APIError('Previous workspace response discarded', 409);
  if (!response.ok) {
    if (
      response.status === 401 &&
      (response.headers.get('WWW-Authenticate')?.startsWith('Bearer') ||
        response.headers.get('X-Timone-Auth-Required') === 'proxy')
    )
      window.dispatchEvent(new Event('timone:unauthorized'));
    throw new APIError(body.error?.message ?? 'Request failed', response.status);
  }
  return body as T;
}
export function useAPI<T>(path: string) {
  return useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<T>(path, { signal }),
    refetchInterval: 30000,
    retry: 1,
  });
}
export const resourceURL = (r: {
  kind: string;
  namespace: string;
  name: string;
  definition?: string;
  version?: string;
}) =>
  r.definition
    ? `/custom-resources/${encodeURIComponent(r.definition)}/${encodeURIComponent(r.namespace || '_')}/${encodeURIComponent(r.name)}${r.version ? `?version=${encodeURIComponent(r.version)}` : ''}`
    : `/resources/${encodeURIComponent(r.kind)}/${encodeURIComponent(r.namespace || '_')}/${encodeURIComponent(r.name)}`;
export function ago(date: string) {
  if (!date) return 'Unknown';
  const seconds = Math.max(0, Math.floor((Date.now() - Date.parse(date)) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}
export const memory = (bytes: number) =>
  bytes >= 2 ** 30 ? `${(bytes / 2 ** 30).toFixed(1)} GiB` : `${(bytes / 2 ** 20).toFixed(0)} MiB`;
