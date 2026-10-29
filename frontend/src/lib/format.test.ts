import { describe, expect, it } from 'vitest'
import { formatAge, readyTone, statusTone } from './format'

describe('formatAge', () => {
  const now = Date.parse('2026-01-10T00:00:00Z')
  it('formats ranges', () => {
    expect(formatAge('2026-01-09T23:59:15Z', now)).toBe('45s')
    expect(formatAge('2026-01-09T23:48:00Z', now)).toBe('12m')
    expect(formatAge('2026-01-09T21:00:00Z', now)).toBe('3h')
    expect(formatAge('2026-01-05T00:00:00Z', now)).toBe('5d')
  })
  it('handles bad input', () => expect(formatAge('', now)).toBe(''))
})

describe('tones', () => {
  it('maps statuses', () => {
    expect(statusTone('Running')).toBe('success')
    expect(statusTone('CrashLoopBackOff')).toBe('danger')
    expect(statusTone('ImagePullBackOff')).toBe('danger')
    expect(statusTone('Pending')).toBe('warning')
    expect(statusTone('Init:0/1')).toBe('warning')
    expect(statusTone('whatever')).toBe('neutral')
  })
  it('maps ready', () => {
    expect(readyTone('1/1')).toBe('success')
    expect(readyTone('1/2')).toBe('warning')
    expect(readyTone('0/2')).toBe('danger')
  })
})
