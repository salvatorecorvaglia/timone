import { waitFor } from '@testing-library/react';
import { vi, it, expect } from 'vitest';
import { request } from '../lib/api';

it.each([200, 409, 503])(
  'resets exactly once when a workspace POST observes a new revision (%s)',
  async (status) => {
    const changed = vi.fn();
    window.addEventListener('timone:workspace-changed', changed);
    let revision = `switch-${status}:1`;
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async (_input, options) =>
        new Response(
          JSON.stringify(
            options?.method === 'POST' && status !== 200
              ? { error: { message: 'Workspace changed' } }
              : {},
          ),
          {
            status: options?.method === 'POST' ? status : 200,
            headers: { 'X-Timone-Workspace': revision },
          },
        ),
    );
    try {
      await request('/api/v1/workspace');
      changed.mockClear();
      revision = `switch-${status}:2`;
      const switching = request('/api/v1/workspace', { method: 'POST' });
      if (status === 200) await switching;
      else await expect(switching).rejects.toMatchObject({ status });
      await request('/api/v1/workspace');
      expect(changed).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener('timone:workspace-changed', changed);
    }
  },
);

it('discards a retired workspace response even when its JSON body completes late', async () => {
  let bodyController: ReadableStreamDefaultController<Uint8Array> | undefined;
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const path = String(input);
    return new Response(
      path.endsWith('/slow')
        ? new ReadableStream<Uint8Array>({
            start(controller) {
              bodyController = controller;
            },
          })
        : '{}',
      {
        headers: {
          'X-Timone-Workspace': path.endsWith('/new') ? 'isolation-test:2' : 'isolation-test:1',
        },
      },
    );
  });
  await request('/api/v1/isolation/start');
  const old = request('/api/v1/isolation/slow');
  await waitFor(() => expect(bodyController).toBeDefined());
  await request('/api/v1/isolation/new');
  const rejected = expect(old).rejects.toMatchObject({ status: 409 });
  bodyController!.enqueue(new TextEncoder().encode('{"old":"workspace"}'));
  bodyController!.close();
  await rejected;
});
