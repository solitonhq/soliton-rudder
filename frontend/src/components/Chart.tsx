import { useId, useRef, useState } from 'react'

export interface ChartPoint {
  t: number
  v: number
}

export interface RefLine {
  label: string
  value: number
  color: string
}

const W = 320
const H = 96
const PAD = { l: 4, r: 4, t: 8, b: 4 }

/** Small area/line chart with hover read-out and optional reference lines (requests/limits). */
export function Chart({
  title,
  points,
  format,
  color,
  refs = [],
  capacity,
}: {
  title: string
  points: ChartPoint[]
  format: (v: number) => string
  color: string
  refs?: RefLine[]
  capacity?: number
}) {
  const gid = useId()
  const svgRef = useRef<SVGSVGElement>(null)
  const [hover, setHover] = useState<number | null>(null)

  const dataMax = Math.max(0, ...points.map((p) => p.v))
  const usable = refs.filter((r) => r.value > 0)
  let max = Math.max(dataMax, 1)
  for (const r of usable) if (r.value <= Math.max(dataMax, 1) * 3) max = Math.max(max, r.value)
  if (capacity && capacity > 0) max = Math.max(max, capacity)
  max *= 1.12

  const t0 = points[0]?.t ?? 0
  const t1 = points[points.length - 1]?.t ?? 1
  const x = (t: number) =>
    PAD.l + (t1 === t0 ? (W - PAD.l - PAD.r) / 2 : ((t - t0) / (t1 - t0)) * (W - PAD.l - PAD.r))
  const y = (v: number) => PAD.t + (1 - v / max) * (H - PAD.t - PAD.b)

  const line = points
    .map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`)
    .join(' ')
  const area =
    points.length > 1
      ? `${line} L${x(t1).toFixed(1)},${H - PAD.b} L${x(t0).toFixed(1)},${H - PAD.b} Z`
      : ''
  const last = points[points.length - 1]
  const shown = hover !== null ? points[hover] : last

  const onMove = (e: React.PointerEvent) => {
    const r = svgRef.current?.getBoundingClientRect()
    if (!r || points.length === 0) return
    const px = ((e.clientX - r.left) / r.width) * W
    let best = 0
    let bd = Infinity
    points.forEach((p, i) => {
      const d = Math.abs(x(p.t) - px)
      if (d < bd) {
        bd = d
        best = i
      }
    })
    setHover(best)
  }

  return (
    <div className="rounded-lg border border-border bg-bg p-3">
      <div className="mb-1 flex items-baseline justify-between">
        <span className="text-[11px] font-semibold uppercase tracking-wide text-muted">
          {title}
        </span>
        <span className="font-mono text-sm font-semibold" style={{ color }}>
          {shown ? format(shown.v) : '—'}
        </span>
      </div>
      <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${H}`}
        className="block h-24 w-full touch-none"
        role="img"
        aria-label={`${title} over time${last ? `, now ${format(last.v)}` : ''}`}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        <defs>
          <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.35" />
            <stop offset="100%" stopColor={color} stopOpacity="0.02" />
          </linearGradient>
        </defs>
        {[0.5, 1].map((f) => (
          <line
            key={f}
            x1={PAD.l}
            x2={W - PAD.r}
            y1={y((max / 1.12) * f)}
            y2={y((max / 1.12) * f)}
            stroke="var(--border)"
            strokeWidth="1"
            strokeDasharray="2 4"
          />
        ))}
        {usable.map(
          (r) =>
            r.value <= max && (
              <g key={r.label}>
                <line
                  x1={PAD.l}
                  x2={W - PAD.r}
                  y1={y(r.value)}
                  y2={y(r.value)}
                  stroke={r.color}
                  strokeWidth="1"
                  strokeDasharray="5 3"
                />
              </g>
            ),
        )}
        {area && <path d={area} fill={`url(#${gid})`} />}
        {points.length > 1 && (
          <path d={line} fill="none" stroke={color} strokeWidth="1.8" strokeLinejoin="round" />
        )}
        {points.length === 1 && (
          <circle cx={x(points[0].t)} cy={y(points[0].v)} r="3" fill={color} />
        )}
        {hover !== null && shown && (
          <>
            <line
              x1={x(shown.t)}
              x2={x(shown.t)}
              y1={PAD.t}
              y2={H - PAD.b}
              stroke="var(--fg-muted)"
              strokeWidth="1"
            />
            <circle
              cx={x(shown.t)}
              cy={y(shown.v)}
              r="3.5"
              fill={color}
              stroke="var(--bg)"
              strokeWidth="1.5"
            />
          </>
        )}
      </svg>
      <div className="mt-1 flex items-center justify-between text-[11px] text-muted">
        <span>{shown ? new Date(shown.t).toLocaleTimeString() : ''}</span>
        <span className="flex gap-3">
          {usable.map((r) => (
            <span key={r.label} className="flex items-center gap-1">
              <span
                className="inline-block h-0 w-3 border-t border-dashed"
                style={{ borderColor: r.color }}
              />{' '}
              {r.label} {format(r.value)}
            </span>
          ))}
        </span>
      </div>
    </div>
  )
}
