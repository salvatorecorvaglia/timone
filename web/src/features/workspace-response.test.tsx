import { waitFor } from '@testing-library/react';
import { vi, it, expect } from 'vitest';
import { request } from '../lib/api';

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
