import { describe, expect, it } from 'vitest'
import { connectionLook } from './connection'

describe('connectionLook', () => {
  it('is green only for a connected, healthy cluster', () => {
    expect(connectionLook(true, 'connected').ring).toBe('border-success')
    expect(connectionLook(true, undefined).ring).toBe('border-success')
  })
  it('is red when the cluster is closed, whatever its last health check said', () => {
    expect(connectionLook(false, 'connected')).toEqual({
      ring: 'border-danger/70',
      label: 'Disconnected',
    })
  })
  it('is red when connected but unreachable, and says why', () => {
    const l = connectionLook(true, 'unreachable', 'connection refused')
    expect(l.ring).toBe('border-danger')
    expect(l.label).toContain('connection refused')
  })
  it('is amber while checking or when unauthorized', () => {
    expect(connectionLook(true, 'checking').ring).toContain('border-warning')
    expect(connectionLook(true, 'unauthorized').ring).toBe('border-warning')
  })
})
