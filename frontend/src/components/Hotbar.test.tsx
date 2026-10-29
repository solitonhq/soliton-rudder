import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Cluster, Health } from '../lib/types'
import { useClusters } from '../store/clusters'
import { useUI } from '../store/ui'
import { Hotbar } from './Hotbar'
import { Home } from '../views/Home'

const cl = (id: string, name: string, pinned = false): Cluster =>
  ({
    id,
    name,
    color: '#888',
    icon: '',
    pinned,
    position: 0,
    source: 'file',
    kubeconfigPath: '',
    context: `ctx-${id}`,
    createdAt: 0,
  }) as Cluster
const h = (status: Health['status']): Health =>
  ({ status, version: 'v1.30', latencyMs: 5, error: '' }) as Health

let update: ReturnType<typeof vi.fn>
let connect: ReturnType<typeof vi.fn>
let disconnect: ReturnType<typeof vi.fn>
beforeEach(() => {
  update = vi.fn().mockResolvedValue(undefined)
  connect = vi.fn().mockResolvedValue(undefined)
  disconnect = vi.fn().mockResolvedValue(undefined)
  useUI.setState({ dialog: null })
  useClusters.setState({
    clusters: [cl('a', 'Alpha', true), cl('b', 'Beta'), cl('c', 'Gamma', true)],
    health: { a: h('connected'), b: h('connected'), c: h('unreachable') },
    connected: { a: true },
    activeId: null,
    loaded: true,
    update: update as never,
    connect: connect as never,
    disconnect: disconnect as never,
    checkAll: vi.fn() as never,
  })
})
afterEach(cleanup)

const ringOf = (name: string) => screen.getByLabelText(`Cluster ${name}`).className

describe('Hotbar', () => {
  it('shows pinned clusters and connected ones, and hides the rest', () => {
    render(<Hotbar />)
    expect(screen.getByLabelText('Cluster Alpha')).toBeTruthy()
    expect(screen.getByLabelText('Cluster Gamma')).toBeTruthy() // pinned, not connected
    expect(screen.queryByLabelText('Cluster Beta')).toBeNull() // neither
    cleanup()
    useClusters.setState({ connected: { a: true, b: true } })
    render(<Hotbar />)
    expect(screen.getByLabelText('Cluster Beta')).toBeTruthy() // connected, not pinned
  })

  it('unpinning a connected cluster keeps it until it is disconnected', () => {
    useClusters.setState({ clusters: [cl('a', 'Alpha', false)], connected: { a: true } })
    const { unmount } = render(<Hotbar />)
    expect(screen.getByLabelText('Cluster Alpha')).toBeTruthy()
    expect(screen.getByLabelText('Cluster Alpha').getAttribute('title')).toContain(
      'disconnect to remove it',
    )
    unmount()
    useClusters.setState({ connected: {} })
    render(<Hotbar />)
    expect(screen.queryByLabelText('Cluster Alpha')).toBeNull()
  })

  it('draws the state as a ring around the icon: green connected, red closed or unreachable', () => {
    render(<Hotbar />)
    expect(ringOf('Alpha')).toContain('border-success')
    expect(ringOf('Gamma')).toContain('border-danger') // pinned but closed
    cleanup()
    useClusters.setState({ connected: { a: true, c: true } })
    render(<Hotbar />)
    expect(ringOf('Gamma')).toContain('border-danger') // connected but unreachable
    expect(screen.getByLabelText('Cluster Gamma').getAttribute('title')).toContain(
      'Connected, but not reachable',
    )
    expect(document.querySelector('.rounded-full.size-2\\.5')).toBeNull() // no loose dot any more
  })

  it('marks the open cluster with a bar, not with the status ring', () => {
    useClusters.setState({ activeId: 'a' })
    const { container } = render(<Hotbar />)
    expect(container.querySelectorAll('span.bg-accent.rounded-r').length).toBe(1)
    expect(ringOf('Alpha')).toContain('border-success')
  })

  it('offers Disconnect for a connected cluster and Connect for a closed one', () => {
    render(<Hotbar />)
    fireEvent.contextMenu(screen.getByLabelText('Cluster Alpha'))
    fireEvent.click(screen.getByText('Disconnect'))
    expect(disconnect).toHaveBeenCalledWith('a')
    cleanup()
    render(<Hotbar />)
    fireEvent.contextMenu(screen.getByLabelText('Cluster Gamma'))
    fireEvent.click(screen.getByText('Connect'))
    expect(connect).toHaveBeenCalledWith('c')
  })

  it('has a Compare clusters button that opens the comparison', () => {
    render(<Hotbar />)
    fireEvent.click(screen.getByLabelText('Compare clusters'))
    expect(useUI.getState().dialog).toEqual({ kind: 'compare' })
  })

  it('explains how to get clusters onto the hotbar when none are pinned or connected', () => {
    useClusters.setState({ clusters: [cl('b', 'Beta')], connected: {} })
    render(<Hotbar />)
    expect(screen.getByText(/Pin clusters on Home/)).toBeTruthy()
  })
})

describe('Home', () => {
  it('lists every cluster and pins or unpins with the pin button', () => {
    render(<Home />)
    expect(screen.getByText('Alpha')).toBeTruthy()
    expect(screen.getByText('Beta')).toBeTruthy()
    expect(screen.getByText('Gamma')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Pin Beta to the hotbar'))
    expect(update).toHaveBeenCalledWith('b', { pinned: true })
    fireEvent.click(screen.getByLabelText('Unpin Alpha from the hotbar'))
    expect(update).toHaveBeenCalledWith('a', { pinned: false })
  })

  it('pinning does not open the cluster', () => {
    const setActive = vi.fn()
    useClusters.setState({ setActive: setActive as never })
    render(<Home />)
    fireEvent.click(screen.getByLabelText('Pin Beta to the hotbar'))
    expect(setActive).not.toHaveBeenCalled()
  })
})

describe('Home connect / disconnect', () => {
  it('connects a closed cluster and disconnects a connected one without opening it', () => {
    const setActive = vi.fn()
    useClusters.setState({ setActive: setActive as never })
    render(<Home />)
    fireEvent.click(screen.getByLabelText('Connect Beta'))
    expect(connect).toHaveBeenCalledWith('b')
    fireEvent.click(screen.getByLabelText('Disconnect Alpha'))
    expect(disconnect).toHaveBeenCalledWith('a')
    expect(setActive).not.toHaveBeenCalled()
  })
})
