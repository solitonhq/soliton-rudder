import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { FinOps } from '../lib/types'
import { FinOpsCard } from './FinOpsCard'

afterEach(cleanup)

const base: FinOps = {
  confidence: 'high',
  source: 'Prometheus 95th percentile over 24 hours',
  cpuHourUsd: 0.03,
  memGbHourUsd: 0.004,
  headroomPct: 20,
  monthlyCostUsd: 1234.5,
  potentialSaveUsd: 456.78,
  notes: ['Estimate = requested resources × configured rates.'],
  namespaces: [
    {
      namespace: 'shop',
      pods: 12,
      monthlyCostUsd: 900,
      potentialSaveUsd: 400,
      requestedCpuM: 6000,
      requestedMem: 12 << 30,
      usedCpuM: 900,
      usedMem: 3 << 30,
      unrequestedPods: 1,
    },
    {
      namespace: 'data',
      pods: 3,
      monthlyCostUsd: 334.5,
      potentialSaveUsd: 56.78,
      requestedCpuM: 2000,
      requestedMem: 4 << 30,
      usedCpuM: 1500,
      usedMem: 3 << 30,
      unrequestedPods: 0,
    },
  ],
  top: [
    {
      kind: 'Deployment',
      namespace: 'shop',
      name: 'api',
      resourceKey: 'apps/v1/deployments',
      pods: 6,
      monthlyCostUsd: 500,
      potentialSaveUsd: 300,
      requestedCpuM: 3000,
      requestedMem: 6 << 30,
      usedCpuM: 300,
      usedMem: 1 << 30,
    },
  ],
}

describe('FinOpsCard', () => {
  it('summarises cost, savings and confidence, with per-namespace rows', () => {
    render(<FinOpsCard finops={base} clusterId="c1" />)
    expect(screen.getByText('$1,235')).toBeTruthy()
    expect(screen.getByText('$457')).toBeTruthy()
    expect(screen.getByText('High confidence')).toBeTruthy()
    expect(screen.getByText('shop')).toBeTruthy()
    expect(screen.getByText(/1 without requests/)).toBeTruthy()
    expect(screen.getByRole('button', { name: /Right-size/ })).toBeTruthy()
  })

  it('does not pretend to know savings without usage data', () => {
    render(
      <FinOpsCard
        finops={{ ...base, confidence: 'none', source: '', top: [], potentialSaveUsd: 0 }}
        clusterId="c1"
      />,
    )
    expect(screen.getByText('No usage data')).toBeTruthy()
    expect(screen.getByText('needs usage data')).toBeTruthy()
    expect(screen.queryByText(/Biggest savings/)).toBeNull()
  })

  it('explains the calculation on demand', () => {
    render(<FinOpsCard finops={base} clusterId="c1" />)
    expect(screen.queryByText(/configured rates/)).toBeNull()
    fireEvent.click(screen.getByText('How is this calculated?'))
    expect(screen.getByText(/configured rates/)).toBeTruthy()
    expect(screen.getByText(/20% headroom/)).toBeTruthy()
  })
})
