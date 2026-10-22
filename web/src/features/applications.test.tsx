import { screen, fireEvent, within } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { App } from '../app/App';
import { show, mockAPI } from './test-helpers';

describe('Application and observation investigations', () => {
  it('opens an application with declared resources and delivery evidence', async () => {
    mockAPI();
    show(<App />, '/applications?namespace=production');
    const table = await screen.findByRole('table', { name: 'Applications' });
    fireEvent.click(within(table).getByRole('button', { name: 'checkout-api' }));
    expect(screen.getByTestId('location')).toHaveTextContent('application=');
    expect(
      await screen.findByRole('heading', { name: 'checkout-api · investigation' }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: /^checkout-api$/ }).length).toBeGreaterThan(0);
  });
  it('distinguishes process observations from Kubernetes event retention', async () => {
    mockAPI();
    show(<App />, '/events');
    fireEvent.click(await screen.findByText('Observed resource changes', { exact: true }));
    expect(await screen.findByText(/Observed by this process only/)).toBeInTheDocument();
    expect(screen.getByText(/Restart clears history/)).toBeInTheDocument();
  });
});
