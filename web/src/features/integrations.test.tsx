import { screen, fireEvent, within } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { App } from '../app/App';
import { CustomResources } from './CustomResources';
import { EffectiveAuthorization } from './EffectiveAuthorization';
import { show, mockAPI } from './test-helpers';

describe('Four scoped work blocks', () => {
  it('renders CRD printer columns, wide columns and OpenAPI without schema defaults', async () => {
    mockAPI();
    show(
      <CustomResources namespace="production" />,
      '/custom-resources?definition=databases.platform.timone.io',
    );
    const table = await screen.findByRole('table', { name: 'Custom resources' });
    expect(within(table).getByRole('columnheader', { name: 'Ready' })).toBeInTheDocument();
    expect(
      within(table).queryByRole('columnheader', { name: 'Generation' }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('Wide printer columns'));
    expect(
      await within(table).findByRole('columnheader', { name: 'Generation' }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByText('OpenAPI v3 schema · v1'));
    expect(screen.getByLabelText('OpenAPI v3 schema')).toHaveTextContent('"minimum": 1');
    expect(screen.getByLabelText('OpenAPI v3 schema')).not.toHaveTextContent(
      'SIMULATED-OMITTED-CUSTOM-SECRET',
    );
  });
  it('queries effective access explicitly and displays the simulated decision', async () => {
    const spy = mockAPI();
    show(<EffectiveAuthorization namespace="production" />);
    fireEvent.click(screen.getByRole('button', { name: 'Check effective access' }));
    expect(await screen.findByText('allowed', { exact: true })).toBeInTheDocument();
    expect(await screen.findByText(/SIMULATED authorization decision/)).toBeInTheDocument();
    expect(spy).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/authorization?'),
      expect.anything(),
    );
  });
  it('computes desired/live visual drift for the selected controller', async () => {
    mockAPI();
    show(<App />, '/gitops');
    fireEvent.click(
      await screen.findByRole('button', { name: 'Compare live drift for checkout-api' }),
    );
    expect(
      await screen.findByRole('table', { name: 'Desired versus live drift' }),
    ).toHaveTextContent('/spec/replicas');
    expect(screen.getByTestId('location')).toHaveTextContent('drift=Application');
  });
  it('compares sanitized Helm values without exposing credentials', async () => {
    mockAPI();
    show(<App />, '/helm/production/checkout-api?from=1&to=3');
    expect(await screen.findByRole('table', { name: 'Helm values differences' })).toHaveTextContent(
      '/replicaCount',
    );
    expect(screen.queryByText(/DEMO-PRIVATE-VALUES/)).not.toBeInTheDocument();
  });
});
