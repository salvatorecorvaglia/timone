export type Invalidation = { full?: boolean; scopes?: { kind: string; namespace: string }[] };
const core = new Set([
  'HorizontalPodAutoscaler',
  'PodDisruptionBudget',
  'ResourceQuota',
  'LimitRange',
  'EndpointSlice',
  'Node',
  'Namespace',
  'Pod',
  'Deployment',
  'ReplicaSet',
  'StatefulSet',
  'DaemonSet',
  'Job',
  'CronJob',
  'Service',
  'Ingress',
  'PersistentVolumeClaim',
  'Event',
]);
const dependencies: Record<string, Set<string>> = {
  overview: core,
  issues: core,
  changes: core,
  applications: new Set([
    ...core,
    'Secret',
    'ConfigMap',
    'ServiceAccount',
    'Application',
    'Kustomization',
    'HelmRelease',
  ]),
  topology: new Set([
    ...core,
    'Secret',
    'ConfigMap',
    'Gateway',
    'HTTPRoute',
    'GatewayClass',
    'ReferenceGrant',
  ]),
  audit: new Set([
    ...core,
    'NetworkPolicy',
    'Role',
    'ClusterRole',
    'RoleBinding',
    'ClusterRoleBinding',
  ]),
  integrations: new Set(['Application', 'Kustomization', 'HelmRelease', 'Secret', 'Service']),
  metrics: new Set(['Pod', 'Node']),
  events: new Set(['Event']),
  namespaces: new Set(['Namespace']),
};
export function shouldInvalidate(path: string, change: Invalidation): boolean {
  if (!path.startsWith('/api/v1/') || path.startsWith('/api/v1/metrics/history?')) return false;
  if (change.full || !change.scopes) return true;
  const url = new URL(path, 'http://timone.local');
  const parts = url.pathname.split('/').map(decodeURIComponent);
  const route = parts[3];
  const namespace =
    (route === 'resources' && parts[5] !== '_' ? parts[5] : '') ||
    url.searchParams.get('namespace') ||
    '';
  return change.scopes.some((scope) => {
    // Network paths can reference Services and source Pods in other namespaces.
    if (route === 'resources' && parts[7] === 'network') return true;
    // Application delivery controllers are read at workspace scope, not the workload filter.
    if (
      route === 'applications' &&
      ['Application', 'Kustomization', 'HelmRelease'].includes(scope.kind)
    )
      return true;
    if (namespace && scope.namespace && namespace !== scope.namespace) return false;
    if (
      route === 'resources' &&
      [
        'incident',
        'logs',
        'storage',
        'topology',
        'snapshot',
        'log-sources',
        'scheduling',
        'rollout',
        'batch',
        'efficiency',
        'configuration',
        'termination',
      ].includes(parts[7] ?? '')
    )
      return true;
    if (route === 'resources')
      return (
        scope.kind === (parts[4] || url.searchParams.get('kind') || 'Pod') ||
        (!!parts[4] &&
          [
            'Event',
            'HorizontalPodAutoscaler',
            'PodDisruptionBudget',
            'ResourceQuota',
            'LimitRange',
            'EndpointSlice',
          ].includes(scope.kind))
      );
    if (route === 'nodes' && parts[5] === 'maintenance')
      return [
        'Pod',
        'Node',
        'PodDisruptionBudget',
        'PersistentVolumeClaim',
        'PersistentVolume',
        'Deployment',
        'ReplicaSet',
        'DaemonSet',
        'StatefulSet',
        'Job',
      ].includes(scope.kind);
    if (route === 'serviceaccounts')
      return [
        'ServiceAccount',
        'Role',
        'ClusterRole',
        'RoleBinding',
        'ClusterRoleBinding',
      ].includes(scope.kind);
    if (route === 'helm') return scope.kind === 'Secret';
    if (route === 'gitops')
      return ['Application', 'Kustomization', 'HelmRelease'].includes(scope.kind);
    if (scope.kind === 'CustomResourceDefinition')
      return ['overview', 'issues', 'custom-resource-types', 'custom-resources', 'search'].includes(
        route,
      );
    if (route === 'search')
      return !url.searchParams.get('kind') || url.searchParams.get('kind') === scope.kind;
    return dependencies[route]?.has(scope.kind) ?? false;
  });
}
