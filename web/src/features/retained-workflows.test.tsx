import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { Link } from 'react-router-dom';
import { expect, it, vi } from 'vitest';
import { Snapshots } from './Snapshots';
import { PersistentHistory } from './PersistentHistory';
import { fixtures, show } from './test-helpers';

const snapshot = fixtures['/api/v1/resources/Deployment/production/checkout-api/snapshot'];
const interval = '/history?since=2026-01-01T10:00:00Z&until=2026-01-01T11:00:00Z';
const upload = (text: Promise<string>, size = 100) => ({ size, text: () => text });
const select = (side: string, file: unknown) =>
  fireEvent.change(screen.getByLabelText(side + ' snapshot file'), { target: { files: [file] } });

it('restores a different snapshot target during same-route navigation', async () => {
  show(
    <>
      <Link to="/snapshots?kind=Pod&resourceNamespace=staging&name=next">Next target</Link>
      <Snapshots namespace="production" />
    </>,
    '/snapshots?kind=Deployment&resourceNamespace=production&name=first',
  );
  expect(screen.getByLabelText('Snapshot resource name')).toHaveValue('first');
  fireEvent.click(screen.getByText('Next target'));
  await waitFor(() => expect(screen.getByLabelText('Snapshot resource name')).toHaveValue('next'));
  expect(screen.getByLabelText('Snapshot namespace')).toHaveValue('staging');
  expect(screen.getByLabelText('Snapshot resource kind')).toHaveValue('Pod');
});

it('keeps the latest imported snapshot when an older read finishes later', async () => {
  let finish!: (text: string) => void;
  const older = new Promise<string>((resolve) => {
    finish = resolve;
  });
  const fetcher = vi.spyOn(globalThis, 'fetch');
  show(<Snapshots namespace="production" />);
  select('Before', upload(older));
  select('Before', upload(Promise.resolve(JSON.stringify(snapshot))));
  await screen.findByText(/Before:/);
  await act(async () => {
    finish('{invalid');
    await older;
  });
  expect(screen.getByText(/Before:/)).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(fetcher).not.toHaveBeenCalled();
});

it('rejects oversized and malformed imports without sending them to the server', async () => {
  const fetcher = vi.spyOn(globalThis, 'fetch');
  show(<Snapshots namespace="production" />);
  select('Before', upload(Promise.resolve('{}'), 2 * 1024 * 1024 + 1));
  await screen.findByText('Snapshot exceeds 2 MiB.');
  select('After', upload(Promise.resolve('{}')));
  await screen.findByText('Unsupported or invalid snapshot schema.');
  expect(fetcher).not.toHaveBeenCalled();
});

it('shows capture failures and permits another attempt', async () => {
  const fetcher = vi
    .spyOn(globalThis, 'fetch')
    .mockResolvedValue(
      new Response(JSON.stringify({ error: { message: 'Capture unavailable' } }), { status: 503 }),
    );
  show(<Snapshots namespace="production" />, '/snapshots?name=checkout-api');
  fireEvent.click(screen.getByRole('button', { name: 'Capture snapshot' }));
  await screen.findByText('Capture unavailable');
  expect(screen.getByRole('button', { name: 'Capture snapshot' })).toBeEnabled();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it('does not request retained observations when the collector is disabled', async () => {
  const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(
      JSON.stringify({
        enabled: false,
        state: 'disabled',
        message: 'Collector disabled',
      }),
    ),
  );
  show(<PersistentHistory namespace="production" />, interval);
  await screen.findByText('Persistent history is disabled');
  expect(fetcher.mock.calls.every(([url]) => String(url) === '/api/v1/history/status')).toBe(true);
});

it('renders collection gaps and coverage warnings and refreshes retained history', async () => {
  const fetcher = vi.spyOn(globalThis, 'fetch').mockImplementation(
    async (input) =>
      new Response(
        JSON.stringify(
          String(input) === '/api/v1/history/status'
            ? {
                enabled: true,
                state: 'sampling',
                intervalSeconds: 30,
                retentionHours: 24,
                maxRecords: 2000,
                maxMiB: 64,
              }
            : {
                samples: ['2026-01-01T10:00:00Z', '2026-01-01T10:05:00Z'],
                changes: [],
                events: [],
                warnings: [{ kind: 'Pod', message: 'Collection incomplete' }],
                message: 'Only readable identities returned',
              },
        ),
      ),
  );
  show(<PersistentHistory namespace="production" />, interval);
  await screen.findByText(/Sampling gap:/);
  expect(screen.getByText('Only readable identities returned')).toBeInTheDocument();
  expect(screen.getByText(/No retained changes/)).toBeInTheDocument();
  const count = () =>
    fetcher.mock.calls.filter(([url]) => String(url).startsWith('/api/v1/history?')).length;
  const initial = count();
  fireEvent.click(screen.getByRole('button', { name: 'Refresh retained history' }));
  await waitFor(() => expect(count()).toBe(initial + 1));
});

it('surfaces unavailable retained history instead of presenting an empty successful result', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
    String(input) === '/api/v1/history/status'
      ? new Response(JSON.stringify({ enabled: true, state: 'sampling', intervalSeconds: 30 }))
      : new Response(JSON.stringify({ error: { message: 'Archive unavailable' } }), {
          status: 503,
        }),
  );
  show(<PersistentHistory namespace="production" />, interval);
  await screen.findByText('Archive unavailable');
  expect(screen.queryByText(/No retained changes/)).not.toBeInTheDocument();
});
