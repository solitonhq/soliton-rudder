import { onEvent } from './backend'
import type { LogBatch, LogEnd } from './types'

export interface LogEntry {
  id: number
  pod: string
  container: string
  ts: string
  text: string
}

export const MAX_LOG_LINES = 50_000

/** Ring buffer of log lines kept outside React; notifies at most once per frame. */
export class LogStore {
  lines: LogEntry[] = []
  version = 0
  private nextId = 1
  private listeners = new Set<() => void>()
  private scheduled = false

  push(b: Pick<LogBatch, 'pod' | 'container' | 'lines'>) {
    for (const l of b.lines) {
      this.lines.push({
        id: this.nextId++,
        pod: b.pod,
        container: b.container,
        ts: l.ts ?? '',
        text: l.text,
      })
    }
    if (this.lines.length > MAX_LOG_LINES) this.lines.splice(0, this.lines.length - MAX_LOG_LINES)
    this.bump()
  }

  clear() {
    this.lines = []
    this.bump()
  }

  subscribe = (l: () => void) => {
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

interface Sink {
  batch: (b: LogBatch) => void
  end: (e: LogEnd) => void
}

class LogRouter {
  private sinks = new Map<string, Sink>()
  private early = new Map<string, { batches: LogBatch[]; end?: LogEnd }>()
  private started = false

  private start() {
    if (this.started) return
    this.started = true
    onEvent<LogBatch>('log', (b) => {
      const s = this.sinks.get(b.sub)
      if (s) s.batch(b)
      else this.buf(b.sub).batches.push(b)
    })
    onEvent<LogEnd>('logend', (e) => {
      const s = this.sinks.get(e.sub)
      if (s) s.end(e)
      else this.buf(e.sub).end = e
    })
  }

  private buf(sub: string) {
    let b = this.early.get(sub)
    if (!b) {
      b = { batches: [] }
      this.early.set(sub, b)
      if (this.early.size > 64) this.early.delete(this.early.keys().next().value!)
    }
    return b
  }

  register(sub: string, sink: Sink) {
    this.start()
    this.sinks.set(sub, sink)
    const b = this.early.get(sub)
    if (b) {
      this.early.delete(sub)
      b.batches.forEach((x) => sink.batch(x))
      if (b.end) sink.end(b.end)
    }
  }

  unregister(sub: string) {
    this.sinks.delete(sub)
    this.early.delete(sub)
  }
}

export const logRouter = new LogRouter()

export type LogLevel = 'error' | 'warn' | 'info' | 'debug' | 'none'

const LEVEL_FIELDS = ['level', 'lvl', 'severity', 'log.level']

/** Detects level from JSON logs ({"level":"warn"}) or common text markers. */
export function detectLevel(text: string): LogLevel {
  if (text.charCodeAt(0) === 123 /* { */) {
    try {
      const o = JSON.parse(text) as Record<string, unknown>
      for (const f of LEVEL_FIELDS) {
        const v = o[f]
        if (typeof v === 'string') return normalizeLevel(v)
      }
    } catch {
      /* fall through to text detection */
    }
  }
  const m =
    /(?:^|[\s[|:=,"])(fatal|panic|critical|crit|error|err|warning|warn|info|debug|dbg|trace)(?=$|[\s\]|:=,"])/i.exec(
      text.slice(0, 200),
    )
  return m ? normalizeLevel(m[1]) : 'none'
}

function normalizeLevel(v: string): LogLevel {
  const s = v.toLowerCase()
  if (/^(fatal|panic|critical|crit|error|err|e)$/.test(s)) return 'error'
  if (/^(warning|warn|w)$/.test(s)) return 'warn'
  if (/^(info|notice|i)$/.test(s)) return 'info'
  if (/^(debug|dbg|trace|d|t)$/.test(s)) return 'debug'
  return 'none'
}

/** Renders a JSON log line as "message  key=value …" for readability; returns null if not JSON. */
export function prettyJSON(text: string): { msg: string; fields: [string, string][] } | null {
  if (text.charCodeAt(0) !== 123) return null
  try {
    const o = JSON.parse(text) as Record<string, unknown>
    if (typeof o !== 'object' || o === null || Array.isArray(o)) return null
    const msgKey = ['msg', 'message', 'log'].find((k) => typeof o[k] === 'string')
    const skip = new Set([msgKey, ...LEVEL_FIELDS, 'time', 'ts', 'timestamp', '@timestamp'])
    const fields: [string, string][] = []
    for (const [k, v] of Object.entries(o)) {
      if (skip.has(k)) continue
      fields.push([k, typeof v === 'string' ? v : JSON.stringify(v)])
    }
    return { msg: msgKey ? String(o[msgKey]) : '', fields }
  } catch {
    return null
  }
}
