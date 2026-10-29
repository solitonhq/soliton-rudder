import { onEvent } from './backend'

interface OutputEvent {
  id: string
  data: string // base64
}
interface ExitEvent {
  id: string
  error: string
}

export interface TermSink {
  output: (bytes: Uint8Array) => void
  exit: (error: string) => void
}

export function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

/** Routes terminal events to sinks; buffers events that arrive before the id is registered. */
class TermRouter {
  private sinks = new Map<string, TermSink>()
  private early = new Map<string, { out: Uint8Array[]; exit?: string }>()
  private started = false

  private start() {
    if (this.started) return
    this.started = true
    onEvent<OutputEvent>('term', (e) => {
      const s = this.sinks.get(e.id)
      const bytes = b64ToBytes(e.data)
      if (s) s.output(bytes)
      else this.buf(e.id).out.push(bytes)
    })
    onEvent<ExitEvent>('termexit', (e) => {
      const s = this.sinks.get(e.id)
      if (s) s.exit(e.error)
      else this.buf(e.id).exit = e.error
    })
  }

  private buf(id: string) {
    let b = this.early.get(id)
    if (!b) {
      b = { out: [] }
      this.early.set(id, b)
      if (this.early.size > 16) this.early.delete(this.early.keys().next().value!)
    }
    return b
  }

  register(id: string, sink: TermSink) {
    this.start()
    this.sinks.set(id, sink)
    const b = this.early.get(id)
    if (b) {
      this.early.delete(id)
      b.out.forEach((o) => sink.output(o))
      if (b.exit !== undefined) sink.exit(b.exit)
    }
  }

  unregister(id: string) {
    this.sinks.delete(id)
    this.early.delete(id)
  }
}

export const termRouter = new TermRouter()
