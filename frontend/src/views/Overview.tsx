import clsx from 'clsx'
import {
  AlertTriangle,
  BookOpen,
  CheckCircle2,
  ClipboardCheck,
  ExternalLink,
  XCircle,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { RunbookDialog } from '../dialogs/RunbookDialog'
import { assess, blackbox, insight } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { ClusterOverview, FzSummary, Incident, OverviewAttention } from '../lib/types'
import { formatBytes, formatCPU } from '../lib/units'
import { usePoll } from '../lib/usePoll'
import { useClusters } from '../store/clusters'
import { useExplorer } from '../store/explorer'
import { useForwards } from '../store/forwards'
import { useUI } from '../store/ui'

type Tone = 'ok' | 'warn' | 'error' | 'none'
const toneText: Record<Tone, string> = {
  ok: 'text-success',
  warn: 'text-warning',
  error: 'text-danger',
  none: 'text-fg',
}

function Stat({
  label,
  value,
  of,
  sub,
  tone = 'none',
}: {
  label: string
  value: number | string
  of?: number
  sub?: ReactNode
  tone?: Tone
}) {
  return (
    <div className="rounded-xl border border-border bg-surface p-4">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className={clsx('mt-1 text-2xl font-semibold tabular-nums', toneText[tone])}>
        {value}
        {of !== undefined && <span className="text-base font-normal text-muted"> / {of}</span>}
      </div>
      <div className="mt-0.5 min-h-4 text-xs text-muted">{sub}</div>
    </div>
  )
}

const pct = (part: number, total: number) =>
  total > 0 ? Math.min(100, Math.round((part / total) * 100)) : 0

/** A capacity bar: what is requested (translucent) and, when metrics are available, what is actually used (solid). */
export function CapacityBar({
  label,
  alloc,
  requested,
  used,
  fmt,
}: {
  label: string
  alloc: number
  requested: number
  used: number | null
  fmt: (n: number) => string
}) {
  const req = pct(requested, alloc)
  const use = used === null ? null : pct(used, alloc)
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between text-xs">
        <span className="font-medium">{label}</span>
        <span className="text-muted">{fmt(alloc)} allocatable</span>
      </div>
      <div
        className="relative h-3 overflow-hidden rounded-full bg-surface-2"
        role="img"
        aria-label={`${label}: ${req}% requested${use === null ? '' : `, ${use}% used`}`}
      >
        <div
          className={clsx(
            'absolute inset-y-0 left-0',
            req >= 90 ? 'bg-danger/35' : req >= 75 ? 'bg-warning/35' : 'bg-accent/30',
          )}
          style={{ width: `${req}%` }}
        />
        {use !== null && (
          <div className="absolute inset-y-0 left-0 bg-accent" style={{ width: `${use}%` }} />
        )}
      </div>
      <div className="mt-1 flex gap-4 text-xs text-muted">
        <span>
          <span className="font-medium text-fg">{req}%</span> requested ({fmt(requested)})
        </span>
        {use !== null && (
          <span>
            <span className="font-medium text-fg">{use}%</span> used ({fmt(used!)})
          </span>
        )}
      </div>
    </div>
  )
}

function Sparkline({ values }: { values: number[] }) {
  if (values.length < 2) return null
  const w = 120
  const h = 28
  const pts = values
    .map(
      (v, i) => `${(i / (values.length - 1)) * w},${h - (Math.max(0, Math.min(100, v)) / 100) * h}`,
    )
    .join(' ')
  return (
    <svg width={w} height={h} className="overflow-visible" role="img" aria-label="Score trend">
      <polyline points={pts} fill="none" stroke="var(--color-accent)" strokeWidth={1.5} />
    </svg>
  )
}

function scoreTone(score: number): Tone {
  return score >= 85 ? 'ok' : score >= 60 ? 'warn' : 'error'
}

function Card({
  title,
  action,
  children,
  className,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={clsx('rounded-xl border border-border bg-surface', className)}>
      <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
        <h2 className="text-[11px] font-semibold uppercase tracking-wide text-muted">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  )
}

const linkBtn = 'cursor-pointer text-xs text-accent hover:underline'

function AttentionRow({
  a,
  onRunbook,
  onOpen,
}: {
  a: OverviewAttention
  onRunbook: () => void
  onOpen: () => void
}) {
  return (
    <li className="flex items-center gap-2 px-4 py-2 text-[13px]">
      {a.severity === 'error' ? (
        <XCircle size={14} className="shrink-0 text-danger" />
      ) : (
        <AlertTriangle size={14} className="shrink-0 text-warning" />
      )}
      <span className="w-20 shrink-0 truncate text-[10px] font-semibold uppercase text-muted">
        {a.kind}
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-xs">
          {a.namespace && <span className="text-muted">{a.namespace}/</span>}
          {a.name}
        </div>
        <div className="select-text truncate text-xs text-muted" title={a.reason}>
          {a.reason}
        </div>
      </div>
      <button
        className={clsx(linkBtn, 'flex items-center gap-1')}
        onClick={onRunbook}
        title="Step-by-step guide for this problem"
      >
        <BookOpen size={12} /> Runbook
      </button>
      {a.resourceKey && (
        <button
          className={clsx(linkBtn, 'flex items-center gap-1 text-muted hover:text-accent')}
          onClick={onOpen}
          aria-label={`Open ${a.name}`}
        >
          <ExternalLink size={12} />
        </button>
      )}
    </li>
  )
}

export function Overview({ clusterId }: { clusterId: string }) {
  const cluster = useClusters((s) => s.clusters.find((c) => c.id === clusterId))
  const setView = useExplorer((s) => s.setView)
  const jump = useExplorer((s) => s.jump)
  const forwards = useForwards(
    (s) => s.items.filter((f) => f.clusterId === clusterId && f.status === 'active').length,
  )
  const [incident, setIncident] = useState<Incident | null>(null)
  const [showAll, setShowAll] = useState(false)

  const ov = usePoll<ClusterOverview>(() => insight.overview(clusterId), clusterId, 15_000)
  const runs = usePoll<FzSummary[]>(() => assess.list(clusterId), clusterId, 60_000)
  const bb = usePoll(() => blackbox.info(clusterId), clusterId, 30_000)
  const o = ov.data

  const open = (key: string, name: string) => {
    setView('resources')
    void jump(key, name)
  }

  if (ov.error && !o) return <div className="p-6 text-[13px] text-danger">{ov.error}</div>
  if (!o) return <div className="p-6 text-[13px] text-muted">Reading the cluster…</div>

  const last = runs.data && runs.data.length > 0 ? runs.data[runs.data.length - 1] : null
  const unhealthyNodes = o.nodes.notReady > 0
  const podTone: Tone = o.pods.failed > 0 || o.pods.notReady > 0 ? 'warn' : 'ok'
  const wlTone: Tone = o.workloads.failing > 0 ? 'error' : o.workloads.degraded > 0 ? 'warn' : 'ok'
  const attention = showAll ? o.attention : o.attention.slice(0, 8)
  const problemPods = o.pods.notReady + o.pods.pending + o.pods.failed

  return (
    <div className="h-full overflow-auto">
      <div className="mx-auto max-w-[1180px] space-y-4 p-6">
        {!o.access.clusterWide && (
          <div
            role="status"
            className={clsx(
              'flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border px-3 py-2 text-[13px]',
              o.access.source === 'none'
                ? 'border-danger/40 bg-danger/10'
                : 'border-warning/40 bg-warning/10',
            )}
          >
            <span className="font-medium">Limited access</span>
            <span className="min-w-0 flex-1 text-muted">
              {o.access.namespaces.length > 0
                ? `These credentials can read ${o.access.namespaces.length} namespace${o.access.namespaces.length === 1 ? '' : 's'}; the figures below cover only those.`
                : o.access.note}
            </span>
            <button
              className={linkBtn}
              onClick={() => useUI.getState().open({ kind: 'access', clusterId })}
            >
              Choose namespaces…
            </button>
          </div>
        )}
        <header className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h1 className="text-lg font-semibold">{cluster?.name ?? 'Cluster'}</h1>
          {o.serverVersion && (
            <span className="rounded bg-surface-2 px-2 py-0.5 font-mono text-xs text-muted">
              {o.serverVersion}
            </span>
          )}
          <span className="text-xs text-muted">Overview · refreshes every 15 s</span>
        </header>

        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
          <Stat
            label="Nodes"
            value={o.nodes.ready}
            of={o.nodes.total}
            tone={
              unhealthyNodes ? 'error' : o.nodes.cordoned + o.nodes.pressure > 0 ? 'warn' : 'ok'
            }
            sub={
              unhealthyNodes || o.nodes.cordoned > 0 || o.nodes.pressure > 0
                ? [
                    unhealthyNodes && `${o.nodes.notReady} not ready`,
                    o.nodes.cordoned > 0 && `${o.nodes.cordoned} cordoned`,
                    o.nodes.pressure > 0 && `${o.nodes.pressure} under pressure`,
                  ]
                    .filter(Boolean)
                    .join(' · ')
                : 'all ready'
            }
          />
          <Stat
            label="Pods"
            value={o.pods.ready}
            of={o.pods.total - o.pods.succeeded}
            tone={podTone}
            sub={
              problemPods > 0
                ? [
                    o.pods.notReady > 0 && `${o.pods.notReady} not ready`,
                    o.pods.pending > 0 && `${o.pods.pending} pending`,
                    o.pods.failed > 0 && `${o.pods.failed} failed`,
                  ]
                    .filter(Boolean)
                    .join(' · ')
                : 'all ready'
            }
          />
          <Stat
            label="Workloads"
            value={o.workloads.healthy}
            of={o.workloads.total}
            tone={wlTone}
            sub={
              o.workloads.failing + o.workloads.degraded > 0
                ? [
                    o.workloads.failing > 0 && `${o.workloads.failing} failing`,
                    o.workloads.degraded > 0 && `${o.workloads.degraded} degraded`,
                  ]
                    .filter(Boolean)
                    .join(' · ')
                : 'Deployments, StatefulSets, DaemonSets'
            }
          />
          <Stat label="Namespaces" value={o.namespaces} />
          <Stat
            label="Warnings (1 h)"
            value={o.warnings.length}
            tone={o.warnings.length > 0 ? 'warn' : 'ok'}
            sub={o.warnings.length > 0 ? 'distinct problems in events' : 'no warning events'}
          />
        </div>

        <div className="grid gap-4 lg:grid-cols-5">
          <Card title="Capacity" className="lg:col-span-3">
            <div className="space-y-4 p-4">
              <CapacityBar
                label="CPU"
                alloc={o.capacity.cpuAllocM}
                requested={o.capacity.cpuRequestedM}
                used={o.capacity.hasUsage ? o.capacity.cpuUsedM : null}
                fmt={formatCPU}
              />
              <CapacityBar
                label="Memory"
                alloc={o.capacity.memAlloc}
                requested={o.capacity.memRequested}
                used={o.capacity.hasUsage ? o.capacity.memUsed : null}
                fmt={formatBytes}
              />
              {!o.capacity.hasUsage && (
                <p className="text-xs text-muted">
                  Live usage needs metrics-server; only requests are shown.
                </p>
              )}
            </div>
          </Card>
          <Card
            title="Assessment"
            className="lg:col-span-2"
            action={
              <button className={linkBtn} onClick={() => setView('reports')}>
                {last ? 'Open' : 'Run one'}
              </button>
            }
          >
            <div className="flex items-center gap-4 p-4">
              {last ? (
                <>
                  <div>
                    <div
                      className={clsx(
                        'text-3xl font-semibold tabular-nums',
                        toneText[scoreTone(last.score)],
                      )}
                    >
                      {last.score}
                    </div>
                    <div className="text-xs text-muted">
                      {formatAge(new Date(last.startedAt * 1000).toISOString())} ago
                    </div>
                  </div>
                  <Sparkline values={(runs.data ?? []).slice(-20).map((r) => r.score)} />
                </>
              ) : (
                <div className="flex items-center gap-2 text-[13px] text-muted">
                  <ClipboardCheck size={16} /> No assessment yet: it scores security, reliability
                  and cost.
                </div>
              )}
            </div>
          </Card>
        </div>

        <div className="grid gap-4 lg:grid-cols-2">
          <Card
            title={`Needs attention${o.attentionTotal > 0 ? ` (${o.attentionTotal})` : ''}`}
            action={
              o.attention.length > 8 ? (
                <button className={linkBtn} onClick={() => setShowAll(!showAll)}>
                  {showAll ? 'Show fewer' : `Show ${o.attention.length}`}
                </button>
              ) : undefined
            }
          >
            {o.attention.length === 0 ? (
              <div className="flex items-center gap-2 p-4 text-[13px] text-success">
                <CheckCircle2 size={16} /> Nothing needs attention right now.
              </div>
            ) : (
              <ul className="divide-y divide-border">
                {attention.map((a) => (
                  <AttentionRow
                    key={`${a.kind}/${a.namespace}/${a.name}`}
                    a={a}
                    onRunbook={() =>
                      setIncident({
                        kind: a.kind,
                        namespace: a.namespace,
                        name: a.name,
                        reason: a.reason,
                        status: a.severity,
                      })
                    }
                    onOpen={() => open(a.resourceKey, a.name)}
                  />
                ))}
              </ul>
            )}
            {o.attentionTotal > o.attention.length && showAll && (
              <p className="border-t border-border px-4 py-2 text-xs text-muted">
                Showing the first {o.attention.length} of {o.attentionTotal}.
              </p>
            )}
          </Card>

          <Card title="Recent warnings">
            {o.warnings.length === 0 ? (
              <div className="p-4 text-[13px] text-muted">No warning events in the last hour.</div>
            ) : (
              <ul className="max-h-[420px] divide-y divide-border overflow-y-auto">
                {o.warnings.map((w) => {
                  const body = (
                    <>
                      <div className="flex items-baseline gap-2">
                        <span className="text-[13px] font-medium text-warning">{w.reason}</span>
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">
                          {w.kind} {w.namespace && `${w.namespace}/`}
                          {w.name}
                        </span>
                        <span className="shrink-0 text-[11px] text-muted">
                          {w.count > 1 && `×${w.count} · `}
                          {formatAge(new Date(w.at).toISOString())}
                        </span>
                      </div>
                      <div className="select-text truncate text-xs text-muted" title={w.message}>
                        {w.message}
                      </div>
                    </>
                  )
                  return (
                    <li key={`${w.kind}/${w.namespace}/${w.name}/${w.reason}`}>
                      {w.resourceKey ? (
                        <button
                          className="block w-full cursor-pointer px-4 py-2 text-left hover:bg-surface-hover"
                          onClick={() => open(w.resourceKey, w.name)}
                          title={`Open ${w.name}`}
                        >
                          {body}
                        </button>
                      ) : (
                        <div className="px-4 py-2">{body}</div>
                      )}
                    </li>
                  )
                })}
              </ul>
            )}
          </Card>
        </div>

        <div className="flex flex-wrap gap-x-6 gap-y-1 px-1 text-xs text-muted">
          <button className="cursor-pointer hover:text-accent" onClick={() => setView('forwards')}>
            {forwards} active port forward{forwards === 1 ? '' : 's'}
          </button>
          <button className="cursor-pointer hover:text-accent" onClick={() => setView('blackbox')}>
            Blackbox:{' '}
            {bb.data?.config.enabled
              ? bb.data.status.lastError
                ? 'recording, with problems'
                : 'recording'
              : 'not recording'}
          </button>
          {o.notes
            .filter((n) => !n.startsWith('Limited access:')) // the banner above says it
            .map((n) => (
              <span key={n} title={n}>
                {n}
              </span>
            ))}
        </div>
      </div>
      {incident && (
        <RunbookDialog
          clusterId={clusterId}
          incident={incident}
          onClose={() => setIncident(null)}
        />
      )}
    </div>
  )
}
