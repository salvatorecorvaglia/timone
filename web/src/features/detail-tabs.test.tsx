import { screen, waitFor } from '@testing-library/react';
import { expect, it } from 'vitest';
import { App } from '../app/App';
import { mockAPI, show } from './test-helpers';

it.each(['logs', 'grants', 'invalid'])(
  'normalizes unsupported Deployment tab %s to Overview',
  async (tab) => {
    mockAPI();
    show(<App />, `/resources/Deployment/production/checkout-api?tab=${tab}&namespace=production`);
    await screen.findByRole('heading', { name: 'checkout-api' });
    expect(screen.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Resource information');
    await waitFor(() => expect(screen.getByTestId('location')).not.toHaveTextContent('tab='));
    expect(screen.getByTestId('location')).toHaveTextContent('namespace=production');
  },
);
