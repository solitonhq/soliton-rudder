import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Cluster } from '../lib/types'
import { useClusters } from '../store/clusters'
import { useDock } from '../store/dock'
import { Dock } from './Dock'

// xterm needs a real canvas; the dock only cares that a tab's content stays mounted
vi.mock('./TerminalTab', () => ({
  TerminalTab: ({ tab }: { tab: { title: string } }) => <div data-testid="term">{tab.title}</div>,
}))

const cl = (id: string, name: string): Cluster =>
  ({
    id,
    name,
    color: '#888',
    icon: '',
    pinned: true,
    position: 0,
    source: 'file',
    kubeconfigPath: '',
    context: `ctx-${id}`,
    createdAt: 0,
  }) as Cluster

beforeEach(() => {
  useDock.setState({ tabs: [], activeId: null, lastActive: {}, expanded: false })
  useClusters.setState({ clusters: [cl('a', 'Alpha'), cl('b', 'Beta')], activeId: 'a' })
  useDock.getState().openTab({ kind: 'terminal', clusterId: 'a', title: 'alpha-shell' })
  useDock.getState().openTab({ kind: 'terminal', clusterId: 'b', title: 'beta-shell' })
})
afterEach(cleanup)

const tabBar = () => within(screen.getByLabelText(/Terminals and logs for/))

describe('Dock', () => {
  it('lists only the open cluster’s tabs in the tab bar', async () => {
    render(<Dock />)
    expect(await screen.findByText('alpha-shell', { selector: 'span' })).toBeTruthy()
    expect(screen.queryByText('beta-shell', { selector: 'span' })).toBeNull()
    expect(screen.getByLabelText('Terminals and logs for Alpha')).toBeTruthy()
  })

  it('switching cluster shows the other cluster’s tabs without losing the first one’s', async () => {
    const { rerender } = render(<Dock />)
    await screen.findByText('alpha-shell', { selector: 'span' })
    useClusters.setState({ activeId: 'b' })
    rerender(<Dock />)
    expect(await screen.findByText('beta-shell', { selector: 'span' })).toBeTruthy()
    expect(screen.queryByText('alpha-shell', { selector: 'span' })).toBeNull()
    // alpha's terminal is still mounted (hidden), so its session survives
    const mounted = (await screen.findAllByTestId('term')).map((n) => n.textContent)
    expect(mounted).toEqual(expect.arrayContaining(['alpha-shell', 'beta-shell']))
    expect(tabBar().getByText('Beta')).toBeTruthy()
  })

  it('is hidden on Home but keeps every tab mounted', async () => {
    useClusters.setState({ activeId: null })
    const { container } = render(<Dock />)
    const section = container.querySelector('section')!
    expect(section.className).toContain('hidden')
    expect(await screen.findAllByTestId('term')).toHaveLength(2)
  })
})
