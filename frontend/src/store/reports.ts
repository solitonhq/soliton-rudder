import { create } from 'zustand'
import { assess, onEvent } from '../lib/backend'
import type { FzDiff, FzDone, FzProgress, FzReport, FzSummary } from '../lib/types'
import { useUI } from './ui'

interface ReportsState {
  clusterId: string | null
  history: FzSummary[]
  selectedId: string | null
  report: FzReport | null
  baselineId: string | null // run to compare against; null = previous run
  diff: FzDiff | null
  running: FzProgress | null
  loadError: string
  load: (clusterId: string) => Promise<void>
  select: (id: string | null) => Promise<void>
  setBaseline: (id: string | null) => Promise<void>
  start: () => Promise<void>
  cancel: () => Promise<void>
  remove: (id: string) => Promise<void>
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export const useReports = create<ReportsState>((set, get) => ({
  clusterId: null,
  history: [],
  selectedId: null,
  report: null,
  baselineId: null,
  diff: null,
  running: null,
  loadError: '',

  async load(clusterId) {
    set({ clusterId, loadError: '' })
    try {
      const [history, run] = await Promise.all([assess.list(clusterId), assess.running(clusterId)])
      if (get().clusterId !== clusterId) return
      set({ history, running: run ? { run, phase: 'Running', pct: 0 } : null })
      const keep = history.find((h) => h.id === get().selectedId)
      await get().select(keep ? keep.id : (history[history.length - 1]?.id ?? null))
    } catch (e) {
      set({ loadError: errText(e) })
    }
  },

  async select(id) {
    set({ selectedId: id, report: null, diff: null })
    if (!id) return
    try {
      const report = await assess.get(id)
      if (get().selectedId !== id) return
      set({ report })
      await refreshDiff(id)
    } catch (e) {
      useUI.getState().toast('error', errText(e))
    }
  },

  async setBaseline(id) {
    set({ baselineId: id })
    const sel = get().selectedId
    if (sel) await refreshDiff(sel)
  },

  async start() {
    const { clusterId } = get()
    if (!clusterId) return
    try {
      const run = await assess.start(clusterId)
      set({ running: { run, phase: 'Starting', pct: 1 } })
    } catch (e) {
      useUI.getState().toast('error', errText(e))
    }
  },

  async cancel() {
    const { clusterId } = get()
    if (clusterId) await assess.cancel(clusterId)
  },

  async remove(id) {
    try {
      await assess.remove(id)
      const history = get().history.filter((h) => h.id !== id)
      set({ history })
      if (get().selectedId === id) await get().select(history[history.length - 1]?.id ?? null)
    } catch (e) {
      useUI.getState().toast('error', errText(e))
    }
  },
}))

/** Compares the selected run with the chosen baseline (default: the run before it). */
async function refreshDiff(selectedId: string) {
  const { history, baselineId } = useReports.getState()
  const idx = history.findIndex((h) => h.id === selectedId)
  const base =
    baselineId && baselineId !== selectedId ? baselineId : idx > 0 ? history[idx - 1].id : null
  if (!base) return void useReports.setState({ diff: null })
  try {
    const diff = await assess.compare(base, selectedId)
    if (useReports.getState().selectedId === selectedId) useReports.setState({ diff })
  } catch {
    useReports.setState({ diff: null })
  }
}

let started = false
export function startReportEvents() {
  if (started) return
  started = true
  onEvent<FzProgress>('fzprogress', (p) => {
    if (useReports.getState().running?.run === p.run || useReports.getState().running)
      useReports.setState({ running: p })
  })
  onEvent<FzDone>('fzdone', async (d) => {
    const s = useReports.getState()
    useReports.setState({ running: null })
    if (d.error) {
      if (d.error !== 'cancelled') useUI.getState().toast('error', `Assessment failed: ${d.error}`)
      return
    }
    if (!s.clusterId) return
    try {
      const history = await assess.list(s.clusterId)
      useReports.setState({ history })
      if (history.some((h) => h.id === d.report)) await useReports.getState().select(d.report)
      useUI.getState().toast('success', 'Assessment complete')
    } catch {
      /* the list reloads when the view is opened */
    }
  })
}
