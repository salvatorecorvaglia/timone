import { screen, fireEvent, waitFor } from '@testing-library/react';
import { vi, it, expect } from 'vitest';
import { WorkspaceSwitcher } from './WorkspaceSwitcher';
import { show } from './test-helpers';

it('confirms a runtime workspace switch with the active revision and clears its route', async () => {
  const close = vi.fn();
  const spy = vi.spyOn(globalThis, 'fetch').mockImplementation(
    async (_input, options) =>
      new Response(
        JSON.stringify(
          options?.method === 'POST'
            ? { revision: 2 }
            : {
                enabled: true,
                context: 'one',
                namespace: 'production',
                revision: 1,
                contexts: ['one', 'two'],
                message: 'Workspace reset',
              },
        ),
        { status: 200 },
      ),
  );
  show(<WorkspaceSwitcher close={close} />, '/resources?namespace=production');
  fireEvent.change(await screen.findByLabelText('Kubeconfig context'), {
    target: { value: 'two' },
  });
  fireEvent.change(screen.getByLabelText('Workspace namespace ceiling'), { target: { value: '' } });
  expect(screen.getByRole('button', { name: 'Switch workspace' })).toBeDisabled();
  fireEvent.click(screen.getByLabelText('Confirm workspace reset'));
  fireEvent.click(screen.getByRole('button', { name: 'Switch workspace' }));
  await waitFor(() => expect(close).toHaveBeenCalled());
  expect(spy).toHaveBeenCalledWith(
    '/api/v1/workspace',
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ context: 'two', namespace: '', revision: 1, confirm: true }),
    }),
  );
});
