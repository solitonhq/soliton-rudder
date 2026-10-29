import { create } from 'zustand'
import { onEvent, tools } from '../lib/backend'
import type { ForwardRuntime } from '../lib/types'

interface ForwardsState {
  clusterId: string | null
  items: ForwardRuntime[]
  load: (clusterId: string) => Promise<void>
}

export const useForwards = create<ForwardsState>((set, get) => ({
  clusterId: null,
  items: [],
  async load(clusterId) {
    set({ clusterId })
    const items = await tools.forwards.list(clusterId)
    if (get().clusterId === clusterId) set({ items })
  },
}))

let started = false
/** Applies backend state pushes to the store (call once at startup). */
export function startForwardEvents() {
  if (started) return
  started = true
  onEvent<ForwardRuntime>('pfstatus', (rt) => {
    const { clusterId, items } = useForwards.getState()
    if (rt.clusterId !== clusterId) return
    const i = items.findIndex((f) => f.id === rt.id)
    useForwards.setState({
      items: i >= 0 ? items.map((f) => (f.id === rt.id ? rt : f)) : [...items, rt],
    })
  })
}
