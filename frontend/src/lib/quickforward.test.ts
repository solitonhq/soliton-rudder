import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FORWARD_KIND, FORWARDABLE, forwardURL, looksLikeWeb, quickForward } from './quickforward'
import type { ForwardRuntime } from './types'
import { recentlyHealed } from '../views/Forwards'

const rt = (o: Partial<ForwardRuntime> = {}): ForwardRuntime => ({
  id: 'f1',
  clusterId: 'c1',
  namespace: 'default',
  kind: 'service',
  name: 'web',
  remotePort: 80,
  localPort: 0,
  auto: false,
  createdAt: 0,
  status: 'connecting',
  activePort: 0,
  pod: '',
  message: '',
  reconnects: 0,
  lastHealedReason: '',
  lastHealedAt: 0,
  ...o,
})

function mockBackend(app: Record<string, unknown>) {
  ;(window as unknown as { go: unknown }).go = { main: { App: app } }
}

beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }))
afterEach(() => {
  vi.useRealTimers()
  delete (window as unknown as { go?: unknown }).go
})

describe('quickForward', () => {
  it('returns the local port once the tunnel is up', async () => {
    let polls = 0
    mockBackend({
      QuickForward: async () => ({
        forward: rt(),
        ports: [{ name: 'http', port: 80, protocol: 'TCP', target: '8080' }],
        needChoice: false,
      }),
      ListPortForwards: async () =>
        ++polls < 2 ? [rt()] : [rt({ status: 'active', activePort: 54321 })],
    })
    const p = quickForward('c1', 'core/v1/services', 'default', 'web')
    await vi.advanceTimersByTimeAsync(1000)
    expect(await p).toEqual({
      kind: 'started',
      text: 'localhost:54321 → web:80',
      url: 'http://localhost:54321',
    })
  })

  it('asks the user to choose when several ports exist', async () => {
    mockBackend({
      QuickForward: async () => ({
        forward: null,
        ports: [{ port: 80 }, { port: 443 }],
        needChoice: true,
      }),
    })
    const out = await quickForward('c1', 'core/v1/services', 'default', 'web')
    expect(out.kind).toBe('choose')
    expect(out.kind === 'choose' && out.ports).toHaveLength(2)
  })

  it('surfaces a failed start instead of hanging', async () => {
    mockBackend({
      QuickForward: async () => ({ forward: rt(), ports: [{ port: 80 }], needChoice: false }),
      ListPortForwards: async () => [
        rt({ status: 'error', message: 'no ready pods behind service web' }),
      ],
    })
    const p = quickForward('c1', 'core/v1/services', 'default', 'web')
    const assertion = expect(p).rejects.toThrow('no ready pods')
    await vi.advanceTimersByTimeAsync(1000)
    await assertion
  })

  it('falls back to a generic message if the tunnel is slow', async () => {
    mockBackend({
      QuickForward: async () => ({ forward: rt(), ports: [{ port: 80 }], needChoice: false }),
      ListPortForwards: async () => [rt()],
    })
    const p = quickForward('c1', 'core/v1/services', 'default', 'web', 600)
    await vi.advanceTimersByTimeAsync(2000)
    const out = await p
    expect(out.kind === 'started' && out.text).toContain('is starting')
  })
})

describe('forwarding helpers', () => {
  it('maps list resources to forward kinds', () => {
    expect(FORWARD_KIND.deployments).toBe('deployment')
    expect(FORWARDABLE.has('services')).toBe(true)
    expect(FORWARDABLE.has('configmaps')).toBe(false)
  })
  it('shows the auto-healed badge for two minutes after a repair', () => {
    const now = Date.now()
    const healed = rt({
      status: 'active',
      reconnects: 2,
      lastHealedAt: Math.floor(now / 1000) - 30,
    })
    expect(recentlyHealed(healed, now)).toBe(true)
    expect(recentlyHealed({ ...healed, lastHealedAt: Math.floor(now / 1000) - 300 }, now)).toBe(
      false,
    )
    expect(recentlyHealed({ ...healed, reconnects: 0 }, now)).toBe(false)
    expect(recentlyHealed({ ...healed, status: 'degraded' }, now)).toBe(false)
  })
})

describe('browser opening helpers', () => {
  it('opens web-looking ports but not databases or caches', () => {
    for (const p of [80, 443, 3000, 8080, 8443, 9090, 5173]) expect(looksLikeWeb(p)).toBe(true)
    for (const p of [5432, 6379, 3306, 27017, 9042, 22]) expect(looksLikeWeb(p)).toBe(false)
  })
  it('uses https only for the TLS ports', () => {
    expect(forwardURL(5555, 8080)).toBe('http://localhost:5555')
    expect(forwardURL(5555, 443)).toBe('https://localhost:5555')
    expect(forwardURL(5555, 8443)).toBe('https://localhost:5555')
  })
  it('quickForward gives no url for a database port', async () => {
    mockBackend({
      QuickForward: async () => ({
        forward: rt({ remotePort: 5432, status: 'active', activePort: 40000 }),
        ports: [{ name: '', port: 5432, protocol: 'TCP', target: 'pg' }],
        needChoice: false,
      }),
    })
    const out = await quickForward('c1', 'core/v1/pods', 'default', 'pg-0')
    expect(out).toEqual({ kind: 'started', text: 'localhost:40000 → pg-0:5432', url: undefined })
  })
})
