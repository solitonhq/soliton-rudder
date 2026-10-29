import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Cluster, Health } from '../lib/types'
import { useClusters } from '../store/clusters'
import { useUI } from '../store/ui'
import { Home, matches } from './Home'

const cl = (id: string, name: string, context: string, pinned = false): Cluster =>
  ({
    id,
    name,
    color: '#888',
    icon: '',
    pinned,
    position: 0,
    source: 'file',
    kubeconfigPath: '',
    context,
    createdAt: 0,
  }) as Cluster
const h = (status: Health['status'], error = ''): Health =>
  ({ status, version: 'v1.30', latencyMs: 12, error }) as Health

let setActive: ReturnType<typeof vi.fn>
let update: ReturnType<typeof vi.fn>
beforeEach(() => {
  localStorage.clear()
  setActive = vi.fn()
  update = vi.fn().mockResolvedValue(undefined)
  useUI.setState({ dialog: null })
  useClusters.setState({
    clusters: [
      cl('a', 'Production EU', 'gke_prod_eu'),
      cl('b', 'Staging', 'gke_stg', true),
      cl('c', 'Dev Kind', 'kind-dev'),
    ],
    health: { a: h('connected'), b: h('connected'), c: h('unreachable', 'connection refused') },
    connected: {},
    activeId: null,
    loaded: true,
    setActive: setActive as never,
    update: update as never,
    checkAll: vi.fn() as never,
  })
})
afterEach(cleanup)

const search = (v: string) =>
  fireEvent.change(screen.getByLabelText('Search clusters'), { target: { value: v } })

describe('matches', () => {
  it('requires every word and looks at name, context and status', () => {
    const c = cl('a', 'Production EU', 'gke_prod_eu')
    expect(matches(c, h('connected'), '')).toBe(true)
    expect(matches(c, h('connected'), 'prod eu')).toBe(true)
    expect(matches(c, h('connected'), 'gke_prod')).toBe(true)
    expect(matches(c, h('connected'), 'connected')).toBe(true)
    expect(matches(c, h('unreachable'), 'connected')).toBe(false)
    expect(matches(c, h('connected'), 'prod us')).toBe(false)
  })
})

describe('Home search', () => {
  it('filters as you type, shows the count, and clears with Escape or the clear button', () => {
    render(<Home />)
    expect(screen.getAllByText(/Production EU|Staging|Dev Kind/).length).toBe(3)
    search('stag')
    expect(screen.getByText('Staging')).toBeTruthy()
    expect(screen.queryByText('Production EU')).toBeNull()
    expect(screen.getByText('1 of 3')).toBeTruthy()
    fireEvent.keyDown(screen.getByLabelText('Search clusters'), { key: 'Escape' })
    expect(screen.getByText('Production EU')).toBeTruthy()
    search('kind')
    fireEvent.click(screen.getByLabelText('Clear search'))
    expect(screen.getByText('Production EU')).toBeTruthy()
  })

  it('finds clusters by status, so problems are one search away', () => {
    render(<Home />)
    search('unreachable')
    expect(screen.getByText('Dev Kind')).toBeTruthy()
    expect(screen.queryByText('Staging')).toBeNull()
  })

  it('Enter opens the cluster when exactly one matches, and does nothing otherwise', () => {
    render(<Home />)
    const box = screen.getByLabelText('Search clusters')
    search('gke')
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(setActive).not.toHaveBeenCalled() // two clusters match
    search('stag')
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(setActive).toHaveBeenCalledWith('b')
  })

  it('says so when nothing matches, and lets you clear', () => {
    render(<Home />)
    search('zzz')
    expect(screen.getByText(/No clusters match “zzz”/)).toBeTruthy()
    fireEvent.click(screen.getByText('Clear search'))
    expect(screen.getByText('Staging')).toBeTruthy()
  })
})

describe('Home layout', () => {
  it('starts as cards, switches to a list with the same clusters, and remembers the choice', () => {
    const { unmount } = render(<Home />)
    expect((screen.getByLabelText('Cards') as HTMLButtonElement).getAttribute('aria-pressed')).toBe(
      'true',
    )
    expect(screen.queryByText('Context')).toBeNull()
    fireEvent.click(screen.getByLabelText('List'))
    expect(screen.getByText('Context')).toBeTruthy() // column header
    expect(screen.getByText('Staging')).toBeTruthy()
    expect(screen.getByText('connection refused')).toBeTruthy() // the error has its own column
    expect(localStorage.getItem('soliton-rudder.home.layout')).toBe('list')
    unmount()
    render(<Home />)
    expect(screen.getByText('Context')).toBeTruthy()
    expect((screen.getByLabelText('List') as HTMLButtonElement).getAttribute('aria-pressed')).toBe(
      'true',
    )
  })

  it('search, open and pin work in the list too', () => {
    localStorage.setItem('soliton-rudder.home.layout', 'list')
    render(<Home />)
    search('dev')
    expect(screen.queryByText('Staging')).toBeNull()
    fireEvent.click(screen.getByText('Dev Kind'))
    expect(setActive).toHaveBeenCalledWith('c')
    fireEvent.click(screen.getByLabelText('Pin Dev Kind to the hotbar'))
    expect(update).toHaveBeenCalledWith('c', { pinned: true })
    expect(setActive).toHaveBeenCalledTimes(1) // pinning did not open it
  })

  it('works when storage is unavailable', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    render(<Home />)
    fireEvent.click(screen.getByLabelText('List'))
    expect(screen.getByText('Context')).toBeTruthy()
    get.mockRestore()
    set.mockRestore()
  })
})
