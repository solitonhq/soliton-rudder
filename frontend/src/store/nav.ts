import { create } from 'zustand'
import { useClusters } from './clusters'
import { useExplorer, type View } from './explorer'

/** A place in the app: Home (clusterId null), or a view of a cluster (and the resource kind for the table). */
export interface Loc {
  clusterId: string | null
  view: View
  key: string | null
}

const MAX = 50
const READY_WAIT_MS = 20_000

interface NavState {
  entries: Loc[]
  index: number
  restoring: boolean
  back: () => Promise<void>
  forward: () => Promise<void>
}

const same = (a: Loc, b: Loc) => a.clusterId === b.clusterId && a.view === b.view && a.key === b.key

/** Where the user is now, or null while a cluster is still connecting (that is not a place worth returning to). */
export function currentLoc(): Loc | null {
  const { activeId } = useClusters.getState()
  if (activeId === null) return { clusterId: null, view: 'resources', key: null }
  const ex = useExplorer.getState()
  if (ex.clusterId !== activeId || ex.phase !== 'ready') return null
  return {
    clusterId: activeId,
    view: ex.view,
    key: ex.view === 'resources' ? ex.selectedKey : null,
  }
}

export const useNav = create<NavState>(() => ({
  entries: [],
  index: -1,
  restoring: false,
  back: () => go(-1),
  forward: () => go(1),
}))

/** Adds the current place to the history, unless it is the one we are already on or we are mid-restore. */
export function record() {
  const { entries, index, restoring } = useNav.getState()
  if (restoring) return
  const loc = currentLoc()
  if (!loc) return
  const cur = entries[index]
  if (cur && same(cur, loc)) return
  // opening a cluster first shows it without a kind and then picks the remembered/default one: that is one place
  if (
    cur &&
    cur.clusterId !== null &&
    cur.clusterId === loc.clusterId &&
    cur.view === 'resources' &&
    cur.key === null
  ) {
    const next = entries.slice(0, index)
    next.push(loc)
    useNav.setState({ entries: next, index: next.length - 1 })
    return
  }
  let next = [...entries.slice(0, index + 1), loc]
  if (next.length > MAX) next = next.slice(next.length - MAX)
  useNav.setState({ entries: next, index: next.length - 1 })
}

function waitUntil(cond: () => boolean, ms: number): Promise<boolean> {
  return new Promise((resolve) => {
    if (cond()) return resolve(true)
    const timer = setTimeout(() => {
      unsub()
      resolve(false)
    }, ms)
    const check = () => {
      if (cond()) {
        clearTimeout(timer)
        unsub()
        resolve(true)
      }
    }
    const u1 = useExplorer.subscribe(check)
    const u2 = useClusters.subscribe(check)
    const unsub = () => {
      u1()
      u2()
    }
  })
}

async function apply(loc: Loc): Promise<boolean> {
  const clusters = useClusters.getState()
  if (loc.clusterId !== null && !clusters.clusters.some((c) => c.id === loc.clusterId)) return false // deleted since
  if (loc.clusterId !== clusters.activeId) clusters.setActive(loc.clusterId)
  if (loc.clusterId === null) return true
  const ready = await waitUntil(() => {
    const ex = useExplorer.getState()
    return ex.clusterId === loc.clusterId && ex.phase === 'ready'
  }, READY_WAIT_MS)
  if (!ready) return true // stay where we landed; the cluster view shows why it could not connect
  const ex = useExplorer.getState()
  if (loc.view === 'resources') {
    if (loc.key && ex.selectedKey !== loc.key && ex.catalog.some((r) => r.key === loc.key))
      await ex.select(loc.key)
    else ex.setView('resources')
  } else ex.setView(loc.view)
  return true
}

async function go(delta: -1 | 1) {
  const { entries, index, restoring } = useNav.getState()
  const target = index + delta
  if (restoring || target < 0 || target >= entries.length) return
  useNav.setState({ index: target, restoring: true })
  try {
    const ok = await apply(entries[target])
    if (!ok) {
      // that cluster no longer exists: forget the place and stay where we are
      const kept = entries.filter((_, i) => i !== target)
      useNav.setState({ entries: kept, index: Math.min(index, kept.length - 1) })
    }
  } finally {
    useNav.setState({ restoring: false })
    record() // lands on the restored place: deduplicated, or recorded if a late redirect moved us
  }
}

export const canGoBack = (s: Pick<NavState, 'index'>) => s.index > 0
export const canGoForward = (s: Pick<NavState, 'index' | 'entries'>) =>
  s.index < s.entries.length - 1

const typing = (t: EventTarget | null) => {
  const el = t as HTMLElement | null
  return (
    !!el &&
    (el.tagName === 'INPUT' ||
      el.tagName === 'TEXTAREA' ||
      el.tagName === 'SELECT' ||
      el.isContentEditable)
  )
}

let stop: (() => void) | null = null

/** Starts recording places and listening for ⌘[ / ⌘] and the mouse's back/forward buttons. Returns a stop function. */
export function startNavigation(): () => void {
  stop?.()
  useNav.setState({ entries: [], index: -1, restoring: false })
  const u1 = useClusters.subscribe(record)
  const u2 = useExplorer.subscribe(record)
  record()
  const onKey = (e: KeyboardEvent) => {
    if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey || typing(e.target)) return
    if (e.key === '[') {
      e.preventDefault()
      void useNav.getState().back()
    } else if (e.key === ']') {
      e.preventDefault()
      void useNav.getState().forward()
    }
  }
  const onMouse = (e: MouseEvent) => {
    if (e.button === 3) {
      e.preventDefault()
      void useNav.getState().back()
    } else if (e.button === 4) {
      e.preventDefault()
      void useNav.getState().forward()
    }
  }
  window.addEventListener('keydown', onKey)
  window.addEventListener('mouseup', onMouse)
  stop = () => {
    u1()
    u2()
    window.removeEventListener('keydown', onKey)
    window.removeEventListener('mouseup', onMouse)
    stop = null
  }
  return stop
}
