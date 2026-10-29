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
  activeId: string | null
  expanded: boolean
  height: number
  openTab: (t: NewTab) => void
  closeTab: (id: string) => void
  setActive: (id: string) => void
  setExpanded: (v: boolean) => void
  setHeight: (h: number) => void
  closeCluster: (clusterId: string) => void
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
  expanded: false,
  height: loadHeight(),
  openTab(t) {
    const id = `tab${++seq}`
    set((s) => ({ tabs: [...s.tabs, { ...t, id } as DockTab], activeId: id, expanded: true }))
  },
  closeTab(id) {
    const { tabs, activeId } = get()
    const idx = tabs.findIndex((t) => t.id === id)
    const next = tabs.filter((t) => t.id !== id)
    set({
      tabs: next,
      activeId: activeId === id ? (next[Math.min(idx, next.length - 1)]?.id ?? null) : activeId,
      expanded: next.length > 0 ? get().expanded : false,
    })
  },
  setActive: (activeId) => set({ activeId, expanded: true }),
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
    const keep = get().tabs.filter((t) => t.clusterId !== clusterId)
    set({ tabs: keep, activeId: keep[0]?.id ?? null, expanded: keep.length > 0 && get().expanded })
  },
}))
