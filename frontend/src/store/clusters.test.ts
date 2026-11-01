import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useClusters } from './clusters'
import { useDock } from './dock'
import { useUI } from './ui'

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    DisconnectCluster: vi.fn().mockResolvedValue(undefined),
    ConnectCluster: vi.fn().mockResolvedValue([]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
  useUI.setState({ toasts: [] })
  useClusters.setState({ activeId: 'a', connected: { a: true, b: true } })
})
afterEach(() => {
  delete (window as unknown as { go?: unknown }).go
})

describe('connection state', () => {
  it('setConnected replaces the whole set', () => {
    useClusters.getState().setConnected(['b', 'c'])
    expect(useClusters.getState().connected).toEqual({ b: true, c: true })
  })

  it('closes the dock tabs of a cluster whose connection went away, and only those', () => {
    useDock.setState({ tabs: [], activeId: null, lastActive: {}, expanded: false })
    useDock.getState().openTab({ kind: 'terminal', clusterId: 'a', title: 'a-shell' })
    useDock.getState().openTab({ kind: 'terminal', clusterId: 'b', title: 'b-shell' })
    useClusters.getState().setConnected(['b']) // a disconnected
    expect(useDock.getState().tabs.map((t) => t.title)).toEqual(['b-shell'])
  })

  it('keeps the dock tabs while a cluster stays connected', () => {
    useDock.setState({ tabs: [], activeId: null, lastActive: {}, expanded: false })
    useDock.getState().openTab({ kind: 'terminal', clusterId: 'a', title: 'a-shell' })
    useClusters.getState().setConnected(['a', 'b'])
    expect(useDock.getState().tabs).toHaveLength(1)
  })

  it('disconnecting the open cluster returns to Home before closing it', async () => {
    await useClusters.getState().disconnect('a')
    expect(useClusters.getState().activeId).toBeNull()
    expect(App.DisconnectCluster).toHaveBeenCalledWith('a')
  })

  it('disconnecting another cluster leaves the open one alone', async () => {
    await useClusters.getState().disconnect('b')
    expect(useClusters.getState().activeId).toBe('a')
    expect(App.DisconnectCluster).toHaveBeenCalledWith('b')
  })

  it('shows why a connection failed instead of failing silently', async () => {
    App.ConnectCluster.mockRejectedValue(new Error('x509: certificate has expired'))
    await useClusters.getState().connect('a')
    expect(useUI.getState().toasts.at(-1)?.text).toContain('x509: certificate has expired')
  })
})
