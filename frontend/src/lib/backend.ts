import type { Theme } from '../themes/themes'
import type {
  ApplyResult,
  CertReport,
  DebugContainer,
  DoctorConfig,
  Fix,
  ArgoApp,
  ArgoDetail,
  Cluster,
  ClusterPatch,
  ConfigData,
  ContextInfo,
  DataEntry,
  FzDiff,
  FzReport,
  FzSummary,
  FluxDetail,
  FluxObject,
  Forward,
  ForwardRuntime,
  Health,
  HealthReport,
  HelmDetail,
  HelmRelease,
  IngressInfo,
  Integrations,
  KubeEvent,
  LogOptions,
  LogTarget,
  AccessInfo,
  AppLinkTemplate,
  Applications,
  BBBucket,
  BBClusterCfg,
  BBInfo,
  BBQuery,
  BBRecord,
  CheckOutput,
  ClusterOverview,
  DrainResult,
  PodProbes,
  ProbeRequest,
  ProbeResult,
  Incident,
  Runbook,
  RunbooksInfo,
  DriftKindInfo,
  DriftObject,
  DriftReport,
  Promotion,
  NetQuery,
  NetResult,
  SimTargets,
  Topology,
  MetricsSeries,
  ObjectMeta,
  PluginInfo,
  PluginInspection,
  PortInfo,
  QuickForwardResult,
  PromCandidate,
  PromConfig,
  PromResult,
  Resource,
  TimelineEvent,
  Usage,
} from './types'

export interface Settings {
  theme: Theme
  checkUpdates: boolean
  lastUpdateCheck: number
}

export interface AppInfo {
  name: string
  version: string
  repo: string
  repoConfigured: boolean
  docsURL: string
  issuesURL: string
  releasesURL: string
  dataDir: string
}

export interface UpdateResult {
  current: string
  latest: string
  available: boolean
  url: string
  notes: string
  publishedAt: string
}

/**
 * Thin wrapper over the Wails-generated bindings. When running outside Wails
 * (plain `vite` in a browser, unit tests) it falls back to localStorage so the
 * UI stays developable without the Go runtime.
 */
type Bindings = {
  GetSettings(): Promise<Settings>
  SetTheme(t: string): Promise<void>
  GetVersion(): Promise<string>
  ListClusters(): Promise<Cluster[]>
  DefaultKubeconfigPath(): Promise<string>
  PickKubeconfigFile(): Promise<string>
  PreviewKubeconfigFile(path: string): Promise<ContextInfo[]>
  PreviewKubeconfigText(text: string): Promise<ContextInfo[]>
  ImportKubeconfigFile(path: string, contexts: string[]): Promise<Cluster[]>
  ImportKubeconfigText(text: string, contexts: string[]): Promise<Cluster[]>
  UpdateCluster(id: string, patch: ClusterPatch): Promise<Cluster>
  DeleteCluster(id: string): Promise<void>
  ReorderClusters(ids: string[]): Promise<void>
  CheckCluster(id: string): Promise<Health>
  ConnectCluster(id: string): Promise<Resource[]>
  DisconnectCluster(id: string): Promise<void>
  GetConnected(): Promise<string[]>
  SubscribeResource(clusterId: string, key: string): Promise<string>
  UnsubscribeResource(sub: string): Promise<void>
  GetResourceYAML(c: string, key: string, ns: string, name: string): Promise<string>
  UpdateResourceYAML(c: string, key: string, ns: string, name: string, doc: string): Promise<string>
  DeleteResource(c: string, key: string, ns: string, name: string): Promise<void>
  ScaleResource(c: string, key: string, ns: string, name: string, replicas: number): Promise<void>
  RestartResource(c: string, key: string, ns: string, name: string): Promise<void>
  GetResourceEvents(c: string, key: string, ns: string, name: string): Promise<KubeEvent[]>
  GetResourceMeta(c: string, key: string, ns: string, name: string): Promise<ObjectMeta>
  OpenPodTerminal(
    c: string,
    ns: string,
    pod: string,
    container: string,
    cols: number,
    rows: number,
  ): Promise<string>
  OpenLocalTerminal(c: string, cols: number, rows: number): Promise<string>
  TerminalWrite(id: string, data: string): Promise<void>
  TerminalResize(id: string, cols: number, rows: number): Promise<void>
  TerminalClose(id: string): Promise<void>
  GetLogTargets(c: string, key: string, ns: string, name: string): Promise<LogTarget[]>
  StartLogs(c: string, opts: LogOptions): Promise<string>
  StopLogs(sub: string): Promise<void>
  ListPortForwards(c: string): Promise<ForwardRuntime[]>
  AddPortForward(f: Partial<Forward>, start: boolean): Promise<ForwardRuntime>
  StartPortForward(id: string): Promise<void>
  StopPortForward(id: string): Promise<void>
  RemovePortForward(id: string): Promise<void>
  SetPortForwardAuto(id: string, auto: boolean): Promise<void>
  PreviewManifest(c: string, doc: string, ns: string, force: boolean): Promise<ApplyResult[]>
  ApplyManifest(c: string, doc: string, ns: string, force: boolean): Promise<ApplyResult[]>
  PickManifestFile(): Promise<string>
  FetchManifestURL(url: string): Promise<string>
  GetResourcePorts(c: string, key: string, ns: string, name: string): Promise<PortInfo[]>
  SaveTextFile(name: string, content: string): Promise<string>
  GetIngressMap(c: string, ns: string): Promise<{ ingresses: IngressInfo[] }>
  GetTopology(c: string, ns: string): Promise<Topology>
  GetSimTargets(c: string): Promise<SimTargets>
  GetClusterOverview(c: string): Promise<ClusterOverview>
  GetApplications(c: string, groupByNamespace: boolean): Promise<Applications>
  GetAppLinks(c: string): Promise<AppLinkTemplate[]>
  SetAppLinks(c: string, links: AppLinkTemplate[]): Promise<AppLinkTemplate[]>
  GetAccess(c: string): Promise<AccessInfo>
  SetAccessNamespaces(c: string, ns: string[]): Promise<AccessInfo>
  GetPodProbes(c: string, ns: string, pod: string): Promise<PodProbes>
  CheckProbe(c: string, ns: string, pod: string, req: ProbeRequest): Promise<ProbeResult>
  ForwardPodPort(c: string, ns: string, pod: string, port: number): Promise<ForwardRuntime>
  GetBlackbox(c: string): Promise<BBInfo>
  SetBlackbox(
    c: string,
    cfg: BBClusterCfg,
    retentionHours: number,
    maxRecords: number,
  ): Promise<BBInfo>
  BlackboxQuery(c: string, q: BBQuery): Promise<BBRecord[]>
  BlackboxStateAt(c: string, ts: number): Promise<BBRecord[]>
  BlackboxBuckets(c: string, from: number, to: number, n: number): Promise<BBBucket[]>
  ClearBlackbox(c: string): Promise<void>
  GetRunbooks(): Promise<RunbooksInfo>
  SaveRunbooks(doc: string): Promise<void>
  MatchRunbooks(inc: Incident): Promise<Runbook[]>
  AllRunbooksFor(inc: Incident): Promise<Runbook[]>
  RunRunbookCheck(
    c: string,
    doStep: string,
    key: string,
    ns: string,
    name: string,
  ): Promise<CheckOutput>
  GetDriftKinds(): Promise<DriftKindInfo[]>
  GetNamespaces(c: string): Promise<string[]>
  CompareClusters(
    sc: string,
    sns: string,
    dc: string,
    dns: string,
    keys: string[],
  ): Promise<DriftReport>
  CompareObject(
    sc: string,
    sns: string,
    dc: string,
    dns: string,
    key: string,
    name: string,
  ): Promise<DriftObject>
  BuildPromotion(
    sc: string,
    sns: string,
    dc: string,
    dns: string,
    key: string,
    name: string,
    paths: string[],
  ): Promise<Promotion>
  SimulateNetwork(c: string, q: NetQuery): Promise<NetResult>
  SimulateDrain(c: string, node: string): Promise<DrainResult>
  GetConfigData(c: string, key: string, ns: string, name: string): Promise<ConfigData>
  RevealSecretKey(c: string, ns: string, name: string, k: string): Promise<DataEntry>
  GetHealth(c: string, key: string, ns: string, name: string): Promise<HealthReport>
  GetTimeline(c: string, key: string, ns: string, name: string): Promise<TimelineEvent[]>
  GetMetricsSeries(
    c: string,
    key: string,
    ns: string,
    name: string,
    rangeSecs: number,
  ): Promise<MetricsSeries>
  GetUsage(c: string, kind: string, ns: string): Promise<Record<string, Usage>>
  StartAssessment(c: string): Promise<string>
  CancelAssessment(c: string): Promise<void>
  AssessmentRunning(c: string): Promise<string>
  ListAssessments(c: string): Promise<FzSummary[]>
  GetAssessment(id: string): Promise<FzReport>
  DeleteAssessment(id: string): Promise<void>
  CompareAssessments(from: string, to: string): Promise<FzDiff>
  ExportAssessment(id: string, format: string): Promise<string>
  GetIntegrations(c: string): Promise<Integrations>
  ListArgoApps(c: string): Promise<ArgoApp[]>
  GetArgoApp(c: string, ns: string, name: string): Promise<ArgoDetail>
  RefreshArgoApp(c: string, ns: string, name: string, hard: boolean): Promise<void>
  SyncArgoApp(
    c: string,
    ns: string,
    name: string,
    o: { revision: string; prune: boolean; force: boolean; dryRun: boolean },
  ): Promise<void>
  RollbackArgoApp(
    c: string,
    ns: string,
    name: string,
    id: number,
    prune: boolean,
    disableAuto: boolean,
  ): Promise<void>
  SetArgoAutoSync(
    c: string,
    ns: string,
    name: string,
    enable: boolean,
    prune: boolean,
    selfHeal: boolean,
  ): Promise<void>
  ListHelmReleases(c: string): Promise<HelmRelease[]>
  GetHelmRelease(c: string, ns: string, name: string, rev: number): Promise<HelmDetail>
  HelmCLIAvailable(): Promise<boolean>
  RollbackHelmRelease(c: string, ns: string, name: string, rev: number): Promise<string>
  UninstallHelmRelease(c: string, ns: string, name: string): Promise<string>
  ListFluxObjects(c: string): Promise<FluxObject[]>
  GetFluxObject(c: string, key: string, ns: string, name: string): Promise<FluxDetail>
  ReconcileFlux(c: string, key: string, ns: string, name: string): Promise<void>
  SuspendFlux(c: string, key: string, ns: string, name: string, suspend: boolean): Promise<void>
  DetectPrometheus(c: string): Promise<PromCandidate[]>
  GetPrometheusConfig(c: string): Promise<{ configured: boolean; config: PromConfig }>
  SavePrometheusConfig(c: string, cfg: PromConfig): Promise<string>
  ClearPrometheusConfig(c: string): Promise<void>
  QueryPrometheus(
    c: string,
    query: string,
    rangeSecs: number,
    stepSecs: number,
  ): Promise<PromResult>
  GetAppInfo(): Promise<AppInfo>
  OpenExternal(url: string): Promise<void>
  CheckForUpdates(): Promise<UpdateResult>
  SetCheckUpdates(on: boolean): Promise<void>
  RevealDataDir(): Promise<void>
  ClearAssessmentHistory(): Promise<void>
  ResetSettings(): Promise<void>
  OpenNotices(): Promise<void>
  QuickForward(c: string, key: string, ns: string, name: string): Promise<QuickForwardResult>
  GetDoctorConfig(): Promise<DoctorConfig>
  SetDoctorConfig(c: DoctorConfig): Promise<DoctorConfig>
  ProposeFixes(c: string, rule: string, key: string, ns: string, name: string): Promise<Fix[]>
  GetFixableRules(): Promise<string[]>
  ListDebugContainers(c: string, ns: string, pod: string): Promise<DebugContainer[]>
  StartDebugContainer(
    c: string,
    ns: string,
    pod: string,
    o: { image: string; target: string; caps: boolean },
  ): Promise<DebugContainer>
  GetCertificates(c: string, ns: string): Promise<CertReport>
  ListPlugins(): Promise<PluginInfo[]>
  InspectPlugin(path: string): Promise<PluginInspection>
  InstallPlugin(path: string, granted: string[]): Promise<PluginInfo>
  PickPluginFolder(): Promise<string>
  PickPluginZip(): Promise<string>
  UninstallPlugin(id: string): Promise<void>
  SetPluginEnabled(id: string, enabled: boolean): Promise<void>
  SetPluginPermissions(id: string, granted: string[]): Promise<void>
  RestartPlugin(id: string): Promise<void>
  GetPluginLogs(id: string): Promise<string[]>
  PluginUICall(id: string, method: string, paramsJSON: string): Promise<string>
  RunPluginCommand(id: string, command: string, contextJSON: string): Promise<string>
}

function bindings(): Bindings | null {
  const w = window as unknown as { go?: { main?: { App?: Bindings } } }
  return w.go?.main?.App ?? null
}

const LS_KEY = 'soliton-rudder.settings'

export const backend = {
  async getSettings(): Promise<Settings> {
    const b = bindings()
    if (b) return b.GetSettings()
    try {
      const raw = localStorage.getItem(LS_KEY)
      if (raw) return JSON.parse(raw) as Settings
    } catch {
      /* ignore */
    }
    return { theme: 'dark-black', checkUpdates: true, lastUpdateCheck: 0 }
  },
  async setTheme(theme: Theme): Promise<void> {
    const b = bindings()
    if (b) return b.SetTheme(theme)
    try {
      localStorage.setItem(LS_KEY, JSON.stringify({ theme }))
    } catch {
      /* ignore */
    }
  },
  async getVersion(): Promise<string> {
    const b = bindings()
    return b ? b.GetVersion() : 'dev (browser)'
  },

  clusters: {
    list: () => call((b) => b.ListClusters(), [] as Cluster[]),
    defaultPath: () => call((b) => b.DefaultKubeconfigPath(), '~/.kube/config'),
    pickFile: () => call((b) => b.PickKubeconfigFile(), ''),
    previewFile: (p: string) => call((b) => b.PreviewKubeconfigFile(p), [] as ContextInfo[]),
    previewText: (t: string) => call((b) => b.PreviewKubeconfigText(t), [] as ContextInfo[]),
    importFile: (p: string, c: string[]) =>
      call((b) => b.ImportKubeconfigFile(p, c), [] as Cluster[]),
    importText: (t: string, c: string[]) =>
      call((b) => b.ImportKubeconfigText(t, c), [] as Cluster[]),
    update: (id: string, patch: ClusterPatch) =>
      call((b) => b.UpdateCluster(id, patch), null as unknown as Cluster),
    remove: (id: string) => call((b) => b.DeleteCluster(id), undefined),
    reorder: (ids: string[]) => call((b) => b.ReorderClusters(ids), undefined),
    check: (id: string) =>
      call((b) => b.CheckCluster(id), {
        status: 'error',
        version: '',
        latencyMs: 0,
        error: 'Backend unavailable',
      } as Health),
  },
}

const unavailable = () => Promise.reject(new Error('Backend unavailable'))

export const kube = {
  connect: (id: string) => call((b) => b.ConnectCluster(id), [] as Resource[]),
  connectStrict: (id: string) => callOrFail((b) => b.ConnectCluster(id)),
  disconnect: (id: string) => call((b) => b.DisconnectCluster(id), undefined),
  connected: () => call((b) => b.GetConnected(), [] as string[]),
  subscribe: (c: string, key: string) => call((b) => b.SubscribeResource(c, key), 'offline'),
  unsubscribe: (sub: string) => call((b) => b.UnsubscribeResource(sub), undefined),
  getYAML: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetResourceYAML(c, key, ns, name)),
  updateYAML: (c: string, key: string, ns: string, name: string, doc: string) =>
    callOrFail((b) => b.UpdateResourceYAML(c, key, ns, name, doc)),
  remove: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.DeleteResource(c, key, ns, name)),
  scale: (c: string, key: string, ns: string, name: string, n: number) =>
    callOrFail((b) => b.ScaleResource(c, key, ns, name, n)),
  restart: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.RestartResource(c, key, ns, name)),
  events: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetResourceEvents(c, key, ns, name)),
  meta: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetResourceMeta(c, key, ns, name)),
}

export const tools = {
  openPodTerminal: (
    c: string,
    ns: string,
    pod: string,
    container: string,
    cols: number,
    rows: number,
  ) => callOrFail((b) => b.OpenPodTerminal(c, ns, pod, container, cols, rows)),
  openLocalTerminal: (c: string, cols: number, rows: number) =>
    callOrFail((b) => b.OpenLocalTerminal(c, cols, rows)),
  termWrite: (id: string, data: string) => call((b) => b.TerminalWrite(id, data), undefined),
  termResize: (id: string, cols: number, rows: number) =>
    call((b) => b.TerminalResize(id, cols, rows), undefined),
  termClose: (id: string) => call((b) => b.TerminalClose(id), undefined),
  logTargets: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetLogTargets(c, key, ns, name)),
  startLogs: (c: string, o: LogOptions) => callOrFail((b) => b.StartLogs(c, o)),
  stopLogs: (sub: string) => call((b) => b.StopLogs(sub), undefined),
  quickForward: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.QuickForward(c, key, ns, name)),
  forwards: {
    list: (c: string) => call((b) => b.ListPortForwards(c), [] as ForwardRuntime[]),
    add: (f: Partial<Forward>, start: boolean) => callOrFail((b) => b.AddPortForward(f, start)),
    start: (id: string) => callOrFail((b) => b.StartPortForward(id)),
    stop: (id: string) => call((b) => b.StopPortForward(id), undefined),
    remove: (id: string) => callOrFail((b) => b.RemovePortForward(id)),
    setAuto: (id: string, a: boolean) => callOrFail((b) => b.SetPortForwardAuto(id, a)),
  },
  previewManifest: (c: string, doc: string, ns: string, force: boolean) =>
    callOrFail((b) => b.PreviewManifest(c, doc, ns, force)),
  applyManifest: (c: string, doc: string, ns: string, force: boolean) =>
    callOrFail((b) => b.ApplyManifest(c, doc, ns, force)),
  pickManifestFile: () => call((b) => b.PickManifestFile(), ''),
  fetchManifestURL: (url: string) => callOrFail((b) => b.FetchManifestURL(url)),
  ports: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetResourcePorts(c, key, ns, name)),
  saveText: (name: string, content: string) => call((b) => b.SaveTextFile(name, content), ''),
}

export interface DriftSide {
  cluster: string
  ns: string
}

export const blackbox = {
  info: (c: string) => callOrFail((b) => b.GetBlackbox(c)),
  set: (c: string, cfg: BBClusterCfg, retentionHours: number, maxRecords: number) =>
    callOrFail((b) => b.SetBlackbox(c, cfg, retentionHours, maxRecords)),
  query: (c: string, q: BBQuery) => callOrFail((b) => b.BlackboxQuery(c, q)),
  stateAt: (c: string, ts: number) => callOrFail((b) => b.BlackboxStateAt(c, ts)),
  buckets: (c: string, from: number, to: number, n: number) =>
    callOrFail((b) => b.BlackboxBuckets(c, from, to, n)),
  clear: (c: string) => callOrFail((b) => b.ClearBlackbox(c)),
}

export const runbooks = {
  info: () => callOrFail((b) => b.GetRunbooks()),
  save: (doc: string) => callOrFail((b) => b.SaveRunbooks(doc)),
  match: (inc: Incident) => callOrFail((b) => b.MatchRunbooks(inc)),
  all: (inc: Incident) => callOrFail((b) => b.AllRunbooksFor(inc)),
  check: (c: string, doStep: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.RunRunbookCheck(c, doStep, key, ns, name)),
}

export const drift = {
  kinds: () => callOrFail((b) => b.GetDriftKinds()),
  /** Connects to the cluster on demand, so it also works for clusters that are not open. */
  namespaces: (c: string) => callOrFail((b) => b.GetNamespaces(c)).then((n) => n ?? []),
  compare: (s: DriftSide, d: DriftSide, keys: string[]) =>
    callOrFail((b) => b.CompareClusters(s.cluster, s.ns, d.cluster, d.ns, keys)),
  object: (s: DriftSide, d: DriftSide, key: string, name: string) =>
    callOrFail((b) => b.CompareObject(s.cluster, s.ns, d.cluster, d.ns, key, name)),
  promotion: (s: DriftSide, d: DriftSide, key: string, name: string, paths: string[]) =>
    callOrFail((b) => b.BuildPromotion(s.cluster, s.ns, d.cluster, d.ns, key, name, paths)),
}

export const probes = {
  list: (c: string, ns: string, pod: string) => callOrFail((b) => b.GetPodProbes(c, ns, pod)),
  check: (c: string, ns: string, pod: string, req: ProbeRequest) =>
    callOrFail((b) => b.CheckProbe(c, ns, pod, req)),
  forward: (c: string, ns: string, pod: string, port: number) =>
    callOrFail((b) => b.ForwardPodPort(c, ns, pod, port)),
}

/** An overview from an older backend, or one with a null list, still has a full access description. */
function normalizeAccess(
  a: Partial<Omit<AccessInfo, 'configured'>> | undefined,
): Omit<AccessInfo, 'configured'> {
  return {
    clusterWide: a?.clusterWide ?? true,
    canListNamespaces: a?.canListNamespaces ?? true,
    source: a?.source ?? 'cluster',
    note: a?.note ?? '',
    namespaces: a?.namespaces ?? [],
  }
}

export const apps = {
  list: (c: string, groupByNamespace: boolean) =>
    callOrFail((b) => b.GetApplications(c, groupByNamespace)).then((a) => ({
      ...a,
      apps: (a.apps ?? []).map((x) => ({
        ...x,
        namespaces: x.namespaces ?? [],
        images: x.images ?? [],
        urls: x.urls ?? [],
        links: x.links ?? [],
        members: x.members ?? [],
        issues: x.issues ?? [],
      })),
      unassigned: a.unassigned ?? [],
      notes: a.notes ?? [],
    })),
  links: (c: string) => callOrFail((b) => b.GetAppLinks(c)).then((l) => l ?? []),
  setLinks: (c: string, links: AppLinkTemplate[]) =>
    callOrFail((b) => b.SetAppLinks(c, links)).then((l) => l ?? []),
}

export const insight = {
  access: (c: string) =>
    callOrFail((b) => b.GetAccess(c)).then((a) => ({
      ...a,
      namespaces: a.namespaces ?? [],
      configured: a.configured ?? [],
    })),
  setAccessNamespaces: (c: string, ns: string[]) =>
    callOrFail((b) => b.SetAccessNamespaces(c, ns)).then((a) => ({
      ...a,
      namespaces: a.namespaces ?? [],
      configured: a.configured ?? [],
    })),
  overview: (c: string) =>
    callOrFail((b) => b.GetClusterOverview(c)).then((o) => ({
      ...o,
      attention: o.attention ?? [],
      warnings: o.warnings ?? [],
      notes: o.notes ?? [],
      access: normalizeAccess(o.access),
    })),
  ingressMap: (c: string, ns: string) =>
    callOrFail((b) => b.GetIngressMap(c, ns)).then((v) => ({
      ingresses: (v.ingresses ?? []).map((i) => ({
        ...i,
        addresses: i.addresses ?? [],
        issues: i.issues ?? [],
        tls: (i.tls ?? []).map((t) => ({ ...t, hosts: t.hosts ?? [] })),
        rules: (i.rules ?? []).map((r) => ({
          ...r,
          paths: (r.paths ?? []).map((p) => ({ ...p, pods: p.pods ?? [] })),
        })),
      })),
    })),
  topology: (c: string, ns: string) => callOrFail((b) => b.GetTopology(c, ns)),
  simTargets: (c: string) => callOrFail((b) => b.GetSimTargets(c)),
  simNetwork: (c: string, q: NetQuery) => callOrFail((b) => b.SimulateNetwork(c, q)),
  simDrain: (c: string, node: string) => callOrFail((b) => b.SimulateDrain(c, node)),
  configData: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetConfigData(c, key, ns, name)),
  revealSecret: (c: string, ns: string, name: string, k: string) =>
    callOrFail((b) => b.RevealSecretKey(c, ns, name, k)),
  health: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetHealth(c, key, ns, name)),
  timeline: (c: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.GetTimeline(c, key, ns, name)),
  series: (c: string, key: string, ns: string, name: string, rangeSecs = 0) =>
    callOrFail((b) => b.GetMetricsSeries(c, key, ns, name, rangeSecs)),
  usage: (c: string, kind: string, ns: string) => callOrFail((b) => b.GetUsage(c, kind, ns)),
}

/** Go encodes a nil slice as null; every array the UI maps over is made a real array here, once, at the edge. */
export function normalizeReport(r: FzReport): FzReport {
  return {
    ...r,
    notes: r.notes ?? [],
    categories: r.categories ?? [],
    rules: (r.rules ?? []).map((x) => ({ ...x, findings: x.findings ?? [] })),
    finops: r.finops && {
      ...r.finops,
      namespaces: r.finops.namespaces ?? [],
      top: r.finops.top ?? [],
      notes: r.finops.notes ?? [],
    },
  }
}

export const assess = {
  start: (c: string) => callOrFail((b) => b.StartAssessment(c)),
  cancel: (c: string) => call((b) => b.CancelAssessment(c), undefined),
  running: (c: string) => call((b) => b.AssessmentRunning(c), ''),
  list: (c: string) => call((b) => b.ListAssessments(c), [] as FzSummary[]),
  get: (id: string) => callOrFail((b) => b.GetAssessment(id)).then(normalizeReport),
  remove: (id: string) => callOrFail((b) => b.DeleteAssessment(id)),
  compare: (from: string, to: string) => callOrFail((b) => b.CompareAssessments(from, to)),
  export: (id: string, format: 'json' | 'html') =>
    callOrFail((b) => b.ExportAssessment(id, format)),
}

export const integ = {
  detect: (c: string) => callOrFail((b) => b.GetIntegrations(c)),
  argo: {
    list: (c: string) => callOrFail((b) => b.ListArgoApps(c)),
    get: (c: string, ns: string, n: string) => callOrFail((b) => b.GetArgoApp(c, ns, n)),
    refresh: (c: string, ns: string, n: string, hard: boolean) =>
      callOrFail((b) => b.RefreshArgoApp(c, ns, n, hard)),
    sync: (
      c: string,
      ns: string,
      n: string,
      o: { revision: string; prune: boolean; force: boolean; dryRun: boolean },
    ) => callOrFail((b) => b.SyncArgoApp(c, ns, n, o)),
    rollback: (
      c: string,
      ns: string,
      n: string,
      id: number,
      prune: boolean,
      disableAuto: boolean,
    ) => callOrFail((b) => b.RollbackArgoApp(c, ns, n, id, prune, disableAuto)),
    autoSync: (
      c: string,
      ns: string,
      n: string,
      enable: boolean,
      prune: boolean,
      selfHeal: boolean,
    ) => callOrFail((b) => b.SetArgoAutoSync(c, ns, n, enable, prune, selfHeal)),
  },
  helm: {
    list: (c: string) => callOrFail((b) => b.ListHelmReleases(c)),
    get: (c: string, ns: string, n: string, rev: number) =>
      callOrFail((b) => b.GetHelmRelease(c, ns, n, rev)),
    cli: () => call((b) => b.HelmCLIAvailable(), false),
    rollback: (c: string, ns: string, n: string, rev: number) =>
      callOrFail((b) => b.RollbackHelmRelease(c, ns, n, rev)),
    uninstall: (c: string, ns: string, n: string) =>
      callOrFail((b) => b.UninstallHelmRelease(c, ns, n)),
  },
  flux: {
    list: (c: string) => callOrFail((b) => b.ListFluxObjects(c)),
    get: (c: string, key: string, ns: string, n: string) =>
      callOrFail((b) => b.GetFluxObject(c, key, ns, n)),
    reconcile: (c: string, key: string, ns: string, n: string) =>
      callOrFail((b) => b.ReconcileFlux(c, key, ns, n)),
    suspend: (c: string, key: string, ns: string, n: string, s: boolean) =>
      callOrFail((b) => b.SuspendFlux(c, key, ns, n, s)),
  },
  prom: {
    detect: (c: string) => callOrFail((b) => b.DetectPrometheus(c)),
    config: (c: string) => callOrFail((b) => b.GetPrometheusConfig(c)),
    save: (c: string, cfg: PromConfig) => callOrFail((b) => b.SavePrometheusConfig(c, cfg)),
    clear: (c: string) => callOrFail((b) => b.ClearPrometheusConfig(c)),
    query: (c: string, q: string, range: number, step: number) =>
      callOrFail((b) => b.QueryPrometheus(c, q, range, step)),
  },
}

export const doctor = {
  getConfig: () =>
    call((b) => b.GetDoctorConfig(), {
      prodNamespaces: [],
      cpuHourUsd: 0.031611,
      memGbHourUsd: 0.004237,
      headroomPct: 20,
    } as DoctorConfig),
  setConfig: (c: DoctorConfig) => callOrFail((b) => b.SetDoctorConfig(c)),
  fixes: (c: string, rule: string, key: string, ns: string, name: string) =>
    callOrFail((b) => b.ProposeFixes(c, rule, key, ns, name)),
  fixable: () => call((b) => b.GetFixableRules(), [] as string[]),
}

export const debug = {
  list: (c: string, ns: string, pod: string) =>
    callOrFail((b) => b.ListDebugContainers(c, ns, pod)),
  start: (
    c: string,
    ns: string,
    pod: string,
    o: { image: string; target: string; caps: boolean },
  ) => callOrFail((b) => b.StartDebugContainer(c, ns, pod, o)),
}

export const certs = {
  list: (c: string, ns: string) => callOrFail((b) => b.GetCertificates(c, ns)),
}

export const system = {
  info: () =>
    call((b) => b.GetAppInfo(), {
      name: 'Soliton Rudder',
      version: 'dev (browser)',
      repo: '',
      repoConfigured: false,
      docsURL: '',
      issuesURL: '',
      releasesURL: '',
      dataDir: '',
    } as AppInfo),
  open: (url: string) => callOrFail((b) => b.OpenExternal(url)),
  checkUpdates: () => callOrFail((b) => b.CheckForUpdates()),
  setCheckUpdates: (on: boolean) => callOrFail((b) => b.SetCheckUpdates(on)),
  revealData: () => callOrFail((b) => b.RevealDataDir()),
  clearHistory: () => callOrFail((b) => b.ClearAssessmentHistory()),
  resetSettings: () => callOrFail((b) => b.ResetSettings()),
  openNotices: () => callOrFail((b) => b.OpenNotices()),
}

export const plugins = {
  list: () => call((b) => b.ListPlugins(), [] as PluginInfo[]),
  inspect: (path: string) => callOrFail((b) => b.InspectPlugin(path)),
  install: (path: string, granted: string[]) => callOrFail((b) => b.InstallPlugin(path, granted)),
  pickFolder: () => call((b) => b.PickPluginFolder(), ''),
  pickZip: () => call((b) => b.PickPluginZip(), ''),
  uninstall: (id: string) => callOrFail((b) => b.UninstallPlugin(id)),
  setEnabled: (id: string, e: boolean) => callOrFail((b) => b.SetPluginEnabled(id, e)),
  setPermissions: (id: string, g: string[]) => callOrFail((b) => b.SetPluginPermissions(id, g)),
  restart: (id: string) => callOrFail((b) => b.RestartPlugin(id)),
  logs: (id: string) => call((b) => b.GetPluginLogs(id), [] as string[]),
  uiCall: (id: string, method: string, params: unknown) =>
    callOrFail((b) => b.PluginUICall(id, method, JSON.stringify(params ?? {}))),
  runCommand: (id: string, command: string, context: unknown) =>
    callOrFail((b) => b.RunPluginCommand(id, command, JSON.stringify(context ?? {}))),
}

export function openURL(url: string) {
  const rt = (window as unknown as { runtime?: { BrowserOpenURL(u: string): void } }).runtime
  if (rt) rt.BrowserOpenURL(url)
  else window.open(url, '_blank')
}

function callOrFail<T>(fn: (b: Bindings) => Promise<T>): Promise<T> {
  const b = bindings()
  return b ? fn(b) : unavailable()
}

/** Subscribes to a Wails runtime event. Returns an unsubscribe function (no-op outside Wails). */
export function onEvent<T>(name: string, cb: (data: T) => void): () => void {
  const rt = (
    window as unknown as { runtime?: { EventsOn(n: string, cb: (d: T) => void): () => void } }
  ).runtime
  return rt ? rt.EventsOn(name, cb) : () => {}
}

/** Runs a Wails call, or returns the fallback when running outside Wails (browser dev/tests). */
function call<T>(fn: (b: Bindings) => Promise<T>, fallback: T): Promise<T> {
  const b = bindings()
  return b ? fn(b) : Promise.resolve(fallback)
}
