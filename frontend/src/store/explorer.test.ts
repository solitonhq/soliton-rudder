import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useExplorer } from './explorer'

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  localStorage.clear()
  App = {
    ConnectCluster: vi.fn().mockResolvedValue([
      {
        key: 'core/v1/pods',
        name: 'pods',
        kind: 'Pod',
        group: '',
        version: 'v1',
        namespaced: true,
      },
      {
        key: 'core/v1/namespaces',
        name: 'namespaces',
        kind: 'Namespace',
        group: '',
        version: 'v1',
        namespaced: false,
      },
    ]),
    SubscribeResource: vi.fn().mockResolvedValue('sub1'),
    UnsubscribeResource: vi.fn().mockResolvedValue(undefined),
    GetIntegrations: vi.fn().mockResolvedValue({}),
    ListPortForwards: vi.fn().mockResolvedValue([]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
  useExplorer.setState({
    view: 'helm',
    selectedKey: 'core/v1/services',
    clusterId: null,
    phase: 'idle',
  })
})
afterEach(() => {
  delete (window as unknown as { go?: unknown }).go
})

describe('opening a cluster', () => {
  it('lands on the Overview, whatever view the previous cluster was on, without starting a table stream', async () => {
    localStorage.setItem('soliton-rudder.explorer.c1', JSON.stringify({ key: 'core/v1/pods' })) // remembered last kind
    await useExplorer.getState().open('c1')
    const s = useExplorer.getState()
    expect(s.phase).toBe('ready')
    expect(s.view).toBe('overview')
    expect(s.selectedKey).toBeNull()
    // only the namespace list is streamed (for the picker); the pods table waits until it is asked for
    const subscribed = App.SubscribeResource.mock.calls.map((c) => c[1])
    expect(subscribed).not.toContain('core/v1/pods')
  })

  it('picking a kind from the sidebar then shows its table', async () => {
    await useExplorer.getState().open('c1')
    await useExplorer.getState().select('core/v1/pods')
    expect(useExplorer.getState().view).toBe('resources')
    expect(useExplorer.getState().selectedKey).toBe('core/v1/pods')
  })
})
