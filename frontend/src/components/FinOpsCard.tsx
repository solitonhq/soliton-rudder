import clsx from 'clsx'
import { Wrench } from 'lucide-react'
import { useState } from 'react'
import { FixDialog } from '../dialogs/FixDialog'
import type { FinOps, FinOpsWorkload } from '../lib/types'
import { formatBytes, formatCPU, formatUSD } from '../lib/units'
import { useUI } from '../store/ui'

const confLabel = { high: 'High confidence', low: 'Low confidence', none: 'No usage data' } as const
const confCls = {
  high: 'bg-success/15 text-success',
  low: 'bg-warning/15 text-warning',
  none: 'bg-surface-2 text-muted',
} as const

function Stat({
  label,
  value,
  sub,
  tone,
}: {
  label: string
  value: string
  sub?: string
  tone?: string
}) {
  return (
    <div className="rounded-xl border border-border bg-surface px-4 py-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className={clsx('mt-1 text-2xl font-semibold', tone)}>{value}</div>
      {sub && <div className="mt-0.5 text-xs text-muted">{sub}</div>}
    </div>
  )
}

/** Namespace-level cost and potential savings from the latest assessment. */
export function FinOpsCard({
  finops,
  clusterId,
  onFixed,
}: {
  finops: FinOps
  clusterId: string
  onFixed?: () => void
}) {
  const openDialog = useUI((s) => s.open)
  const [all, setAll] = useState(false)
  const [notes, setNotes] = useState(false)
  const [fixing, setFixing] = useState<FinOpsWorkload | null>(null)
  const nss = all ? finops.namespaces : finops.namespaces.slice(0, 12)
  const maxCost = Math.max(1, ...finops.namespaces.map((n) => n.monthlyCostUsd))
  const pct =
    finops.monthlyCostUsd > 0
      ? Math.round((finops.potentialSaveUsd / finops.monthlyCostUsd) * 100)
      : 0

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <Stat
          label="Estimated monthly cost"
          value={formatUSD(finops.monthlyCostUsd)}
          sub="of requested resources"
        />
        <Stat
          label="Potential monthly savings"
          value={finops.confidence === 'none' ? '—' : formatUSD(finops.potentialSaveUsd)}
          sub={finops.confidence === 'none' ? 'needs usage data' : `${pct}% of the estimate`}
          tone={finops.potentialSaveUsd > 0 ? 'text-success' : undefined}
        />
        <div className="rounded-xl border border-border bg-surface px-4 py-3">
          <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">Basis</div>
          <div className="mt-1.5">
            <span
              className={clsx(
                'rounded-full px-2.5 py-0.5 text-xs font-semibold',
                confCls[finops.confidence],
              )}
            >
              {confLabel[finops.confidence]}
            </span>
          </div>
          <div className="mt-1.5 text-xs text-muted">{finops.source || 'requests only'}</div>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-border bg-surface">
        <div className="grid grid-cols-[1fr_90px_130px_130px_1.3fr] gap-3 border-b border-border px-4 py-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
          <span>Namespace</span>
          <span className="text-right">Pods</span>
          <span>Requested</span>
          <span>Used</span>
          <span>Monthly cost · savings</span>
        </div>
        {nss.map((n) => (
          <div
            key={n.namespace}
            className="grid grid-cols-[1fr_90px_130px_130px_1.3fr] items-center gap-3 border-b border-border/50 px-4 py-2 text-[13px] last:border-0"
          >
            <span className="truncate font-medium" title={n.namespace}>
              {n.namespace}
              {n.unrequestedPods > 0 && (
                <span
                  className="ml-2 text-[11px] font-normal text-warning"
                  title="Pods without requests are costed from their usage"
                >
                  {n.unrequestedPods} without requests
                </span>
              )}
            </span>
            <span className="text-right font-mono text-xs">{n.pods}</span>
            <span className="font-mono text-xs text-muted">
              {formatCPU(n.requestedCpuM)} · {formatBytes(n.requestedMem)}
            </span>
            <span className="font-mono text-xs text-muted">
              {finops.confidence === 'none'
                ? '—'
                : `${formatCPU(n.usedCpuM)} · ${formatBytes(n.usedMem)}`}
            </span>
            <span className="flex items-center gap-2">
              <span
                className="relative h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-surface-2"
                aria-hidden
              >
                <span
                  className="absolute inset-y-0 left-0 rounded-full bg-accent/50"
                  style={{ width: `${(n.monthlyCostUsd / maxCost) * 100}%` }}
                />
                <span
                  className="absolute inset-y-0 left-0 rounded-full bg-success"
                  style={{ width: `${(n.potentialSaveUsd / maxCost) * 100}%` }}
                />
              </span>
              <span className="w-28 shrink-0 text-right font-mono text-xs">
                {formatUSD(n.monthlyCostUsd)}
                {n.potentialSaveUsd >= 0.5 && (
                  <span className="text-success"> −{formatUSD(n.potentialSaveUsd)}</span>
                )}
              </span>
            </span>
          </div>
        ))}
        {finops.namespaces.length === 0 && (
          <div className="p-6 text-center text-xs text-muted">No running pods.</div>
        )}
        {finops.namespaces.length > 12 && (
          <button
            onClick={() => setAll(!all)}
            className="w-full cursor-pointer border-t border-border py-2 text-xs text-accent hover:bg-surface-hover"
          >
            {all ? 'Show fewer' : `Show all ${finops.namespaces.length} namespaces`}
          </button>
        )}
      </div>

      {finops.top.length > 0 && (
        <div className="overflow-hidden rounded-xl border border-border bg-surface">
          <div className="border-b border-border px-4 py-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
            Biggest savings opportunities
          </div>
          {finops.top.map((w) => (
            <div
              key={w.namespace + w.kind + w.name}
              className="flex items-center gap-3 border-b border-border/50 px-4 py-2 text-[13px] last:border-0"
            >
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">
                  {w.name}{' '}
                  <span className="font-normal text-muted">
                    · {w.kind} · {w.namespace} · {w.pods} pod{w.pods === 1 ? '' : 's'}
                  </span>
                </div>
                <div className="text-xs text-muted">
                  requests {formatCPU(w.requestedCpuM)} / {formatBytes(w.requestedMem)}, using{' '}
                  {formatCPU(w.usedCpuM)} / {formatBytes(w.usedMem)}
                </div>
              </div>
              <span className="font-mono text-xs text-success">
                −{formatUSD(w.potentialSaveUsd)}/mo
              </span>
              {w.resourceKey &&
                ['Deployment', 'StatefulSet', 'DaemonSet', 'CronJob'].includes(w.kind) && (
                  <button
                    onClick={() => setFixing(w)}
                    className="flex cursor-pointer items-center gap-1 rounded-md border border-border px-2 py-1 text-xs hover:border-accent hover:text-accent"
                  >
                    <Wrench size={12} /> Right-size…
                  </button>
                )}
            </div>
          ))}
        </div>
      )}

      <div className="text-xs text-muted">
        <button
          onClick={() => setNotes(!notes)}
          className="cursor-pointer text-accent hover:underline"
        >
          {notes ? 'Hide' : 'How is this calculated?'}
        </button>
        {' · '}
        <button
          onClick={() => openDialog({ kind: 'settings', tab: 'doctor' })}
          className="cursor-pointer text-accent hover:underline"
        >
          Edit prices and headroom
        </button>
        {notes && (
          <ul className="mt-2 list-disc space-y-1 pl-5">
            {finops.notes.map((n, i) => (
              <li key={i}>{n}</li>
            ))}
            <li>
              Right-sizing keeps {finops.headroomPct}% headroom above observed usage (minimum 10m
              CPU / 32Mi memory per pod). System namespaces are never right-sized.
            </li>
          </ul>
        )}
      </div>
      {fixing && (
        <FixDialog
          clusterId={clusterId}
          ruleId="finops-rightsize"
          resourceKey={fixing.resourceKey}
          ns={fixing.namespace}
          name={fixing.name}
          onClose={() => setFixing(null)}
          onApplied={onFixed}
        />
      )}
    </div>
  )
}
