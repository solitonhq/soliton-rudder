import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Cluster, Resource } from '../lib/types'
import { useClusters } from './clusters'
import { useExplorer, type View } from './explorer'
import { canGoBack, canGoForward, currentLoc, record, startNavigation, useNav } from './nav'

const cl = (id: string) =>
  ({
    id,
    name: id,
    color: '#888',
    icon: '',
    pinned: false,
    position: 0,
    source: 'file',
    kubeconfigPath: '',
    context: id,
    createdAt: 0,
  }) as Cluster
const res = (key: string) =>
  ({
    key,
    name: key.split('/').pop(),
    kind: key,
    group: '',
    version: 'v1',
    namespaced: true,
  }) as unknown as Resource

let stop: () => void
const at = () =>
  useNav.getState().entries.map((e) => `${e.clusterId ?? 'home'}:${e.view}:${e.key ?? '-'}`)

/** What the real Explorer does: opening a cluster connects, then shows its remembered kind. */
function openCluster(id: string, key: string | null = 'core/v1/pods', view: View = 'resources') {
  useExplorer.setState({
    clusterId: id,
    phase: 'ready',
    view,
    selectedKey: key,
    catalog: [res('core/v1/pods'), res('core/v1/services'), res('apps/v1/deployments')],
  })
}

beforeEach(() => {
  useClusters.setState({
    clusters: [cl('a'), cl('b')],
    activeId: null,
    setActive: ((id: string | null) => {
      useClusters.setState({ activeId: id })
      if (id) setTimeout(() => openCluster(id, 'core/v1/pods'), 5) // connecting takes a moment
    }) as never,
  })
  useExplorer.setState({
    clusterId: null,
    phase: 'idle',
    view: 'resources',
    selectedKey: null,
    catalog: [],
    select: (async (key: string) =>
      useExplorer.setState({ selectedKey: key, view: 'resources' })) as never,
    setView: ((v: View) => useExplorer.setState({ view: v })) as never,
  })
  stop = startNavigation()
})
afterEach(() => stop())

describe('navigation history', () => {
  it('starts at Home with nothing to go back to', () => {
    expect(at()).toEqual(['home:resources:-'])
    expect(canGoBack(useNav.getState())).toBe(false)
    expect(canGoForward(useNav.getState())).toBe(false)
  })

  it('records each place and goes back and forward through them', async () => {
    useClusters.setState({ activeId: 'a' })
    expect(currentLoc()).toBeNull() // still connecting: not a place yet
    openCluster('a', 'core/v1/pods')
    await useExplorer.getState().select('core/v1/services')
    useExplorer.getState().setView('helm')
    expect(at()).toEqual([
      'home:resources:-',
      'a:resources:core/v1/pods',
      'a:resources:core/v1/services',
      'a:helm:-',
    ])

    await useNav.getState().back()
    expect(useExplorer.getState().view).toBe('resources')
    expect(useExplorer.getState().selectedKey).toBe('core/v1/services')
    await useNav.getState().back()
    expect(useExplorer.getState().selectedKey).toBe('core/v1/pods')
    expect(useNav.getState().index).toBe(1)
    expect(at().length).toBe(4) // going back does not add entries

    await useNav.getState().forward()
    expect(useExplorer.getState().selectedKey).toBe('core/v1/services')
    expect(canGoForward(useNav.getState())).toBe(true)
  })

  it('a new place after going back drops the forward history', async () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    await useExplorer.getState().select('core/v1/services')
    await useNav.getState().back()
    await useExplorer.getState().select('apps/v1/deployments')
    expect(at()).toEqual([
      'home:resources:-',
      'a:resources:core/v1/pods',
      'a:resources:apps/v1/deployments',
    ])
    expect(canGoForward(useNav.getState())).toBe(false)
  })

  it('opening a cluster is one place, not two (no kind yet, then the remembered kind)', () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', null)
    expect(at()).toEqual(['home:resources:-', 'a:resources:-'])
    useExplorer.setState({ selectedKey: 'core/v1/pods' })
    expect(at()).toEqual(['home:resources:-', 'a:resources:core/v1/pods'])
  })

  it('goes back to Home and into another cluster, waiting for it to connect', async () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    useClusters.setState({ activeId: null })
    useClusters.setState({ activeId: 'b' })
    openCluster('b', 'core/v1/services')
    expect(at()).toEqual([
      'home:resources:-',
      'a:resources:core/v1/pods',
      'home:resources:-',
      'b:resources:core/v1/services',
    ])
    await useNav.getState().back() // Home
    expect(useClusters.getState().activeId).toBeNull()
    await useNav.getState().back() // cluster a, which has to reconnect first
    expect(useClusters.getState().activeId).toBe('a')
    expect(useExplorer.getState().clusterId).toBe('a')
    expect(useExplorer.getState().selectedKey).toBe('core/v1/pods')
  })

  it('skips places whose cluster was deleted', async () => {
    useClusters.setState({ activeId: 'b' })
    openCluster('b', 'core/v1/pods')
    useClusters.setState({ activeId: null })
    useClusters.setState({ clusters: [cl('a')] }) // b is gone
    await useNav.getState().back()
    expect(useClusters.getState().activeId).toBeNull()
    expect(at().some((e) => e.startsWith('b:'))).toBe(false)
  })

  it('keeps at most 50 places', () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    for (let i = 0; i < 80; i++) {
      useExplorer.setState({ view: i % 2 ? 'helm' : 'reports' })
    }
    expect(useNav.getState().entries.length).toBe(50)
    expect(useNav.getState().index).toBe(49)
  })

  it('ignores a repeated place', () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    const n = at().length
    record()
    record()
    expect(at().length).toBe(n)
  })
})

describe('shortcuts', () => {
  const press = (init: KeyboardEventInit, target?: EventTarget) => {
    const ev = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
    ;(target ?? window).dispatchEvent(ev)
  }

  it('⌘[ goes back and ⌘] goes forward', async () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    await useExplorer.getState().select('core/v1/services')
    press({ key: '[', metaKey: true })
    await vi.waitFor(() => expect(useExplorer.getState().selectedKey).toBe('core/v1/pods'))
    press({ key: ']', ctrlKey: true })
    await vi.waitFor(() => expect(useExplorer.getState().selectedKey).toBe('core/v1/services'))
  })

  it('does not steal the keys while typing', async () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    await useExplorer.getState().select('core/v1/services')
    const input = document.createElement('input')
    document.body.appendChild(input)
    press({ key: '[', metaKey: true }, input)
    await new Promise((r) => setTimeout(r, 20))
    expect(useExplorer.getState().selectedKey).toBe('core/v1/services')
    input.remove()
  })

  it('the mouse back and forward buttons work too', async () => {
    useClusters.setState({ activeId: 'a' })
    openCluster('a', 'core/v1/pods')
    await useExplorer.getState().select('core/v1/services')
    window.dispatchEvent(new MouseEvent('mouseup', { button: 3, cancelable: true }))
    await vi.waitFor(() => expect(useExplorer.getState().selectedKey).toBe('core/v1/pods'))
    window.dispatchEvent(new MouseEvent('mouseup', { button: 4, cancelable: true }))
    await vi.waitFor(() => expect(useExplorer.getState().selectedKey).toBe('core/v1/services'))
  })
})
