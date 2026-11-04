const workloads = ['Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'];
const tabs: Record<string, readonly string[]> = {
  grants: ['ServiceAccount'],
  network: ['Service', 'Ingress', 'HTTPRoute', 'Gateway'],
  batch: ['Job', 'CronJob'],
  efficiency: workloads,
  configuration: workloads,
  termination: ['Pod', 'PersistentVolumeClaim', 'Namespace'],
  scheduling: ['Pod'],
  maintenance: ['Node'],
  rollout: ['Deployment', 'StatefulSet', 'DaemonSet'],
  storage: [...workloads, 'PersistentVolumeClaim', 'PersistentVolume'],
  logs: workloads,
};

export function supportsResourceTab(kind: string, tab: string, custom = false): boolean {
  if (['summary', 'manifest', 'events'].includes(tab)) return true;
  if (custom) return false;
  return tab === 'incident' || (Object.hasOwn(tabs, tab) && tabs[tab].includes(kind));
}
