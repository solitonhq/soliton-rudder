import { useVirtualizer } from '@tanstack/react-virtual'
import clsx from 'clsx'
import { ArrowDownToLine, Download, Eraser, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { tools } from '../lib/backend'
import {
  detectLevel,
  logRouter,
  LogStore,
  prettyJSON,
  type LogEntry,
  type LogLevel,
} from '../lib/logstore'
import type { LogTarget } from '../lib/types'
import type { DockTab } from '../store/dock'
import { useUI } from '../store/ui'
import { Button } from './ui'

type LogsTab = Extract<DockTab, { kind: 'logs' }>

const MAX_STREAMS = 20
const TAILS = [100, 500, 1000, 5000, 0]
const SINCE: [string, number][] = [
  ['All time', 0],
  ['Last 5m', 300],
  ['Last 1h', 3600],
  ['Last 24h', 86400],
]
const LEVELS: LogLevel[] = ['error', 'warn', 'info', 'debug']
const levelCls: Record<LogLevel, string> = {
  error: 'text-danger',
  warn: 'text-warning',
  info: '',
  debug: 'text-muted',
  none: '',
}
const SOURCE_COLORS = [
  '#5b8def',
  '#3fb97f',
  '#e0a63a',
  '#a77bf3',
  '#2fc4c4',
  '#ef7bb0',
  '#e5584f',
  '#8b95a5',
]

const hash = (s: string) => [...s].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7)
const srcKey = (pod: string, c: string) => `${pod}/${c}`
const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

export function LogsTabView({ tab, active }: { tab: LogsTab; active: boolean }) {
  const toast = useUI((s) => s.toast)
  const store = useMemo(() => new LogStore(), [])
  const version = useSyncExternalStore(store.subscribe, store.getVersion)

  const [targets, setTargets] = useState<LogTarget[] | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [tail, setTail] = useState(500)
  const [since, setSince] = useState(0)
  const [previous, setPrevious] = useState(false)
  const [follow, setFollow] = useState(true)
  const [showTs, setShowTs] = useState(false)
  const [wrap, setWrap] = useState(false)
  const [pretty, setPretty] = useState(false)
  const [search, setSearch] = useState('')
  const [onlyMatches, setOnlyMatches] = useState(false)
  const [levels, setLevels] = useState<Set<LogLevel>>(new Set([...LEVELS, 'none']))
  const [stick, setStick] = useState(true)
  const [openSources, setOpenSources] = useState(false)
  const [live, setLive] = useState(0)
  const [streamErr, setStreamErr] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  // discover pods/containers
  useEffect(() => {
    let cancelled = false
    tools
      .logTargets(tab.clusterId, tab.key, tab.ns, tab.name)
      .then((t) => {
        if (cancelled) return
        setTargets(t)
        const keys: string[] = []
        for (const p of t) for (const c of p.containers) keys.push(srcKey(p.pod, c))
        if (keys.length > MAX_STREAMS)
          toast('error', `Showing the first ${MAX_STREAMS} of ${keys.length} containers`)
        setSelected(new Set(keys.slice(0, MAX_STREAMS)))
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      cancelled = true
    }
  }, [tab.clusterId, tab.key, tab.ns, tab.name, toast])

  // (re)start streams whenever source/options change
  const selKey = [...selected].sort().join('|')
  useEffect(() => {
    if (!targets || selected.size === 0) return
    let cancelled = false
    const subs: string[] = []
    store.clear()
    setStreamErr('')
    setLive(0)
    for (const k of selected) {
      const i = k.indexOf('/')
      const pod = k.slice(0, i)
      const container = k.slice(i + 1)
      tools
        .startLogs(tab.clusterId, {
          namespace: tab.ns,
          pod,
          container,
          follow,
          tailLines: tail,
          sinceSecs: since,
          previous,
          timestamps: true,
        })
        .then((sub) => {
          if (cancelled) return void tools.stopLogs(sub)
          subs.push(sub)
          setLive((n) => n + 1)
          logRouter.register(sub, {
            batch: (b) => store.push(b),
            end: (e) => {
              setLive((n) => Math.max(0, n - 1))
              if (e.error) setStreamErr(`${pod}/${container}: ${e.error}`)
            },
          })
        })
        .catch((e) => setStreamErr(e instanceof Error ? e.message : String(e)))
    }
    return () => {
      cancelled = true
      subs.forEach((s) => {
        logRouter.unregister(s)
        void tools.stopLogs(s)
      })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [targets, selKey, tail, since, previous, follow, tab.clusterId, tab.ns, store])

  const multi = selected.size > 1
  const q = search.trim()
  const lines = useMemo(() => {
    const ql = q.toLowerCase()
    const out: { e: LogEntry; level: LogLevel }[] = []
    for (const e of store.lines) {
      const level = detectLevel(e.text)
      if (!levels.has(level)) continue
      if (onlyMatches && ql && !e.text.toLowerCase().includes(ql)) continue
      out.push({ e, level })
    }
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [store, version, levels, onlyMatches, q])

  // eslint-disable-next-line react-hooks/incompatible-library
  const virt = useVirtualizer({
    count: lines.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 20,
    overscan: 20,
    measureElement: wrap ? undefined : () => 20,
  })

  useEffect(() => {
    if (stick && lines.length > 0 && active) virt.scrollToIndex(lines.length - 1, { align: 'end' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lines.length, stick, active])

  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    if (!atBottom && stick) setStick(false)
    else if (atBottom && !stick) setStick(true)
  }

  const re = useMemo(() => (q ? new RegExp(`(${esc(q)})`, 'ig') : null), [q])
  const highlight = (text: string) =>
    re
      ? text.split(re).map((p, i) =>
          i % 2 ? (
            <mark key={i} className="rounded bg-warning/40 text-fg">
              {p}
            </mark>
          ) : (
            p
          ),
        )
      : text

  const download = async () => {
    const text = lines
      .map(
        ({ e }) =>
          `${showTs && e.ts ? e.ts + ' ' : ''}${multi ? `[${e.pod}/${e.container}] ` : ''}${e.text}`,
      )
      .join('\n')
    try {
      const p = await tools.saveText(`${tab.name}.log`, text)
      if (p) toast('success', `Saved ${lines.length} lines`)
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e))
    }
  }

  const toggleLevel = (l: LogLevel) =>
    setLevels((s) => {
      const n = new Set(s)
      if (!n.delete(l)) n.add(l)
      n.add('none')
      return n
    })

  if (error) return <div className="p-4 text-[13px] text-danger">{error}</div>
  if (!targets) return <div className="p-4 text-[13px] text-muted">Loading…</div>
  if (targets.length === 0) return <div className="p-4 text-[13px] text-muted">No pods found.</div>

  const pill = (on: boolean) =>
    clsx(
      'cursor-pointer rounded px-2 py-1 text-xs font-medium',
      on ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
    )

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-1.5 text-xs">
        <div className="relative">
          <Button className="h-7 text-xs" onClick={() => setOpenSources((o) => !o)}>
            {selected.size} source{selected.size === 1 ? '' : 's'} ▾
          </Button>
          {openSources && (
            <div className="absolute left-0 top-8 z-30 max-h-72 w-80 overflow-auto rounded-lg border border-border bg-surface p-2 shadow-xl">
              {targets.map((p) => (
                <div key={p.pod} className="mb-1.5">
                  <div className="truncate px-1 text-[11px] font-semibold text-muted" title={p.pod}>
                    {p.pod} <span className="font-normal">· {p.phase}</span>
                  </div>
                  {p.containers.map((c) => {
                    const k = srcKey(p.pod, c)
                    return (
                      <label
                        key={k}
                        className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 hover:bg-surface-hover"
                      >
                        <input
                          type="checkbox"
                          checked={selected.has(k)}
                          className="accent-[var(--accent)]"
                          onChange={() =>
                            setSelected((s) => {
                              const n = new Set(s)
                              if (!n.delete(k)) n.add(k)
                              return n
                            })
                          }
                        />
                        <span className="truncate text-[13px]">{c}</span>
                      </label>
                    )
                  })}
                </div>
              ))}
              <div className="flex gap-2 border-t border-border pt-2">
                <Button
                  variant="ghost"
                  className="h-7 text-xs"
                  onClick={() =>
                    setSelected(
                      new Set(
                        targets
                          .flatMap((p) => p.containers.map((c) => srcKey(p.pod, c)))
                          .slice(0, MAX_STREAMS),
                      ),
                    )
                  }
                >
                  All
                </Button>
                <Button
                  variant="ghost"
                  className="h-7 text-xs"
                  onClick={() => setSelected(new Set())}
                >
                  None
                </Button>
              </div>
            </div>
          )}
        </div>

        <select
          value={tail}
          onChange={(e) => setTail(Number(e.target.value))}
          className="no-drag h-7 rounded-md border border-border bg-surface-2 px-1.5 text-xs"
        >
          {TAILS.map((t) => (
            <option key={t} value={t}>
              {t === 0 ? 'All lines' : `Last ${t}`}
            </option>
          ))}
        </select>
        <select
          value={since}
          onChange={(e) => setSince(Number(e.target.value))}
          className="no-drag h-7 rounded-md border border-border bg-surface-2 px-1.5 text-xs"
        >
          {SINCE.map(([l, v]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </select>

        <div className="flex items-center rounded-md border border-border">
          <button
            className={pill(follow)}
            onClick={() => setFollow(!follow)}
            title="Stream new lines"
          >
            Follow
          </button>
          <button className={pill(showTs)} onClick={() => setShowTs(!showTs)}>
            Time
          </button>
          <button className={pill(wrap)} onClick={() => setWrap(!wrap)}>
            Wrap
          </button>
          <button
            className={pill(pretty)}
            onClick={() => setPretty(!pretty)}
            title="Format JSON log lines"
          >
            JSON
          </button>
          <button
            className={pill(previous)}
            onClick={() => setPrevious(!previous)}
            title="Logs of the previous container instance"
          >
            Previous
          </button>
        </div>

        <div className="flex items-center gap-0.5">
          {LEVELS.map((l) => (
            <button
              key={l}
              onClick={() => toggleLevel(l)}
              className={clsx(pill(levels.has(l)), 'capitalize')}
            >
              {l}
            </button>
          ))}
        </div>

        <div className="relative ml-auto">
          <Search size={12} className="absolute left-2 top-2 text-muted" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Find in logs"
            className="no-drag h-7 w-44 select-text rounded-md border border-border bg-bg pl-7 pr-2 text-xs outline-none focus:border-accent"
          />
        </div>
        <button
          className={pill(onlyMatches)}
          onClick={() => setOnlyMatches(!onlyMatches)}
          title="Show only matching lines"
        >
          Filter
        </button>
        <Button variant="ghost" className="h-7 px-2" onClick={() => store.clear()} title="Clear">
          <Eraser size={13} />
        </Button>
        <Button
          variant="ghost"
          className="h-7 px-2"
          onClick={() => void download()}
          title="Save to file"
        >
          <Download size={13} />
        </Button>
      </div>

      <div className="relative min-h-0 flex-1 bg-bg">
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="h-full select-text overflow-auto font-mono text-[12px] leading-5"
        >
          <div
            style={{
              height: virt.getTotalSize(),
              position: 'relative',
              minWidth: wrap ? undefined : 'max-content',
            }}
          >
            {virt.getVirtualItems().map((v) => {
              const { e, level } = lines[v.index]
              const color = SOURCE_COLORS[hash(srcKey(e.pod, e.container)) % SOURCE_COLORS.length]
              const pj = pretty ? prettyJSON(e.text) : null
              return (
                <div
                  key={e.id}
                  ref={wrap ? virt.measureElement : undefined}
                  data-index={v.index}
                  className={clsx(
                    'absolute left-0 w-full px-3 hover:bg-surface-hover',
                    levelCls[level],
                    wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre',
                  )}
                  style={{ transform: `translateY(${v.start}px)`, minHeight: 20 }}
                >
                  {showTs && e.ts && (
                    <span className="mr-2 text-muted">
                      {e.ts.replace('T', ' ').replace(/\.\d+Z$/, '')}
                    </span>
                  )}
                  {multi && (
                    <span className="mr-2 font-semibold" style={{ color }}>
                      {shortSrc(e.pod, e.container, targets)}
                    </span>
                  )}
                  {pj ? (
                    <>
                      {highlight(pj.msg)}
                      {pj.fields.map(([k, val]) => (
                        <span key={k} className="ml-2">
                          <span className="text-muted">{k}=</span>
                          <span className="text-accent">{highlight(val)}</span>
                        </span>
                      ))}
                    </>
                  ) : (
                    highlight(e.text)
                  )}
                </div>
              )
            })}
          </div>
          {lines.length === 0 && (
            <div className="p-4 text-muted">{live > 0 ? 'Waiting for logs…' : 'No log lines.'}</div>
          )}
        </div>
        {!stick && (
          <Button
            variant="primary"
            className="absolute bottom-3 right-4 shadow-lg"
            onClick={() => {
              setStick(true)
              virt.scrollToIndex(lines.length - 1, { align: 'end' })
            }}
          >
            <ArrowDownToLine size={13} /> Latest
          </Button>
        )}
      </div>
      <div className="flex items-center justify-between border-t border-border px-3 py-1 text-[11px] text-muted">
        <span>
          {lines.length.toLocaleString()} lines
          {lines.length !== store.lines.length && ` (of ${store.lines.length.toLocaleString()})`}
        </span>
        <span className={streamErr ? 'text-danger' : ''}>
          {streamErr || (live > 0 && follow ? `● streaming ${live}` : 'idle')}
        </span>
      </div>
    </div>
  )
}

/** Shortest readable label: pod suffix (hash part) plus container when a pod has several. */
function shortSrc(pod: string, container: string, targets: LogTarget[]): string {
  const p = targets.find((t) => t.pod === pod)
  const suffix = pod.split('-').slice(-2).join('-')
  return p && p.containers.length > 1 ? `${suffix}/${container}` : suffix || pod
}
