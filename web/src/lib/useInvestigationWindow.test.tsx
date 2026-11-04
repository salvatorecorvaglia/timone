import { fireEvent, screen, waitFor } from '@testing-library/react';
import { Link } from 'react-router-dom';
import { expect, it } from 'vitest';
import { useInvestigationWindow } from './useInvestigationWindow';
import { show } from '../features/test-helpers';

function Controls() {
  const window = useInvestigationWindow();
  return (
    <>
      {window.controls}
      <output data-testid="valid">{String(window.valid)}</output>
      <Link to="?since=2026-01-01T10:00:00Z&until=2026-01-01T11:00:00Z&namespace=production">
        Saved interval
      </Link>
    </>
  );
}

it('restores shared intervals during same-page navigation and preserves namespace when applying', async () => {
  show(<Controls />, '/history?since=invalid&until=invalid&namespace=production');
  expect(screen.getByTestId('valid')).toHaveTextContent('false');
  fireEvent.click(screen.getByText('Saved interval'));
  await waitFor(() => expect(screen.getByTestId('valid')).toHaveTextContent('true'));
  expect(screen.getByLabelText('Investigation from')).not.toHaveValue('');
  fireEvent.click(screen.getByRole('button', { name: 'Apply interval' }));
  expect(screen.getByTestId('location')).toHaveTextContent('namespace=production');
  expect(screen.getByTestId('valid')).toHaveTextContent('true');
});

it('rejects reversed and excessively long windows from shared URLs', () => {
  const view = show(<Controls />, '/history?since=2026-01-02T10:00:00Z&until=2026-01-01T11:00:00Z');
  expect(screen.getByTestId('valid')).toHaveTextContent('false');
  expect(screen.getByRole('alert')).toHaveTextContent('Select a past interval');
  view.unmount();
  show(<Controls />, '/history?since=2026-01-01T10:00:00Z&until=2026-01-03T11:00:00Z');
  expect(screen.getByTestId('valid')).toHaveTextContent('false');
});
