import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { useMobileNavigation } from './useMobileNavigation';

function Navigation() {
  const [open, setOpen] = useState(false);
  const { sidebar, trigger, narrow } = useMobileNavigation(open, () => setOpen(false));
  return (
    <>
      <button ref={trigger} onClick={() => setOpen(true)}>
        Open navigation
      </button>
      <aside ref={sidebar} data-testid="sidebar" inert={narrow && !open}>
        <a href="#first">First</a>
        <button>Last</button>
      </aside>
      <output>{narrow ? 'Mobile' : 'Desktop'}</output>
    </>
  );
}

afterEach(() => vi.unstubAllGlobals());

it('traps mobile focus in both directions and restores the trigger on Escape', () => {
  vi.stubGlobal('innerWidth', 390);
  render(<Navigation />);
  expect(screen.getByTestId('sidebar')).toHaveAttribute('inert');
  fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }));
  const first = screen.getByRole('link', { name: 'First' });
  const last = screen.getByRole('button', { name: 'Last' });
  expect(first).toHaveFocus();
  fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
  expect(last).toHaveFocus();
  fireEvent.keyDown(document, { key: 'Tab' });
  expect(first).toHaveFocus();
  fireEvent.keyDown(document, { key: 'Escape' });
  expect(screen.getByRole('button', { name: 'Open navigation' })).toHaveFocus();
  expect(screen.getByTestId('sidebar')).toHaveAttribute('inert');
});

it('releases the mobile focus trap when resized to desktop', () => {
  vi.stubGlobal('innerWidth', 390);
  render(<Navigation />);
  fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }));
  vi.stubGlobal('innerWidth', 1440);
  fireEvent(window, new Event('resize'));
  expect(screen.getByText('Desktop')).toBeInTheDocument();
  expect(screen.getByTestId('sidebar')).not.toHaveAttribute('inert');
});
