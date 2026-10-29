import { describe, expect, it } from 'vitest'
import { formatBytes, formatCPU, formatPercent, formatUSD } from './units'

describe('units', () => {
  it('formats cpu', () => {
    expect(formatCPU(250)).toBe('250m')
    expect(formatCPU(1500)).toBe('1.5')
    expect(formatCPU(2000)).toBe('2')
    expect(formatCPU(16000)).toBe('16')
  })
  it('formats bytes', () => {
    expect(formatBytes(512)).toBe('512B')
    expect(formatBytes(128 * 1024 * 1024)).toBe('128Mi')
    expect(formatBytes(1.5 * 1024 ** 3)).toBe('1.5Gi')
    expect(formatBytes(200 * 1024 ** 3)).toBe('200Gi')
  })
  it('formats percent', () => {
    expect(formatPercent(1, 4)).toBe('25%')
    expect(formatPercent(1, 0)).toBe('')
  })
  it('formats dollars', () => {
    expect(formatUSD(12.345)).toBe('$12.35')
    expect(formatUSD(0)).toBe('$0.00')
    expect(formatUSD(450.4)).toBe('$450')
    expect(formatUSD(12345.6)).toBe('$12,346')
  })
})
