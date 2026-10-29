import { describe, expect, it } from 'vitest'
import type { Row } from './types'
import { computeRows, moveSelection, type RowOptions } from './tablerows'

const mk = (i: number, ns = `ns${i % 20}`): Row => ({
  uid: `u${i}`,
  ns,
  name: `pod-${i}`,
  created: new Date(1_700_000_000_000 + i * 1000).toISOString(),
  cells: [
    `pod-${i}`,
    `${i % 3}/3`,
    i % 7 === 0 ? 'CrashLoopBackOff' : 'Running',
    i % 50,
    `10.0.${i % 255}.${i % 200}`,
  ],
})
const map = (n: number) => new Map(Array.from({ length: n }, (_, i) => [`u${i}`, mk(i)] as const))
const base: RowOptions = {
  namespaced: true,
  namespaces: [],
  search: '',
  sort: null,
  cols: [
    { id: 'c0', index: 0 },
    { id: 'c3', index: 3 },
    { id: 'ns', index: -1 },
  ],
  usage: null,
  isNode: false,
}

describe('computeRows', () => {
  it('sorts by namespace then name by default', () => {
    const r = computeRows(map(60), base)
    expect(r[0].ns <= r[1].ns).toBe(true)
    expect(new Set(r.map((x) => x.uid)).size).toBe(60)
  })
  it('filters by namespace and search, ignoring case', () => {
    const rows = map(100)
    expect(computeRows(rows, { ...base, namespaces: ['ns3'] }).every((r) => r.ns === 'ns3')).toBe(
      true,
    )
    const hits = computeRows(rows, { ...base, search: 'CRASHLOOP' })
    expect(hits.length).toBeGreaterThan(0)
    expect(hits.every((r) => r.cells[2] === 'CrashLoopBackOff')).toBe(true)
    expect(computeRows(rows, { ...base, search: 'zzz-nothing' })).toHaveLength(0)
  })
  it('ignores namespace filter for cluster-scoped kinds', () => {
    expect(computeRows(map(10), { ...base, namespaced: false, namespaces: ['nope'] })).toHaveLength(
      10,
    )
  })
  it('sorts numerically and naturally, both directions', () => {
    const rows = map(30)
    const asc = computeRows(rows, { ...base, sort: { col: 'c3', dir: 'asc' } })
    expect(asc.map((r) => r.cells[3] as number)).toEqual(
      [...asc.map((r) => r.cells[3] as number)].sort((a, b) => a - b),
    )
    const byName = computeRows(rows, { ...base, sort: { col: 'c0', dir: 'asc' } })
    expect(byName[1].name).toBe('pod-1') // natural order: pod-2 < pod-10
    expect(byName[2].name).toBe('pod-2')
    expect(computeRows(rows, { ...base, sort: { col: 'c0', dir: 'desc' } })[0].name).toBe('pod-29')
  })
  it('sorts by usage columns', () => {
    const rows = map(5)
    const usage = Object.fromEntries(
      [...rows.values()].map((r, i) => [`${r.ns}/${r.name}`, { cpu: 100 - i * 10, mem: i }]),
    )
    const out = computeRows(rows, {
      ...base,
      usage,
      cols: [{ id: 'cpu', index: -3 }],
      sort: { col: 'cpu', dir: 'asc' },
    })
    expect(out[0].name).toBe('pod-4')
  })

  it('stays fast on a 20,000-row cluster', () => {
    const rows = map(20_000)
    const t0 = performance.now()
    computeRows(rows, { ...base, sort: { col: 'c0', dir: 'asc' } })
    const sortMs = performance.now() - t0
    const t1 = performance.now()
    computeRows(rows, { ...base, search: 'crashloop' })
    const firstSearchMs = performance.now() - t1
    const t2 = performance.now()
    computeRows(rows, { ...base, search: 'crashloopb' }) // typing another letter: search text is cached
    const nextSearchMs = performance.now() - t2
    console.info(
      `20k rows: sort ${sortMs.toFixed(0)}ms, first search ${firstSearchMs.toFixed(0)}ms, next search ${nextSearchMs.toFixed(0)}ms`,
    )
    expect(sortMs).toBeLessThan(1500) // generous: guards against accidental O(n²), not CI jitter
    expect(nextSearchMs).toBeLessThan(500)
  })
})

describe('moveSelection', () => {
  it('moves within bounds', () => {
    expect(moveSelection(-1, 5, 'ArrowDown')).toBe(0)
    expect(moveSelection(-1, 5, 'ArrowUp')).toBe(4)
    expect(moveSelection(4, 5, 'ArrowDown')).toBe(4)
    expect(moveSelection(0, 5, 'ArrowUp')).toBe(0)
    expect(moveSelection(2, 100, 'PageDown')).toBe(12)
    expect(moveSelection(2, 100, 'PageUp')).toBe(0)
    expect(moveSelection(2, 100, 'End')).toBe(99)
    expect(moveSelection(50, 100, 'Home')).toBe(0)
    expect(moveSelection(3, 0, 'ArrowDown')).toBe(-1)
    expect(moveSelection(3, 5, 'x')).toBe(3)
  })
})
