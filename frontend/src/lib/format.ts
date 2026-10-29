/** Compact kubectl-style age: 45s, 12m, 3h, 5d, 2y. */
export function formatAge(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, Math.floor((now - t) / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 48) return m % 60 && h < 10 ? `${h}h${m % 60}m` : `${h}h`
  const d = Math.floor(h / 24)
  if (d < 365) return `${d}d`
  return `${Math.floor(d / 365)}y${Math.floor((d % 365) / 30)}mo`
}

export type Tone = 'success' | 'warning' | 'danger' | 'neutral'

const OK =
  /^(running|active|bound|ready|true|complete|completed|succeeded|available|healthy|established|approved|normal|bound|resolved)$/i
const WARN =
  /^(pending|containercreating|podinitializing|terminating|init:|unknown|released|progressing|suspended|waiting|notready|false)/i
const BAD =
  /(error|failed|failure|crashloop|backoff|evicted|oomkilled|invalid|lost|degraded|unhealthy|imagepull|errimage|createcontainer|unschedulable)/i

/** Maps a status-like cell to a colour tone. */
export function statusTone(v: string): Tone {
  if (BAD.test(v)) return 'danger'
  if (OK.test(v)) return 'success'
  if (WARN.test(v) || /^init:/i.test(v)) return 'warning'
  return 'neutral'
}

/** "2/3" style ready columns: success when complete, warning otherwise. */
export function readyTone(v: string): Tone {
  const m = /^(\d+)\/(\d+)$/.exec(v)
  if (!m) return 'neutral'
  if (m[2] === '0') return 'neutral'
  return m[1] === m[2] ? 'success' : Number(m[1]) === 0 ? 'danger' : 'warning'
}

export const isStatusColumn = (name: string) => /^(status|phase|state)$/i.test(name)

export function cellText(v: unknown): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return JSON.stringify(v)
}
