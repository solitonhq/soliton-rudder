import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TopoNode, Topology as Topo } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { Topology } from './Topology'

const node = (
  kind: string,
  name: string,
  tier: number,
  status: TopoNode['status'],
  detail = '',
): TopoNode => ({
  id: `${kind}/shop/${name}`,
  kind,
  name,
  namespace: 'shop',
  tier,
  status,
  detail,
  resourceKey: `k/${kind}`,
  missing: false,
})
const graph: Topo = {
  nodes: [
    node('Ingress', 'web', 0, 'error'),
    node('Service', 'web', 1, 'error', 'selector matches no pods'),
    node('Deployment', 'web', 2, 'ok'),
    node('Service', 'healthy', 1, 'ok'),
    node('Secret', 'web-tls', 4, 'unknown'),
  ],
  edges: [
    { from: 'Ingress/shop/web', to: 'Service/shop/web', kind: 'routes' },
    { from: 'Deployment/shop/web', to: 'Secret/shop/web-tls', kind: 'mounts' },
  ],
  truncated: false,
  notes: ['Showing 800 of 900 objects: pick a namespace to see the rest.'],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = { GetTopology: vi.fn().mockResolvedValue(graph) }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
  useExplorer.setState({ namespaces: [] })
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('Topology view', () => {
  it('draws every tier and reports problems', async () => {
    render(<Topology clusterId="c1" />)
    await waitFor(() => expect(screen.getByRole('img', { name: 'Cluster topology' })).toBeTruthy())
    expect(App.GetTopology).toHaveBeenCalledWith('c1', '')
    expect(screen.getAllByText('Ingress', { selector: 'text' }).length).toBe(2) // column header + node
    expect(screen.getByText('Workloads')).toBeTruthy()
    expect(screen.getByText(/Showing 800 of 900/)).toBeTruthy()
    // config nodes are hidden until asked for
    expect(screen.queryByText('web-tls')).toBeNull()
    fireEvent.click(screen.getByText('Config'))
    expect(screen.getByText('web-tls')).toBeTruthy()
  })

  it('"Problems only" keeps just the paths that lead to a failure', async () => {
    render(<Topology clusterId="c1" />)
    await screen.findByRole('img', { name: 'Cluster topology' })
    expect(screen.getAllByText('healthy').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByText('Problems only'))
    await waitFor(() => expect(screen.queryByText('healthy')).toBeNull())
    expect(screen.getAllByText('web').length).toBe(2) // ingress + service
  })

  it('shows details of a clicked node and can open it in the table', async () => {
    const jump = vi.fn().mockResolvedValue(undefined)
    useExplorer.setState({ jump })
    render(<Topology clusterId="c1" />)
    await screen.findByRole('img', { name: 'Cluster topology' })
    fireEvent.click(screen.getAllByText('web', { selector: 'text' })[1])
    expect(await screen.findByText('selector matches no pods', { selector: 'div' })).toBeTruthy()
    fireEvent.click(screen.getByText('Open in table'))
    expect(jump).toHaveBeenCalledWith('k/Service', 'web')
  })

  it('shows the backend error when the graph cannot be built', async () => {
    App.GetTopology.mockRejectedValue(new Error('forbidden'))
    render(<Topology clusterId="c1" />)
    expect(await screen.findByText(/forbidden/)).toBeTruthy()
  })
})
