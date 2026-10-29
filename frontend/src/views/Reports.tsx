import clsx from 'clsx'
import {
  Wrench,
  ChevronDown,
  ChevronRight,
  Download,
  Play,
  Search,
  Square,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Chart } from '../components/Chart'
import { FinOpsCard } from '../components/FinOpsCard'
import { FixDialog } from '../dialogs/FixDialog'
import { useFixableRules } from '../lib/doctor'
import { Button } from '../components/ui'
import { assess } from '../lib/backend'
import { catLabel, scoreColor, scoreLabel, sevChip, sevRank, SEVERITIES } from '../lib/fz'
import type {
  FzCategory,
  FzDiff,
  FzFinding,
  FzReport,
  FzRule,
  FzSummary,
  Severity,
} from '../lib/types'
import { useExplorer } from '../store/explorer'
import { useReports } from '../store/reports'
import { useUI } from '../store/ui'

const CATS: FzCategory[] = ['resources', 'reliability', 'security', 'maintenance']
const fmtDate = (ts: number) =>
  new Date(ts * 1000).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })

function Gauge({ score, size = 132 }: { score: number; size?: number }) {
  const r = (size - 16) / 2
  const c = 2 * Math.PI * r
  return (
    <div
      className="relative shrink-0"
      style={{ width: size, height: size }}
      role="img"
      aria-label={`Score ${score} out of 100`}
    >
      <svg width={size} height={size} className="-rotate-90">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="var(--border)"
          strokeWidth="9"
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={scoreColor(score)}
          strokeWidth="9"
          strokeLinecap="round"
          strokeDasharray={`${(score / 100) * c} ${c}`}
          style={{ transition: 'stroke-dasharray .6s ease' }}
        />
      </svg>
      <div className="absolute inset-0 grid place-items-center text-center">
        <div>
          <div className="text-4xl font-bold leading-none" style={{ color: scoreColor(score) }}>
            {score}
          </div>
          <div className="mt-1 text-[11px] text-muted">{scoreLabel(score)}</div>
        </div>
      </div>
    </div>
  )
}

function Delta({ v, className }: { v: number; className?: string }) {
  if (v === 0) return <span className={clsx('text-muted', className)}>±0</span>
  return (
    <span className={clsx(v > 0 ? 'text-success' : 'text-danger', className)}>
      {v > 0 ? `+${v}` : v}
    </span>
  )
}

function Hero({ report, prev }: { report: FzReport; prev?: FzSummary }) {
  const toast = useUI((s) => s.toast)
  const [menu, setMenu] = useState(false)
  const doExport = async (f: 'json' | 'html') => {
    setMenu(false)
    try {
      const p = await assess.export(report.id, f)
      if (p) toast('success', `Saved ${p}`)
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <div className="flex flex-wrap items-center gap-6 rounded-xl border border-border bg-surface p-5">
      <Gauge score={report.score} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <h2 className="text-base font-semibold">{report.clusterName}</h2>
          {prev && (
            <span className="rounded-full bg-surface-2 px-2 py-0.5 text-xs font-medium">
              <Delta v={report.score - prev.score} /> vs previous
            </span>
          )}
        </div>
        <div className="mt-0.5 text-xs text-muted">
          {fmtDate(report.startedAt)} · Kubernetes {report.serverVersion || '?'} ·{' '}
          {report.stats.nodes} nodes · {report.stats.pods} pods · {report.stats.rulesRun} checks ·{' '}
          {(report.durationMs / 1000).toFixed(1)}s
        </div>
        <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
          {report.categories.map((c) => (
            <div key={c.category} className="rounded-lg border border-border bg-bg px-3 py-2">
              <div className="flex items-baseline justify-between">
                <span className="text-[11px] font-semibold uppercase tracking-wide text-muted">
                  {catLabel[c.category]}
                </span>
                <span
                  className="font-mono text-sm font-semibold"
                  style={{ color: scoreColor(c.score) }}
                >
                  {c.score}
                </span>
              </div>
              <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-surface-2">
                <div
                  className="h-full rounded-full"
                  style={{ width: `${c.score}%`, background: scoreColor(c.score) }}
                />
              </div>
              <div className="mt-1 text-[11px] text-muted">
                {c.findings} finding{c.findings === 1 ? '' : 's'}
              </div>
            </div>
          ))}
        </div>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {SEVERITIES.map((s) => (
            <span
              key={s}
              className={clsx(
                'rounded-full px-2.5 py-0.5 text-xs font-semibold capitalize',
                sevChip[s],
              )}
            >
              {report.counts[s]} {s}
            </span>
          ))}
        </div>
      </div>
      <div className="relative self-start">
        <Button variant="ghost" onClick={() => setMenu((m) => !m)}>
          <Download size={14} /> Export
        </Button>
        {menu && (
          <div
            className="absolute right-0 top-9 z-20 w-52 rounded-lg border border-border bg-surface p-1 shadow-xl"
            onMouseLeave={() => setMenu(false)}
          >
            <button
              onClick={() => void doExport('html')}
              className="block w-full cursor-pointer rounded-md px-2.5 py-1.5 text-left text-[13px] hover:bg-surface-hover"
            >
              Report (HTML — print to PDF)
            </button>
            <button
              onClick={() => void doExport('json')}
              className="block w-full cursor-pointer rounded-md px-2.5 py-1.5 text-left text-[13px] hover:bg-surface-hover"
            >
              Raw data (JSON)
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

function Trend({
  history,
  selectedId,
  onSelect,
}: {
  history: FzSummary[]
  selectedId: string | null
  onSelect: (id: string) => void
}) {
  const [series, setSeries] = useState<'overall' | FzCategory>('overall')
  const points = history.map((h) => ({
    t: h.startedAt * 1000,
    v:
      series === 'overall'
        ? h.score
        : (h.categories.find((c) => c.category === series)?.score ?? 0),
  }))
  return (
    <div className="rounded-xl border border-border bg-surface p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted">
          Score over time
        </h3>
        <div className="flex gap-0.5 rounded-md border border-border p-0.5">
          {(['overall', ...CATS] as const).map((s) => (
            <button
              key={s}
              onClick={() => setSeries(s)}
              className={clsx(
                'cursor-pointer rounded px-2 py-0.5 text-xs font-medium capitalize',
                series === s ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
              )}
            >
              {s}
            </button>
          ))}
        </div>
      </div>
      {history.length < 2 ? (
        <p className="py-6 text-center text-[13px] text-muted">
          Run the assessment again later to see how your score changes over time.
        </p>
      ) : (
        <>
          <Chart
            title={series === 'overall' ? 'Overall score' : catLabel[series]}
            color="var(--accent)"
            points={points}
            format={(v) => String(Math.round(v))}
            capacity={100}
          />
          <div className="mt-2 flex flex-wrap gap-1.5">
            {history.map((h) => (
              <button
                key={h.id}
                onClick={() => onSelect(h.id)}
                title={fmtDate(h.startedAt)}
                className={clsx(
                  'size-3 cursor-pointer rounded-full border',
                  h.id === selectedId ? 'border-fg' : 'border-transparent',
                )}
                style={{ background: scoreColor(h.score) }}
                aria-label={`Run ${fmtDate(h.startedAt)}, score ${h.score}`}
              />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function History({ history, selectedId }: { history: FzSummary[]; selectedId: string | null }) {
  const { select, remove } = useReports()
  const rows = [...history].reverse()
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-surface">
      <div className="border-b border-border px-4 py-2.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
        History ({history.length})
      </div>
      <div className="max-h-72 overflow-auto">
        {rows.map((h, i) => {
          const prev = rows[i + 1]
          return (
            <div
              key={h.id}
              onClick={() => void select(h.id)}
              className={clsx(
                'group flex cursor-pointer items-center gap-3 border-b border-border px-4 py-2 last:border-0',
                h.id === selectedId ? 'bg-accent/10' : 'hover:bg-surface-hover',
              )}
            >
              <span
                className="w-9 text-right font-mono text-sm font-semibold"
                style={{ color: scoreColor(h.score) }}
              >
                {h.score}
              </span>
              {prev ? (
                <Delta v={h.score - prev.score} className="w-7 text-xs" />
              ) : (
                <span className="w-7" />
              )}
              <div className="min-w-0 flex-1">
                <div className="truncate text-[13px]">{fmtDate(h.startedAt)}</div>
                <div className="truncate text-xs text-muted">
                  {h.counts.critical + h.counts.high > 0
                    ? `${h.counts.critical + h.counts.high} critical/high · `
                    : ''}
                  {h.counts.medium} medium · {h.counts.low} low
                </div>
              </div>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  if (confirm('Delete this assessment?')) void remove(h.id)
                }}
                className="cursor-pointer rounded p-1 text-muted opacity-0 hover:text-danger group-hover:opacity-100"
                aria-label="Delete assessment"
              >
                <Trash2 size={13} />
              </button>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function ObjectLink({ f }: { f: FzFinding }) {
  const jump = useExplorer((s) => s.jump)
  const label = f.name ? `${f.kind}/${f.namespace ? f.namespace + '/' : ''}${f.name}` : f.kind
  if (!f.resourceKey || !f.name) return <span className="font-mono text-xs">{label}</span>
  return (
    <button
      onClick={() => void jump(f.resourceKey, f.name)}
      className="cursor-pointer text-left font-mono text-xs text-accent hover:underline"
      title="Show in explorer"
    >
      {label}
    </button>
  )
}

function RuleCard({
  rule,
  defaultOpen,
  clusterId,
}: {
  rule: FzRule
  defaultOpen: boolean
  clusterId: string
}) {
  const [open, setOpen] = useState(defaultOpen)
  const fixable = useFixableRules().has(rule.id)
  const [fixing, setFixing] = useState<FzFinding | null>(null)
  const sev = (rule.severity || 'info') as Severity
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full cursor-pointer items-center gap-3 px-3 py-2.5 text-left hover:bg-surface-hover"
      >
        {open ? (
          <ChevronDown size={14} className="shrink-0 text-muted" />
        ) : (
          <ChevronRight size={14} className="shrink-0 text-muted" />
        )}
        <span
          className={clsx(
            'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
            sevChip[sev],
          )}
        >
          {sev}
        </span>
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{rule.title}</span>
        <span className="shrink-0 text-xs text-muted">
          {rule.affected}
          {rule.checked > 0 && ` / ${rule.checked}`} · {catLabel[rule.category]}
        </span>
      </button>
      {open && (
        <div className="border-t border-border px-4 py-3">
          <p className="text-[13px] text-muted">{rule.description}</p>
          <div className="mt-2 rounded-md bg-accent/10 px-3 py-2 text-[13px]">
            <span className="font-semibold">How to fix: </span>
            {rule.remediation}
          </div>
          <div className="mt-3 divide-y divide-border/50">
            {rule.findings.map((f, i) => (
              <div key={i} className="flex items-start gap-3 py-1.5">
                <span
                  className={clsx(
                    'mt-0.5 shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
                    sevChip[f.severity],
                  )}
                >
                  {f.severity}
                </span>
                <div className="min-w-0 flex-1">
                  <ObjectLink f={f} />
                  <div className="select-text break-words text-[13px]">{f.message}</div>
                </div>
                {fixable && f.resourceKey && f.name && (
                  <button
                    onClick={() => setFixing(f)}
                    className="flex shrink-0 cursor-pointer items-center gap-1 rounded-md border border-border px-2 py-1 text-xs hover:border-accent hover:text-accent"
                  >
                    <Wrench size={12} /> Fix…
                  </button>
                )}
              </div>
            ))}
          </div>
          {rule.truncated && (
            <p className="mt-2 text-xs text-muted">
              Showing the first {rule.findings.length} of {rule.affected} findings.
            </p>
          )}
          {fixing && (
            <FixDialog
              clusterId={clusterId}
              ruleId={rule.id}
              resourceKey={fixing.resourceKey}
              ns={fixing.namespace}
              name={fixing.name}
              onClose={() => setFixing(null)}
            />
          )}
        </div>
      )}
    </div>
  )
}

function Findings({ report }: { report: FzReport }) {
  const [cat, setCat] = useState<FzCategory | 'all'>('all')
  const [sevs, setSevs] = useState<Set<Severity>>(new Set(SEVERITIES))
  const [q, setQ] = useState('')
  const [passed, setPassed] = useState(false)

  const ql = q.trim().toLowerCase()
  const failing = useMemo(
    () =>
      report.rules
        .filter((r) => r.affected > 0 && (cat === 'all' || r.category === cat))
        .map((r) => ({
          ...r,
          findings: r.findings.filter(
            (f) =>
              sevs.has(f.severity) &&
              (!ql ||
                `${f.kind} ${f.namespace} ${f.name} ${f.message} ${r.title}`
                  .toLowerCase()
                  .includes(ql)),
          ),
        }))
        .filter((r) => r.findings.length > 0)
        .sort(
          (a, b) =>
            sevRank[(b.severity || 'info') as Severity] -
              sevRank[(a.severity || 'info') as Severity] || b.affected - a.affected,
        ),
    [report, cat, sevs, ql],
  )
  const ok = report.rules.filter(
    (r) => r.affected === 0 && !r.skipped && (cat === 'all' || r.category === cat),
  )
  const skipped = report.rules.filter((r) => r.skipped)

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="flex gap-0.5 rounded-md border border-border p-0.5">
          {(['all', ...CATS] as const).map((c) => (
            <button
              key={c}
              onClick={() => setCat(c)}
              className={clsx(
                'cursor-pointer rounded px-2.5 py-1 text-xs font-medium',
                cat === c ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
              )}
            >
              {c === 'all' ? 'All' : catLabel[c]}
            </button>
          ))}
        </div>
        <div className="flex gap-1">
          {SEVERITIES.map((s) => (
            <button
              key={s}
              onClick={() =>
                setSevs((cur) => {
                  const n = new Set(cur)
                  if (!n.delete(s)) n.add(s)
                  return n
                })
              }
              className={clsx(
                'cursor-pointer rounded-full px-2.5 py-0.5 text-xs font-semibold capitalize',
                sevs.has(s) ? sevChip[s] : 'bg-surface-2 text-muted line-through opacity-60',
              )}
            >
              {s}
            </button>
          ))}
        </div>
        <div className="relative ml-auto">
          <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Filter findings…"
            className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
          />
        </div>
      </div>

      <div className="space-y-2">
        {failing.map((r) => (
          <RuleCard
            key={r.id}
            rule={r}
            clusterId={report.clusterId}
            defaultOpen={
              failing.length <= 3 ||
              (sevRank[(r.severity || 'info') as Severity] >= 3 && failing.indexOf(r) < 2)
            }
          />
        ))}
        {failing.length === 0 && (
          <p className="py-10 text-center text-[13px] text-muted">
            {report.rules.some((r) => r.affected > 0)
              ? 'No findings match the filters.'
              : 'No findings — every check passed.'}
          </p>
        )}
      </div>

      {ok.length > 0 && (
        <div className="mt-5">
          <button
            onClick={() => setPassed(!passed)}
            className="flex cursor-pointer items-center gap-1 text-xs font-medium text-muted hover:text-fg"
          >
            {passed ? <ChevronDown size={13} /> : <ChevronRight size={13} />} {ok.length} passed
            check{ok.length === 1 ? '' : 's'}
          </button>
          {passed && (
            <ul className="mt-2 space-y-1 text-[13px]">
              {ok.map((r) => (
                <li key={r.id} className="flex gap-2">
                  <span className="text-success">✓</span>
                  <span>{r.title}</span>
                  <span className="text-xs text-muted">({r.checked} checked)</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      {skipped.length > 0 && (
        <div className="mt-5 rounded-lg border border-border bg-bg p-3 text-xs text-muted">
          <div className="mb-1 font-semibold">Skipped checks</div>
          {skipped.map((r) => (
            <div key={r.id}>
              {r.title} — {r.skipped}
            </div>
          ))}
        </div>
      )}
      {report.notes
        .filter((n) => !n.startsWith('Skipped'))
        .map((n, i) => (
          <p key={i} className="mt-3 text-xs text-muted">
            {n}
          </p>
        ))}
    </div>
  )
}

function Changes({
  diff,
  history,
  selectedId,
}: {
  diff: FzDiff | null
  history: FzSummary[]
  selectedId: string
}) {
  const { baselineId, setBaseline } = useReports()
  const choices = history.filter((h) => h.id !== selectedId)
  return (
    <div>
      <div className="mb-3 flex items-center gap-2 text-[13px]">
        <span className="text-muted">Compare with</span>
        <select
          value={baselineId ?? ''}
          onChange={(e) => void setBaseline(e.target.value || null)}
          className="no-drag h-8 rounded-md border border-border bg-surface-2 px-2 text-[13px]"
        >
          <option value="">Previous run</option>
          {[...choices].reverse().map((h) => (
            <option key={h.id} value={h.id}>
              {fmtDate(h.startedAt)} (score {h.score})
            </option>
          ))}
        </select>
      </div>
      {!diff ? (
        <p className="py-10 text-center text-[13px] text-muted">
          {choices.length === 0
            ? 'This is the first assessment — nothing to compare yet.'
            : 'Loading…'}
        </p>
      ) : (
        <>
          <div className="mb-4 flex flex-wrap gap-3">
            <div className="rounded-lg border border-border bg-surface px-4 py-2">
              <div className="text-[11px] uppercase text-muted">Score</div>
              <div className="text-lg font-semibold">
                {diff.from.score} → {diff.to.score}{' '}
                <Delta v={diff.scoreDelta} className="text-sm" />
              </div>
            </div>
            <div className="rounded-lg border border-border bg-surface px-4 py-2">
              <div className="text-[11px] uppercase text-muted">New</div>
              <div className="text-lg font-semibold text-danger">{diff.new.length}</div>
            </div>
            <div className="rounded-lg border border-border bg-surface px-4 py-2">
              <div className="text-[11px] uppercase text-muted">Resolved</div>
              <div className="text-lg font-semibold text-success">{diff.resolved.length}</div>
            </div>
            <div className="rounded-lg border border-border bg-surface px-4 py-2">
              <div className="text-[11px] uppercase text-muted">Unchanged</div>
              <div className="text-lg font-semibold">{diff.unchanged}</div>
            </div>
          </div>
          {diff.approximate && (
            <p className="mb-3 text-xs text-warning">
              Some checks have more findings than are stored per run, so the lists below may be
              incomplete.
            </p>
          )}
          <DiffList title="New problems" tone="danger" items={diff.new} />
          <DiffList title="Resolved" tone="success" items={diff.resolved} />
          {diff.ruleDeltas.length > 0 && (
            <div className="mt-5">
              <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
                By check
              </h3>
              <div className="overflow-hidden rounded-lg border border-border">
                {diff.ruleDeltas.map((r) => (
                  <div
                    key={r.id}
                    className="flex items-center justify-between border-b border-border bg-surface px-3 py-1.5 text-[13px] last:border-0"
                  >
                    <span>{r.title}</span>
                    <span className="font-mono text-xs">
                      {r.before} → {r.after} <Delta v={-(r.after - r.before)} />
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function DiffList({
  title,
  tone,
  items,
}: {
  title: string
  tone: 'danger' | 'success'
  items: FzDiff['new']
}) {
  if (items.length === 0) return null
  return (
    <div className="mb-4">
      <h3
        className={clsx(
          'mb-2 text-[11px] font-semibold uppercase tracking-wide',
          tone === 'danger' ? 'text-danger' : 'text-success',
        )}
      >
        {title} ({items.length})
      </h3>
      <div className="divide-y divide-border/50 rounded-lg border border-border bg-surface">
        {items.slice(0, 200).map((it, i) => (
          <div key={i} className="flex items-start gap-3 px-3 py-2">
            <span
              className={clsx(
                'mt-0.5 shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
                sevChip[it.finding.severity],
              )}
            >
              {it.finding.severity}
            </span>
            <div className="min-w-0">
              <div className="text-xs text-muted">{it.title}</div>
              <ObjectLink f={it.finding} />
              <div className="break-words text-[13px]">{it.finding.message}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export function Reports({ clusterId }: { clusterId: string }) {
  const { history, selectedId, report, diff, running, loadError, load, select, start, cancel } =
    useReports()
  const [tab, setTab] = useState<'findings' | 'cost' | 'changes'>('findings')

  useEffect(() => {
    void load(clusterId)
  }, [clusterId, load])

  const idx = history.findIndex((h) => h.id === selectedId)
  const prev = idx > 0 ? history[idx - 1] : undefined

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4">
        <h1 className="text-sm font-semibold">Assessments</h1>
        <span className="font-mono text-xs text-muted">{history.length}</span>
        <div className="flex-1" />
        {running ? (
          <div className="flex items-center gap-3">
            <div className="w-56">
              <div className="mb-1 flex justify-between text-[11px] text-muted">
                <span className="truncate">{running.phase}</span>
                <span>{running.pct}%</span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-surface-2">
                <div
                  className="h-full rounded-full bg-accent transition-all"
                  style={{ width: `${running.pct}%` }}
                />
              </div>
            </div>
            <Button onClick={() => void cancel()}>
              <Square size={12} /> Cancel
            </Button>
          </div>
        ) : (
          <Button variant="primary" onClick={() => void start()}>
            <Play size={13} /> Run assessment
          </Button>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-4">
        {loadError && <p className="text-[13px] text-danger">{loadError}</p>}
        {!loadError && history.length === 0 && !running && (
          <div className="mx-auto mt-14 max-w-md text-center">
            <h2 className="text-base font-semibold">How healthy is this cluster?</h2>
            <p className="mt-2 text-[13px] text-muted">
              An assessment checks capacity, reliability, security and upkeep with {`~30`} read-only
              checks, gives a score and tells you how to fix what it finds. Every run is saved so
              you can watch the trend and compare runs.
            </p>
            <Button variant="primary" className="mt-5" onClick={() => void start()}>
              <Play size={13} /> Run first assessment
            </Button>
          </div>
        )}
        {report && (
          <div className="mx-auto max-w-5xl space-y-4">
            <Hero report={report} prev={prev} />
            <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
              <Trend history={history} selectedId={selectedId} onSelect={(id) => void select(id)} />
              <History history={history} selectedId={selectedId} />
            </div>
            <div>
              <div className="mb-3 flex gap-1 border-b border-border">
                {(['findings', 'cost', 'changes'] as const).map((t) => (
                  <button
                    key={t}
                    onClick={() => setTab(t)}
                    className={clsx(
                      '-mb-px cursor-pointer border-b-2 px-3 py-2 text-[13px] font-medium capitalize',
                      tab === t
                        ? 'border-accent text-fg'
                        : 'border-transparent text-muted hover:text-fg',
                    )}
                  >
                    {t === 'changes' ? 'Changes since…' : t === 'cost' ? 'Cost' : 'Findings'}
                    {t === 'changes' && diff && (
                      <span className="ml-1.5 text-xs text-muted">
                        +{diff.new.length} / −{diff.resolved.length}
                      </span>
                    )}
                  </button>
                ))}
              </div>
              {tab === 'findings' ? (
                <Findings report={report} />
              ) : tab === 'cost' ? (
                report.finops ? (
                  <FinOpsCard finops={report.finops} clusterId={report.clusterId} />
                ) : (
                  <p className="py-10 text-center text-[13px] text-muted">
                    This report was created before cost estimation existed. Run a new assessment.
                  </p>
                )
              ) : (
                selectedId && <Changes diff={diff} history={history} selectedId={selectedId} />
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
