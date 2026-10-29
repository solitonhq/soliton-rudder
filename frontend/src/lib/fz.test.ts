import { describe, expect, it } from 'vitest'
import { scoreColor, scoreLabel } from './fz'

describe('fz helpers', () => {
  it('maps scores to colours and labels', () => {
    expect(scoreColor(95)).toBe('var(--success)')
    expect(scoreColor(70)).toBe('var(--warning)')
    expect(scoreColor(10)).toBe('var(--danger)')
    expect(scoreLabel(92)).toBe('Excellent')
    expect(scoreLabel(30)).toBe('Critical')
  })
})
