import { describe, expect, it } from 'vitest'
import { edgePath, layoutTopology, NODE_H, problemSet, relatedTo } from './topology'
import type { TopoEdge, TopoNode } from './types'

const n = (
  id: string,
  tier: number,
  status: TopoNode['status'] = 'ok',
  kind = 'Pod',
): TopoNode => ({
  id,
  kind,
  name: id,
  namespace: 'ns',
  tier,
  status,
  detail: '',
  resourceKey: '',
  missing: false,
})
const e = (from: string, to: string): TopoEdge => ({ from, to, kind: 'selects' })

const nodes = [
  n('i', 0),
  n('s1', 1),
  n('s2', 1),
  n('w1', 2),
  n('w2', 2, 'error'),
  n('p1', 3),
  n('p2', 3),
  n('x', 3),
]
const edges = [
  e('i', 's1'),
  e('i', 's2'),
  e('s1', 'w1'),
  e('s2', 'w2'),
  e('w1', 'p1'),
  e('w2', 'p2'),
]

describe('layoutTopology', () => {
  it('puts each tier in its own column, left to right, without overlaps', () => {
    const l = layoutTopology(nodes, edges)
    expect(l.columns).toEqual([0, 1, 2, 3])
    const xs = (t: number) =>
      [...l.placed.values()].filter((p) => p.node.tier === t).map((p) => p.x)
    expect(new Set(xs(1)).size).toBe(1)
    expect(xs(0)[0]).toBeLessThan(xs(1)[0])
    const col = [...l.placed.values()]
      .filter((p) => p.node.tier === 3)
      .map((p) => p.y)
      .sort((a, b) => a - b)
    for (let i = 1; i < col.length; i++) expect(col[i] - col[i - 1]).toBeGreaterThanOrEqual(NODE_H)
  })

  it('orders rows to keep connected nodes level (no crossing for a simple fork)', () => {
    const l = layoutTopology(nodes, edges)
    const y = (id: string) => l.placed.get(id)!.y
    expect(y('s1') < y('s2')).toBe(y('w1') < y('w2'))
    expect(y('w1') < y('w2')).toBe(y('p1') < y('p2'))
  })

  it('is deterministic', () => {
    const a = layoutTopology(nodes, edges)
    const b = layoutTopology([...nodes].reverse(), [...edges].reverse())
    for (const [id, p] of a.placed)
      expect([b.placed.get(id)!.x, b.placed.get(id)!.y]).toEqual([p.x, p.y])
  })

  it('handles an empty graph', () => {
    const l = layoutTopology([], [])
    expect(l.placed.size).toBe(0)
    expect(l.width).toBeGreaterThan(0)
  })
})

describe('relatedTo / problemSet', () => {
  it('follows edges both ways but not sideways into siblings', () => {
    const r = relatedTo('w1', edges)
    expect([...r].sort()).toEqual(['i', 'p1', 's1', 'w1'])
  })
  it('selects only paths through failing nodes', () => {
    const p = problemSet(nodes, edges)
    expect([...p].sort()).toEqual(['i', 'p2', 's2', 'w2'])
    expect(p.has('x')).toBe(false)
  })
})

describe('edgePath', () => {
  it('starts at the right edge of the source and ends at the left edge of the target', () => {
    const l = layoutTopology(nodes, edges)
    const d = edgePath(l.placed.get('i')!, l.placed.get('s1')!)
    expect(d.startsWith(`M${l.placed.get('i')!.x + 210},`)).toBe(true)
    expect(d.endsWith(`${l.placed.get('s1')!.x},${l.placed.get('s1')!.y + NODE_H / 2}`)).toBe(true)
  })
})
