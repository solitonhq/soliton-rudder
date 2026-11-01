export interface Cluster {
  id: string
  name: string
  color: string
  icon: string
  pinned: boolean
  position: number
  source: 'file' | 'inline'
  kubeconfigPath: string
  context: string
  createdAt: number
}

export interface ContextInfo {
  name: string
  cluster: string
  user: string
  server: string
  namespace: string
  current: boolean
  exists: boolean
}

export type HealthStatus = 'connected' | 'unreachable' | 'unauthorized' | 'error' | 'checking'

export interface Health {
  status: HealthStatus
  version: string
  latencyMs: number
  error: string
}

export interface ClusterPatch {
  name?: string
  color?: string
  icon?: string
  pinned?: boolean
}

export interface Resource {
  key: string
  group: string
  version: string
  name: string
  kind: string
  namespaced: boolean
  shortNames: string[] | null
  verbs: string[]
}

export interface Column {
  name: string
  type: string
  priority: number
  description: string
}

export interface Row {
  uid: string
  ns: string
  name: string
  created: string
  cells: unknown[]
}

export interface RowsEvent {
  sub: string
  reset: boolean
  /** With reset: replace only the rows of this namespace (a table fed by several namespaces) */
  scope?: string
  columns?: Column[]
  upsert: Row[] | null
  delete: string[] | null
}

export interface StatusEvent {
  sub: string
  state: 'connecting' | 'live' | 'error'
  message: string
}

export interface KubeEvent {
  type: string
  reason: string
  message: string
  count: number
  last: string
  source: string
}

export interface ObjectMeta {
  labels: Record<string, string> | null
  annotations: Record<string, string> | null
  owners: string[] | null
  created: string
  uid: string
  finalizers: string[] | null
}

export interface LogTarget {
  pod: string
  phase: string
  containers: string[]
  initContainers: string[] | null
}

export interface LogOptions {
  namespace: string
  pod: string
  container: string
  follow: boolean
  tailLines: number
  sinceSecs: number
  previous: boolean
  timestamps: boolean
}

export interface LogLine {
  ts?: string
  text: string
}

export interface LogBatch {
  sub: string
  pod: string
  container: string
  lines: LogLine[]
}

export interface LogEnd {
  sub: string
  error: string
}

export interface PortInfo {
  name: string
  port: number
  protocol: string
  target: string
}

export type ForwardKind =
  'pod' | 'service' | 'deployment' | 'statefulset' | 'daemonset' | 'replicaset'

export interface Forward {
  id: string
  clusterId: string
  namespace: string
  kind: ForwardKind
  name: string
  remotePort: number
  localPort: number
  auto: boolean
  createdAt: number
}

export interface ForwardRuntime extends Forward {
  status: 'stopped' | 'connecting' | 'active' | 'healing' | 'degraded' | 'error'
  activePort: number
  pod: string
  message: string
  reconnects: number
  lastHealedReason: string
  lastHealedAt: number
}

export interface QuickForwardResult {
  forward: ForwardRuntime | null
  ports: PortInfo[]
  needChoice: boolean
}

export interface ApplyResult {
  index: number
  kind: string
  name: string
  namespace: string
  action: 'create' | 'configure' | 'unchanged' | 'error'
  before: string
  after: string
  error: string
}

export interface PodRef {
  name: string
  ready: boolean
  ip: string
}

export interface IngressPath {
  path: string
  pathType: string
  service: string
  port: string
  resource: string
  clusterIP: string
  serviceType: string
  found: boolean
  ready: number
  total: number
  pods: PodRef[]
  issue: string
}

export interface IngressRule {
  host: string
  secure: boolean
  paths: IngressPath[]
}

export interface IngressTLS {
  hosts: string[]
  secretName: string
  issuer: string
  notAfter: string
  daysLeft: number
  names: string[] | null
  error: string
}

export interface IngressIssue {
  severity: 'error' | 'warn' | 'info'
  message: string
}

export interface IngressInfo {
  name: string
  namespace: string
  class: string
  addresses: string[]
  created: string
  tls: IngressTLS[]
  rules: IngressRule[]
  issues: IngressIssue[]
}

export interface CertSummary {
  subject: string
  issuer: string
  issuerKind: 'letsencrypt' | 'letsencrypt-staging' | 'self-signed' | 'other'
  sans: string[]
  notBefore: string
  notAfter: string
  daysLeft: number
  expired: boolean
  keyAlg: string
  isCA: boolean
  selfSigned: boolean
  serial: string
  chainLen: number
}

export interface CertItem extends CertSummary {
  namespace: string
  name: string
  source: 'cert-manager' | 'manual'
  managed: string
  cmReady: string
  cmMessage: string
  renewal: string
  usedBy: string[]
  error: string
}

export interface CertReport {
  items: CertItem[]
  counts: { expired: number; soon14: number; soon30: number; ok: number; errors: number }
}

export interface DebugContainer {
  name: string
  image: string
  target: string
  state: 'running' | 'waiting' | 'terminated' | 'unknown'
  reason: string
  message: string
}

export interface DataEntry {
  key: string
  size: number
  format: 'json' | 'yaml' | 'env' | 'cert' | 'text' | 'binary'
  value: string
  masked: boolean
  truncated: boolean
  info: string
  cert: CertSummary | null
}

export interface ConfigData {
  kind: string
  type: string
  entries: DataEntry[]
}

export interface HealthCheck {
  name: string
  status: 'ok' | 'warn' | 'fail'
  detail: string
}

export interface HealthReport {
  status: 'healthy' | 'degraded' | 'failing' | 'unknown'
  summary: string
  checks: HealthCheck[]
  hints: string[]
}

export interface TimelineEvent extends KubeEvent {
  object: string
}

export interface Usage {
  cpu: number
  mem: number
}

export interface SeriesPoint {
  t: number
  cpu: number
  mem: number
}

export interface MetricsSeries {
  available: boolean
  error: string
  source: string
  intervalSecs: number
  points: SeriesPoint[]
  members: number
  cpuRequest: number
  cpuLimit: number
  memRequest: number
  memLimit: number
  cpuCapacity: number
  memCapacity: number
}

export type Severity = 'critical' | 'high' | 'medium' | 'low' | 'info'
export type FzCategory = 'resources' | 'reliability' | 'security' | 'maintenance'

export interface FzFinding {
  severity: Severity
  kind: string
  namespace: string
  name: string
  resourceKey: string
  message: string
}

export interface FzRule {
  id: string
  title: string
  category: FzCategory
  severity: Severity | ''
  description: string
  remediation: string
  checked: number
  affected: number
  findings: FzFinding[]
  truncated: boolean
  skipped: string
}

export interface FzCategoryScore {
  category: FzCategory
  score: number
  findings: number
}

export interface FzCounts {
  critical: number
  high: number
  medium: number
  low: number
  info: number
}

export interface FzSummary {
  id: string
  clusterId: string
  startedAt: number
  durationMs: number
  score: number
  serverVersion: string
  counts: FzCounts
  categories: FzCategoryScore[]
}

export interface FzReport extends FzSummary {
  clusterName: string
  stats: {
    nodes: number
    namespaces: number
    pods: number
    workloads: number
    services: number
    ingresses: number
    rulesRun: number
    rulesSkipped: number
  }
  notes: string[]
  finops?: FinOps
  rules: FzRule[]
}

export interface FzDiffItem {
  ruleId: string
  title: string
  finding: FzFinding
}

export interface FzDiff {
  from: FzSummary
  to: FzSummary
  scoreDelta: number
  new: FzDiffItem[]
  resolved: FzDiffItem[]
  unchanged: number
  ruleDeltas: { id: string; title: string; before: number; after: number }[]
  approximate: boolean
}

export interface FzProgress {
  run: string
  phase: string
  pct: number
}

export interface FzDone {
  run: string
  report: string
  error: string
}

export interface Integrations {
  argocd: boolean
  flux: boolean
  helm: boolean
  prometheusConfigured: boolean
}

export interface ArgoApp {
  name: string
  namespace: string
  project: string
  repo: string
  path: string
  targetRevision: string
  destServer: string
  destName: string
  destNamespace: string
  sync: string
  health: string
  healthMessage: string
  revision: string
  auto: boolean
  prune: boolean
  selfHeal: boolean
  phase: string
  opMessage: string
  opFinished: string
  reconciledAt: string
  created: string
  resources: number
  outOfSync: number
  unhealthy: number
  errors: number
  multiSource: boolean
}

export interface ArgoDetail extends ArgoApp {
  sources: string[]
  items: {
    group: string
    kind: string
    namespace: string
    name: string
    sync: string
    health: string
    message: string
    hook: boolean
  }[]
  history: { id: number; revision: string; deployedAt: string; source: string }[]
  conditions: { type: string; message: string; at: string }[]
  opResults: { kind: string; name: string; status: string; message: string }[]
  syncOptions: string[]
  images: string[]
}

export interface HelmRelease {
  name: string
  namespace: string
  revision: number
  status: string
  chart: string
  chartVersion: string
  appVersion: string
  description: string
  updated: string
  storage: string
}

export interface HelmDetail extends HelmRelease {
  notes: string
  values: string
  allValues: string
  manifest: string
  objects: { kind: string; namespace: string; name: string }[]
  history: {
    revision: number
    status: string
    chartVersion: string
    appVersion: string
    description: string
    updated: string
  }[]
}

export interface FluxObject {
  key: string
  kind: string
  name: string
  namespace: string
  ready: string
  reason: string
  message: string
  suspended: boolean
  revision: string
  source: string
  path: string
  interval: string
  lastTransition: string
  created: string
}

export interface FluxDetail extends FluxObject {
  conditions: { type: string; status: string; reason: string; message: string; at: string }[]
  inventory: { namespace: string; name: string; group: string; kind: string }[]
}

export interface PromConfig {
  namespace: string
  service: string
  port: string
  scheme: string
  path: string
}

export interface PromCandidate extends PromConfig {
  headless: boolean
}

export interface PromSeriesData {
  metric: Record<string, string>
  points: [number, number][]
}

export interface PromResult {
  series: PromSeriesData[]
  truncated: boolean
}

export interface PluginPermission {
  id: string
  title: string
  description: string
  risk: 'low' | 'medium' | 'high'
}

export interface PluginPanel {
  id: string
  title: string
  entry: string
  placement: 'cluster-nav' | 'dock'
  icon: string
}

export interface PluginManifest {
  id: string
  name: string
  version: string
  description: string
  author: string
  homepage: string
  apiVersion: number
  permissions: string[] | null
  backend?: { command: string; args: string[] | null }
  ui?: { root: string; panels: PluginPanel[] | null }
  commands: { id: string; title: string; panel: string }[] | null
  assessmentRules:
    | {
        id: string
        title: string
        category: string
        severity: string
        description: string
        remediation: string
      }[]
    | null
}

export interface PluginInfo {
  manifest: PluginManifest
  enabled: boolean
  granted: string[]
  state: 'running' | 'starting' | 'stopped' | 'crashed' | 'ui-only'
  error: string
  installedAt: number
  permissions: PluginPermission[]
}

export interface PluginInspection {
  manifest: PluginManifest
  permissions: PluginPermission[]
  update: boolean
  existingVersion: string
  hasBackend: boolean
  warnings: string[]
}

export interface FinOpsNamespace {
  namespace: string
  pods: number
  monthlyCostUsd: number
  potentialSaveUsd: number
  requestedCpuM: number
  requestedMem: number
  usedCpuM: number
  usedMem: number
  unrequestedPods: number
}

export interface FinOpsWorkload {
  kind: string
  namespace: string
  name: string
  resourceKey: string
  pods: number
  monthlyCostUsd: number
  potentialSaveUsd: number
  requestedCpuM: number
  requestedMem: number
  usedCpuM: number
  usedMem: number
}

export interface FinOps {
  confidence: 'high' | 'low' | 'none'
  source: string
  cpuHourUsd: number
  memGbHourUsd: number
  headroomPct: number
  monthlyCostUsd: number
  potentialSaveUsd: number
  namespaces: FinOpsNamespace[]
  top: FinOpsWorkload[]
  notes: string[]
}

export interface DoctorConfig {
  prodNamespaces: string[]
  cpuHourUsd: number
  memGbHourUsd: number
  headroomPct: number
}

export interface Fix {
  id: string
  title: string
  description: string
  action: 'apply' | 'delete' | 'none'
  manifest: string
  risk: 'low' | 'medium' | 'high'
  notes: string[]
  managedBy: string
  target: { kind: string; namespace: string; name: string; resourceKey: string }
}

export type TopoStatus = 'ok' | 'warn' | 'error' | 'unknown'

export interface TopoNode {
  id: string
  kind: string
  name: string
  namespace: string
  tier: number
  status: TopoStatus
  detail: string
  resourceKey: string
  missing: boolean
}

export interface TopoEdge {
  from: string
  to: string
  kind: 'routes' | 'selects' | 'owns' | 'runs-on' | 'mounts' | 'scales'
}

export interface Topology {
  nodes: TopoNode[]
  edges: TopoEdge[]
  truncated: boolean
  notes: string[]
}

export interface NetEndpoint {
  kind: 'Pod' | 'Service' | 'External'
  namespace: string
  name: string
  ip: string
}
export interface NetQuery {
  from: NetEndpoint
  to: NetEndpoint
  port: string
  protocol: string
}
export interface NetStep {
  side: 'egress' | 'ingress'
  subject: string
  isolated: boolean
  allowed: boolean
  policies: string[]
  matched: string
  reason: string
}
export interface NetPath {
  target: string
  port: number
  allowed: boolean
  steps: NetStep[]
}
export interface NetResult {
  verdict: 'allowed' | 'denied' | 'partial' | 'unknown'
  summary: string
  paths: NetPath[]
  notes: string[]
}
export interface SimTargets {
  namespaces: string[]
  pods: string[]
  services: string[]
  nodes: string[]
}
export type Sev = 'ok' | 'warn' | 'error'
export interface DrainPod {
  namespace: string
  name: string
  owner: string
  action: 'evict' | 'skip'
  severity: Sev
  reason: string
  movesTo: string
  cpuM: number
  mem: number
}
export interface PDBImpact {
  namespace: string
  name: string
  rule: string
  matching: number
  healthy: number
  allowed: number
  evicting: number
  severity: Sev
  message: string
}
export interface HPADrill {
  namespace: string
  name: string
  target: string
  current: number
  max: number
  fits: number
  severity: Sev
  message: string
}
export interface DrainResult {
  node: string
  verdict: 'safe' | 'disruptive' | 'blocked'
  summary: string[]
  pods: DrainPod[]
  pdbs: PDBImpact[]
  capacity: { freeCpuM: number; freeMem: number; needCpuM: number; needMem: number; nodes: number }
  hpas: HPADrill[]
  notes: string[]
}

export interface DriftKindInfo {
  key: string
  label: string
}
export interface FieldDiff {
  path: string
  op: 'changed' | 'added' | 'removed'
  src: unknown
  dst: unknown
  promotable: boolean
  envSpecific: boolean
  note: string
}
export interface DriftItem {
  name: string
  namespace: string
  status: 'same' | 'differs' | 'onlySource' | 'onlyTarget'
  changes: number
}
export interface DriftGroup {
  key: string
  label: string
  items: DriftItem[]
  error: string
}
export interface DriftReport {
  groups: DriftGroup[]
  notes: string[]
}
export interface DriftObject {
  name: string
  status: DriftItem['status']
  fields: FieldDiff[]
  srcYaml: string
  dstYaml: string
  srcExists: boolean
  dstExists: boolean
}
export interface Promotion {
  manifest: string
  create: boolean
  fields: number
}

export interface BBRecord {
  id: number
  ts: number
  kind: 'state' | 'event'
  key: string
  objKind: string
  namespace: string
  name: string
  severity: 'info' | 'warn' | 'error'
  status: string
  reason: string
  message: string
  prev: string
}
export interface BBBucket {
  ts: number
  info: number
  warn: number
  error: number
}
export interface BBClusterCfg {
  enabled: boolean
  namespaces: string[]
  intervalSec: number
}
export interface BBInfo {
  config: BBClusterCfg
  retentionHours: number
  maxRecords: number
  status: { running: boolean; lastRun: number; lastError: string; recorded: number }
  span: { min: number; max: number; count: number }
}
export interface BBQuery {
  from: number
  to: number
  kinds: string[]
  namespace: string
  minSeverity: '' | 'warn' | 'error'
  text: string
  limit: number
  newest: boolean
}
export interface RunbookStep {
  title: string
  do: string
  text?: string
  replicas?: number
  applicable: boolean
  readOnly: boolean
}
export interface Runbook {
  id: string
  title: string
  description?: string
  builtin: boolean
  steps: RunbookStep[]
  score: number
}
export interface Incident {
  kind: string
  namespace: string
  name: string
  reason: string
  status: string
}
export interface RunbooksInfo {
  builtin: { id: string; title: string; description?: string }[]
  custom: string
  error: string
}
export interface CheckOutput {
  title: string
  text: string
}

export interface ProbeHeader {
  name: string
  value: string
}
export interface ProbeInfo {
  container: string
  kind: 'liveness' | 'readiness' | 'startup'
  type: 'http' | 'tcp' | 'exec' | 'grpc'
  scheme: string
  path: string
  port: number
  portName: string
  host: string
  headers: ProbeHeader[]
  command: string[]
  grpcService: string
  initialDelay: number
  period: number
  timeout: number
  successThreshold: number
  failureThreshold: number
}
export interface ContainerState {
  name: string
  ready: boolean
  started: boolean
  restarts: number
  state: string
}
export interface PodProbes {
  podIP: string
  phase: string
  probes: ProbeInfo[]
  containers: ContainerState[]
}
export interface ProbeRequest {
  type: string
  scheme: string
  port: number
  path: string
  headers: ProbeHeader[]
}
export interface ProbeResult {
  ok: boolean
  status: number
  durationMs: number
  detail: string
  body: string
}

export interface OverviewAttention {
  severity: 'error' | 'warn'
  kind: string
  namespace: string
  name: string
  reason: string
  resourceKey: string
}
export interface OverviewWarning {
  at: number
  namespace: string
  kind: string
  name: string
  reason: string
  message: string
  count: number
  resourceKey: string
}
export interface ClusterOverview {
  serverVersion: string
  namespaces: number
  nodes: { total: number; ready: number; notReady: number; cordoned: number; pressure: number }
  pods: {
    total: number
    ready: number
    notReady: number
    pending: number
    failed: number
    succeeded: number
  }
  workloads: { total: number; healthy: number; degraded: number; failing: number }
  capacity: {
    cpuAllocM: number
    memAlloc: number
    cpuRequestedM: number
    memRequested: number
    cpuUsedM: number
    memUsed: number
    hasUsage: boolean
  }
  attention: OverviewAttention[]
  attentionTotal: number
  warnings: OverviewWarning[]
  notes: string[]
  access: Omit<AccessInfo, 'configured'>
}

export interface AccessInfo {
  clusterWide: boolean
  canListNamespaces: boolean
  namespaces: string[]
  source: 'cluster' | 'configured' | 'discovered' | 'kubeconfig' | 'none'
  note: string
  configured: string[]
}

export type AppStatus = 'ok' | 'warn' | 'error' | 'unknown'
export interface AppMember {
  kind: string
  namespace: string
  name: string
  resourceKey: string
  status: AppStatus
  detail: string
}
export interface Application {
  id: string
  name: string
  groupedBy: 'helm' | 'part-of' | 'instance' | 'app' | 'namespace'
  namespaces: string[]
  status: AppStatus
  counts: {
    workloads: number
    workloadsHealthy: number
    pods: number
    podsReady: number
    restarts: number
  }
  version: string
  chart: string
  chartVersion: string
  appVersion: string
  images: string[]
  helm: {
    release: string
    namespace: string
    revision: number
    status: string
    updated: string
    revisions: number
  } | null
  urls: string[]
  links: { title: string; url: string }[]
  members: AppMember[]
  lastChange: string
  issues: { severity: 'error' | 'warn' | 'info'; message: string }[]
}
export interface Applications {
  apps: Application[]
  unassigned: AppMember[]
  unassignedTotal: number
  notes: string[]
}
export interface AppLinkTemplate {
  title: string
  url: string
}

/** What a pod's containers read from the cluster. Secret and ConfigMap values are never part of it. */
export interface EnvVar {
  name: string
  source: 'literal' | 'secret' | 'configmap' | 'field' | 'resource'
  value: string
  ref: string
  key: string
  optional: boolean
  detail: string
}
export interface EnvFromRef {
  kind: 'secret' | 'configmap'
  name: string
  prefix: string
  optional: boolean
}
export interface VolumeMount {
  volume: string
  path: string
  readOnly: boolean
  subPath: string
}
export interface ContainerEnv {
  name: string
  init: boolean
  env: EnvVar[]
  envFrom: EnvFromRef[]
  mounts: VolumeMount[]
}
export interface VolumeSource {
  kind: string
  name: string
}
export interface PodVolume {
  name: string
  kind: string
  ref: string
  sources: VolumeSource[]
}
export interface PodEnv {
  containers: ContainerEnv[]
  volumes: PodVolume[]
}
