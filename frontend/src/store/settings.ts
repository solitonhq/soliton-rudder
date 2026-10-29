import { create } from 'zustand'
import { backend, onEvent, system, type UpdateResult } from '../lib/backend'
import { applyTheme, DEFAULT_THEME, isTheme, type Theme } from '../themes/themes'
import { useUI } from './ui'

interface SettingsState {
  theme: Theme
  checkUpdates: boolean
  /** Set when a newer release is known (from the daily check or "Check now"). */
  update: UpdateResult | null
  ready: boolean
  load: () => Promise<void>
  setTheme: (t: Theme) => Promise<void>
  setCheckUpdates: (on: boolean) => Promise<void>
  setUpdate: (u: UpdateResult | null) => void
}

export const useSettings = create<SettingsState>((set, get) => ({
  theme: DEFAULT_THEME,
  checkUpdates: true,
  update: null,
  ready: false,
  async load() {
    try {
      const s = await backend.getSettings()
      const theme = isTheme(s.theme) ? s.theme : DEFAULT_THEME
      applyTheme(theme)
      set({ theme, checkUpdates: s.checkUpdates !== false, ready: true })
    } catch {
      applyTheme(DEFAULT_THEME)
      set({ ready: true })
    }
  },
  async setTheme(theme) {
    const prev = get().theme
    applyTheme(theme)
    set({ theme })
    try {
      await backend.setTheme(theme)
    } catch {
      applyTheme(prev)
      set({ theme: prev })
    }
  },
  async setCheckUpdates(on) {
    const prev = get().checkUpdates
    set({ checkUpdates: on })
    try {
      await system.setCheckUpdates(on)
    } catch (e) {
      set({ checkUpdates: prev })
      useUI.getState().toast('error', e instanceof Error ? e.message : String(e))
    }
  },
  setUpdate: (update) => set({ update }),
}))

let started = false
/** Listens for the background update check (once per day when enabled). */
export function startUpdateEvents() {
  if (started) return
  started = true
  onEvent<UpdateResult>('updateavailable', (u) => {
    useSettings.getState().setUpdate(u)
    useUI
      .getState()
      .toast('success', `Soliton Rudder ${u.latest} is available — see Settings → About`)
  })
}
