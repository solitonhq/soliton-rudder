import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Cluster, ClusterOverview } from '../lib/types'
import { useClusters } from '../store/clusters'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { CapacityBar, Overview } from './Overview'

const cl = {
  id: 'c1',
  name: 'Production EU',
  color: '#888',
  icon: '',
  pinned: true,
  position: 0,
  source: 'file',
  kubeconfigPath: '',
  context: 'x',
  createdAt: 0,
} as Cluster

const base: ClusterOverview = {
  serverVersion: 'v1.27.10',
  namespaces: 144,
  nodes: { total: 5, ready: 4, notReady: 1, cordoned: 1, pressure: 0 },
  pods: { total: 220, ready: 200, notReady: 4, pending: 3, failed: 2, succeeded: 11 },
  workloads: { total: 211, healthy: 205, degraded: 4, failing: 2 },
  capacity: {
    cpuAllocM: 16000,
    memAlloc: 64 * 1024 ** 3,
    cpuRequestedM: 12000,
    memRequested: 40 * 1024 ** 3,
    cpuUsedM: 4000,
    memUsed: 20 * 1024 ** 3,
    hasUsage: true,
  },
  attention: [
    {
      severity: 'error',
      kind: 'Node',
      namespace: '',
      name: 'worker-3',
      reason: 'NotReady',
      resourceKey: 'core/v1/nodes',
    },
    {
      severity: 'error',
      kind: 'Pod',
      namespace: 'shop',
      name: 'web-1',
      reason: 'CrashLoopBackOff · 7 restarts',
      resourceKey: 'core/v1/pods',
    },
  ],
  attentionTotal: 2,
  warnings: [
    {
      at: Date.now() - 120_000,
      namespace: 'shop',
      kind: 'Pod',
      name: 'web-1',
      reason: 'BackOff',
      message: 'Back-off restarting failed container',
      count: 12,
      resourceKey: 'core/v1/pods',
    },
  ],
  notes: [],
  access: {
    clusterWide: true,
    canListNamespaces: true,
    namespaces: [],
    source: 'cluster',
    note: '',
  },
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetClusterOverview: vi.fn().mockResolvedValue(base),
    ListAssessments: vi.fn().mockResolvedValue([
      {
        id: 'a',
        clusterId: 'c1',
        startedAt: Math.floor(Date.now() / 1000) - 7200,
        durationMs: 1,
        score: 61,
        serverVersion: '',
        counts: {},
        categories: [],
      },
      {
        id: 'b',
        clusterId: 'c1',
        startedAt: Math.floor(Date.now() / 1000) - 3600,
        durationMs: 1,
        score: 72,
        serverVersion: '',
        counts: {},
        categories: [],
      },
    ]),
    GetBlackbox: vi.fn().mockResolvedValue({
      config: { enabled: true, namespaces: [], intervalSec: 15 },
      retentionHours: 24,
      maxRecords: 1,
      status: { running: true, lastRun: 0, lastError: '', recorded: 0 },
      span: { min: 0, max: 0, count: 0 },
    }),
    MatchRunbooks: vi.fn().mockResolvedValue([]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
  useClusters.setState({ clusters: [cl] })
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('Overview', () => {
  it('summarises nodes, pods, workloads and warnings with what is wrong', async () => {
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText('Production EU')).toBeTruthy()
    expect(screen.getByText('v1.27.10')).toBeTruthy()
    expect(screen.getByText('1 not ready · 1 cordoned')).toBeTruthy()
    expect(screen.getByText('4 not ready · 3 pending · 2 failed')).toBeTruthy()
    expect(screen.getByText('2 failing · 4 degraded')).toBeTruthy()
    expect(screen.getByText('144')).toBeTruthy()
    expect(screen.getByText('Warnings (1 h)')).toBeTruthy()
  })

  it('shows requests and real usage against capacity', async () => {
    render(<Overview clusterId="c1" />)
    expect(await screen.findByRole('img', { name: 'CPU: 75% requested, 25% used' })).toBeTruthy()
    expect(screen.getByRole('img', { name: 'Memory: 63% requested, 31% used' })).toBeTruthy()
  })

  it('explains when live usage is not available', async () => {
    App.GetClusterOverview.mockResolvedValue({
      ...base,
      capacity: { ...base.capacity, hasUsage: false },
    })
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText(/needs metrics-server/)).toBeTruthy()
    expect(screen.getByRole('img', { name: 'CPU: 75% requested' })).toBeTruthy()
  })

  it('lists what needs attention and opens the runbook for the clicked item', async () => {
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText('worker-3')).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', { name: /Runbook/ })[1])
    await waitFor(() =>
      expect(App.MatchRunbooks).toHaveBeenCalledWith({
        kind: 'Pod',
        namespace: 'shop',
        name: 'web-1',
        reason: 'CrashLoopBackOff · 7 restarts',
        status: 'error',
      }),
    )
  })

  it('opens the object in its table when the open icon or a warning is clicked', async () => {
    const jump = vi.fn().mockResolvedValue(undefined)
    const setView = vi.fn()
    useExplorer.setState({ jump, setView } as never)
    render(<Overview clusterId="c1" />)
    fireEvent.click(await screen.findByLabelText('Open worker-3'))
    expect(setView).toHaveBeenCalledWith('resources')
    expect(jump).toHaveBeenCalledWith('core/v1/nodes', 'worker-3')
    fireEvent.click(screen.getByTitle('Open web-1'))
    expect(jump).toHaveBeenLastCalledWith('core/v1/pods', 'web-1')
    expect(screen.getByText('×12 ·', { exact: false })).toBeTruthy()
  })

  it('shows the latest assessment score and a trend, and says so when there is none', async () => {
    const { unmount } = render(<Overview clusterId="c1" />)
    expect(await screen.findByText('72')).toBeTruthy()
    expect(screen.getByRole('img', { name: 'Score trend' })).toBeTruthy()
    unmount()
    App.ListAssessments.mockResolvedValue([])
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText(/No assessment yet/)).toBeTruthy()
  })

  it('reports a healthy cluster calmly', async () => {
    App.GetClusterOverview.mockResolvedValue({
      ...base,
      nodes: { total: 3, ready: 3, notReady: 0, cordoned: 0, pressure: 0 },
      pods: { total: 10, ready: 10, notReady: 0, pending: 0, failed: 0, succeeded: 0 },
      workloads: { total: 4, healthy: 4, degraded: 0, failing: 0 },
      attention: [],
      attentionTotal: 0,
      warnings: [],
    })
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText('Nothing needs attention right now.')).toBeTruthy()
    expect(screen.getByText('No warning events in the last hour.')).toBeTruthy()
    expect(screen.getAllByText('all ready').length).toBe(2)
  })

  it('shows the error when the cluster cannot be read', async () => {
    App.GetClusterOverview.mockRejectedValue(new Error('forbidden: pods'))
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText(/forbidden: pods/)).toBeTruthy()
  })

  it('shows only the first few attention items until asked', async () => {
    const many = Array.from({ length: 12 }, (_, i) => ({
      severity: 'warn' as const,
      kind: 'Pod',
      namespace: 'shop',
      name: `p${i}`,
      reason: 'Pending',
      resourceKey: 'core/v1/pods',
    }))
    App.GetClusterOverview.mockResolvedValue({ ...base, attention: many, attentionTotal: 12 })
    render(<Overview clusterId="c1" />)
    await screen.findByText('p0')
    expect(screen.queryByText('p11')).toBeNull()
    fireEvent.click(screen.getByText('Show 12'))
    expect(screen.getByText('p11')).toBeTruthy()
  })
})

describe('Overview with limited access', () => {
  it('says that the figures cover only the readable namespaces and offers to choose them', async () => {
    App.GetClusterOverview.mockResolvedValue({
      ...base,
      access: {
        clusterWide: false,
        canListNamespaces: false,
        namespaces: ['team-a', 'team-b'],
        source: 'discovered',
        note: '',
      },
      notes: ['Limited access: figures cover only the namespaces you can read.'],
    })
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText('Limited access')).toBeTruthy()
    expect(
      screen.getByText(/can read 2 namespaces; the figures below cover only those/),
    ).toBeTruthy()
    expect(screen.queryAllByText(/Limited access: figures cover/).length).toBe(0) // not repeated as a footer note
    fireEvent.click(screen.getByText('Choose namespaces…'))
    expect(useUI.getState().dialog).toEqual({ kind: 'access', clusterId: 'c1' })
  })

  it('shows what to do when no namespace could be found', async () => {
    App.GetClusterOverview.mockResolvedValue({
      ...base,
      access: {
        clusterWide: false,
        canListNamespaces: false,
        namespaces: [],
        source: 'none',
        note: 'No namespace could be found that these credentials can read.',
      },
    })
    render(<Overview clusterId="c1" />)
    expect(await screen.findByText(/No namespace could be found/)).toBeTruthy()
  })

  it('shows no banner with full access', async () => {
    render(<Overview clusterId="c1" />)
    await screen.findByText('Production EU')
    expect(screen.queryByText('Limited access')).toBeNull()
  })
})

describe('CapacityBar', () => {
  it('caps at 100% and survives a cluster with no capacity', () => {
    render(
      <CapacityBar label="CPU" alloc={1000} requested={5000} used={null} fmt={(n) => `${n}`} />,
    )
    expect(screen.getByRole('img', { name: 'CPU: 100% requested' })).toBeTruthy()
    cleanup()
    render(<CapacityBar label="CPU" alloc={0} requested={0} used={0} fmt={(n) => `${n}`} />)
    expect(screen.getByRole('img', { name: 'CPU: 0% requested, 0% used' })).toBeTruthy()
  })
})
