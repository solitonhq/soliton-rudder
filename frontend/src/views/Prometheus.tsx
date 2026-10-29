import { Play, Unplug } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { EmptyState, ViewHeader } from '../components/GitOps'
import { MultiChart, PALETTE, formatSI } from '../components/MultiChart'
import { Button, Field, Input } from '../components/ui'
import { integ } from '../lib/backend'
import type { PromConfig, PromResult } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const RANGES: [string, number][] = [
  ['15m', 900],
  ['1h', 3600],
  ['6h', 21600],
  ['24h', 86400],
  ['7d', 604800],
]

const PRESETS: { label: string; query: string }[] = [
  {
    label: 'Cluster CPU (cores)',
    query: 'sum(rate(container_cpu_usage_seconds_total{container!=""}[5m]))',
  },
  {
    label: 'Memory by namespace (top 10)',
    query: 'topk(10, sum by (namespace) (container_memory_working_set_bytes{container!=""}))',
  },
  {
    label: 'CPU by namespace (top 10)',
    query:
      'topk(10, sum by (namespace) (rate(container_cpu_usage_seconds_total{container!=""}[5m])))',
  },
  {
    label: 'Pod restarts, last hour',
    query:
      'topk(10, sum by (namespace, pod) (increase(kube_pod_container_status_restarts_total[1h])))',
  },
  {
    label: 'Pods not ready',
    query: 'sum by (namespace) (kube_pod_status_ready{condition="false"})',
  },
  { label: 'API server requests/s', query: 'sum(rate(apiserver_request_total[5m]))' },
]

function labelOf(m: Record<string, string>): string {
  const keys = Object.keys(m).filter((k) => k !== '__name__')
  if (keys.length === 0) return m.__name__ ?? 'value'
  return keys.map((k) => `${k}=${m[k]}`).join(', ')
}

export function Prometheus({ clusterId }: { clusterId: string }) {
  const [nonce, setNonce] = useState(0)
  const { data: state, loading } = usePoll(
    () => integ.prom.config(clusterId),
    `${clusterId}/${nonce}`,
    0,
  )
  const toast = useUI((s) => s.toast)

  const refresh = () => {
    setNonce((n) => n + 1)
    void integ
      .detect(clusterId)
      .then((i) => useExplorer.setState({ integrations: i }))
      .catch(() => {})
  }

  if (loading || !state) return <div className="p-6 text-[13px] text-muted">Loading…</div>
  if (!state.configured) return <Setup clusterId={clusterId} onDone={refresh} />
  return (
    <Explore
      clusterId={clusterId}
      cfg={state.config}
      onDisconnect={async () => {
        try {
          await integ.prom.clear(clusterId)
          toast('success', 'Prometheus disconnected')
          refresh()
        } catch (e) {
          toast('error', errText(e))
        }
      }}
    />
  )
}

function Setup({ clusterId, onDone }: { clusterId: string; onDone: () => void }) {
  const toast = useUI((s) => s.toast)
  const { data: found, loading } = usePoll(() => integ.prom.detect(clusterId), clusterId, 0)
  const [cfg, setCfg] = useState<PromConfig>({
    namespace: 'monitoring',
    service: '',
    port: '9090',
    scheme: 'http',
    path: '',
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const save = async (c: PromConfig) => {
    setBusy(true)
    setError('')
    try {
      const v = await integ.prom.save(clusterId, c)
      toast('success', `Connected to Prometheus ${v}`)
      onDone()
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <ViewHeader title="Prometheus" />
      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div className="mx-auto max-w-xl space-y-6">
          <div>
            <h2 className="text-base font-semibold">Connect Prometheus</h2>
            <p className="mt-1 text-[13px] text-muted">
              Soliton Rudder reaches Prometheus through the Kubernetes API (service proxy), so no
              port-forward or ingress is needed. Once connected, resource charts can show up to 7
              days of history, and you can run PromQL queries.
            </p>
          </div>
          <section>
            <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
              Detected in this cluster
            </h3>
            {loading && <p className="text-[13px] text-muted">Looking for Prometheus services…</p>}
            {found && found.length === 0 && (
              <p className="text-[13px] text-muted">
                No Prometheus service found automatically. Enter it manually below.
              </p>
            )}
            <div className="space-y-2">
              {(found ?? []).map((c) => (
                <div
                  key={c.namespace + c.service}
                  className="flex items-center justify-between rounded-lg border border-border bg-surface px-3 py-2"
                >
                  <div className="min-w-0">
                    <div className="truncate text-[13px] font-medium">{c.service}</div>
                    <div className="text-xs text-muted">
                      {c.namespace} · port {c.port}
                      {c.headless && ' · headless'}
                    </div>
                  </div>
                  <Button variant="primary" disabled={busy} onClick={() => void save(c)}>
                    Connect
                  </Button>
                </div>
              ))}
            </div>
          </section>
          <section>
            <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
              Manual
            </h3>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Namespace">
                <Input
                  value={cfg.namespace}
                  onChange={(e) => setCfg({ ...cfg, namespace: e.target.value })}
                />
              </Field>
              <Field label="Service">
                <Input
                  value={cfg.service}
                  onChange={(e) => setCfg({ ...cfg, service: e.target.value })}
                  placeholder="prometheus-operated"
                />
              </Field>
              <Field label="Port (number or name)">
                <Input
                  value={cfg.port}
                  onChange={(e) => setCfg({ ...cfg, port: e.target.value })}
                />
              </Field>
              <Field label="Scheme">
                <select
                  value={cfg.scheme}
                  onChange={(e) => setCfg({ ...cfg, scheme: e.target.value })}
                  className="h-8 w-full rounded-md border border-border bg-bg px-2 text-[13px]"
                >
                  <option>http</option>
                  <option>https</option>
                </select>
              </Field>
              <Field label="Path prefix (optional)">
                <Input
                  value={cfg.path}
                  onChange={(e) => setCfg({ ...cfg, path: e.target.value })}
                  placeholder="/prometheus"
                />
              </Field>
            </div>
            {error && <p className="mt-3 select-text break-words text-xs text-danger">{error}</p>}
            <Button
              variant="primary"
              className="mt-4"
              disabled={busy || !cfg.service || !cfg.namespace || !cfg.port}
              onClick={() => void save(cfg)}
            >
              Test &amp; connect
            </Button>
          </section>
        </div>
      </div>
    </div>
  )
}

function Explore({
  clusterId,
  cfg,
  onDisconnect,
}: {
  clusterId: string
  cfg: PromConfig
  onDisconnect: () => void
}) {
  const [query, setQuery] = useState(PRESETS[0].query)
  const [range, setRange] = useState(3600)
  const [res, setRes] = useState<{ loading: boolean; error: string; data: PromResult | null }>({
    loading: false,
    error: '',
    data: null,
  })
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const seq = useRef(0)

  const run = async (q = query, r = range) => {
    const my = ++seq.current
    setRes((s) => ({ ...s, loading: true, error: '' }))
    try {
      const data = await integ.prom.query(clusterId, q, r, 0)
      if (my === seq.current) {
        setRes({ loading: false, error: '', data })
        setHidden(new Set())
      }
    } catch (e) {
      if (my === seq.current) setRes({ loading: false, error: errText(e), data: null })
    }
  }

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial query on mount
    void run()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const rows = useMemo(
    () =>
      (res.data?.series ?? []).map((s, i) => {
        const vals = s.points.map((p) => p[1])
        return {
          label: labelOf(s.metric),
          color: PALETTE[i % PALETTE.length],
          points: s.points,
          last: vals[vals.length - 1] ?? 0,
          min: Math.min(...vals),
          max: Math.max(...vals),
        }
      }),
    [res.data],
  )
  const visible = rows.filter((r) => !hidden.has(r.label)).slice(0, 12)

  return (
    <div className="flex h-full flex-col">
      <ViewHeader title="Prometheus">
        <span className="font-mono text-xs text-muted">
          {cfg.namespace}/{cfg.service}:{cfg.port}
        </span>
        <Button variant="ghost" onClick={onDisconnect}>
          <Unplug size={13} /> Disconnect
        </Button>
      </ViewHeader>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <div className="mx-auto max-w-5xl space-y-4">
          <div className="flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <button
                key={p.label}
                onClick={() => {
                  setQuery(p.query)
                  void run(p.query)
                }}
                className="cursor-pointer rounded-full border border-border px-3 py-1 text-xs hover:border-accent hover:text-accent"
              >
                {p.label}
              </button>
            ))}
          </div>
          <div className="rounded-xl border border-border bg-surface p-3">
            <textarea
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault()
                  void run()
                }
              }}
              spellCheck={false}
              rows={3}
              placeholder="PromQL, e.g. sum(rate(container_cpu_usage_seconds_total[5m]))"
              className="w-full select-text resize-y rounded-md border border-border bg-bg p-2.5 font-mono text-xs outline-none focus:border-accent"
            />
            <div className="mt-2 flex items-center gap-2">
              <div className="flex gap-0.5 rounded-md border border-border p-0.5">
                {RANGES.map(([l, s]) => (
                  <button
                    key={l}
                    onClick={() => {
                      setRange(s)
                      void run(query, s)
                    }}
                    className={`cursor-pointer rounded px-2.5 py-1 text-xs font-medium ${range === s ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg'}`}
                  >
                    {l}
                  </button>
                ))}
              </div>
              <div className="flex-1" />
              <span className="text-[11px] text-muted">⌘↵ to run</span>
              <Button
                variant="primary"
                disabled={res.loading || !query.trim()}
                onClick={() => void run()}
              >
                <Play size={13} /> Run
              </Button>
            </div>
          </div>
          {res.error && (
            <div className="select-text break-words rounded-md bg-danger/10 px-3 py-2 text-[13px] text-danger">
              {res.error}
            </div>
          )}
          {res.loading && !res.data && <p className="text-[13px] text-muted">Querying…</p>}
          {res.data && (
            <div className="rounded-xl border border-border bg-surface p-4">
              {rows.length === 0 ? (
                <EmptyState>The query returned no series.</EmptyState>
              ) : (
                <MultiChart series={visible} />
              )}
              {res.data.truncated && (
                <p className="mt-2 text-xs text-warning">Showing the first 50 series.</p>
              )}
            </div>
          )}
          {rows.length > 0 && (
            <div className="overflow-hidden rounded-xl border border-border bg-surface">
              <div className="grid grid-cols-[1fr_90px_90px_90px] border-b border-border px-4 py-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
                <span>Series ({rows.length})</span>
                <span className="text-right">Last</span>
                <span className="text-right">Min</span>
                <span className="text-right">Max</span>
              </div>
              <div className="max-h-72 overflow-auto">
                {rows.map((r) => {
                  const off = hidden.has(r.label)
                  return (
                    <button
                      key={r.label}
                      onClick={() =>
                        setHidden((h) => {
                          const n = new Set(h)
                          if (!n.delete(r.label)) n.add(r.label)
                          return n
                        })
                      }
                      className="grid w-full cursor-pointer grid-cols-[1fr_90px_90px_90px] items-center border-b border-border/50 px-4 py-1.5 text-left text-[13px] last:border-0 hover:bg-surface-hover"
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        <span
                          className="size-2.5 shrink-0 rounded-full"
                          style={{ background: off ? 'var(--border)' : r.color }}
                        />
                        <span
                          className={`truncate font-mono text-xs ${off ? 'text-muted line-through' : ''}`}
                          title={r.label}
                        >
                          {r.label}
                        </span>
                      </span>
                      <span className="text-right font-mono text-xs">{formatSI(r.last)}</span>
                      <span className="text-right font-mono text-xs text-muted">
                        {formatSI(r.min)}
                      </span>
                      <span className="text-right font-mono text-xs text-muted">
                        {formatSI(r.max)}
                      </span>
                    </button>
                  )
                })}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
