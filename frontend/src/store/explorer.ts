import { create } from 'zustand'
import { insight, integ, kube } from '../lib/backend'
import { RowStore, router } from '../lib/rowstore'
import { useForwards } from './forwards'
import type { AccessInfo, Integrations, Resource, Usage } from '../lib/types'

export type View =
  | 'overview'
  | 'applications'
  | 'resources'
  | 'forwards'
  | 'ingress'
  | 'topology'
  | 'simulator'
  | 'blackbox'
  | 'reports'
  | 'argocd'
  | 'helm'
  | 'flux'
  | 'prometheus'
  | 'certificates'
  | 'plugin'

export interface RowRef {
  uid: string
  ns: string
  name: string
}

export interface Sort {
  col: string
  dir: 'asc' | 'desc'
}

interface ExplorerState {
  clusterId: string | null
  phase: 'idle' | 'connecting' | 'ready' | 'error'
  error: string
  catalog: Resource[]
  selectedKey: string | null
  view: View
  main: RowStore
  nsStore: RowStore
  namespaces: string[] // selected namespaces; empty = all
  search: string
  sort: Sort | null
  wide: boolean
  selected: RowRef | null
  /** how much of the cluster these credentials can read (null until known) */
  access: AccessInfo | null
  refreshAccess: () => Promise<void>

  setView: (v: View) => void
  pluginPanel: { pluginId: string; panelId: string } | null
  openPluginPanel: (pluginId: string, panelId: string) => void
  /** Open a resource kind with a search term pre-filled (cross-view navigation). */
  jump: (key: string, search: string) => Promise<void>
  integrations: Integrations | null
  usage: Record<string, Usage> | null
  setUsage: (u: Record<string, Usage> | null) => void
  open: (clusterId: string) => Promise<void>
  close: () => void
  select: (key: string) => Promise<void>
  setNamespaces: (ns: string[]) => void
  setSearch: (s: string) => void
  toggleSort: (col: string) => void
  setWide: (w: boolean) => void
  /** The open detail panel has unsaved edits (set by the YAML tab). Leaving the object asks first. */
  detailDirty: boolean
  setDetailDirty: (dirty: boolean) => void
  setSelected: (r: RowRef | null) => void
}

// Live subscription ids live outside React state: they are lifecycle handles, not render inputs.
let mainSub: string | null = null
let nsSub: string | null = null
let token = 0

const NS_KEY = 'core/v1/namespaces'

const lsKey = (id: string) => `soliton-rudder.explorer.${id}`

function loadPrefs(id: string): { key?: string; namespaces?: string[] } {
  try {
    return JSON.parse(localStorage.getItem(lsKey(id)) ?? '{}')
  } catch {
    return {}
  }
}

function savePrefs(id: string, p: { key?: string; namespaces?: string[] }) {
  try {
    localStorage.setItem(lsKey(id), JSON.stringify({ ...loadPrefs(id), ...p }))
  } catch {
    /* ignore */
  }
}

async function kubeIntegrations(clusterId: string, my: number) {
  try {
    const integrations = await integ.detect(clusterId)
    if (my === token || useExplorer.getState().clusterId === clusterId)
      useExplorer.setState({ integrations })
  } catch {
    /* integrations are optional */
  }
}

function drop(sub: string | null) {
  if (!sub) return
  router.unregister(sub)
  void kube.unsubscribe(sub)
}

async function attach(clusterId: string, key: string, store: RowStore): Promise<string> {
  const sub = await kube.subscribe(clusterId, key)
  router.register(sub, store)
  return sub
}

export const useExplorer = create<ExplorerState>((set, get) => ({
  clusterId: null,
  phase: 'idle',
  error: '',
  catalog: [],
  selectedKey: null,
  view: 'resources',
  pluginPanel: null,
  usage: null,
  integrations: null,
  main: new RowStore(),
  nsStore: new RowStore(),
  namespaces: [],
  search: '',
  sort: null,
  wide: false,
  selected: null,
  detailDirty: false,
  access: null,

  async refreshAccess() {
    const id = get().clusterId
    if (!id) return
    try {
      const access = await insight.access(id)
      if (get().clusterId === id) set({ access })
    } catch {
      /* the pages show their own errors */
    }
  },

  async open(clusterId) {
    const my = ++token
    drop(mainSub)
    drop(nsSub)
    mainSub = nsSub = null
    const prefs = loadPrefs(clusterId)
    set({
      clusterId,
      phase: 'connecting',
      error: '',
      catalog: [],
      selectedKey: null,
      selected: null,
      access: null,
      namespaces: prefs.namespaces ?? [],
      search: '',
      sort: null,
      main: new RowStore(),
      nsStore: new RowStore(),
    })
    try {
      const catalog = await kube.connect(clusterId)
      if (my !== token) return
      // a cluster opens on its Overview; the table of a kind starts when one is picked
      set({ catalog, phase: 'ready', view: 'overview', selected: null })
      void useForwards.getState().load(clusterId)
      void get().refreshAccess()
      void kubeIntegrations(clusterId, my)
      if (catalog.some((r) => r.key === NS_KEY)) {
        const store = get().nsStore
        const sub = await attach(clusterId, NS_KEY, store)
        if (my !== token) return drop(sub)
        nsSub = sub
      }
    } catch (e) {
      if (my === token) set({ phase: 'error', error: e instanceof Error ? e.message : String(e) })
    }
  },

  close() {
    token++
    drop(mainSub)
    drop(nsSub)
    mainSub = nsSub = null
    set({ clusterId: null, phase: 'idle', selected: null })
  },

  async select(key) {
    const { clusterId } = get()
    if (!clusterId) return
    const my = ++token
    drop(mainSub)
    mainSub = null
    const store = new RowStore()
    set({
      selectedKey: key,
      view: 'resources',
      main: store,
      selected: null,
      search: '',
      sort: null,
    })
    savePrefs(clusterId, { key })
    try {
      const sub = await attach(clusterId, key, store)
      if (my !== token) return drop(sub)
      mainSub = sub
    } catch (e) {
      store.setStatus({
        sub: '',
        state: 'error',
        message: e instanceof Error ? e.message : String(e),
      })
    }
  },

  setView: (view) => set({ view }),
  openPluginPanel: (pluginId, panelId) =>
    set({ view: 'plugin', pluginPanel: { pluginId, panelId } }),
  setUsage: (usage) => set({ usage }),
  async jump(key, search) {
    await get().select(key)
    set({ search })
  },

  setNamespaces(namespaces) {
    const { clusterId } = get()
    set({ namespaces })
    if (clusterId) savePrefs(clusterId, { namespaces })
  },
  setSearch: (search) => set({ search }),
  toggleSort(col) {
    const s = get().sort
    if (!s || s.col !== col) set({ sort: { col, dir: 'asc' } })
    else if (s.dir === 'asc') set({ sort: { col, dir: 'desc' } })
    else set({ sort: null })
  },
  setWide: (wide) => set({ wide }),
  setDetailDirty: (detailDirty) => set({ detailDirty }),
  setSelected(next) {
    const { selected, detailDirty } = get()
    const leaving = !!selected && selected.uid !== next?.uid
    if (leaving && detailDirty && !window.confirm(`Discard unsaved changes to ${selected.name}?`))
      return
    set({ selected: next, detailDirty: leaving ? false : detailDirty })
  },
}))

export function currentResource(): Resource | undefined {
  const s = useExplorer.getState()
  return s.catalog.find((r) => r.key === s.selectedKey)
}
