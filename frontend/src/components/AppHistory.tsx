import clsx from 'clsx'
import { lazy, Suspense, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { integ } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { Application, HelmDetail } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useUI } from '../store/ui'

const DiffView = lazy(() => import('./DiffView').then((m) => ({ default: m.DiffView })))
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const statusTone: Record<string, string> = {
  deployed: 'bg-success/15 text-success',
  superseded: 'bg-surface-2 text-muted',
  failed: 'bg-danger/15 text-danger',
}
const tone = (s: string) =>
  statusTone[s] ??
  (s.startsWith('pending') ? 'bg-warning/15 text-warning' : 'bg-surface-2 text-muted')

function useRevision(clusterId: string, ns: string, release: string, rev: number | null) {
  return usePoll<HelmDetail | null>(
    () => (rev ? integ.helm.get(clusterId, ns, release, rev) : Promise.resolve(null)),
    `${clusterId}|${ns}|${release}|${rev}`,
    0,
  )
}

function RollbackDialog({
  clusterId,
  app,
  rev,
  currentRev,
  onClose,
  onDone,
}: {
  clusterId: string
  app: Application
  rev: number
  currentRev: number
  onClose: () => void
  onDone: () => void
}) {
  const h = app.helm!
  const toast = useUI((s) => s.toast)
  const target = useRevision(clusterId, h.namespace, h.release, rev)
  const current = useRevision(clusterId, h.namespace, h.release, currentRev)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ready = !!target.data && !!current.data
  const changed = ready && target.data!.manifest !== current.data!.manifest
  return (
    <Dialog title={`Roll back ${h.release} to revision ${rev}`} onClose={onClose} width={820}>
      <div className="space-y-3 text-[13px]">
        <p className="text-muted">
          Helm redeploys revision {rev} as a <strong>new revision</strong> (the history is kept) and
          Kubernetes then rolls the workloads. This is what would change in the cluster manifests
          (revision {currentRev} → {rev}):
        </p>
        <div className="h-72 overflow-hidden rounded-md border border-border">
          {!ready ? (
            <div className="p-4 text-muted">
              {target.error || current.error || 'Loading both revisions…'}
            </div>
          ) : !changed ? (
            <div className="p-4 text-muted">
              The manifests of both revisions are identical: nothing would change.
            </div>
          ) : (
            <Suspense fallback={<div className="p-4 text-muted">Loading diff…</div>}>
              <DiffView before={current.data!.manifest} after={target.data!.manifest} />
            </Suspense>
          )}
        </div>
        {error && (
          <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="danger"
            disabled={!ready || busy}
            onClick={async () => {
              setBusy(true)
              try {
                await integ.helm.rollback(clusterId, h.namespace, h.release, rev)
                toast('success', `Rolled back to revision ${rev}`)
                onDone()
                onClose()
              } catch (e) {
                setError(errText(e))
                setBusy(false)
              }
            }}
          >
            {busy ? 'Rolling back…' : `Roll back to ${rev}`}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

/** The Helm revisions of an application, what changed between two of them, and rollback with a preview. */
export function AppHistory({ clusterId, app }: { clusterId: string; app: Application }) {
  const h = app.helm!
  const [tick, setTick] = useState(0) // bumped after a rollback so the list is read again
  const latest = usePoll(
    () => integ.helm.get(clusterId, h.namespace, h.release, 0),
    `${clusterId}|${h.namespace}|${h.release}|${h.revision}|${tick}`,
    0,
  )
  const cli = usePoll(() => integ.helm.cli(), 'cli', 0)
  const history = latest.data?.history ?? []
  const [fromSel, setFrom] = useState<number | null>(null)
  const [toSel, setTo] = useState<number | null>(null)
  const [tab, setTab] = useState<'values' | 'manifest'>('values')
  const [rollback, setRollback] = useState<number | null>(null)
  const toRev = toSel ?? history[0]?.revision ?? null
  const fromRev = fromSel ?? history[1]?.revision ?? null
  const a = useRevision(clusterId, h.namespace, h.release, fromRev)
  const b = useRevision(clusterId, h.namespace, h.release, toRev)

  if (latest.error && !latest.data)
    return <div className="select-text p-4 text-[13px] text-danger">{latest.error}</div>
  if (!latest.data)
    return <div className="p-4 text-[13px] text-muted">Reading the release history…</div>

  const pick =
    'no-drag h-8 cursor-pointer rounded-md border border-border bg-bg px-2 text-[13px] outline-none focus:border-accent'
  const before = tab === 'values' ? a.data?.values : a.data?.manifest
  const after = tab === 'values' ? b.data?.values : b.data?.manifest
  return (
    <div className="space-y-4 p-4 text-[13px]">
      <section>
        <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
          Revisions{' '}
          {h.revisions > history.length && (
            <span className="font-normal normal-case">
              (newest {history.length} of {h.revisions})
            </span>
          )}
        </h3>
        <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border">
          {history.map((r) => (
            <li key={r.revision} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
              <span className="w-10 font-mono text-xs font-semibold">#{r.revision}</span>
              <span
                className={clsx(
                  'rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
                  tone(r.status),
                )}
              >
                {r.status}
              </span>
              <span className="font-mono text-xs">{r.chartVersion}</span>
              {r.appVersion && (
                <span className="font-mono text-xs text-muted">app {r.appVersion}</span>
              )}
              <span className="min-w-0 flex-1 truncate text-xs text-muted" title={r.description}>
                {r.description}
              </span>
              <span className="text-xs text-muted">
                {r.updated ? `${formatAge(r.updated)} ago` : ''}
              </span>
              {cli.data && r.revision !== history[0]?.revision && (
                <button
                  className="cursor-pointer text-xs text-accent hover:underline"
                  onClick={() => setRollback(r.revision)}
                >
                  Roll back…
                </button>
              )}
            </li>
          ))}
        </ul>
        {!cli.data && (
          <p className="mt-1.5 text-xs text-muted">
            Rolling back uses your local <span className="font-mono">helm</span> command, which was
            not found.
          </p>
        )}
      </section>

      <section>
        <div className="mb-1.5 flex flex-wrap items-center gap-2">
          <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted">
            What changed
          </h3>
          <select
            aria-label="From revision"
            value={fromRev ?? ''}
            onChange={(e) => setFrom(+e.target.value)}
            className={pick}
          >
            {history.map((r) => (
              <option key={r.revision} value={r.revision}>
                #{r.revision}
              </option>
            ))}
          </select>
          <span className="text-muted">→</span>
          <select
            aria-label="To revision"
            value={toRev ?? ''}
            onChange={(e) => setTo(+e.target.value)}
            className={pick}
          >
            {history.map((r) => (
              <option key={r.revision} value={r.revision}>
                #{r.revision}
              </option>
            ))}
          </select>
          <div className="ml-auto flex gap-1">
            {(['values', 'manifest'] as const).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className={clsx(
                  'cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium capitalize',
                  tab === t ? 'bg-accent/15 text-accent' : 'text-muted hover:text-fg',
                )}
              >
                {t}
              </button>
            ))}
          </div>
        </div>
        <div className="h-80 overflow-hidden rounded-md border border-border">
          {history.length < 2 ? (
            <div className="p-4 text-muted">
              There is only one revision, so there is nothing to compare.
            </div>
          ) : fromRev === toRev ? (
            <div className="p-4 text-muted">Pick two different revisions.</div>
          ) : before === undefined || after === undefined ? (
            <div className="p-4 text-muted">{a.error || b.error || 'Loading both revisions…'}</div>
          ) : before === after ? (
            <div className="p-4 text-muted">
              No difference in the {tab} between these revisions.
            </div>
          ) : (
            <Suspense fallback={<div className="p-4 text-muted">Loading diff…</div>}>
              <DiffView before={before || ''} after={after || ''} />
            </Suspense>
          )}
        </div>
        <p className="mt-1 text-xs text-muted">
          Values are what was supplied at deploy time; the manifest is what Helm rendered for the
          cluster.
        </p>
      </section>
      {rollback !== null && (
        <RollbackDialog
          clusterId={clusterId}
          app={app}
          rev={rollback}
          currentRev={history[0]?.revision ?? h.revision}
          onClose={() => setRollback(null)}
          onDone={() => setTick((t) => t + 1)}
        />
      )}
    </div>
  )
}
