import { expect, it, vi } from 'vitest';
import { request } from './api';

it.each([200, 503])('reports unreadable responses with their HTTP status (%s)', async (status) => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response('<html>unavailable</html>', { status }),
  );
  await expect(request('/api/v1/cluster')).rejects.toMatchObject({
    status,
    message: expect.stringContaining('unreadable response'),
  });
});

it('does not sign out the browser for a Kubernetes credential failure', async () => {
  const unauthorized = vi.fn();
  window.addEventListener('timone:unauthorized', unauthorized);
  try {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { message: 'Kubernetes authentication failed' } }), {
        status: 401,
      }),
    );
    await expect(request('/api/v1/resources')).rejects.toMatchObject({ status: 401 });
    expect(unauthorized).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener('timone:unauthorized', unauthorized);
  }
});
