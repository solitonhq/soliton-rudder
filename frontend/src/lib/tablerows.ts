import { cellText } from './format'
import type { Row, Usage } from './types'

export interface SortSpec {
  col: string
  dir: 'asc' | 'desc'
}

/** The part of a display column the row pipeline needs. index: -1 namespace, -2 age, -3 cpu, -4 memory. */
export interface ColRef {
  id: string
  index: number
}

export interface RowOptions {
  namespaced: boolean
  namespaces: string[]
  search: string
  sort: SortSpec | null
  cols: ColRef[]
  usage: Record<string, Usage> | null
  isNode: boolean
}

// Intl.Collator is an order of magnitude faster than String.localeCompare(…, options) on big lists.
const natural = new Intl.Collator(undefined, { numeric: true })
const plain = new Intl.Collator()

// Rows are replaced (not mutated) on every update, so a WeakMap keyed by row never goes stale.
const searchCache = new WeakMap<Row, string>()

function searchText(r: Row): string {
  let t = searchCache.get(r)
  if (t === undefined) {
    t = (r.name + '\u0000' + r.cells.map(cellText).join('\u0000')).toLowerCase()
    searchCache.set(r, t)
  }
  return t
}

export const usageKey = (r: Row, isNode: boolean) => (isNode ? r.name : `${r.ns}/${r.name}`)

function sortKey(r: Row, c: ColRef, o: RowOptions): string | number {
  if (c.index === -3) return o.usage?.[usageKey(r, o.isNode)]?.cpu ?? -1
  if (c.index === -4) return o.usage?.[usageKey(r, o.isNode)]?.mem ?? -1
  if (c.index === -1) return r.ns
  if (c.index === -2) return Date.parse(r.created) || 0
  const v = r.cells[c.index]
  return typeof v === 'number' ? v : cellText(v).toLowerCase()
}

/** Filters (namespace + text) and sorts the rows shown in the table. */
export function computeRows(source: Map<string, Row>, o: RowOptions): Row[] {
  const q = o.search.trim().toLowerCase()
  const nsSet = o.namespaces.length && o.namespaced ? new Set(o.namespaces) : null
  const list: Row[] = []
  for (const r of source.values()) {
    if (nsSet && !nsSet.has(r.ns)) continue
    if (q && !searchText(r).includes(q)) continue
    list.push(r)
  }

  const sc = o.sort ? o.cols.find((c) => c.id === o.sort!.col) : undefined
  if (sc && o.sort) {
    const dir = o.sort.dir === 'asc' ? 1 : -1
    const keys = new Map<Row, string | number>()
    for (const r of list) keys.set(r, sortKey(r, sc, o)) // compute once per row, not once per comparison
    list.sort((a, b) => {
      const x = keys.get(a)!
      const y = keys.get(b)!
      const c =
        typeof x === 'number' && typeof y === 'number'
          ? x - y
          : natural.compare(String(x), String(y))
      return c * dir || plain.compare(a.name, b.name)
    })
  } else {
    list.sort((a, b) => plain.compare(a.ns, b.ns) || plain.compare(a.name, b.name))
  }
  return list
}

/** Next selected index for arrow/Home/End/Page keys in a list of `count` rows (-1 = nothing selected). */
export function moveSelection(current: number, count: number, key: string, page = 10): number {
  if (count <= 0) return -1
  switch (key) {
    case 'ArrowDown':
      return current < 0 ? 0 : Math.min(current + 1, count - 1)
    case 'ArrowUp':
      return current < 0 ? count - 1 : Math.max(current - 1, 0)
    case 'PageDown':
      return Math.min(Math.max(current, 0) + page, count - 1)
    case 'PageUp':
      return Math.max(Math.max(current, 0) - page, 0)
    case 'Home':
      return 0
    case 'End':
      return count - 1
  }
  return current
}
