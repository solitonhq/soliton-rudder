import clsx from 'clsx'
import { AlertTriangle, CheckCircle2, Info, XCircle } from 'lucide-react'
import { useState } from 'react'
import { insight } from '../lib/backend'
import { formatAge } from '../lib/format'
import { formatBytes, formatCPU, formatPercent } from '../lib/units'
import { usePoll } from '../lib/usePoll'
import type { HealthReport, Resource } from '../lib/types'
import type { RowRef } from '../store/explorer'
import { useExplorer } from '../store/explorer'
import { Chart } from './Chart'

interface Props {
  clusterId: string
  resource: Resource
  target: RowRef
}

const statusStyle: Record<HealthReport['status'], string> = {
  healthy: 'bg-success/15 text-success',
  degraded: 'bg-warning/15 text-warning',
  failing: 'bg-danger/15 text-danger',
  unknown: 'bg-surface-2 text-muted',
}

const checkIcon = {
  ok: <CheckCircle2 size={14} className="text-success" />,
  warn: <AlertTriangle size={14} className="text-warning" />,
  fail: <XCircle size={14} className="text-danger" />,
}

export function HealthCard({ clusterId, resource, target }: Props) {
  const { data, error } = usePoll(
    () => insight.health(clusterId, resource.key, target.ns, target.name),
    `${clusterId}/${resource.key}/${target.ns}/${target.name}`,
    10_000,
  )
  if (error && !data) return null
  if (!data || data.checks.length === 0) return null
  return (
    <section className="mb-5 rounded-lg border border-border bg-bg p-3">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted">Health</h3>
        <span
          className={clsx(
            'rounded-full px-2.5 py-0.5 text-xs font-semibold capitalize',
            statusStyle[data.status],
          )}
        >
          {data.status} · {data.summary}
        </span>
      </div>
      <ul className="space-y-1.5">
        {data.checks.map((c, i) => (
          <li key={i} className="flex gap-2 text-[13px]">
            <span className="mt-0.5 shrink-0">{checkIcon[c.status]}</span>
            <span>
              <span className="font-medium">{c.name}</span>
              {c.detail && <span className="text-muted"> — {c.detail}</span>}
            </span>
          </li>
        ))}
      </ul>
      {data.hints.length > 0 && (
        <div className="mt-3 space-y-1.5 rounded-md bg-accent/10 p-2.5">
          {data.hints.map((h, i) => (
            <div key={i} className="flex gap-2 text-[13px]">
              <Info size={14} className="mt-0.5 shrink-0 text-accent" />
              <span>{h}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

const METRIC_KINDS = new Set([
  'pods',
  'nodes',
  'deployments',
  'statefulsets',
  'daemonsets',
  'replicasets',
  'jobs',
])

const HISTORY_RANGES: [string, number][] = [
  ['Live', 0],
  ['1h', 3600],
  ['6h', 21600],
  ['24h', 86400],
  ['7d', 604800],
]

export function MetricsSection({ clusterId, resource, target }: Props) {
  const enabled = METRIC_KINDS.has(resource.name)
  const prom = useExplorer((s) => s.integrations?.prometheusConfigured ?? false)
  const [range, setRange] = useState(0)
  const canHistory = prom && resource.name !== 'nodes'
  const effRange = canHistory ? range : 0
  const { data, error } = usePoll(
    () =>
      enabled
        ? insight.series(clusterId, resource.key, target.ns, target.name, effRange)
        : Promise.resolve(null),
    `${clusterId}/${resource.key}/${target.ns}/${target.name}/${effRange}`,
    effRange > 0 ? 60_000 : 15_000,
  )
  if (!enabled) return null
  if (error && !data) return <p className="mb-5 text-xs text-muted">Metrics unavailable: {error}</p>
  if (!data) return null
  if (!data.available) {
    return (
      <p className="mb-5 text-xs text-muted">
        Metrics unavailable{data.error ? `: ${data.error}` : ''}
      </p>
    )
  }
  const last = data.points[data.points.length - 1]
  const isNode = resource.name === 'nodes'
  const cpuRefs = isNode
    ? []
    : [
        { label: 'request', value: data.cpuRequest, color: 'var(--warning)' },
        { label: 'limit', value: data.cpuLimit, color: 'var(--danger)' },
      ]
  const memRefs = isNode
    ? []
    : [
        { label: 'request', value: data.memRequest, color: 'var(--warning)' },
        { label: 'limit', value: data.memLimit, color: 'var(--danger)' },
      ]
  return (
    <section className="mb-5">
      <div className="mb-2 flex items-baseline justify-between">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted">Usage</h3>
        {canHistory && (
          <div className="flex gap-0.5 rounded-md border border-border p-0.5">
            {HISTORY_RANGES.map(([l, v]) => (
              <button
                key={l}
                onClick={() => setRange(v)}
                className={clsx(
                  'cursor-pointer rounded px-2 py-0.5 text-[11px] font-medium',
                  range === v ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
                )}
              >
                {l}
              </button>
            ))}
          </div>
        )}
        <span className="text-[11px] text-muted">
          {data.members > 1 ? `${data.members} pods · ` : ''}
          {data.source === 'prometheus' ? 'Prometheus · ' : ''}
          {data.points.length > 1
            ? `last ${Math.round((data.points.length * data.intervalSecs) / 60)} min`
            : 'collecting…'}
        </span>
      </div>
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
        <Chart
          title="CPU"
          color="var(--accent)"
          points={data.points.map((p) => ({ t: p.t, v: p.cpu }))}
          format={formatCPU}
          refs={cpuRefs}
          capacity={data.cpuCapacity}
        />
        <Chart
          title="Memory"
          color="var(--success)"
          points={data.points.map((p) => ({ t: p.t, v: p.mem }))}
          format={formatBytes}
          refs={memRefs}
          capacity={data.memCapacity}
        />
      </div>
      {last && (
        <p className="mt-2 text-xs text-muted">
          {isNode
            ? `CPU ${formatPercent(last.cpu, data.cpuCapacity)} of ${formatCPU(data.cpuCapacity)} · Memory ${formatPercent(last.mem, data.memCapacity)} of ${formatBytes(data.memCapacity)}`
            : data.cpuRequest > 0 || data.memRequest > 0
              ? `Of requests — CPU ${formatPercent(last.cpu, data.cpuRequest) || '—'} · Memory ${formatPercent(last.mem, data.memRequest) || '—'}`
              : 'No resource requests set'}
        </p>
      )}
    </section>
  )
}

export function Timeline({ clusterId, resource, target }: Props) {
  const { data, error } = usePoll(
    () => insight.timeline(clusterId, resource.key, target.ns, target.name),
    `${clusterId}/${resource.key}/${target.ns}/${target.name}`,
    10_000,
  )
  const events = data ?? []
  return (
    <section className="mt-5">
      <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
        Timeline ({events.length})
      </h3>
      {error && !data ? (
        <div className="text-xs text-muted">{error}</div>
      ) : events.length === 0 ? (
        <div className="text-xs text-muted">No recent events</div>
      ) : (
        <ol className="relative ml-1.5 border-l border-border">
          {events.map((e, i) => {
            const warn = e.type === 'Warning'
            const own = e.object.endsWith('/' + target.name)
            return (
              <li key={i} className="relative pb-3 pl-4">
                <span
                  className={clsx(
                    'absolute -left-[5px] top-1.5 size-2.5 rounded-full ring-2 ring-surface',
                    warn ? 'bg-warning' : 'bg-success',
                  )}
                />
                <div className="flex items-center justify-between gap-2 text-xs">
                  <span className={clsx('font-semibold', warn && 'text-warning')}>{e.reason}</span>
                  <span className="shrink-0 text-muted">
                    {e.count > 1 && `×${e.count} · `}
                    {formatAge(e.last)} ago
                  </span>
                </div>
                {!own && <div className="font-mono text-[11px] text-muted">{e.object}</div>}
                <div className="mt-0.5 break-words text-[13px]">{e.message}</div>
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}
