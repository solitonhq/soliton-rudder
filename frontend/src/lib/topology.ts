import type { TopoEdge, TopoNode } from './types'

export const NODE_W = 210
export const NODE_H = 46
const COL_GAP = 110
const ROW_GAP = 14
const PAD = 24

export interface Placed {
  node: TopoNode
  x: number
  y: number
}

export interface Layout {
  placed: Map<string, Placed>
  width: number
  height: number
  columns: number[] // tiers that are present, left to right
}

const KIND_ORDER: Record<string, number> = {
  Node: 0,
  ConfigMap: 1,
  Secret: 2,
  PersistentVolumeClaim: 3,
}

/**
 * Column per tier, rows ordered by the barycentre of their neighbours so edges cross as little as
 * possible (a few sweeps are enough for the sizes we draw). Deterministic: ties break on id.
 */
export function layoutTopology(nodes: TopoNode[], edges: TopoEdge[]): Layout {
  const tiers = [...new Set(nodes.map((n) => n.tier))].sort((a, b) => a - b)
  const cols = new Map<number, TopoNode[]>()
  for (const t of tiers) cols.set(t, [])
  for (const n of nodes) cols.get(n.tier)!.push(n)
  const rank = (n: TopoNode) => (n.tier === 4 ? (KIND_ORDER[n.kind] ?? 9) : 0)
  for (const list of cols.values())
    list.sort((a, b) => rank(a) - rank(b) || a.id.localeCompare(b.id))

  const adj = new Map<string, string[]>()
  for (const e of edges) {
    if (!adj.has(e.from)) adj.set(e.from, [])
    if (!adj.has(e.to)) adj.set(e.to, [])
    adj.get(e.from)!.push(e.to)
    adj.get(e.to)!.push(e.from)
  }
  const tierOf = new Map(nodes.map((n) => [n.id, n.tier]))
  const pos = new Map<string, number>()
  const index = () => {
    for (const list of cols.values()) list.forEach((n, i) => pos.set(n.id, i))
  }
  index()
  const sweep = (order: number[]) => {
    for (const t of order) {
      const list = cols.get(t)!
      const key = new Map<string, number>()
      list.forEach((n, i) => {
        const ps = (adj.get(n.id) ?? []).filter((m) => tierOf.get(m) !== t)
        key.set(n.id, ps.length ? ps.reduce((s, m) => s + pos.get(m)!, 0) / ps.length : i)
      })
      list.sort(
        (a, b) => rank(a) - rank(b) || key.get(a.id)! - key.get(b.id)! || a.id.localeCompare(b.id),
      )
      list.forEach((n, i) => pos.set(n.id, i))
    }
  }
  for (let i = 0; i < 3; i++) {
    sweep(tiers.slice(1))
    sweep([...tiers].reverse().slice(1))
  }

  const placed = new Map<string, Placed>()
  let height = 0
  tiers.forEach((t, ci) => {
    cols.get(t)!.forEach((n, ri) => {
      const x = PAD + ci * (NODE_W + COL_GAP)
      const y = PAD + ri * (NODE_H + ROW_GAP)
      placed.set(n.id, { node: n, x, y })
      height = Math.max(height, y + NODE_H + PAD)
    })
  })
  return {
    placed,
    width: PAD * 2 + tiers.length * NODE_W + Math.max(0, tiers.length - 1) * COL_GAP,
    height: Math.max(height, PAD * 2),
    columns: tiers,
  }
}

/** Every node reachable from id following edges downstream and upstream (the "blast path"). */
export function relatedTo(id: string, edges: TopoEdge[]): Set<string> {
  const out = new Map<string, string[]>()
  const inc = new Map<string, string[]>()
  for (const e of edges) {
    if (!out.has(e.from)) out.set(e.from, [])
    out.get(e.from)!.push(e.to)
    if (!inc.has(e.to)) inc.set(e.to, [])
    inc.get(e.to)!.push(e.from)
  }
  const seen = new Set([id])
  const walk = (start: string, g: Map<string, string[]>) => {
    const stack = [start]
    while (stack.length) {
      for (const n of g.get(stack.pop()!) ?? [])
        if (!seen.has(n)) {
          seen.add(n)
          stack.push(n)
        }
    }
  }
  walk(id, out)
  walk(id, inc)
  return seen
}

/** Nodes on a path that ends in a failing node: everything that feeds or depends on a problem. */
export function problemSet(nodes: TopoNode[], edges: TopoEdge[]): Set<string> {
  const out = new Set<string>()
  for (const n of nodes)
    if (n.status === 'error') for (const r of relatedTo(n.id, edges)) out.add(r)
  return out
}

/** Smooth horizontal Bézier between the right side of a and the left side of b. */
export function edgePath(a: Placed, b: Placed): string {
  const leftToRight = b.x >= a.x
  const x1 = leftToRight ? a.x + NODE_W : a.x
  const x2 = leftToRight ? b.x : b.x + NODE_W
  const y1 = a.y + NODE_H / 2
  const y2 = b.y + NODE_H / 2
  const dx = Math.max(40, Math.abs(x2 - x1) / 2)
  const s = leftToRight ? 1 : -1
  return `M${x1},${y1} C${x1 + s * dx},${y1} ${x2 - s * dx},${y2} ${x2},${y2}`
}
