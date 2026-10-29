import { describe, expect, it } from 'vitest'
import { applyTheme, isTheme, THEMES, THEME_META } from './themes'

describe('themes', () => {
  it('has exactly the three planned themes with metadata', () => {
    expect([...THEMES]).toEqual(['dark-black', 'dark-blue', 'light'])
    for (const t of THEMES) expect(THEME_META[t].label).toBeTruthy()
  })
  it('validates theme ids', () => {
    expect(isTheme('light')).toBe(true)
    expect(isTheme('neon')).toBe(false)
    expect(isTheme(undefined)).toBe(false)
  })
  it('applies data-theme attribute', () => {
    const el = document.createElement('div')
    applyTheme('dark-blue', el)
    expect(el.getAttribute('data-theme')).toBe('dark-blue')
  })
})
