import { create } from 'zustand'

export interface Toast {
  id: number
  kind: 'success' | 'error'
  text: string
}

export type DialogState =
  | null
  | { kind: 'add' }
  | { kind: 'settings'; tab?: string }
  | { kind: 'edit'; id: string }
  | { kind: 'compare' }
  | { kind: 'access'; clusterId: string }

interface UIState {
  dialog: DialogState
  toasts: Toast[]
  toast: (kind: Toast['kind'], text: string) => void
  palette: boolean
  setPalette: (v: boolean) => void
  open: (d: NonNullable<DialogState>) => void
  close: () => void
}

let toastSeq = 0

export const useUI = create<UIState>((set) => ({
  dialog: null,
  toasts: [],
  palette: false,
  setPalette: (palette) => set({ palette }),
  toast(kind, text) {
    const id = ++toastSeq
    set((s) => ({ toasts: [...s.toasts, { id, kind, text }] }))
    setTimeout(
      () => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
      kind === 'error' ? 8000 : 3500,
    )
  },
  open: (dialog) => set({ dialog }),
  close: () => set({ dialog: null }),
}))
