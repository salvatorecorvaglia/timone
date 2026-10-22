import { screen, fireEvent } from '@testing-library/react';
import { Route, Routes } from 'react-router-dom';
import { describe, it, expect } from 'vitest';
import { ResourceDetail } from './ResourceDetail';
import { ServiceAccountGrants } from './ServiceAccountGrants';
import { show, mockAPI, fixtures } from './test-helpers';

describe('ServiceAccount RBAC grants', () => {
  it('navigates from a Pod to its ServiceAccount grant tab', async () => {
    mockAPI();
    show(
      <Routes>
        <Route path="/resources/:kind/:namespace/:name" element={<ResourceDetail />} />
      </Routes>,
      '/resources/Pod/production/checkout-api-7c8d9f-m4n7q',
    );
    fireEvent.click(await screen.findByRole('link', { name: 'default · RBAC grants' }));
    expect(
      await screen.findByRole('heading', { name: 'Observed RBAC grants' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'RBAC grants' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByText('Simulated RBAC')).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'ServiceAccount RBAC bindings' })).toBeInTheDocument();
  });
  it('shows literal resource-name restrictions and provenance with a shareable selection', async () => {
    mockAPI();
    show(
      <ServiceAccountGrants namespace="production" name="default" />,
      '/resources/ServiceAccount/production/default?tab=grants',
    );
    const button = await screen.findByRole('button', {
      name: 'Inspect RBAC binding commerce-config-reader',
    });
    button.focus();
    expect(button).toHaveFocus();
    fireEvent.click(button);
    expect(screen.getByRole('table', { name: 'Observed RBAC policy rules' })).toHaveTextContent(
      'storefront-config',
    );
    expect(screen.getByText('Group system:serviceaccounts:production')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Role/storefront-config-reader' })).toHaveAttribute(
      'href',
      '/resources/Role/production/storefront-config-reader',
    );
    expect(screen.getByTestId('location')).toHaveTextContent(
      'grantBinding=RoleBinding%3Aproduction%3Acommerce-config-reader',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Close binding' }));
    expect(
      screen.queryByRole('table', { name: 'Observed RBAC policy rules' }),
    ).not.toBeInTheDocument();
  });
  it('keeps an unreadable or missing role unresolved instead of declaring no permissions', async () => {
    mockAPI();
    show(<ServiceAccountGrants namespace="production" name="default" />);
    fireEvent.click(
      await screen.findByRole('button', { name: 'Inspect RBAC binding legacy-reader' }),
    );
    expect(screen.getByText('Rules unavailable')).toBeInTheDocument();
    expect(screen.getByText(/No empty permission set is inferred/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Role/retired-role' })).not.toBeInTheDocument();
    expect(screen.getByText(/Inventory incomplete/)).toBeInTheDocument();
    expect(
      screen.queryByRole('table', { name: 'Observed RBAC policy rules' }),
    ).not.toBeInTheDocument();
  });
  it('restores binding pagination from the URL and loads the preceding page', async () => {
    mockAPI();
    show(
      <ServiceAccountGrants namespace="production" name="default" />,
      '/resources/ServiceAccount/production/default?tab=grants&grantOffset=2',
    );
    expect(await screen.findByText('3–5 of 5 observed')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Previous RBAC bindings' }));
    expect(await screen.findByText('1–5 of 5 observed')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Previous RBAC bindings' })).toBeDisabled();
  });
  it('shows failed subject authorization as an error, with retry', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    api.mockImplementation((input, options) =>
      String(input).includes('/grants?')
        ? Promise.resolve(
            new Response(JSON.stringify({ error: { message: 'ServiceAccount read denied' } }), {
              status: 403,
            }),
          )
        : original(input, options),
    );
    show(<ServiceAccountGrants namespace="production" name="default" />);
    expect(await screen.findByRole('alert')).toHaveTextContent('ServiceAccount read denied');
    expect(
      screen.queryByRole('table', { name: 'ServiceAccount RBAC bindings' }),
    ).not.toBeInTheDocument();
    api.mockImplementation(original);
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(
      await screen.findByRole('heading', { name: 'Observed RBAC grants' }),
    ).toBeInTheDocument();
  });
  it('does not call a role empty when the page rule budget omitted its rules', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    const report = structuredClone(
      fixtures['/api/v1/serviceaccounts/production/default/grants?limit=25&offset=0'],
    ) as {
      items: {
        binding: { id: string; name: string };
        resolution: string;
        rules: unknown[];
        rulesTruncated: boolean;
      }[];
    };
    const item = report.items.find((g) => g.binding.name === 'commerce-config-reader')!;
    item.rules = [];
    item.rulesTruncated = true;
    api.mockImplementation((input, options) =>
      String(input).includes('/grants?')
        ? Promise.resolve(new Response(JSON.stringify(report)))
        : original(input, options),
    );
    show(<ServiceAccountGrants namespace="production" name="default" />);
    fireEvent.click(
      await screen.findByRole('button', { name: 'Inspect RBAC binding commerce-config-reader' }),
    );
    expect(screen.getByText('Rules unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No rules in the observed role')).not.toBeInTheDocument();
  });
  it('does not expose native RBAC inspection for a custom kind named ServiceAccount', async () => {
    const api = mockAPI();
    const original = api.getMockImplementation()!;
    api.mockImplementation((input, options) =>
      String(input).includes('/custom-resources/databases.platform.timone.io/production/orders-db')
        ? Promise.resolve(
            new Response(
              JSON.stringify({
                ...(fixtures[
                  '/api/v1/custom-resources/databases.platform.timone.io/production/orders-db'
                ] as object),
                resource: {
                  ...(
                    fixtures[
                      '/api/v1/custom-resources/databases.platform.timone.io/production/orders-db'
                    ] as { resource: object }
                  ).resource,
                  kind: 'ServiceAccount',
                },
              }),
            ),
          )
        : original(input, options),
    );
    show(
      <Routes>
        <Route
          path="/custom-resources/:definition/:namespace/:name"
          element={<ResourceDetail custom />}
        />
      </Routes>,
      '/custom-resources/databases.platform.timone.io/production/orders-db',
    );
    await screen.findByRole('heading', { name: 'orders-db' });
    expect(screen.queryByRole('tab', { name: 'RBAC grants' })).not.toBeInTheDocument();
    expect(api.mock.calls.some(([url]) => String(url).includes('/grants?'))).toBe(false);
  });
});
