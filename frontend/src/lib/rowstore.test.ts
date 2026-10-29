import { describe, expect, it } from 'vitest'
import { RowStore } from './rowstore'
import type { Row } from './types'

const row = (uid: string, ns: string): Row => ({ uid, ns, name: uid, created: '', cells: [uid] })

describe('RowStore', () => {
  it('a snapshot without a scope replaces everything', () => {
    const s = new RowStore()
    s.apply({ sub: 'x', reset: true, upsert: [row('a', 'one'), row('b', 'two')], delete: [] })
    s.apply({ sub: 'x', reset: true, upsert: [row('c', 'one')], delete: [] })
    expect([...s.rows.keys()]).toEqual(['c'])
  })

  it("a scoped snapshot replaces only its namespace's rows, so namespaces feeding one table do not wipe each other", () => {
    const s = new RowStore()
    s.apply({
      sub: 'x',
      reset: true,
      scope: 'one',
      columns: [{ name: 'Name', type: 'string', priority: 0, description: '' }],
      upsert: [row('a', 'one'), row('b', 'one')],
      delete: [],
    })
    s.apply({ sub: 'x', reset: true, scope: 'two', upsert: [row('c', 'two')], delete: [] })
    expect([...s.rows.keys()].sort()).toEqual(['a', 'b', 'c'])
    expect(s.columns.length).toBe(1) // the columns survive a snapshot that does not repeat them
    // namespace "one" relists: b is gone, d is new; namespace "two" is untouched
    s.apply({
      sub: 'x',
      reset: true,
      scope: 'one',
      upsert: [row('a', 'one'), row('d', 'one')],
      delete: [],
    })
    expect([...s.rows.keys()].sort()).toEqual(['a', 'c', 'd'])
  })
})
