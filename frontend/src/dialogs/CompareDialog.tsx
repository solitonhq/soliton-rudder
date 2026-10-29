import clsx from 'clsx'
import { ArrowRight, GitCompare, Play } from 'lucide-react'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { drift, type DriftSide } from '../lib/backend'
import type { DriftItem, DriftKindInfo, DriftObject, DriftReport, FieldDiff } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useClusters } from '../store/clusters'
import { PromoteDialog } from './PromoteDialog'

const DiffView = lazy(() => import('../components/DiffView').then((m) => ({ default: m.DiffView })))
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))
const DEFAULT_KINDS = new Set([
  'apps/v1/deployments',
  'apps/v1/statefulsets',
  'core/v1/configmaps',
  'core/v1/services',
])

const field =
  'no-drag h-8 cursor-pointer rounded-md border border-border bg-bg px-2 text-[13px] outline-none focus:border-accent'

const statusChip: Record<DriftItem['status'], { label: string; cls: string }> = {
  same: { label: 'same', cls: 'bg-surface-2 text-muted' },
  differs: { label: 'differs', cls: 'bg-warning/15 text-warning' },
  onlySource: { label: 'missing in target', cls: 'bg-accent/15 text-accent' },
  onlyTarget: { label: 'only in target', cls: 'bg-surface-2 text-muted' },
}

function show(v: unknown): string {
  if (v === undefined) return '—'
  const s = typeof v === 'string' ? v : JSON.stringify(v)
  return s.length > 140 ? s.slice(0, 139) + '…' : s
}

function ClusterNs({
  label,
  side,
  setSide,
}: {
  label: string
  side: DriftSide
  setSide: (s: DriftSide) => void
}) {
  const clusters = useClusters((s) => s.clusters)
  const [attempt, setAttempt] = useState(0)
  // Connects to the cluster if needed; the state is keyed by cluster, so switching never shows the old list
  const ns = usePoll(
    () => (side.cluster ? drift.namespaces(side.cluster) : Promise.resolve([] as string[])),
    `${side.cluster}|${attempt}`,
    0,
  )
  const loading = !!side.cluster && ns.loading
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] font-semibold uppercase text-muted">{label}</span>
      <div className="flex gap-1.5">
        <select
          aria-label={`${label} cluster`}
          value={side.cluster}
          onChange={(e) => setSide({ cluster: e.target.value, ns: '' })}
          className={clsx(field, 'w-44')}
        >
          <option value="">Cluster…</option>
          {clusters.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
        <select
          aria-label={`${label} namespace`}
          value={side.ns}
          onChange={(e) => setSide({ ...side, ns: e.target.value })}
          className={clsx(field, 'w-40')}
          disabled={!side.cluster || loading}
        >
          <option value="">{loading ? 'Loading namespaces…' : 'Namespace…'}</option>
          {(ns.data ?? []).map((n) => (
            <option key={n}>{n}</option>
          ))}
        </select>
      </div>
      {ns.error && (
        <div className="flex max-w-80 items-start gap-1.5 text-[11px] text-danger">
          <span className="select-text break-words">{ns.error}</span>
          <button
            className="shrink-0 cursor-pointer text-accent hover:underline"
            onClick={() => setAttempt((x) => x + 1)}
          >
            Retry
          </button>
        </div>
      )}
    </div>
  )
}

function Detail({
  src,
  dst,
  srcName,
  dstName,
  kindKey,
  item,
  onApplied,
}: {
  src: DriftSide
  dst: DriftSide
  srcName: string
  dstName: string
  kindKey: string
  item: DriftItem
  onApplied: () => void
}) {
  const [obj, setObj] = useState<DriftObject | null>(null)
  const [error, setError] = useState('')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [tab, setTab] = useState<'fields' | 'yaml'>('fields')
  const [promoting, setPromoting] = useState(false)

  // the parent keys this component by object, so state starts fresh for every selection
  useEffect(() => {
    let cancelled = false
    drift
      .object(src, dst, kindKey, item.name)
      .then((o) => !cancelled && setObj(o))
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [src, dst, kindKey, item.name])

  const toggle = (p: string) =>
    setPicked((s) => {
      const n = new Set(s)
      if (!n.delete(p)) n.add(p)
      return n
    })
  const promotable = (obj?.fields ?? []).filter((f) => f.promotable)
  const paths = [...picked].sort()

  return (
    <div className="flex h-full min-w-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2">
        <span className="font-mono text-sm font-semibold">{item.name}</span>
        <span
          className={clsx(
            'rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
            statusChip[item.status].cls,
          )}
        >
          {statusChip[item.status].label}
        </span>
        <div className="flex-1" />
        {obj?.status === 'onlySource' && (
          <Button variant="primary" onClick={() => setPromoting(true)}>
            Create in {dstName}…
          </Button>
        )}
        {obj?.status === 'differs' && (
          <Button variant="primary" disabled={picked.size === 0} onClick={() => setPromoting(true)}>
            Promote {picked.size} field{picked.size === 1 ? '' : 's'}…
          </Button>
        )}
      </div>
      {error && <div className="select-text p-4 text-[13px] text-danger">{error}</div>}
      {!obj && !error && <div className="p-4 text-[13px] text-muted">Comparing…</div>}
      {obj && (
        <>
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-3">
            {(['fields', 'yaml'] as const).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className={clsx(
                  '-mb-px cursor-pointer border-b-2 px-3 py-1.5 text-xs font-medium',
                  tab === t
                    ? 'border-accent text-fg'
                    : 'border-transparent text-muted hover:text-fg',
                )}
              >
                {t === 'fields' ? `Fields (${obj.fields.length})` : 'YAML diff'}
              </button>
            ))}
            {tab === 'fields' && promotable.length > 0 && (
              <div className="ml-auto flex gap-2 text-xs">
                <button
                  className="cursor-pointer text-accent hover:underline"
                  onClick={() =>
                    setPicked(new Set(promotable.filter((f) => !f.envSpecific).map((f) => f.path)))
                  }
                >
                  Select all except environment-specific
                </button>
                <button
                  className="cursor-pointer text-muted hover:underline"
                  onClick={() => setPicked(new Set())}
                >
                  Clear
                </button>
              </div>
            )}
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            {tab === 'yaml' ? (
              <Suspense fallback={<div className="p-4 text-muted">Loading diff…</div>}>
                <DiffView before={obj.dstYaml} after={obj.srcYaml} />
              </Suspense>
            ) : obj.fields.length === 0 ? (
              <div className="p-4 text-[13px] text-muted">
                {obj.status === 'onlySource'
                  ? `Not present in ${dstName}.`
                  : obj.status === 'onlyTarget'
                    ? `Only present in ${dstName}.`
                    : 'No differences.'}
              </div>
            ) : (
              <table className="w-full text-[12px]">
                <thead className="sticky top-0 bg-surface text-left text-[10px] uppercase text-muted">
                  <tr>
                    <th className="w-8 px-3 py-1.5" />
                    <th className="px-2 py-1.5">Field</th>
                    <th className="px-2 py-1.5">{dstName}</th>
                    <th className="px-2 py-1.5">{srcName}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {obj.fields.map((f: FieldDiff) => (
                    <tr key={f.path} className={clsx(!f.promotable && 'opacity-60')}>
                      <td className="px-3 py-1.5">
                        <input
                          type="checkbox"
                          aria-label={`Promote ${f.path}`}
                          disabled={!f.promotable}
                          checked={picked.has(f.path)}
                          onChange={() => toggle(f.path)}
                          className="accent-[var(--accent)]"
                        />
                      </td>
                      <td className="select-text px-2 py-1.5 align-top font-mono">
                        {f.path}
                        {f.envSpecific && (
                          <span
                            title={f.note}
                            className="ml-1.5 rounded bg-warning/15 px-1 py-px font-sans text-[9px] font-bold uppercase text-warning"
                          >
                            env-specific
                          </span>
                        )}
                        {!f.promotable && f.note && (
                          <div className="font-sans text-[10px] text-muted">{f.note}</div>
                        )}
                      </td>
                      <td
                        className={clsx(
                          'select-text break-all px-2 py-1.5 align-top font-mono',
                          f.op !== 'added' && 'text-danger',
                        )}
                      >
                        {f.op === 'added' ? '—' : show(f.dst)}
                      </td>
                      <td
                        className={clsx(
                          'select-text break-all px-2 py-1.5 align-top font-mono',
                          f.op !== 'removed' && 'text-success',
                        )}
                      >
                        {f.op === 'removed' ? '—' : show(f.src)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
      {promoting && obj && (
        <PromoteDialog
          src={src}
          dst={dst}
          srcName={srcName}
          dstName={dstName}
          resourceKey={kindKey}
          name={item.name}
          paths={obj.status === 'onlySource' ? [] : paths}
          onClose={() => setPromoting(false)}
          onApplied={onApplied}
        />
      )}
    </div>
  )
}

export function CompareDialog({ onClose }: { onClose: () => void }) {
  const clusters = useClusters((s) => s.clusters)
  const activeId = useClusters((s) => s.activeId)
  const [src, setSrc] = useState<DriftSide>({ cluster: activeId ?? clusters[0]?.id ?? '', ns: '' })
  const [dst, setDst] = useState<DriftSide>({
    cluster: clusters.find((c) => c.id !== (activeId ?? clusters[0]?.id))?.id ?? '',
    ns: '',
  })
  const [kinds, setKinds] = useState<DriftKindInfo[]>([])
  const [chosen, setChosen] = useState<Set<string>>(DEFAULT_KINDS)
  const [report, setReport] = useState<DriftReport | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [onlyDiff, setOnlyDiff] = useState(true)
  const [sel, setSel] = useState<{ key: string; name: string } | null>(null)
  // the sides the report was computed for: editing the selectors afterwards must not retarget the detail pane
  const [ran, setRan] = useState<{ src: DriftSide; dst: DriftSide } | null>(null)

  useEffect(() => {
    drift
      .kinds()
      .then(setKinds)
      .catch(() => setKinds([]))
  }, [])

  const name = (id: string) => clusters.find((c) => c.id === id)?.name ?? id
  const ready = src.cluster && dst.cluster && src.ns && dst.ns && chosen.size > 0

  const run = async () => {
    setBusy(true)
    setError('')
    setSel(null)
    try {
      setReport(
        await drift.compare(
          src,
          dst,
          kinds.filter((k) => chosen.has(k.key)).map((k) => k.key),
        ),
      )
      setRan({ src, dst })
    } catch (e) {
      setReport(null)
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const groups = useMemo(
    () =>
      (report?.groups ?? [])
        .map((g) => ({ ...g, items: g.items.filter((i) => !onlyDiff || i.status !== 'same') }))
        .filter((g) => g.items.length > 0 || g.error),
    [report, onlyDiff],
  )
  const current =
    sel && report?.groups.find((g) => g.key === sel.key)?.items.find((i) => i.name === sel.name)
  const same = src.cluster === dst.cluster && src.ns === dst.ns

  return (
    <Dialog title="Compare clusters" onClose={onClose} width={1180}>
      <div className="space-y-3 text-[13px]">
        <div className="flex flex-wrap items-end gap-4">
          <ClusterNs label="Source" side={src} setSide={setSrc} />
          <ArrowRight size={16} className="mb-2 text-muted" />
          <ClusterNs label="Target" side={dst} setSide={setDst} />
          <Button variant="primary" disabled={!ready || busy || same} onClick={() => void run()}>
            <Play size={13} /> {busy ? 'Comparing…' : 'Compare'}
          </Button>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {kinds.map((k) => (
            <button
              key={k.key}
              aria-pressed={chosen.has(k.key)}
              onClick={() =>
                setChosen((s) => {
                  const n = new Set(s)
                  if (!n.delete(k.key)) n.add(k.key)
                  return n
                })
              }
              className={clsx(
                'cursor-pointer rounded-md border px-2 py-0.5 text-xs',
                chosen.has(k.key)
                  ? 'border-accent bg-accent/15 text-accent'
                  : 'border-border text-muted hover:border-muted',
              )}
            >
              {k.label}
            </button>
          ))}
        </div>
        {same && src.ns && (
          <div className="text-xs text-warning">Source and target are the same namespace.</div>
        )}
        {error && (
          <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
        )}

        {report && ran && (
          <div className="flex h-[52vh] overflow-hidden rounded-lg border border-border">
            <div className="flex w-80 shrink-0 flex-col border-r border-border">
              <label className="flex shrink-0 cursor-pointer items-center gap-2 border-b border-border px-3 py-2 text-xs text-muted">
                <input
                  type="checkbox"
                  checked={onlyDiff}
                  onChange={(e) => setOnlyDiff(e.target.checked)}
                  className="accent-[var(--accent)]"
                />
                Only differences
              </label>
              <div className="min-h-0 flex-1 overflow-y-auto">
                {groups.length === 0 && (
                  <div className="p-4 text-muted">
                    {onlyDiff ? 'No differences found.' : 'Nothing to show.'}
                  </div>
                )}
                {groups.map((g) => (
                  <div key={g.key}>
                    <div className="sticky top-0 bg-surface px-3 py-1 text-[10px] font-semibold uppercase text-muted">
                      {g.label}
                    </div>
                    {g.error && <div className="px-3 py-1 text-xs text-danger">{g.error}</div>}
                    {g.items.map((i) => (
                      <button
                        key={i.name}
                        onClick={() => setSel({ key: g.key, name: i.name })}
                        className={clsx(
                          'flex w-full cursor-pointer items-center gap-2 px-3 py-1.5 text-left hover:bg-surface-hover',
                          sel?.key === g.key && sel.name === i.name && 'bg-accent/10',
                        )}
                      >
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">{i.name}</span>
                        <span
                          className={clsx(
                            'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
                            statusChip[i.status].cls,
                          )}
                        >
                          {i.status === 'differs' ? `${i.changes}` : statusChip[i.status].label}
                        </span>
                      </button>
                    ))}
                  </div>
                ))}
              </div>
              {report.notes.map((n) => (
                <div
                  key={n}
                  className="shrink-0 border-t border-border px-3 py-2 text-[10px] text-muted"
                >
                  {n}
                </div>
              ))}
            </div>
            <div className="min-w-0 flex-1">
              {sel && current ? (
                <Detail
                  key={`${sel.key}/${sel.name}/${ran.src.cluster}/${ran.dst.cluster}`}
                  src={ran.src}
                  dst={ran.dst}
                  srcName={name(ran.src.cluster)}
                  dstName={name(ran.dst.cluster)}
                  kindKey={sel.key}
                  item={current}
                  onApplied={() => void run()}
                />
              ) : (
                <div className="flex h-full flex-col items-center justify-center gap-2 text-muted">
                  <GitCompare size={28} />
                  <span>Select an object to see what differs.</span>
                </div>
              )}
            </div>
          </div>
        )}
        <p className="text-xs text-muted">
          Read-only until you promote. Secrets are never read or copied. Nothing is written to the
          target without a dry-run you confirm.
        </p>
      </div>
    </Dialog>
  )
}
