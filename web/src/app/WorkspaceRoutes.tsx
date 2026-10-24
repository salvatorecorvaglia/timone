import { lazy, Suspense } from 'react';
import { Route, Routes, useLocation } from 'react-router-dom';
import { PageBoundary } from '../components/PageBoundary';
import { Empty, Loading } from '../components/ui';
import type { Cluster } from '../types';

const Overview = lazy(() => import('../features/Overview').then((m) => ({ default: m.Overview })));
const Resources = lazy(() =>
  import('../features/Resources').then((m) => ({ default: m.Resources })),
);
const ResourceDetail = lazy(() =>
  import('../features/ResourceDetail').then((m) => ({ default: m.ResourceDetail })),
);
const CustomResources = lazy(() =>
  import('../features/CustomResources').then((m) => ({ default: m.CustomResources })),
);
const Issues = lazy(() => import('../features/Issues').then((m) => ({ default: m.Issues })));
const Timeline = lazy(() => import('../features/Timeline').then((m) => ({ default: m.Timeline })));
const Audit = lazy(() => import('../features/Audit').then((m) => ({ default: m.Audit })));
const Integrations = lazy(() =>
  import('../features/Integrations').then((m) => ({ default: m.Integrations })),
);
const MetricsPage = lazy(() =>
  import('../features/MetricsPage').then((m) => ({ default: m.MetricsPage })),
);
const Settings = lazy(() => import('../features/Settings').then((m) => ({ default: m.Settings })));
const Applications = lazy(() =>
  import('../features/Applications').then((m) => ({ default: m.Applications })),
);
const HelmRelease = lazy(() =>
  import('../features/Helm').then((m) => ({ default: m.HelmRelease })),
);
const Topology = lazy(() => import('../features/Topology').then((m) => ({ default: m.Topology })));

export function WorkspaceRoutes({
  namespace: actualNS,
  cluster,
}: {
  namespace: string;
  cluster: Cluster | undefined;
}) {
  const location = useLocation();
  return (
    <PageBoundary key={location.pathname}>
      <Suspense fallback={<Loading />}>
        <Routes>
          <Route path="/" element={<Overview namespace={actualNS} />} />
          <Route path="/resources" element={<Resources namespace={actualNS} />} />
          <Route path="/custom-resources" element={<CustomResources namespace={actualNS} />} />
          <Route
            path="/custom-resources/:definition/:namespace/:name"
            element={<ResourceDetail custom />}
          />
          <Route path="/applications" element={<Applications namespace={actualNS} />} />
          <Route path="/resources/:kind/:namespace/:name" element={<ResourceDetail />} />
          <Route path="/issues" element={<Issues namespace={actualNS} />} />
          <Route path="/issues/:id" element={<Issues namespace={actualNS} />} />
          <Route path="/events" element={<Timeline namespace={actualNS} />} />
          <Route path="/topology" element={<Topology namespace={actualNS} />} />
          <Route path="/metrics" element={<MetricsPage namespace={actualNS} />} />
          <Route path="/audit" element={<Audit namespace={actualNS} />} />
          <Route path="/integrations" element={<Integrations namespace={actualNS} />} />
          <Route path="/helm" element={<Integrations namespace={actualNS} filter="Helm" />} />
          <Route path="/helm/:namespace/:name" element={<HelmRelease />} />
          <Route path="/gitops" element={<Integrations namespace={actualNS} filter="GitOps" />} />
          <Route path="/settings" element={<Settings cluster={cluster} />} />
          <Route
            path="*"
            element={
              <Empty
                title="Page not found"
                description="Choose a view from the navigation to continue."
              />
            }
          />
        </Routes>
      </Suspense>
    </PageBoundary>
  );
}
