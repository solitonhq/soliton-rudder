import clsx from 'clsx'
import {
  AlertTriangle,
  BookOpen,
  CircleDot,
  History,
  Pause,
  Play,
  Settings2,
  XCircle,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '../components/ui'
import { RunbookDialog } from '../dialogs/RunbookDialog'
import { RunbooksEditor } from '../dialogs/RunbooksEditor'
import { blackbox } from '../lib/backend'
import type { BBBucket, BBInfo, BBRecord, Incident } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useUI } from '../store/ui'

const MIN = 60_000
const HOUR = 60 * MIN

export const clock = (ms: number) =>
  new Date(ms).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })

export const stamp = (ms: number) => {
  const d = new Date(ms)
  const today = new Date()
  const day =
    d.toDateString() === today.toDateString()
      ? ''
      : d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' '
  return day + clock(ms)
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

function Sev({ s, size = 13 }: { s: string; size?: number }) {
  if (s === 'error') return <XCircle size={size} className="shrink-0 text-danger" />
  if (s === 'warn') return <AlertTriangle size={size} className="shrink-0 text-warning" />
  return <CircleDot size={size} className="shrink-0 text-muted" />
}

export function incidentOf(r: BBRecord): Incident {
  const status = r.kind === 'state' ? r.status : r.severity === 'info' ? 'ok' : r.severity
  return { kind: r.objKind, namespace: r.namespace, name: r.name, reason: r.reason, status }
}

function useNow(ms: number) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(t)
  }, [ms])
  return now
}

function Density({
  buckets,
  start,
  end,
  cursor,
}: {
  buckets: BBBucket[]
  start: number
  end: number
  cursor: number
}) {
  const max = Math.max(1, ...buckets.map((b) => b.error + b.warn))
  const w = 100 / Math.max(1, buckets.length)
  const x = ((cursor - start) / Math.max(1, end - start)) * 100
  return (
    <svg
      viewBox="0 0 100 24"
      preserveAspectRatio="none"
      className="h-10 w-full rounded bg-surface-2"
      role="img"
      aria-label="Problems over time"
    >
      {buckets.map((b, i) => {
        const e = (b.error / max) * 22
        const wn = (b.warn / max) * 22
        return (
          <g key={i}>
            <rect x={i * w} width={w * 0.8} y={24 - e} height={e} fill="var(--color-danger)" />
            <rect
              x={i * w}
              width={w * 0.8}
              y={24 - e - wn}
              height={wn}
              fill="var(--color-warning)"
            />
          </g>
        )
      })}
      <line
        x1={x}
        x2={x}
        y1={0}
        y2={24}
        stroke="var(--color-accent)"
        strokeWidth={0.6}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

function SettingsPanel({
  clusterId,
  info,
  onSaved,
}: {
  clusterId: string
  info: BBInfo
  onSaved: () => void
}) {
  const toast = useUI((s) => s.toast)
  const [interval, setIntervalSec] = useState(info.config.intervalSec || 15)
  const [ns, setNs] = useState(info.config.namespaces.join(', '))
  const [retention, setRetention] = useState(info.retentionHours)
  const [max, setMax] = useState(info.maxRecords)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)

  const save = async (enabled: boolean) => {
    setBusy(true)
    setError('')
    try {
      await blackbox.set(
        clusterId,
        {
          enabled,
          intervalSec: interval,
          namespaces: ns
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
        },
        retention,
        max,
      )
      toast('success', enabled ? 'Recording' : 'Recording stopped')
      onSaved()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const sel =
    'no-drag h-8 cursor-pointer rounded-md border border-border bg-bg px-2 text-[13px] outline-none focus:border-accent'
  return (
    <div className="space-y-3 border-b border-border bg-surface px-4 py-3 text-[13px]">
      <div className="flex flex-wrap items-end gap-4">
        <label className="flex flex-col gap-1">
          <span className="text-[11px] font-semibold uppercase text-muted">Check every</span>
          <select
            aria-label="Interval"
            value={interval}
            onChange={(e) => setIntervalSec(+e.target.value)}
            className={sel}
          >
            {[5, 15, 30, 60, 120].map((v) => (
              <option key={v} value={v}>
                {v} seconds
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[11px] font-semibold uppercase text-muted">
            Namespaces (empty = all)
          </span>
          <input
            aria-label="Namespaces"
            value={ns}
            onChange={(e) => setNs(e.target.value)}
            placeholder="shop, payments"
            className={clsx(sel, 'w-56 cursor-text select-text')}
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[11px] font-semibold uppercase text-muted">Keep for</span>
          <select
            aria-label="Retention"
            value={retention}
            onChange={(e) => setRetention(+e.target.value)}
            className={sel}
          >
            {[1, 6, 24, 72, 168].map((v) => (
              <option key={v} value={v}>
                {v < 24 ? `${v} hour${v > 1 ? 's' : ''}` : `${v / 24} day${v > 24 ? 's' : ''}`}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[11px] font-semibold uppercase text-muted">At most</span>
          <select
            aria-label="Record limit"
            value={max}
            onChange={(e) => setMax(+e.target.value)}
            className={sel}
          >
            {[50_000, 200_000, 500_000, 1_000_000].map((v) => (
              <option key={v} value={v}>
                {v.toLocaleString()} records
              </option>
            ))}
          </select>
        </label>
        <Button variant="primary" disabled={busy} onClick={() => void save(true)}>
          {info.config.enabled ? 'Apply' : 'Start recording'}
        </Button>
        {info.config.enabled && (
          <Button disabled={busy} onClick={() => void save(false)}>
            Stop
          </Button>
        )}
        {!confirmClear ? (
          <Button
            variant="ghost"
            onClick={() => setConfirmClear(true)}
            disabled={info.span.count === 0}
          >
            Delete recordings…
          </Button>
        ) : (
          <Button
            variant="danger"
            onClick={() => {
              void blackbox.clear(clusterId).then(onSaved)
              setConfirmClear(false)
            }}
          >
            Yes, delete {info.span.count.toLocaleString()} records
          </Button>
        )}
      </div>
      {error && (
        <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
      )}
      <p className="max-w-3xl text-xs text-muted">
        Only <strong>changes</strong> are stored (health transitions and Kubernetes events), on this
        Mac, never uploaded. Recording happens <strong>only while Soliton Rudder is open</strong>;
        when it starts again, changes that happened in between are recorded at that moment. Secrets
        are never read.
      </p>
    </div>
  )
}

const WINDOWS = [
  { label: '1 h', ms: HOUR },
  { label: '6 h', ms: 6 * HOUR },
  { label: '24 h', ms: 24 * HOUR },
  { label: '7 d', ms: 168 * HOUR },
]
const EVENT_WINDOWS = [
  { label: '1 min', ms: MIN },
  { label: '5 min', ms: 5 * MIN },
  { label: '15 min', ms: 15 * MIN },
  { label: '1 h', ms: HOUR },
]
const SPEEDS = [10, 60, 300]

export function Blackbox({ clusterId }: { clusterId: string }) {
  const now = useNow(5000)
  const [showSettings, setShowSettings] = useState(false)
  const [editor, setEditor] = useState(false)
  const [incident, setIncident] = useState<Incident | null>(null)
  const [win, setWin] = useState(HOUR)
  const [evWin, setEvWin] = useState(5 * MIN)
  const [sev, setSev] = useState<'' | 'warn' | 'error'>('')
  const [text, setText] = useState('')
  const [cursor, setCursor] = useState<number | null>(null) // null = live
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(60)
  const [tick, setTick] = useState(0)

  const info = usePoll(() => blackbox.info(clusterId), `${clusterId}|${tick}`, 5_000)
  const data = info.data

  const end = Math.ceil(now / MIN) * MIN
  const start = Math.max(data && data.span.count > 0 ? data.span.min : end - win, end - win)
  const T = cursor ?? now

  const buckets = usePoll(
    () => blackbox.buckets(clusterId, start, end, 120),
    `${clusterId}|${start}|${end}|${tick}`,
    0,
  )
  const states = usePoll(
    () => blackbox.stateAt(clusterId, cursor ?? Date.now()),
    `${clusterId}|${cursor ?? 'live'}|${tick}`,
    cursor === null ? 5_000 : 0,
  )
  const events = usePoll(
    () => {
      const t = cursor ?? Date.now()
      return blackbox.query(clusterId, {
        from: t - evWin,
        to: t,
        kinds: [],
        namespace: '',
        minSeverity: sev,
        text,
        limit: 300,
        newest: true,
      })
    },
    `${clusterId}|${cursor ?? 'live'}|${evWin}|${sev}|${text}|${tick}`,
    cursor === null ? 5_000 : 0,
  )

  const cursorRef = useRef(cursor)
  const endRef = useRef(end)
  useEffect(() => {
    cursorRef.current = cursor
    endRef.current = end
  })
  useEffect(() => {
    if (!playing) return
    const t = setInterval(() => {
      const next = (cursorRef.current ?? endRef.current) + speed * 1000
      if (next >= endRef.current - 1000) {
        setPlaying(false)
        setCursor(null)
      } else setCursor(next)
    }, 1000)
    return () => clearInterval(t)
  }, [playing, speed])

  const counts = useMemo(() => {
    const c = { ok: 0, warn: 0, error: 0 }
    for (const r of states.data ?? [])
      c[r.status === 'error' ? 'error' : r.status === 'warn' ? 'warn' : 'ok']++
    return c
  }, [states.data])
  const problems = useMemo(
    () =>
      (states.data ?? [])
        .filter((r) => r.status === 'warn' || r.status === 'error')
        .sort((a, b) => (a.status === b.status ? 0 : a.status === 'error' ? -1 : 1)),
    [states.data],
  )

  const recording = !!data?.config.enabled
  const empty = !!data && data.span.count === 0

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
        <h1 className="text-sm font-semibold">Blackbox</h1>
        {data && (
          <span
            className={clsx(
              'flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium',
              recording ? 'bg-danger/15 text-danger' : 'bg-surface-2 text-muted',
            )}
            title={data.status.lastError || undefined}
          >
            <span
              className={clsx(
                'h-1.5 w-1.5 rounded-full',
                recording ? 'animate-pulse bg-danger' : 'bg-muted',
              )}
            />
            {recording
              ? data.status.lastError
                ? 'Recording, with problems'
                : 'Recording'
              : 'Not recording'}
          </span>
        )}
        {data && data.span.count > 0 && (
          <span className="font-mono text-xs text-muted">
            {data.span.count.toLocaleString()} records
          </span>
        )}
        <div className="flex-1" />
        <Button onClick={() => setEditor(true)}>
          <BookOpen size={13} /> Runbooks…
        </Button>
        <Button onClick={() => setShowSettings(!showSettings)}>
          <Settings2 size={13} /> Recording
        </Button>
      </div>

      {data && (showSettings || (!recording && empty)) && (
        <SettingsPanel
          key={`${clusterId}|${recording}|${data.span.count === 0}`}
          clusterId={clusterId}
          info={data}
          onSaved={() => setTick((t) => t + 1)}
        />
      )}
      {data?.status.lastError && recording && (
        <div className="shrink-0 border-b border-border bg-warning/10 px-4 py-1.5 text-xs text-warning">
          {data.status.lastError}
        </div>
      )}
      {info.error && !data && <div className="p-4 text-[13px] text-danger">{info.error}</div>}

      {data && empty ? (
        <div className="grid flex-1 place-items-center p-8 text-center">
          <div className="max-w-md">
            <History size={32} className="mx-auto text-muted" />
            <h2 className="mt-3 text-base font-semibold">Nothing recorded yet</h2>
            <p className="mt-1 text-[13px] text-muted">
              The blackbox keeps the health changes and events of this cluster so you can go back to
              the moment something broke, see what was failing around it and follow a runbook. Start
              recording above; it is cheap and bounded.
            </p>
          </div>
        </div>
      ) : (
        data && (
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="shrink-0 space-y-2 border-b border-border px-4 py-3">
              <div className="flex items-center gap-2 text-xs">
                <button
                  aria-label={playing ? 'Pause' : 'Play'}
                  onClick={() => {
                    if (!playing && cursor === null) setCursor(start)
                    setPlaying(!playing)
                  }}
                  className="no-drag grid h-8 w-8 cursor-pointer place-items-center rounded-md border border-border bg-surface-2 hover:bg-surface-hover"
                >
                  {playing ? <Pause size={14} /> : <Play size={14} />}
                </button>
                <select
                  aria-label="Playback speed"
                  value={speed}
                  onChange={(e) => setSpeed(+e.target.value)}
                  className="no-drag h-8 cursor-pointer rounded-md border border-border bg-bg px-2 text-xs"
                >
                  {SPEEDS.map((s) => (
                    <option key={s} value={s}>
                      {s < 60 ? `${s}×` : `${s / 60} min/s`}
                    </option>
                  ))}
                </select>
                <span className="font-mono text-sm font-semibold" aria-label="Selected time">
                  {cursor === null ? 'Live' : stamp(cursor)}
                </span>
                {cursor !== null && (
                  <button
                    onClick={() => {
                      setCursor(null)
                      setPlaying(false)
                    }}
                    className="cursor-pointer text-accent hover:underline"
                  >
                    Back to live
                  </button>
                )}
                <div className="flex-1" />
                {WINDOWS.map((w) => (
                  <button
                    key={w.label}
                    onClick={() => setWin(w.ms)}
                    className={clsx(
                      'cursor-pointer rounded-md border px-2 py-1',
                      win === w.ms
                        ? 'border-accent bg-accent/15 text-accent'
                        : 'border-border text-muted hover:border-muted',
                    )}
                  >
                    {w.label}
                  </button>
                ))}
              </div>
              <Density buckets={buckets.data ?? []} start={start} end={end} cursor={T} />
              <input
                type="range"
                aria-label="Time"
                min={start}
                max={end}
                step={1000}
                value={Math.min(Math.max(T, start), end)}
                onChange={(e) => {
                  const v = +e.target.value
                  setPlaying(false)
                  setCursor(v >= end - MIN ? null : v)
                }}
                className="w-full accent-[var(--accent)]"
              />
              <div className="flex justify-between text-[10px] text-muted">
                <span>{stamp(start)}</span>
                <span>{stamp(end)}</span>
              </div>
            </div>

            <div className="grid min-h-0 flex-1 grid-cols-[minmax(320px,2fr)_3fr]">
              <section className="flex min-h-0 flex-col border-r border-border">
                <div className="shrink-0 border-b border-border px-4 py-2 text-xs">
                  <span className="font-semibold">
                    Cluster {cursor === null ? 'now' : `at ${clock(cursor)}`}
                  </span>
                  <span className="ml-2 text-muted">
                    {counts.ok} healthy ·{' '}
                    <span className={counts.warn ? 'text-warning' : ''}>{counts.warn} warning</span>{' '}
                    ·{' '}
                    <span className={counts.error ? 'text-danger' : ''}>
                      {counts.error} failing
                    </span>
                  </span>
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto">
                  {states.loading && (
                    <div className="p-4 text-[13px] text-muted">Reconstructing…</div>
                  )}
                  {!states.loading && problems.length === 0 && (
                    <div className="p-4 text-[13px] text-muted">
                      {counts.ok + counts.warn + counts.error === 0
                        ? 'Nothing was recorded at this time.'
                        : 'Everything recorded was healthy.'}
                    </div>
                  )}
                  {problems.map((r) => (
                    <button
                      key={r.key}
                      onClick={() => setIncident(incidentOf(r))}
                      className="flex w-full cursor-pointer items-start gap-2 border-b border-border px-4 py-2 text-left hover:bg-surface-hover"
                    >
                      <Sev s={r.status} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px]">
                          <span className="text-[10px] font-semibold uppercase text-muted">
                            {r.objKind}
                          </span>{' '}
                          <span className="font-mono">
                            {r.namespace ? `${r.namespace}/` : ''}
                            {r.name}
                          </span>
                        </div>
                        <div className="select-text text-xs text-muted">
                          {r.reason} · since {clock(r.ts)}
                          {r.prev && <> · was {r.prev}</>}
                        </div>
                      </div>
                    </button>
                  ))}
                </div>
              </section>

              <section className="flex min-h-0 flex-col">
                <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2 text-xs">
                  <span className="font-semibold">
                    Before {cursor === null ? 'now' : clock(cursor)}
                  </span>
                  {EVENT_WINDOWS.map((w) => (
                    <button
                      key={w.label}
                      onClick={() => setEvWin(w.ms)}
                      className={clsx(
                        'cursor-pointer rounded border px-1.5 py-0.5',
                        evWin === w.ms
                          ? 'border-accent bg-accent/15 text-accent'
                          : 'border-border text-muted',
                      )}
                    >
                      {w.label}
                    </button>
                  ))}
                  <div className="flex-1" />
                  <select
                    aria-label="Severity"
                    value={sev}
                    onChange={(e) => setSev(e.target.value as '' | 'warn' | 'error')}
                    className="no-drag h-7 cursor-pointer rounded border border-border bg-bg px-1.5 text-xs"
                  >
                    <option value="">All</option>
                    <option value="warn">Warnings and errors</option>
                    <option value="error">Errors only</option>
                  </select>
                  <input
                    aria-label="Filter"
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                    placeholder="Filter…"
                    className="no-drag h-7 w-32 select-text rounded border border-border bg-bg px-2 text-xs outline-none placeholder:text-muted focus:border-accent"
                  />
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto">
                  {events.error && (
                    <div className="p-4 text-[13px] text-danger">{events.error}</div>
                  )}
                  {!events.loading && !events.error && (events.data ?? []).length === 0 && (
                    <div className="p-4 text-[13px] text-muted">
                      Nothing happened in this window.
                    </div>
                  )}
                  {(events.data ?? []).map((r) => (
                    <button
                      key={r.id}
                      onClick={() => setIncident(incidentOf(r))}
                      className="flex w-full cursor-pointer items-start gap-2 border-b border-border px-4 py-1.5 text-left hover:bg-surface-hover"
                    >
                      <Sev s={r.severity} />
                      <span className="w-16 shrink-0 font-mono text-[11px] text-muted">
                        {clock(r.ts)}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px]">
                          <span className="text-[10px] font-semibold uppercase text-muted">
                            {r.objKind}
                          </span>{' '}
                          <span className="font-mono">
                            {r.namespace ? `${r.namespace}/` : ''}
                            {r.name}
                          </span>{' '}
                          <span
                            className={clsx(
                              'font-medium',
                              r.severity === 'error' && 'text-danger',
                              r.severity === 'warn' && 'text-warning',
                            )}
                          >
                            {r.reason}
                          </span>
                        </div>
                        {(r.message || r.prev) && (
                          <div className="select-text truncate text-xs text-muted">
                            {r.kind === 'state'
                              ? r.prev
                                ? `was ${r.prev}`
                                : 'first seen'
                              : r.message}
                          </div>
                        )}
                      </div>
                    </button>
                  ))}
                </div>
              </section>
            </div>
          </div>
        )
      )}
      {incident && (
        <RunbookDialog
          clusterId={clusterId}
          incident={incident}
          onClose={() => setIncident(null)}
        />
      )}
      {editor && <RunbooksEditor onClose={() => setEditor(false)} />}
    </div>
  )
}
