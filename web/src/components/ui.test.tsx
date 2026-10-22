import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { CopyButton } from './ui';
if (!('clipboard' in navigator))
  Object.defineProperty(navigator, 'clipboard', { configurable: true, get: () => undefined });

describe('Clipboard feedback', () => {
  it('handles a missing clipboard without throwing', () => {
    vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue(undefined as unknown as Clipboard);
    render(<CopyButton text="safe text" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    expect(screen.getByRole('button', { name: 'Clipboard unavailable' })).toBeVisible();
  });
  it('handles rejection and clears the error on a successful retry', async () => {
    const writeText = vi
      .fn()
      .mockRejectedValueOnce(new Error('denied'))
      .mockResolvedValueOnce(undefined);
    vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue({ writeText } as unknown as Clipboard);
    render(<CopyButton text="safe text" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Clipboard unavailable' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Copied' })).toBeVisible());
    expect(writeText).toHaveBeenCalledWith('safe text');
  });
});
