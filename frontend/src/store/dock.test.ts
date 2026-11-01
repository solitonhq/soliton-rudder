import { beforeEach, describe, expect, it } from 'vitest'
import { currentTabId, tabsOf, useDock } from './dock'

const term = (clusterId: string, title = 'sh') => ({ kind: 'terminal' as const, clusterId, title })

function reset() {
  useDock.setState({ tabs: [], activeId: null, lastActive: {}, expanded: false })
}

describe('dock is scoped to a cluster', () => {
  beforeEach(reset)

  it('shows only the open cluster’s tabs', () => {
    const d = useDock.getState()
    d.openTab(term('a', 'a1'))
    d.openTab(term('b', 'b1'))
    const { tabs } = useDock.getState()
    expect(tabsOf(tabs, 'a').map((t) => t.title)).toEqual(['a1'])
    expect(tabsOf(tabs, 'b').map((t) => t.title)).toEqual(['b1'])
    expect(tabsOf(tabs, null)).toEqual([])
  })

  it('remembers each cluster’s active tab when switching back', () => {
    const d = useDock.getState()
    d.openTab(term('a', 'a1'))
    d.openTab(term('a', 'a2'))
    d.openTab(term('b', 'b1'))
    const s = useDock.getState()
    const a1 = s.tabs.find((t) => t.title === 'a1')!
    s.setActive(a1.id)
    useDock.getState().setActive(s.tabs.find((t) => t.title === 'b1')!.id)
    // back on cluster a: a1 was the one showing, not the newest
    expect(currentTabId(useDock.getState(), 'a')).toBe(a1.id)
  })

  it('falls back to the cluster’s last tab and returns null when it has none', () => {
    const d = useDock.getState()
    d.openTab(term('a', 'a1'))
    expect(currentTabId({ tabs: useDock.getState().tabs, lastActive: {} }, 'a')).toBe(
      useDock.getState().tabs[0]!.id,
    )
    expect(currentTabId(useDock.getState(), 'b')).toBeNull()
    expect(currentTabId(useDock.getState(), null)).toBeNull()
  })

  it('closing a tab activates a neighbour from the same cluster', () => {
    const d = useDock.getState()
    d.openTab(term('a', 'a1'))
    d.openTab(term('b', 'b1'))
    d.openTab(term('a', 'a2'))
    const s = useDock.getState()
    const a2 = s.tabs.find((t) => t.title === 'a2')!
    s.closeTab(a2.id)
    const after = useDock.getState()
    expect(currentTabId(after, 'a')).toBe(after.tabs.find((t) => t.title === 'a1')!.id)
    expect(tabsOf(after.tabs, 'b')).toHaveLength(1)
  })

  it('closeCluster drops only that cluster and keeps the others’ active tab', () => {
    const d = useDock.getState()
    d.openTab(term('a', 'a1'))
    d.openTab(term('b', 'b1'))
    const b1 = useDock.getState().tabs.find((t) => t.clusterId === 'b')!
    useDock.getState().closeCluster('a')
    const s = useDock.getState()
    expect(s.tabs.map((t) => t.title)).toEqual(['b1'])
    expect(s.activeId).toBe(b1.id)
    expect(s.lastActive).toEqual({ b: b1.id })
  })

  it('collapses the dock when the last tab closes', () => {
    const d = useDock.getState()
    d.openTab(term('a'))
    expect(useDock.getState().expanded).toBe(true)
    useDock.getState().closeTab(useDock.getState().tabs[0]!.id)
    expect(useDock.getState().expanded).toBe(false)
    expect(useDock.getState().activeId).toBeNull()
  })
})
