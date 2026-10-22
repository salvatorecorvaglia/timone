import { screen, fireEvent, waitFor } from '@testing-library/react';
import { Route, Routes } from 'react-router-dom';
import { vi, describe, it, expect } from 'vitest';
import { MetricsHistory } from './MetricsHistory';
import { historyRows } from '../lib/history';
import { MetricsPage } from './Observability';
import type { MetricHistory } from '../types';
import { show, mockAPI, fixtures } from './test-helpers';

describe('Historical metrics', () => {
  it('opens a current container measurement directly into a history investigation', async () => {
    mockAPI();
    show(
      <Routes>
        <Route path="/metrics" element={<MetricsPage namespace="production" />} />
      </Routes>,
      '/metrics',
    );
    fireEvent.click(
      await screen.findByRole('link', {
        name: 'History for payments-7c8d9f-k9x2p payments',
      }),
    );
    expect(await screen.findByText('Pod/payments-7c8d9f-k9x2p')).toBeInTheDocument();
    expect(screen.getByLabelText('History resource name')).toHaveValue('payments-7c8d9f-k9x2p');
  });
  it('persists resource selection and range in the URL and exposes simulated sample gaps', async () => {
    mockAPI();
    show(<MetricsHistory namespace="" />, '/metrics');
    expect(await screen.findByText(/Simulated historical CPU and memory/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('History resource type'), { target: { value: 'Node' } });
    fireEvent.change(screen.getByLabelText('History resource name'), {
      target: { value: 'harbor-worker-01' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'View history' }));
    expect(await screen.findByText('Node/harbor-worker-01')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('historyName=harbor-worker-01');
    expect(screen.getByLabelText('Historical CPU chart')).toBeInTheDocument();
    fireEvent.click(screen.getByText('View sample table'));
    fireEvent(screen.getByText('View sample table').parentElement!, new Event('toggle'));
    // Test the data grid directly as DOM does not lay out the SVG container.
    const history = fixtures[
      '/api/v1/metrics/history?kind=Node&name=harbor-worker-01&range=1h'
    ] as MetricHistory;
    expect(historyRows(history, 'cpu').some((row) => row.series0 === null)).toBe(true);
    fireEvent.change(screen.getByLabelText('History time range'), { target: { value: '6h' } });
    fireEvent.click(screen.getByRole('button', { name: 'View history' }));
    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent('historyRange=6h'),
    );
  });
  it('restores a container investigation from a shared URL and keeps the namespace ceiling', async () => {
    const api = mockAPI();
    show(
      <MetricsHistory namespace="production" />,
      '/metrics?historyKind=Pod&historyName=checkout-api-7c8d9f-m4n7q&historyNamespace=other&historyContainer=checkout-api',
    );
    expect(await screen.findByText('Pod/checkout-api-7c8d9f-m4n7q')).toBeInTheDocument();
    expect(screen.getByLabelText('History namespace')).toHaveValue('production');
    expect(screen.getByLabelText('History namespace')).toBeDisabled();
    expect(
      api.mock.calls.some(
        ([url]) =>
          String(url).includes('/history?') && String(url).includes('namespace=production'),
      ),
    ).toBe(true);
  });
  it('explains disabled history without issuing monitoring queries', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify({
            state: 'disabled',
            source: 'prometheus',
            message: 'An administrator must configure a Prometheus endpoint.',
          }),
        ),
    );
    show(<MetricsHistory namespace="" />, '/metrics?historyKind=Node&historyName=harbor-worker-01');
    expect(
      await screen.findByRole('heading', { name: 'Historical metrics not configured' }),
    ).toBeInTheDocument();
    expect(fetch.mock.calls.every(([url]) => String(url).endsWith('/history/status'))).toBe(true);
  });
  it('preserves missing timestamps as gaps and refuses to show unavailable history as zero', async () => {
    const fixture = fixtures[
      '/api/v1/metrics/history?kind=Node&name=harbor-worker-01&range=1h'
    ] as MetricHistory;
    const history = {
      ...fixture,
      series: fixture.series.map((s) => ({ ...s, points: s.points.slice(1) })),
    };
    expect(historyRows(history, 'cpu')[0].series0).toBeNull();
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async (input) =>
        new Response(
          JSON.stringify(
            String(input).endsWith('/status')
              ? {
                  state: 'configured',
                  source: 'prometheus',
                  message: 'Configured, availability unverified.',
                }
              : {
                  ...fixture,
                  state: 'unavailable',
                  source: 'prometheus',
                  mode: 'cluster',
                  series: [],
                  message: 'Prometheus query permission denied.',
                  warnings: [
                    {
                      kind: 'cpu',
                      message: 'Prometheus authentication or query permission denied',
                    },
                  ],
                },
          ),
        ),
    );
    show(<MetricsHistory namespace="" />, '/metrics?historyKind=Node&historyName=harbor-worker-01');
    expect(
      await screen.findByRole('heading', { name: 'No usable historical samples' }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText('Historical CPU chart')).not.toBeInTheDocument();
  });
});
