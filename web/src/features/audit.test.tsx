import { screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { Audit } from './Audit';
import { show, mockAPI } from './test-helpers';

describe('Security audit evidence', () => {
  it('shows readable findings and coverage, and keeps filtering distinct from an empty audit', async () => {
    mockAPI();
    show(<Audit namespace="production" />);
    expect(await screen.findByRole('heading', { name: 'Configuration findings' })).toBeVisible();
    expect(screen.getByRole('heading', { name: 'Check coverage' })).toBeVisible();
    expect(screen.getByText('configuration findings')).toBeVisible();
    fireEvent.change(screen.getByLabelText('Search findings, rules, or resources…'), {
      target: { value: 'no-such-evidence' },
    });
    expect(await screen.findByText('No matching findings')).toBeVisible();
    expect(screen.getByRole('heading', { name: 'Check coverage' })).toBeVisible();
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'critical' } });
    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent('status=critical'),
    );
  });
  it('reports failed collections rather than a clean security result', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(JSON.stringify({ error: { message: 'Audit read failed' } }), { status: 503 }),
    );
    show(<Audit namespace="production" />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Audit read failed');
    expect(screen.queryByText('No matching findings')).not.toBeInTheDocument();
  });
});
