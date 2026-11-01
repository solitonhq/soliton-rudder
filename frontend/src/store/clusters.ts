import { create } from 'zustand'
import { backend, kube, onEvent } from '../lib/backend'
import type { Cluster, ClusterPatch, Health } from '../lib/types'
import { useDock } from './dock'
import { useUI } from './ui'

const HEALTH_INTERVAL_MS = 30_000

interface ClusterState {
  clusters: Cluster[]
  health: Record<string, Health>
  /** clusters with a live connection (opened, or connected from Home) */
  connected: Record<string, boolean>
  /** null = home screen */
  activeId: string | null
  loaded: boolean
  load: () => Promise<void>
  setActive: (id: string | null) => void
  setConnected: (ids: string[]) => void
  connect: (id: string) => Promise<void>
  disconnect: (id: string) => Promise<void>
  checkHealth: (id: string) => Promise<void>
  checkAll: () => Promise<void>
  update: (id: string, patch: ClusterPatch) => Promise<void>
  remove: (id: string) => Promise<void>
  reorder: (ids: string[]) => Promise<void>
  addImported: (added: Cluster[]) => Promise<void>
}

export const useClusters = create<ClusterState>((set, get) => ({
  clusters: [],
  health: {},
  connected: {},
  activeId: null,
  loaded: false,

  async load() {
    const clusters = await backend.clusters.list()
    set({ clusters, loaded: true })
    void get().checkAll()
  },

  setActive: (activeId) => set({ activeId }),

  setConnected(ids) {
    const next = Object.fromEntries(ids.map((i) => [i, true]))
    // a shell or log stream belongs to its connection: when the cluster disconnects, its tabs go too
    for (const id of Object.keys(get().connected))
      if (!next[id]) useDock.getState().closeCluster(id)
    set({ connected: next })
  },

  async connect(id) {
    try {
      await kube.connectStrict(id)
    } catch (e) {
      useUI
        .getState()
        .toast('error', `Could not connect: ${e instanceof Error ? e.message : String(e)}`)
    }
  },

  /** Closes the connection. If the cluster is open, go Home first so the view does not keep using it. */
  async disconnect(id) {
    if (get().activeId === id) set({ activeId: null })
    await kube.disconnect(id)
  },

  async checkHealth(id) {
    set((s) => ({
      health: { ...s.health, [id]: { ...(s.health[id] ?? blank), status: 'checking' } },
    }))
    const h = await backend.clusters.check(id)
    // cluster may have been removed while probing
    if (get().clusters.some((c) => c.id === id)) set((s) => ({ health: { ...s.health, [id]: h } }))
  },

  async checkAll() {
    await Promise.all(get().clusters.map((c) => get().checkHealth(c.id)))
  },

  async update(id, patch) {
    await backend.clusters.update(id, patch)
    set({ clusters: await backend.clusters.list() })
  },

  async remove(id) {
    await backend.clusters.remove(id)
    useDock.getState().closeCluster(id)
    set((s) => ({ activeId: s.activeId === id ? null : s.activeId }))
    set({ clusters: await backend.clusters.list() })
  },

  async reorder(ids) {
    const byId = new Map(get().clusters.map((c) => [c.id, c]))
    set({ clusters: ids.map((i) => byId.get(i)).filter((c): c is Cluster => !!c) }) // optimistic
    try {
      await backend.clusters.reorder(ids)
    } finally {
      set({ clusters: await backend.clusters.list() })
    }
  },

  async addImported(added) {
    set({ clusters: await backend.clusters.list() })
    await Promise.all(added.map((c) => get().checkHealth(c.id)))
  },
}))

const blank: Health = { status: 'checking', version: '', latencyMs: 0, error: '' }

/** Starts periodic health polling; returns a stop function. Pauses while the window is hidden. */
export function startHealthPolling(): () => void {
  const t = setInterval(() => {
    if (!document.hidden) void useClusters.getState().checkAll()
  }, HEALTH_INTERVAL_MS)
  return () => clearInterval(t)
}

let connectionEvents = false
/** Keeps `connected` in step with the backend (it emits the full list whenever a connection opens or closes). */
export function startConnectionEvents() {
  if (connectionEvents) return
  connectionEvents = true
  onEvent<string[]>('connections', (ids) => useClusters.getState().setConnected(ids ?? []))
  void kube.connected().then((ids) => useClusters.getState().setConnected(ids))
}
