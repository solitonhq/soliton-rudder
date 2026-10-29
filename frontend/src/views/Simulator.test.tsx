import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DrainResult, NetResult } from '../lib/types'
import { Simulator } from './Simulator'

const targets = {
  namespaces: ['shop'],
  pods: ['shop/web', 'shop/db'],
  services: ['shop/web'],
  nodes: ['n1', 'n2'],
}
const denied: NetResult = {
  verdict: 'denied',
  summary:
    'Blocked at the ingress of shop/db: isolated by default-deny but no rule allows TCP/5432 here',
  paths: [
    {
      target: 'shop/db',
      port: 5432,
      allowed: false,
      steps: [
        {
          side: 'ingress',
          subject: 'shop/db',
          isolated: true,
          allowed: false,
          policies: ['default-deny'],
          matched: '',
          reason: 'isolated by default-deny but no rule allows TCP/5432 here',
        },
      ],
    },
  ],
  notes: ['Enforcement depends on your CNI.'],
}
const drain: DrainResult = {
  node: 'n1',
  verdict: 'blocked',
  summary: ['PDB shop/solo: allows no disruption right now'],
  pods: [
    {
      namespace: 'shop',
      name: 'solo',
      owner: 'Deployment/solo',
      action: 'evict',
      severity: 'warn',
      reason: 'will be evicted and rescheduled',
      movesTo: 'n2',
      cpuM: 100,
      mem: 1 << 26,
    },
  ],
  pdbs: [
    {
      namespace: 'shop',
      name: 'solo',
      rule: 'minAvailable 1',
      matching: 1,
      healthy: 1,
      allowed: 0,
      evicting: 1,
      severity: 'error',
      message: 'allows no disruption right now',
    },
  ],
  capacity: { freeCpuM: 4000, freeMem: 8 << 30, needCpuM: 100, needMem: 1 << 26, nodes: 1 },
  hpas: [
    {
      namespace: 'shop',
      name: 'web',
      target: 'Deployment/web',
      current: 2,
      max: 10,
      fits: 2,
      severity: 'warn',
      message: 'could not reach 10 replicas: room for only 2 of the 8 extra',
    },
  ],
  notes: [],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetSimTargets: vi.fn().mockResolvedValue(targets),
    SimulateNetwork: vi.fn().mockResolvedValue(denied),
    SimulateDrain: vi.fn().mockResolvedValue(drain),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('Simulator', () => {
  it('sends the query and shows which policy blocks the traffic', async () => {
    render(<Simulator clusterId="c1" />)
    fireEvent.change(screen.getByLabelText('From name'), { target: { value: 'shop/web' } })
    fireEvent.change(screen.getByLabelText('To name'), { target: { value: 'shop/db' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '5432' } })
    fireEvent.click(screen.getByRole('button', { name: /Simulate/ }))
    await waitFor(() =>
      expect(App.SimulateNetwork).toHaveBeenCalledWith('c1', {
        from: { kind: 'Pod', namespace: 'shop', name: 'web', ip: '' },
        to: { kind: 'Pod', namespace: 'shop', name: 'db', ip: '' },
        port: '5432',
        protocol: 'TCP',
      }),
    )
    expect(await screen.findByText('denied')).toBeTruthy()
    expect(screen.getByText('default-deny')).toBeTruthy()
    expect(screen.getByText(/Enforcement depends on your CNI/)).toBeTruthy()
  })

  it('cannot simulate before both ends are filled in', () => {
    render(<Simulator clusterId="c1" />)
    expect((screen.getByRole('button', { name: /Simulate/ }) as HTMLButtonElement).disabled).toBe(
      true,
    )
  })

  it('sends an external source as an IP', async () => {
    render(<Simulator clusterId="c1" />)
    fireEvent.change(screen.getByLabelText('From kind'), { target: { value: 'External' } })
    fireEvent.change(screen.getByLabelText('From IP'), { target: { value: '203.0.113.9' } })
    fireEvent.change(screen.getByLabelText('To name'), { target: { value: 'shop/web' } })
    fireEvent.click(screen.getByRole('button', { name: /Simulate/ }))
    await waitFor(() => expect(App.SimulateNetwork).toHaveBeenCalled())
    expect(App.SimulateNetwork.mock.calls[0][1].from).toEqual({
      kind: 'External',
      namespace: '',
      name: '',
      ip: '203.0.113.9',
    })
  })

  it('shows the network backend error', async () => {
    App.SimulateNetwork.mockRejectedValue(new Error('forbidden: networkpolicies'))
    render(<Simulator clusterId="c1" />)
    fireEvent.change(screen.getByLabelText('From name'), { target: { value: 'a/b' } })
    fireEvent.change(screen.getByLabelText('To name'), { target: { value: 'c/d' } })
    fireEvent.click(screen.getByRole('button', { name: /Simulate/ }))
    expect(await screen.findByText(/forbidden: networkpolicies/)).toBeTruthy()
  })

  it('simulates a drain and surfaces the blocking PDB, capacity and HPA headroom', async () => {
    render(<Simulator clusterId="c1" />)
    fireEvent.click(screen.getByText('Node drain'))
    const btn = screen.getByRole('button', { name: /Simulate drain/ }) as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    await waitFor(() => expect(screen.getByRole('option', { name: 'n1' })).toBeTruthy())
    fireEvent.change(screen.getByLabelText('Node'), { target: { value: 'n1' } })
    fireEvent.click(btn)
    await waitFor(() => expect(App.SimulateDrain).toHaveBeenCalledWith('c1', 'n1'))
    expect(await screen.findByText('blocked')).toBeTruthy()
    expect(screen.getAllByText(/allows no disruption right now/).length).toBeGreaterThan(0)
    expect(screen.getByText(/room for only 2 of the 8 extra/)).toBeTruthy()
    expect(screen.getAllByText('shop/solo').length).toBe(2) // pod row + PDB row
  })
})
