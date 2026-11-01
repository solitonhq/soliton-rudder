import { create } from 'zustand'

interface TabBase {
  id: string
  clusterId: string
  title: string
}

export type DockTab =
  | (TabBase & { kind: 'terminal'; pod?: { ns: string; name: string; container: string } })
  | (TabBase & { kind: 'logs'; key: string; ns: string; name: string; resourceKind: string })
  | (TabBase & { kind: 'apply'; ns: string })
  | (TabBase & { kind: 'plugin'; pluginId: string; panelId: string; entry: string })

type NewTab = DockTab extends infer T ? (T extends DockTab ? Omit<T, 'id'> : never) : never

interface DockState {
  tabs: DockTab[]
  /** the most recently activated tab (of any cluster); use `currentTabId` for what the dock shows */
  activeId: string | null
  /** the tab each cluster last showed, so switching back restores it */
  lastActive: Record<string, string>
  expanded: boolean
  height: number
  openTab: (t: NewTab) => void
  closeTab: (id: string) => void
  setActive: (id: string) => void
  setExpanded: (v: boolean) => void
  setHeight: (h: number) => void
  closeCluster: (clusterId: string) => void
}

/** The tabs that belong to one cluster. The dock only ever shows these. */
export function tabsOf(tabs: DockTab[], clusterId: string | null): DockTab[] {
  return clusterId ? tabs.filter((t) => t.clusterId === clusterId) : []
}

/** The tab to show for a cluster: its last active one, else its last tab. */
export function currentTabId(
  s: Pick<DockState, 'tabs' | 'lastActive'>,
  clusterId: string | null,
): string | null {
  const own = tabsOf(s.tabs, clusterId)
  if (own.length === 0 || !clusterId) return null
  const last = s.lastActive[clusterId]
  return own.find((t) => t.id === last)?.id ?? own[own.length - 1]!.id
}

const HEIGHT_KEY = 'soliton-rudder.dock.height'
let seq = 0

function loadHeight(): number {
  try {
    const n = Number(localStorage.getItem(HEIGHT_KEY))
    if (n >= 160 && n <= 1200) return n
  } catch {
    /* ignore */
  }
  return 340
}

export const useDock = create<DockState>((set, get) => ({
  tabs: [],
  activeId: null,
  lastActive: {},
  expanded: false,
  height: loadHeight(),
  openTab(t) {
    const id = `tab${++seq}`
    set((s) => ({
      tabs: [...s.tabs, { ...t, id } as DockTab],
      activeId: id,
      lastActive: { ...s.lastActive, [t.clusterId]: id },
      expanded: true,
    }))
  },
  closeTab(id) {
    const { tabs, activeId, lastActive } = get()
    const closing = tabs.find((t) => t.id === id)
    if (!closing) return
    const own = tabsOf(tabs, closing.clusterId)
    const idx = own.findIndex((t) => t.id === id)
    const next = tabs.filter((t) => t.id !== id)
    const ownNext = tabsOf(next, closing.clusterId)
    const la = { ...lastActive }
    // the neighbour of the closed tab (within the same cluster) takes over
    if (la[closing.clusterId] === id || ownNext.length === 0) {
      const neighbour = ownNext[Math.min(idx, ownNext.length - 1)]
      if (neighbour) la[closing.clusterId] = neighbour.id
      else delete la[closing.clusterId]
    }
    set({
      tabs: next,
      activeId:
        activeId === id ? (la[closing.clusterId] ?? next[next.length - 1]?.id ?? null) : activeId,
      lastActive: la,
      expanded: next.length > 0 ? get().expanded : false,
    })
  },
  setActive(id) {
    const t = get().tabs.find((x) => x.id === id)
    if (!t) return
    set((s) => ({
      activeId: id,
      lastActive: { ...s.lastActive, [t.clusterId]: id },
      expanded: true,
    }))
  },
  setExpanded: (expanded) => set({ expanded }),
  setHeight(height) {
    const h = Math.max(160, Math.min(height, 1200))
    set({ height: h })
    try {
      localStorage.setItem(HEIGHT_KEY, String(h))
    } catch {
      /* ignore */
    }
  },
  closeCluster: (clusterId) => {
    const { tabs, activeId, lastActive } = get()
    const keep = tabs.filter((t) => t.clusterId !== clusterId)
    if (keep.length === tabs.length) return
    const la = { ...lastActive }
    delete la[clusterId]
    set({
      tabs: keep,
      activeId: keep.some((t) => t.id === activeId)
        ? activeId
        : (keep[keep.length - 1]?.id ?? null),
      lastActive: la,
      expanded: keep.length > 0 && get().expanded,
    })
  },
}))
