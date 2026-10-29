import { create } from 'zustand'
import { onEvent, plugins } from '../lib/backend'
import type { PluginInfo } from '../lib/types'
import { useUI } from './ui'

interface PluginsState {
  list: PluginInfo[]
  loaded: boolean
  refresh: () => Promise<void>
}

export const usePlugins = create<PluginsState>((set) => ({
  list: [],
  loaded: false,
  async refresh() {
    try {
      set({ list: await plugins.list(), loaded: true })
    } catch {
      set({ loaded: true })
    }
  },
}))

type EventListener = (plugin: string, name: string, data: unknown) => void
const listeners = new Set<EventListener>()

/** Subscribes to events emitted by plugin backends (ui.emit). Returns an unsubscribe function. */
export function onPluginEvent(l: EventListener): () => void {
  listeners.add(l)
  return () => listeners.delete(l)
}

let started = false
export function startPluginEvents() {
  if (started) return
  started = true
  void usePlugins.getState().refresh()
  onEvent<null>('pluginschanged', () => void usePlugins.getState().refresh())
  onEvent<{ plugin: string; kind: 'success' | 'error'; text: string }>('plugintoast', (t) => {
    const name =
      usePlugins.getState().list.find((p) => p.manifest.id === t.plugin)?.manifest.name ?? t.plugin
    useUI.getState().toast(t.kind, `${name}: ${t.text}`)
  })
  onEvent<{ plugin: string; name: string; data: unknown }>('pluginevent', (e) =>
    listeners.forEach((l) => l(e.plugin, e.name, e.data)),
  )
}

/** Enabled plugins' UI panels, flattened. */
export function usePanels(placement: 'cluster-nav' | 'dock') {
  const list = usePlugins((s) => s.list)
  return list
    .filter((p) => p.enabled && p.manifest.ui)
    .flatMap((p) =>
      (p.manifest.ui?.panels ?? [])
        .filter((x) => x.placement === placement)
        .map((panel) => ({ plugin: p, panel })),
    )
}
