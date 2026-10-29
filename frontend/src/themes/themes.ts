export const THEMES = ['dark-black', 'dark-blue', 'light'] as const
export type Theme = (typeof THEMES)[number]

export const DEFAULT_THEME: Theme = 'dark-black'

export const THEME_META: Record<Theme, { label: string; swatch: [string, string] }> = {
  'dark-black': { label: 'Dark Black', swatch: ['#0a0a0b', '#5b8def'] },
  'dark-blue': { label: 'Dark Blue', swatch: ['#0b1220', '#4aa3ff'] },
  light: { label: 'Light', swatch: ['#f6f7f9', '#2f6fe0'] },
}

export function isTheme(v: unknown): v is Theme {
  return typeof v === 'string' && (THEMES as readonly string[]).includes(v)
}

export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement) {
  root.setAttribute('data-theme', theme)
}
