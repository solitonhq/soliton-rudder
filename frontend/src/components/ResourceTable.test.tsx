import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { RowStore } from '../lib/rowstore'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { ResourceTable } from './ResourceTable'

// jsdom has no layout, so the virtualiser would render no rows: render every row instead
vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 30,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, i) => ({ index: i, start: i * 30, key: i })),
    scrollToIndex: () => {},
  }),
}))

const scale = vi.fn().mockResolvedValue(undefined)
const restart = vi.fn().mockResolvedValue(undefined)
vi.mock('../lib/backend', async (orig) => ({
  ...(await orig<typeof import('../lib/backend')>()),
  kube: { scale: (...a: unknown[]) => scale(...a), restart: (...a: unknown[]) => restart(...a) },
}))

function seed(key: string, cells: unknown[][]) {
  const main = new RowStore()
  main.apply({
    sub: 's',
    reset: true,
    delete: null,
    columns: [
      { name: 'Name', type: 'string', priority: 0, description: '' },
      { name: 'Ready', type: 'string', priority: 0, description: '' },
    ],
    upsert: cells.map((c, i) => ({
      uid: `u${i}`,
      ns: 'shop',
      name: String(c[0]),
      created: '2026-10-29T10:00:00Z',
      cells: c,
    })),
  })
  useExplorer.setState({
    main,
    clusterId: 'c1',
    selectedKey: key,
    namespaces: [],
    search: '',
    sort: null,
    selected: null,
    detailDirty: false,
  })
}

beforeEach(() => {
  scale.mockClear()
  restart.mockClear()
  useUI.setState({ toasts: [] })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('row actions on workloads', () => {
  it('offers Scale and Restart on a deployment row and nothing on a pod row', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    const { unmount } = render(
      <ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />,
    )
    expect(screen.getByLabelText('Scale web')).toBeTruthy()
    expect(screen.getByLabelText('Restart web')).toBeTruthy()
    unmount()
    seed('core/v1/configmaps', [['cfg', '']])
    render(<ResourceTable namespaced resourceName="configmaps" onAction={vi.fn()} />)
    expect(screen.queryByLabelText('Scale cfg')).toBeNull()
    expect(screen.queryByLabelText('Restart cfg')).toBeNull()
  })

  it('DaemonSets can restart but have no replica count to scale', () => {
    seed('apps/v1/daemonsets', [['agent', '3/3']])
    render(<ResourceTable namespaced resourceName="daemonsets" onAction={vi.fn()} />)
    expect(screen.getByLabelText('Restart agent')).toBeTruthy()
    expect(screen.queryByLabelText('Scale agent')).toBeNull()
  })

  it('scales from the row with the current count prefilled, without selecting the row', async () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.click(screen.getByLabelText('Scale web'))
    const input = (await screen.findByLabelText('Replicas')) as HTMLInputElement
    expect(input.value).toBe('3')
    expect(useExplorer.getState().selected).toBeNull() // the click did not open the detail panel
    fireEvent.change(input, { target: { value: '5' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Scale' }))
    })
    expect(scale).toHaveBeenCalledWith('c1', 'apps/v1/deployments', 'shop', 'web', 5)
  })

  it('restarts only after confirmation', async () => {
    seed('apps/v1/deployments', [['web', '3/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.click(screen.getByLabelText('Restart web'))
    expect(restart).not.toHaveBeenCalled()
    await act(async () => {
      fireEvent.click(await screen.findByRole('button', { name: 'Restart' }))
    })
    expect(restart).toHaveBeenCalledWith('c1', 'apps/v1/deployments', 'shop', 'web')
  })

  it('also offers both in the right-click menu', () => {
    seed('apps/v1/deployments', [['web', '3/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.contextMenu(screen.getByRole('row', { name: /web/ }))
    expect(screen.getByText('Scale…')).toBeTruthy()
    expect(screen.getByText('Restart…')).toBeTruthy()
  })
})

describe('closing the detail panel by clicking the empty area', () => {
  const ref = { uid: 'u0', ns: 'shop', name: 'web' }

  it('closes the panel when the empty area is clicked', () => {
    seed('apps/v1/deployments', [['web', '3/3']])
    useExplorer.setState({ selected: ref })
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.click(screen.getByRole('grid'))
    expect(useExplorer.getState().selected).toBeNull()
  })

  it('does not treat a click inside a row as the empty area', () => {
    seed('apps/v1/deployments', [
      ['web', '3/3'],
      ['api', '1/1'],
    ])
    useExplorer.setState({ selected: ref })
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.click(screen.getByRole('row', { name: /api/ })) // switches to api, panel stays open
    expect(useExplorer.getState().selected?.name).toBe('api')
  })

  it('asks first when the open panel has unsaved edits, and keeps it if declined', () => {
    seed('apps/v1/deployments', [['web', '3/3']])
    useExplorer.setState({ selected: ref, detailDirty: true })
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(false))
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.click(screen.getByRole('grid'))
    expect(useExplorer.getState().selected).toEqual(ref)
  })
})

describe('resizing columns', () => {
  beforeEach(() => localStorage.clear())

  const handle = (name: string) => screen.getByRole('separator', { name: `Resize ${name} column` })

  it('has a resize handle on every column header, and resizing does not sort', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    expect(handle('Name')).toBeTruthy()
    expect(handle('Ready')).toBeTruthy()
    fireEvent.click(handle('Name'))
    expect(useExplorer.getState().sort).toBeNull()
  })

  it('resizes with the keyboard and remembers the width for the kind', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    const { unmount } = render(
      <ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />,
    )
    const before = Number(handle('Name').getAttribute('aria-valuenow'))
    fireEvent.keyDown(handle('Name'), { key: 'ArrowRight' })
    expect(Number(handle('Name').getAttribute('aria-valuenow'))).toBe(before + 16)
    fireEvent.keyDown(handle('Name'), { key: 'ArrowRight', shiftKey: true })
    expect(Number(handle('Name').getAttribute('aria-valuenow'))).toBe(before + 16 + 64)
    expect(JSON.parse(localStorage.getItem('soliton-rudder.colwidths.deployments')!)).toMatchObject(
      {
        Name0: before + 80,
      },
    )
    // a fresh table of the same kind starts with the saved width
    unmount()
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    expect(Number(handle('Name').getAttribute('aria-valuenow'))).toBe(before + 80)
  })

  it('never goes below the minimum width', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    for (let i = 0; i < 60; i++)
      fireEvent.keyDown(handle('Name'), { key: 'ArrowLeft', shiftKey: true })
    expect(Number(handle('Name').getAttribute('aria-valuenow'))).toBe(48)
  })

  it('Enter or a double-click puts a column back to its automatic width', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    const auto = Number(handle('Name').getAttribute('aria-valuenow'))
    fireEvent.keyDown(handle('Name'), { key: 'ArrowRight', shiftKey: true })
    fireEvent.keyDown(handle('Name'), { key: 'Enter' })
    expect(Number(handle('Name').getAttribute('aria-valuenow'))).toBe(auto)
    fireEvent.keyDown(handle('Name'), { key: 'ArrowRight' })
    fireEvent.doubleClick(handle('Name'))
    expect(Number(handle('Name').getAttribute('aria-valuenow'))).toBe(auto)
    expect(localStorage.getItem('soliton-rudder.colwidths.deployments')).toBeNull()
  })

  it('"Reset column widths" in the header menu clears every override', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    fireEvent.keyDown(handle('Name'), { key: 'ArrowRight' })
    fireEvent.keyDown(handle('Ready'), { key: 'ArrowRight' })
    fireEvent.contextMenu(screen.getByRole('button', { name: 'Sort by Name' }))
    fireEvent.click(screen.getByText('Reset column widths'))
    expect(localStorage.getItem('soliton-rudder.colwidths.deployments')).toBeNull()
  })
})

describe('dragging a column edge', () => {
  beforeEach(() => {
    localStorage.clear()
    // jsdom has no pointer capture
    Element.prototype.setPointerCapture = vi.fn()
  })

  it('follows the pointer and saves the width on release', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    const h = screen.getByRole('separator', { name: 'Resize Name column' })
    const start = Number(h.getAttribute('aria-valuenow'))
    fireEvent.pointerDown(h, { clientX: 100, pointerId: 1 })
    fireEvent.pointerMove(h, { clientX: 160, pointerId: 1 })
    expect(Number(h.getAttribute('aria-valuenow'))).toBe(start + 60)
    expect(localStorage.getItem('soliton-rudder.colwidths.deployments')).toBeNull() // not saved mid-drag
    fireEvent.pointerUp(h, { clientX: 160, pointerId: 1 })
    expect(JSON.parse(localStorage.getItem('soliton-rudder.colwidths.deployments')!)).toMatchObject(
      {
        Name0: start + 60,
      },
    )
  })

  it('a click without movement saves nothing', () => {
    seed('apps/v1/deployments', [['web', '2/3']])
    render(<ResourceTable namespaced resourceName="deployments" onAction={vi.fn()} />)
    const h = screen.getByRole('separator', { name: 'Resize Name column' })
    fireEvent.pointerDown(h, { clientX: 100, pointerId: 1 })
    fireEvent.pointerUp(h, { clientX: 100, pointerId: 1 })
    expect(localStorage.getItem('soliton-rudder.colwidths.deployments')).toBeNull()
  })
})
