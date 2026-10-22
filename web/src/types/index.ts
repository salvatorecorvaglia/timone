export type Health = 'healthy' | 'warning' | 'critical' | 'unknown';
export interface PrinterColumn {
  name: string;
  type: string;
  format?: string;
  description?: string;
  jsonPath: string;
  priority: number;
}
export interface CustomPresentation {
  columns: PrinterColumn[];
  schema: Record<string, unknown> | null;
  truncated: boolean;
}
export interface CustomType {
  presentations?: Record<string, CustomPresentation>;
  definition: string;
  kind: string;
  group: string;
  plural: string;
  namespaced: boolean;
  versions: string[];
  preferredVersion: string;
}
export interface CustomCatalog {
  items: CustomType[];
  warnings: Warning[];
  enabled: boolean;
  mode: string;
  message: string;
}
export interface RBACRule {
  apiGroups: string[];
  resources: string[];
  verbs: string[];
  resourceNames: string[];
  nonResourceURLs: string[];
}
export interface RBACReference {
  kind: string;
  name: string;
  namespace: string;
}
export interface RBACGrant {
  binding: Resource;
  role?: Resource | null;
  reference: RBACReference;
  subjects: string[];
  scope: string;
  namespace: string;
  resolution: string;
  rules: RBACRule[];
  rulesTruncated: boolean;
  notes: string[];
}
export interface GrantCoverage {
  kind: string;
  state: string;
  observed?: number | null;
}
export interface GrantReport {
  serviceAccount: Resource;
  identity: string;
  groups: string[];
  mode: string;
  scope: string;
  items: RBACGrant[];
  total: number;
  limit: number;
  offset: number;
  complete: boolean;
  truncated: boolean;
  coverage: GrantCoverage[];
  warnings: Warning[];
  limitations: string[];
  observedAt: string;
}
export interface CustomDiagnosticCoverage {
  state: string;
  typesInspected: number;
  resourcesObserved: number;
  readinessUnknown: number;
  candidateLimit: number;
  candidatesOmitted: number;
  message: string;
}
export interface DiagnosticPolicy {
  startupGraceSeconds: number;
  warningEventWindowSeconds: number;
  restartThreshold: number;
}
export interface SearchHit {
  id: string;
  kind: string;
  name: string;
  namespace: string;
  health: string;
  status: string;
  definition?: string;
  version?: string;
}
export interface SearchCoverage {
  kind: string;
  definition?: string;
  state: string;
  observed?: number | null;
  matched?: number | null;
}
export interface SearchResult {
  items: SearchHit[];
  total: number;
  limit: number;
  offset: number;
  truncated: boolean;
  complete: boolean;
  state: string;
  namespace: string;
  coverage: SearchCoverage[];
  warnings: Warning[];
  observedAt: string;
}
export interface Resource {
  printerCells?: string[];
  definition?: string;
  version?: string;
  id: string;
  kind: string;
  name: string;
  namespace: string;
  status: string;
  health: Health;
  ready: string;
  restarts: number;
  createdAt: string;
  labels: Record<string, string> | null;
}
export interface ResourcePage {
  items: Resource[];
  total: number;
  limit: number;
  offset: number;
  truncated: boolean;
}
export interface KEvent {
  id: string;
  namespace: string;
  resource: string;
  kind: string;
  resourceId: string;
  resourceNamespace: string;
  resourceUid?: string;
  customDefinition?: string;
  type: string;
  reason: string;
  message: string;
  count: number;
  timestamp: string;
  firstObserved?: string;
}
export interface Issue {
  recentChanges: string[] | null;
  diagnosticPolicy: DiagnosticPolicy;
  id: string;
  severity: Health;
  title: string;
  namespace: string;
  resource: Resource;
  evidence: string[];
  causes: string[];
  recommendations: string[];
  events: KEvent[];
  lastObserved: string;
}
export interface Finding {
  id: string;
  rule: string;
  severity: Health | 'info';
  resource: Resource;
  evidence: string;
  recommendation: string;
}
export interface Warning {
  kind: string;
  message: string;
}
export interface Snapshot {
  resources: Resource[];
  events: KEvent[];
  warnings: Warning[];
  observedAt: string;
}
export interface GraphEdge {
  routing?: RoutingAssessment | null;
  id: string;
  source: string;
  target: string;
  relation: string;
  confidence: string;
}
export interface RoutingAssessment {
  state: string;
  reference: string;
  evidence: string[];
  limitations: string[];
  accepted?: 'True' | 'False' | 'Unknown';
  resolvedRefs?: 'True' | 'False' | 'Unknown';
}
export interface Graph {
  nodes: Resource[];
  edges: GraphEdge[];
  truncated: boolean;
  warnings: Warning[];
}
export interface Metric {
  name: string;
  namespace: string;
  kind: string;
  container?: string;
  cpu: number;
  memory: number;
  cpuCapacity?: number;
  memoryCapacity?: number;
  timestamp: string;
  cpuRequest: number | null;
  memoryRequest: number | null;
  cpuLimit: number | null;
  memoryLimit: number | null;
}
export interface Metrics {
  podTotals: {
    resource: Resource;
    state: string;
    cpu: number | null;
    memory: number | null;
    timestamp: string;
    expected: number;
    observed: number;
    missing: string[];
    reason: string;
  }[];
  available: boolean;
  source: string;
  message: string;
  items: Metric[];
  allocations: Allocation[];
  warnings: Warning[];
}
export interface Allocation {
  node: string;
  pods: number | null;
  cpuRequest: number | null;
  memoryRequest: number | null;
  cpuAllocatable: number | null;
  memoryAllocatable: number | null;
  scope: string;
  cpuLimits: LimitBudget;
  memoryLimits: LimitBudget;
}
export interface LimitBudget {
  state: string;
  total: number | null;
  knownSubtotal: number | null;
  finitePods: number | null;
  missingPods: number | null;
  invalidPods: number | null;
}
export interface Connection {
  retentionWarnings?: Warning[];
  retainedBytes?: number;
  retentionBudgetBytes?: number;
  state: string;
  mode: string;
  context: string;
  namespace: string;
  auth: string;
  serverVersion?: string;
  discovery: string;
  message: string;
  lastSuccessfulRead?: string | null;
  checkedAt: string;
  collectionFailures: Warning[];
  streams: StreamStatus[];
}
export interface StreamStatus {
  kind: string;
  namespace: string;
  state: string;
  message?: string;
  lastSync?: string | null;
}
export interface HistoryQuery {
  kind: string;
  namespace?: string;
  name: string;
  container?: string;
  range?: string;
}
export interface HistoryCapability {
  state: string;
  source: string;
  message: string;
}
export interface HistoryPoint {
  timestamp: number;
  value: number | null;
}
export interface HistorySeries {
  metric: string;
  unit: string;
  container?: string;
  points: HistoryPoint[];
}
export interface HistoryData {
  state: string;
  message: string;
  series: HistorySeries[];
  warnings: Warning[];
}
export interface MetricHistory extends HistoryData {
  source: string;
  mode: string;
  resource: Resource;
  range: string;
  start: string;
  end: string;
  stepSeconds: number;
  identity: string;
}
export interface HelmResource {
  apiVersion: string;
  kind: string;
  namespace: string;
  name: string;
}
export interface HelmRevision {
  targetNamespace?: string;
  valuesAvailable: boolean;
  configurationAvailable: boolean;
  revision: number;
  status: string;
  chart: string;
  chartVersion: string;
  appVersion: string;
  deployedAt: string;
  inventory: HelmResource[];
  inventoryAvailable: boolean;
}
export interface HelmHistory {
  name: string;
  namespace: string;
  mode: string;
  revisions: HelmRevision[];
  warnings: Warning[];
  retention: string;
}
export interface FieldChange {
  path: string;
  change: string;
  before: unknown;
  after: unknown;
}
export interface HelmComparison {
  valuesChanges: FieldChange[];
  valuesComparable: boolean;
  valuesTruncated: boolean;
  changes: {
    resource: HelmResource;
    path: string;
    change: string;
    before: unknown;
    after: unknown;
  }[];
  configurationComparable: boolean;
  changesTruncated: boolean;
  from: HelmRevision;
  to: HelmRevision;
  added: HelmResource[];
  removed: HelmResource[];
  inventoryComparable: boolean;
  limitations: string;
}
export interface IntegrationCondition {
  type: string;
  status: string;
  reason: string;
  message: string;
  timestamp: string;
  stale: boolean;
}
export interface IntegrationRecord {
  resource: Resource;
  name: string;
  status: string;
  health?: string;
  revision?: string;
  attemptedRevision?: string;
  retainedRevisions?: number;
  chart?: string;
  drift?: string;
  operation?: string;
  suspended: boolean;
  stale: boolean;
  observation: string;
  observedAt?: string;
  conditions: IntegrationCondition[];
  evidence: string[];
  recommendations: string[];
}
export interface Integration {
  name: string;
  status: string;
  message: string;
  records: IntegrationRecord[];
  warnings: Warning[];
  truncated: boolean;
  detailsEnabled: boolean;
  detected?: boolean | null;
}
export interface Cluster {
  name: string;
  mode: 'demo' | 'cluster';
  scope: string;
  kinds: { name: string; namespaced: boolean }[];
}
export interface Overview {
  cluster: string;
  mode: string;
  scope: string;
  health: Health;
  counts: Record<string, number>;
  podStates: Record<string, number>;
  availableReplicas: number;
  desiredReplicas: number;
  namespaces: string[];
  nodes: Resource[];
  workloads: Resource[];
  issues: Issue[];
  events: KEvent[];
  warnings: Warning[];
  observedAt: string;
  diagnosticPolicy: DiagnosticPolicy;
  customDiagnostics: CustomDiagnosticCoverage;
}
export interface Detail {
  resource: Resource;
  manifest: Record<string, unknown>;
  events: KEvent[];
  warnings: Warning[];
  history: string;
  redaction: string;
  type?: CustomType;
}
export interface CustomPage extends ResourcePage {
  type: CustomType;
  version: string;
}
export interface AccessSession {
  authenticated: boolean;
  authRequired: boolean;
  mode: string;
  identity?: { name: string; method: string };
}
export interface ApplicationView {
  id: string;
  name: string;
  namespace: string;
  health: Health;
  basis: string;
  resources: Resource[];
  relationships: GraphEdge[];
  issues: Issue[];
  integrations: IntegrationRecord[];
  truncated: boolean;
}
export interface ApplicationPage {
  items: ApplicationView[];
  total: number;
  limit: number;
  offset: number;
  warnings: Warning[];
  observedAt: string;
  mode: string;
  limitations: string;
}

export interface DriftReport {
  controller: Resource;
  revision: string;
  source: string;
  state: string;
  mode: string;
  checked: number;
  total: number;
  changesTruncated: boolean;
  changes: {
    resource: Resource;
    path: string;
    desired: unknown;
    actual: unknown;
    change: string;
  }[];
  warnings: Warning[];
  observedAt: string;
  limitations: string;
}

export interface AccessReview {
  query: { verb: string; kind: string; namespace: string; name?: string };
  state: string;
  identity: string;
  message: string;
  mode: string;
  observedAt: string;
}
