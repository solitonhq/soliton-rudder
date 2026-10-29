import { useRef, useState } from 'react'

export interface LineSeries {
  label: string
  color: string
  points: [number, number][] // [unix seconds, value]
}

const W = 760
const H = 240
const P = { l: 52, r: 10, t: 10, b: 22 }

export const PALETTE = [
  '#5b8def',
  '#3fb97f',
  '#e0a63a',
  '#a77bf3',
  '#2fc4c4',
  '#ef7bb0',
  '#e5584f',
  '#8b95a5',
]

/** Compact SI formatting for arbitrary PromQL values (1.2k, 3.4M, 5.6G). */
export function formatSI(v: number): string {
  const a = Math.abs(v)
  if (a === 0) return '0'
  if (a >= 1e12) return (v / 1e12).toFixed(1) + 'T'
  if (a >= 1e9) return (v / 1e9).toFixed(1) + 'G'
  if (a >= 1e6) return (v / 1e6).toFixed(1) + 'M'
  if (a >= 1e4) return (v / 1e3).toFixed(1) + 'k'
  if (a >= 100) return v.toFixed(0)
  if (a >= 1) return v.toFixed(2).replace(/\.?0+$/, '')
  return v.toPrecision(2)
}

export function MultiChart({
  series,
  format = formatSI,
}: {
  series: LineSeries[]
  format?: (v: number) => string
}) {
  const ref = useRef<SVGSVGElement>(null)
  const [hover, setHover] = useState<number | null>(null) // unix seconds

  const all = series.flatMap((s) => s.points)
  if (all.length === 0)
    return <div className="grid h-40 place-items-center text-[13px] text-muted">No data points</div>
  const t0 = Math.min(...all.map((p) => p[0]))
  const t1 = Math.max(...all.map((p) => p[0]))
  const vMax = Math.max(...all.map((p) => p[1]), 0)
  const vMin = Math.min(...all.map((p) => p[1]), 0)
  const span = vMax - vMin || 1
  const hi = vMax + span * 0.08
  const x = (t: number) => P.l + (t1 === t0 ? 0.5 : (t - t0) / (t1 - t0)) * (W - P.l - P.r)
  const y = (v: number) => P.t + (1 - (v - vMin) / (hi - vMin)) * (H - P.t - P.b)
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => vMin + f * (hi - vMin))
  const times = [0, 0.25, 0.5, 0.75, 1].map((f) => t0 + f * (t1 - t0))
  const fmtT = (t: number) =>
    new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

  const onMove = (e: React.PointerEvent) => {
    const r = ref.current?.getBoundingClientRect()
    if (!r) return
    const px = ((e.clientX - r.left) / r.width) * W
    const t = t0 + ((px - P.l) / (W - P.l - P.r)) * (t1 - t0)
    // snap to the nearest sample time of the first series with data
    let best = t
    let bd = Infinity
    for (const p of all) {
      const d = Math.abs(p[0] - t)
      if (d < bd) {
        bd = d
        best = p[0]
      }
    }
    setHover(best)
  }

  const valueAt = (s: LineSeries, t: number) => {
    let best: [number, number] | undefined
    let bd = Infinity
    for (const p of s.points) {
      const d = Math.abs(p[0] - t)
      if (d < bd) {
        bd = d
        best = p
      }
    }
    return bd <= (t1 - t0) / 20 + 1 ? best : undefined
  }

  return (
    <div>
      <svg
        ref={ref}
        viewBox={`0 0 ${W} ${H}`}
        className="block w-full touch-none"
        role="img"
        aria-label="Time series chart"
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        {ticks.map((v, i) => (
          <g key={i}>
            <line
              x1={P.l}
              x2={W - P.r}
              y1={y(v)}
              y2={y(v)}
              stroke="var(--border)"
              strokeDasharray="2 4"
            />
            <text x={P.l - 6} y={y(v) + 3} textAnchor="end" fontSize="10" fill="var(--fg-muted)">
              {format(v)}
            </text>
          </g>
        ))}
        {times.map((t, i) => (
          <text
            key={i}
            x={x(t)}
            y={H - 6}
            textAnchor={i === 0 ? 'start' : i === 4 ? 'end' : 'middle'}
            fontSize="10"
            fill="var(--fg-muted)"
          >
            {fmtT(t)}
          </text>
        ))}
        {series.map((s) => (
          <path
            key={s.label}
            d={s.points
              .map((p, i) => `${i ? 'L' : 'M'}${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`)
              .join(' ')}
            fill="none"
            stroke={s.color}
            strokeWidth="1.6"
            strokeLinejoin="round"
          />
        ))}
        {hover !== null && (
          <line x1={x(hover)} x2={x(hover)} y1={P.t} y2={H - P.b} stroke="var(--fg-muted)" />
        )}
        {hover !== null &&
          series.map((s) => {
            const p = valueAt(s, hover)
            return p ? (
              <circle
                key={s.label}
                cx={x(p[0])}
                cy={y(p[1])}
                r="3"
                fill={s.color}
                stroke="var(--bg)"
                strokeWidth="1.2"
              />
            ) : null
          })}
      </svg>
      {hover !== null && (
        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-xs">
          <span className="text-muted">{new Date(hover * 1000).toLocaleString()}</span>
          {series.slice(0, 8).map((s) => {
            const p = valueAt(s, hover)
            return p ? (
              <span key={s.label} className="flex items-center gap-1">
                <span
                  className="inline-block size-2 rounded-full"
                  style={{ background: s.color }}
                />
                {s.label.slice(0, 40)} <span className="font-mono">{format(p[1])}</span>
              </span>
            ) : null
          })}
        </div>
      )}
    </div>
  )
}
