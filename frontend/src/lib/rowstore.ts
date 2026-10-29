import { onEvent } from './backend'
import type { Column, Row, RowsEvent, StatusEvent } from './types'

type Listener = () => void

/**
 * Holds one live table's rows outside React state (Map keyed by uid) so that
 * thousands of rows can change without re-allocating arrays; React subscribes
 * through `version`, notified at most once per animation frame.
 */
export class RowStore {
  rows = new Map<string, Row>()
  columns: Column[] = []
  status: StatusEvent['state'] = 'connecting'
  message = ''
  version = 0
  private listeners = new Set<Listener>()
  private scheduled = false

  apply(ev: RowsEvent) {
    if (ev.reset) {
      if (ev.scope) {
        // one namespace of a table fed by several: replace only its rows, never the others'
        for (const [uid, r] of this.rows) if (r.ns === ev.scope) this.rows.delete(uid)
        this.columns = ev.columns ?? this.columns
      } else {
        this.rows = new Map()
        this.columns = ev.columns ?? this.columns
      }
    }
    for (const r of ev.upsert ?? []) this.rows.set(r.uid, r)
    for (const uid of ev.delete ?? []) this.rows.delete(uid)
    this.bump()
  }

  setStatus(s: StatusEvent) {
    this.status = s.state
    this.message = s.message
    this.bump()
  }

  subscribe = (l: Listener) => {
    this.listeners.add(l)
    return () => {
      this.listeners.delete(l)
    }
  }

  getVersion = () => this.version

  private bump() {
    this.version++
    if (this.scheduled) return
    this.scheduled = true
    const run = () => {
      this.scheduled = false
      this.listeners.forEach((l) => l())
    }
    if (typeof requestAnimationFrame === 'function') requestAnimationFrame(run)
    else setTimeout(run, 16)
  }
}

/** Routes backend "rows"/"rowstatus" events to the RowStore registered for a subscription id. */
class Router {
  private stores = new Map<string, RowStore>()
  // Events can arrive before the subscribe() call resolves with its id.
  private early = new Map<string, { rows: RowsEvent[]; status: StatusEvent[] }>()
  private started = false

  private start() {
    if (this.started) return
    this.started = true
    onEvent<RowsEvent>('rows', (ev) => {
      const s = this.stores.get(ev.sub)
      if (s) s.apply(ev)
      else this.buffer(ev.sub).rows.push(ev)
    })
    onEvent<StatusEvent>('rowstatus', (ev) => {
      const s = this.stores.get(ev.sub)
      if (s) s.setStatus(ev)
      else this.buffer(ev.sub).status.push(ev)
    })
  }

  private buffer(sub: string) {
    let b = this.early.get(sub)
    if (!b) {
      b = { rows: [], status: [] }
      this.early.set(sub, b)
      if (this.early.size > 32) this.early.delete(this.early.keys().next().value!)
    }
    return b
  }

  register(sub: string, store: RowStore) {
    this.start()
    this.stores.set(sub, store)
    const b = this.early.get(sub)
    if (b) {
      this.early.delete(sub)
      b.status.forEach((e) => store.setStatus(e))
      b.rows.forEach((e) => store.apply(e))
    }
  }

  unregister(sub: string) {
    this.stores.delete(sub)
    this.early.delete(sub)
  }
}

export const router = new Router()
