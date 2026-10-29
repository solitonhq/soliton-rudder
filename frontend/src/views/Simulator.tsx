import clsx from 'clsx'
import { AlertTriangle, ArrowRight, CheckCircle2, Play, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { insight } from '../lib/backend'
import { formatBytes, formatCPU } from '../lib/units'
import type { DrainResult, NetEndpoint, NetResult, Sev, SimTargets } from '../lib/types'
import { usePoll } from '../lib/usePoll'

type Tab = 'network' | 'drain'
type Side = NetEndpoint['kind']

const field =
  'no-drag h-8 select-text rounded-md border border-border bg-bg px-2 text-[13px] outline-none placeholder:text-muted focus:border-accent'

function SevIcon({
  s,
  size = 14,
}: {
  s: Sev | 'allowed' | 'denied' | 'safe' | 'blocked' | 'disruptive'
  size?: number
}) {
  if (s === 'ok' || s === 'allowed' || s === 'safe')
    return <CheckCircle2 size={size} className="shrink-0 text-success" />
  if (s === 'warn' || s === 'disruptive')
    return <AlertTriangle size={size} className="shrink-0 text-warning" />
  return <XCircle size={size} className="shrink-0 text-danger" />
}

function Banner({ tone, children }: { tone: 'ok' | 'warn' | 'error'; children: ReactNode }) {
  return (
    <div
      className={clsx(
        'flex items-start gap-2 rounded-lg border p-3 text-[13px]',
        tone === 'ok' && 'border-success/40 bg-success/10',
        tone === 'warn' && 'border-warning/40 bg-warning/10',
        tone === 'error' && 'border-danger/40 bg-danger/10',
      )}
    >
      <SevIcon s={tone} size={16} />
      <div className="min-w-0 select-text">{children}</div>
    </div>
  )
}

function parseRef(v: string): { namespace: string; name: string } {
  const i = v.indexOf('/')
  return i < 0 ? { namespace: '', name: v } : { namespace: v.slice(0, i), name: v.slice(i + 1) }
}

function Endpoint({
  label,
  kind,
  setKind,
  value,
  setValue,
  targets,
  kinds,
}: {
  label: string
  kind: Side
  setKind: (k: Side) => void
  value: string
  setValue: (v: string) => void
  targets: SimTargets | null
  kinds: Side[]
}) {
  const list = `sim-${label}`
  const options = kind === 'Pod' ? targets?.pods : kind === 'Service' ? targets?.services : []
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] font-semibold uppercase text-muted">{label}</span>
      <div className="flex gap-1.5">
        <select
          aria-label={`${label} kind`}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value as Side)
            setValue('')
          }}
          className={clsx(field, 'cursor-pointer')}
        >
          {kinds.map((k) => (
            <option key={k}>{k}</option>
          ))}
        </select>
        <input
          aria-label={`${label} ${kind === 'External' ? 'IP' : 'name'}`}
          list={list}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={kind === 'External' ? '203.0.113.10' : 'namespace/name'}
          className={clsx(field, 'w-64')}
        />
        <datalist id={list}>
          {(options ?? []).map((o) => (
            <option key={o} value={o} />
          ))}
        </datalist>
      </div>
    </div>
  )
}

function NetworkTab({ clusterId, targets }: { clusterId: string; targets: SimTargets | null }) {
  const [fromKind, setFromKind] = useState<Side>('Pod')
  const [from, setFrom] = useState('')
  const [toKind, setToKind] = useState<Side>('Pod')
  const [to, setTo] = useState('')
  const [port, setPort] = useState('')
  const [proto, setProto] = useState('TCP')
  const [res, setRes] = useState<NetResult | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const ep = (k: Side, v: string): NetEndpoint =>
    k === 'External'
      ? { kind: k, namespace: '', name: '', ip: v.trim() }
      : { kind: k, ...parseRef(v.trim()), ip: '' }
  const ready = from.trim() !== '' && to.trim() !== ''

  const run = async () => {
    setBusy(true)
    setErr('')
    try {
      setRes(
        await insight.simNetwork(clusterId, {
          from: ep(fromKind, from),
          to: ep(toKind, to),
          port: port.trim(),
          protocol: proto,
        }),
      )
    } catch (e) {
      setRes(null)
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const tone = res?.verdict === 'allowed' ? 'ok' : res?.verdict === 'denied' ? 'error' : 'warn'
  return (
    <div className="space-y-4 p-4">
      <p className="max-w-3xl text-[13px] text-muted">
        Can this source reach that destination? The cluster’s NetworkPolicies are evaluated for both
        the source’s egress and the destination’s ingress. Nothing is sent over the network.
      </p>
      <div className="flex flex-wrap items-end gap-4">
        <Endpoint
          label="From"
          kind={fromKind}
          setKind={setFromKind}
          value={from}
          setValue={setFrom}
          targets={targets}
          kinds={['Pod', 'External']}
        />
        <ArrowRight size={16} className="mb-2 text-muted" />
        <Endpoint
          label="To"
          kind={toKind}
          setKind={setToKind}
          value={to}
          setValue={setTo}
          targets={targets}
          kinds={['Pod', 'Service', 'External']}
        />
        <div className="flex flex-col gap-1">
          <span className="text-[11px] font-semibold uppercase text-muted">Port</span>
          <div className="flex gap-1.5">
            <input
              aria-label="Port"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder="8080 or http"
              className={clsx(field, 'w-32')}
            />
            <select
              aria-label="Protocol"
              value={proto}
              onChange={(e) => setProto(e.target.value)}
              className={clsx(field, 'cursor-pointer')}
            >
              <option>TCP</option>
              <option>UDP</option>
              <option>SCTP</option>
            </select>
          </div>
        </div>
        <button
          disabled={!ready || busy}
          onClick={() => void run()}
          className="no-drag flex h-8 cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-fg disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Play size={13} /> {busy ? 'Evaluating…' : 'Simulate'}
        </button>
      </div>

      {err && <div className="select-text text-[13px] text-danger">{err}</div>}
      {res && (
        <div className="max-w-4xl space-y-3">
          <Banner tone={tone}>
            <div className="font-semibold capitalize">{res.verdict}</div>
            <div>{res.summary}</div>
          </Banner>
          {res.paths.map((p) => (
            <div key={p.target} className="rounded-lg border border-border">
              <div className="flex items-center gap-2 border-b border-border px-3 py-2 text-[13px]">
                <SevIcon s={p.allowed ? 'allowed' : 'denied'} />
                <span className="font-mono">{p.target}</span>
                {p.port > 0 && <span className="text-muted">:{p.port}</span>}
              </div>
              <ul className="divide-y divide-border">
                {p.steps.map((s) => (
                  <li
                    key={s.side + s.subject}
                    className="flex items-start gap-2 px-3 py-2 text-[13px]"
                  >
                    <SevIcon s={s.allowed ? 'allowed' : 'denied'} />
                    <div className="min-w-0">
                      <div>
                        <span className="font-semibold capitalize">{s.side}</span> of{' '}
                        <span className="font-mono">{s.subject}</span>
                      </div>
                      <div className="select-text text-muted">{s.reason}</div>
                      {s.policies.length > 0 && (
                        <div className="mt-1 flex flex-wrap gap-1">
                          {s.policies.map((n) => (
                            <span
                              key={n}
                              className={clsx(
                                'rounded px-1.5 py-0.5 font-mono text-[11px]',
                                n === s.matched
                                  ? 'bg-success/15 text-success'
                                  : 'bg-surface-2 text-muted',
                              )}
                            >
                              {n}
                            </span>
                          ))}
                        </div>
                      )}
                    </div>
                  </li>
                ))}
                {p.steps.length === 0 && (
                  <li className="px-3 py-2 text-[13px] text-muted">
                    External source: only the destination’s ingress applies, and it has no steps.
                  </li>
                )}
              </ul>
            </div>
          ))}
          {res.notes.map((n) => (
            <div key={n} className="text-xs text-muted">
              {n}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function Meter({
  label,
  need,
  free,
  fmt,
}: {
  label: string
  need: number
  free: number
  fmt: (n: number) => string
}) {
  const pct = free <= 0 ? (need > 0 ? 100 : 0) : Math.min(100, Math.round((need / free) * 100))
  return (
    <div className="min-w-48 flex-1">
      <div className="mb-1 flex justify-between text-xs text-muted">
        <span>{label}</span>
        <span>
          {fmt(need)} needed / {fmt(Math.max(free, 0))} free
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-surface-2">
        <div
          className={clsx(
            'h-full',
            pct >= 100 ? 'bg-danger' : pct > 80 ? 'bg-warning' : 'bg-success',
          )}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

function DrainTab({ clusterId, targets }: { clusterId: string; targets: SimTargets | null }) {
  const [node, setNode] = useState('')
  const [res, setRes] = useState<DrainResult | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    setErr('')
    try {
      setRes(await insight.simDrain(clusterId, node))
    } catch (e) {
      setRes(null)
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const tone = res?.verdict === 'safe' ? 'ok' : res?.verdict === 'disruptive' ? 'warn' : 'error'
  return (
    <div className="space-y-4 p-4">
      <p className="max-w-3xl text-[13px] text-muted">
        What would <code className="font-mono">kubectl drain</code> do to this node? Pods,
        PodDisruptionBudgets, spare capacity and autoscalers are checked. Nothing is cordoned or
        evicted.
      </p>
      <div className="flex items-end gap-2">
        <div className="flex flex-col gap-1">
          <span className="text-[11px] font-semibold uppercase text-muted">Node</span>
          <select
            aria-label="Node"
            value={node}
            onChange={(e) => setNode(e.target.value)}
            className={clsx(field, 'w-64 cursor-pointer')}
          >
            <option value="">Select a node…</option>
            {(targets?.nodes ?? []).map((n) => (
              <option key={n}>{n}</option>
            ))}
          </select>
        </div>
        <button
          disabled={!node || busy}
          onClick={() => void run()}
          className="no-drag flex h-8 cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-fg disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Play size={13} /> {busy ? 'Simulating…' : 'Simulate drain'}
        </button>
      </div>
      {err && <div className="select-text text-[13px] text-danger">{err}</div>}
      {res && (
        <div className="max-w-5xl space-y-4">
          <Banner tone={tone}>
            <div className="font-semibold capitalize">{res.verdict}</div>
            {res.summary.map((s) => (
              <div key={s}>{s}</div>
            ))}
          </Banner>
          <div className="flex flex-wrap gap-6">
            <Meter
              label="CPU"
              need={res.capacity.needCpuM}
              free={res.capacity.freeCpuM}
              fmt={formatCPU}
            />
            <Meter
              label="Memory"
              need={res.capacity.needMem}
              free={res.capacity.freeMem}
              fmt={formatBytes}
            />
          </div>

          <section>
            <h3 className="mb-1 text-xs font-semibold uppercase text-muted">
              Pods on {res.node} ({res.pods.length})
            </h3>
            <div className="divide-y divide-border rounded-lg border border-border">
              {res.pods.map((p) => (
                <div
                  key={p.namespace + p.name}
                  className="flex items-start gap-2 px-3 py-2 text-[13px]"
                >
                  <SevIcon s={p.action === 'skip' ? 'ok' : p.severity} />
                  <div className="min-w-0 flex-1">
                    <div className="font-mono">
                      {p.namespace}/{p.name}
                    </div>
                    <div className="select-text text-muted">{p.reason}</div>
                  </div>
                  {p.action === 'evict' && p.movesTo && (
                    <span className="flex shrink-0 items-center gap-1 text-xs text-muted">
                      <ArrowRight size={12} /> {p.movesTo}
                    </span>
                  )}
                </div>
              ))}
              {res.pods.length === 0 && (
                <div className="px-3 py-2 text-[13px] text-muted">No pods on this node.</div>
              )}
            </div>
          </section>

          {res.pdbs.length > 0 && (
            <section>
              <h3 className="mb-1 text-xs font-semibold uppercase text-muted">
                PodDisruptionBudgets
              </h3>
              <div className="divide-y divide-border rounded-lg border border-border">
                {res.pdbs.map((b) => (
                  <div
                    key={b.namespace + b.name}
                    className="flex items-start gap-2 px-3 py-2 text-[13px]"
                  >
                    <SevIcon s={b.severity} />
                    <div className="min-w-0">
                      <div>
                        <span className="font-mono">
                          {b.namespace}/{b.name}
                        </span>{' '}
                        <span className="text-muted">
                          ({b.rule}, {b.healthy}/{b.matching} healthy)
                        </span>
                      </div>
                      <div className="select-text text-muted">{b.message}</div>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          )}

          {res.hpas.length > 0 && (
            <section>
              <h3 className="mb-1 text-xs font-semibold uppercase text-muted">
                Autoscaling after the drain
              </h3>
              <div className="divide-y divide-border rounded-lg border border-border">
                {res.hpas.map((h) => (
                  <div
                    key={h.namespace + h.name}
                    className="flex items-start gap-2 px-3 py-2 text-[13px]"
                  >
                    <SevIcon s={h.severity} />
                    <div className="min-w-0">
                      <div>
                        <span className="font-mono">
                          {h.namespace}/{h.name}
                        </span>{' '}
                        <span className="text-muted">
                          → {h.target} ({h.current}/{h.max})
                        </span>
                      </div>
                      <div className="select-text text-muted">{h.message}</div>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          )}
          {res.notes.map((n) => (
            <div key={n} className="text-xs text-muted">
              {n}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export function Simulator({ clusterId }: { clusterId: string }) {
  const [tab, setTab] = useState<Tab>('network')
  const { data: targets, error } = usePoll(() => insight.simTargets(clusterId), clusterId, 0)
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4">
        <h1 className="text-sm font-semibold">Simulator</h1>
        {(['network', 'drain'] as Tab[]).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={clsx(
              'no-drag h-8 cursor-pointer rounded-md px-3 text-[13px] font-medium',
              tab === t
                ? 'bg-accent/15 text-accent'
                : 'text-muted hover:bg-surface-hover hover:text-fg',
            )}
          >
            {t === 'network' ? 'Network policy' : 'Node drain'}
          </button>
        ))}
      </div>
      {error && !targets && <div className="p-4 text-[13px] text-danger">{error}</div>}
      <div className="min-h-0 flex-1 overflow-auto">
        {tab === 'network' ? (
          <NetworkTab clusterId={clusterId} targets={targets} />
        ) : (
          <DrainTab clusterId={clusterId} targets={targets} />
        )}
      </div>
    </div>
  )
}
