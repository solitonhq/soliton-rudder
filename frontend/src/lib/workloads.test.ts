import { describe, expect, it } from 'vitest'
import { desiredReplicas, RESTARTABLE, SCALABLE } from './workloads'

describe('workload kinds', () => {
  it('knows what can be scaled and what can be rolled', () => {
    expect(SCALABLE.has('deployments')).toBe(true)
    expect(SCALABLE.has('daemonsets')).toBe(false) // no replica count
    expect(RESTARTABLE.has('daemonsets')).toBe(true)
    expect(RESTARTABLE.has('replicasets')).toBe(false)
  })
})

describe('desiredReplicas', () => {
  it('reads the desired count from a ready/desired cell', () => {
    expect(desiredReplicas(['web', '2/3', 3, '5d'])).toBe(3)
    expect(desiredReplicas(['0/0'])).toBe(0)
  })
  it('returns null when no cell has that shape', () => {
    expect(desiredReplicas(['rs', 2, 2, 2])).toBeNull()
    expect(desiredReplicas(['1/x', 'abc'])).toBeNull()
    expect(desiredReplicas([])).toBeNull()
  })
})
