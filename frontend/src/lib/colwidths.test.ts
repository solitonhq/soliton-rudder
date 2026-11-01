import { beforeEach, describe, expect, it } from 'vitest'
import { applyWidths, clampWidth, loadWidths, MAX_COL, MIN_COL, saveWidths } from './colwidths'

beforeEach(() => localStorage.clear())

describe('column widths', () => {
  it('clamps to a usable range', () => {
    expect(clampWidth(5)).toBe(MIN_COL)
    expect(clampWidth(5000)).toBe(MAX_COL)
    expect(clampWidth(120.4)).toBe(120)
  })

  it('remembers widths per resource kind and forgets them when reset', () => {
    saveWidths('pods', { name0: 300, ns: 150 })
    expect(loadWidths('pods')).toEqual({ name0: 300, ns: 150 })
    expect(loadWidths('deployments')).toEqual({}) // another kind is not affected
    saveWidths('pods', {})
    expect(localStorage.getItem('soliton-rudder.colwidths.pods')).toBeNull()
  })

  it('ignores damaged or hostile stored data instead of breaking the table', () => {
    localStorage.setItem('soliton-rudder.colwidths.pods', '{not json')
    expect(loadWidths('pods')).toEqual({})
    localStorage.setItem('soliton-rudder.colwidths.pods', '[1,2]')
    expect(loadWidths('pods')).toEqual({})
    localStorage.setItem(
      'soliton-rudder.colwidths.pods',
      JSON.stringify({ a: 'wide', b: 99999, c: 10, d: null }),
    )
    expect(loadWidths('pods')).toEqual({ b: MAX_COL, c: MIN_COL })
  })

  it('applies overrides and leaves other columns untouched', () => {
    const cols = [
      { id: 'a', width: 100, label: 'A' },
      { id: 'b', width: 200, label: 'B' },
    ]
    const out = applyWidths(cols, { b: 320 })
    expect(out.map((c) => c.width)).toEqual([100, 320])
    expect(out[1]!.label).toBe('B')
    expect(cols[1]!.width).toBe(200) // the input is not mutated
  })
})
