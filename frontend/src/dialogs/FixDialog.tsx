import clsx from 'clsx'
import { AlertTriangle, ShieldAlert, Wrench } from 'lucide-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { doctor, kube, tools } from '../lib/backend'
import type { ApplyResult, Fix } from '../lib/types'
import { useUI } from '../store/ui'

const DiffView = lazy(() => import('../components/DiffView').then((m) => ({ default: m.DiffView })))

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const riskChip: Record<Fix['risk'], string> = {
  low: 'bg-success/15 text-success',
  medium: 'bg-warning/15 text-warning',
  high: 'bg-danger/15 text-danger',
}

const actionChip: Record<ApplyResult['action'], string> = {
  create: 'bg-success/15 text-success',
  configure: 'bg-warning/15 text-warning',
  unchanged: 'bg-surface-2 text-muted',
  error: 'bg-danger/15 text-danger',
}

/**
 * Reviews and applies the fix for an assessment finding. Nothing is changed until the user presses Apply, and the
 * change is first shown as a server-side dry-run diff.
 */
export function FixDialog({
  clusterId,
  ruleId,
  resourceKey,
  ns,
  name,
  onClose,
  onApplied,
}: {
  clusterId: string
  ruleId: string
  resourceKey: string
  ns: string
  name: string
  onClose: () => void
  onApplied?: () => void
}) {
  const toast = useUI((s) => s.toast)
  const [fixes, setFixes] = useState<Fix[] | null>(null)
  const [error, setError] = useState('')
  const [sel, setSel] = useState(0)
  const [force, setForce] = useState(false)
  const [ack, setAck] = useState(false)
  const [preview, setPreview] = useState<{
    loading: boolean
    results: ApplyResult[] | null
    error: string
  }>({ loading: false, results: null, error: '' })
  const [tab, setTab] = useState<'diff' | 'yaml'>('diff')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    doctor
      .fixes(clusterId, ruleId, resourceKey, ns, name)
      .then((f) => !cancelled && setFixes(f))
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [clusterId, ruleId, resourceKey, ns, name])

  const fix = fixes?.[sel]

  useEffect(() => {
    if (!fix || fix.action !== 'apply') return
    let cancelled = false
    setPreview({ loading: true, results: null, error: '' })
    tools
      .previewManifest(clusterId, fix.manifest, fix.target.namespace || ns, force)
      .then((results) => !cancelled && setPreview({ loading: false, results, error: '' }))
      .catch((e) => !cancelled && setPreview({ loading: false, results: null, error: errText(e) }))
    return () => {
      cancelled = true
    }
  }, [fix, clusterId, ns, force])

  const result = preview.results?.[0]
  const needsAck = fix?.risk === 'high'
  const canApply =
    !!fix &&
    !busy &&
    (!needsAck || ack) &&
    (fix.action === 'delete' ||
      (fix.action === 'apply' &&
        !!result &&
        result.action !== 'error' &&
        result.action !== 'unchanged'))

  const apply = async () => {
    if (!fix) return
    setBusy(true)
    try {
      if (fix.action === 'delete') {
        await kube.remove(clusterId, fix.target.resourceKey, fix.target.namespace, fix.target.name)
      } else {
        const res = await tools.applyManifest(
          clusterId,
          fix.manifest,
          fix.target.namespace || ns,
          force,
        )
        const bad = res.find((r) => r.action === 'error')
        if (bad) throw new Error(bad.error)
      }
      toast('success', `${fix.title}: done. Run the assessment again to see the new score.`)
      onApplied?.()
      onClose()
    } catch (e) {
      setPreview((p) => ({ ...p, error: errText(e) }))
      setBusy(false)
    }
  }

  return (
    <Dialog title="Fix" onClose={onClose} width={760}>
      <div className="space-y-4 text-[13px]">
        <div className="text-xs text-muted">
          <span className="font-mono">{resourceKey.split('/').pop()}</span> /{' '}
          <span className="font-mono text-fg">{name}</span>
          {ns && <> · {ns}</>}
        </div>
        {!fixes && !error && <div className="text-muted">Looking at the live object…</div>}
        {error && (
          <div className="select-text break-words rounded-md bg-danger/10 px-3 py-2 text-danger">
            {error}
          </div>
        )}
        {fixes && fixes.length === 0 && (
          <div className="text-muted">
            No automatic fix applies to this object (it may already be compliant).
          </div>
        )}

        {fixes && fixes.length > 1 && (
          <div className="flex flex-wrap gap-1.5">
            {fixes.map((f, i) => (
              <button
                key={f.id}
                onClick={() => {
                  setSel(i)
                  setAck(false)
                }}
                className={clsx(
                  'cursor-pointer rounded-md border px-2.5 py-1 text-xs',
                  i === sel
                    ? 'border-accent bg-accent/15 text-accent'
                    : 'border-border hover:border-muted',
                )}
              >
                {f.title}
              </button>
            ))}
          </div>
        )}

        {fix && (
          <>
            <div>
              <div className="flex items-center gap-2">
                <Wrench size={14} className="text-accent" />
                <span className="font-semibold">{fix.title}</span>
                <span
                  className={clsx(
                    'rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
                    riskChip[fix.risk],
                  )}
                >
                  {fix.risk} risk
                </span>
                {fix.managedBy && (
                  <span className="rounded bg-warning/15 px-1.5 py-0.5 text-[10px] font-semibold text-warning">
                    managed by {fix.managedBy}
                  </span>
                )}
              </div>
              <p className="mt-1 text-muted">{fix.description}</p>
            </div>

            {fix.notes.length > 0 && (
              <div
                className={clsx(
                  'space-y-1.5 rounded-md px-3 py-2 text-xs',
                  fix.risk === 'high' ? 'bg-danger/10 text-danger' : 'bg-warning/10 text-warning',
                )}
              >
                {fix.notes.map((n, i) => (
                  <div key={i} className="flex gap-2">
                    {fix.risk === 'high' ? (
                      <ShieldAlert size={14} className="mt-px shrink-0" />
                    ) : (
                      <AlertTriangle size={14} className="mt-px shrink-0" />
                    )}
                    <span>{n}</span>
                  </div>
                ))}
              </div>
            )}

            {fix.action === 'none' && (
              <div className="rounded-md bg-surface-2 px-3 py-2 text-muted">{fix.description}</div>
            )}

            {fix.action === 'delete' && (
              <div className="rounded-md border border-border px-3 py-2">
                This deletes{' '}
                <span className="font-mono">
                  {fix.target.kind}/{fix.target.name}
                </span>
                .
              </div>
            )}

            {fix.action === 'apply' && (
              <div>
                <div className="mb-2 flex items-center gap-1 border-b border-border">
                  {(['diff', 'yaml'] as const).map((t) => (
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
                      {t === 'diff' ? 'Changes (dry-run)' : 'Manifest'}
                    </button>
                  ))}
                  {result && (
                    <span
                      className={clsx(
                        'ml-auto rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
                        actionChip[result.action],
                      )}
                    >
                      {result.action}
                    </span>
                  )}
                </div>
                <div className="h-64 overflow-hidden rounded-md border border-border">
                  {tab === 'yaml' ? (
                    <pre className="h-full select-text overflow-auto whitespace-pre-wrap bg-bg p-3 font-mono text-xs">
                      {fix.manifest}
                    </pre>
                  ) : preview.loading ? (
                    <div className="p-4 text-muted">Running a server-side dry-run…</div>
                  ) : preview.error ? (
                    <div className="select-text p-4 text-danger">{preview.error}</div>
                  ) : result?.action === 'error' ? (
                    <div className="select-text p-4 text-danger">{result.error}</div>
                  ) : result?.action === 'unchanged' ? (
                    <div className="p-4 text-muted">The object already matches this change.</div>
                  ) : result ? (
                    <Suspense fallback={<div className="p-4 text-muted">Loading diff…</div>}>
                      <DiffView before={result.before} after={result.after} />
                    </Suspense>
                  ) : null}
                </div>
                {(result?.action === 'error' && /conflict/i.test(result.error)) ||
                (preview.error && /conflict/i.test(preview.error)) ? (
                  <p className="mt-2 text-xs text-warning">
                    Another tool owns some of these fields. Enable “Take over fields” below to
                    override it.
                  </p>
                ) : null}
                <label className="mt-2 flex cursor-pointer items-center gap-2 text-xs text-muted">
                  <input
                    type="checkbox"
                    checked={force}
                    onChange={(e) => setForce(e.target.checked)}
                    className="accent-[var(--accent)]"
                  />
                  Take over fields owned by other tools (force conflicts)
                </label>
              </div>
            )}

            {needsAck && fix.action !== 'none' && (
              <label className="flex cursor-pointer items-start gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={ack}
                  onChange={(e) => setAck(e.target.checked)}
                  className="mt-0.5 accent-[var(--accent)]"
                />
                I have read the warnings above and want to apply this change.
              </label>
            )}
          </>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          {fix && fix.action !== 'none' && (
            <Button
              variant={fix.action === 'delete' ? 'danger' : 'primary'}
              disabled={!canApply}
              onClick={() => void apply()}
            >
              {busy ? 'Applying…' : fix.action === 'delete' ? 'Delete' : 'Apply fix'}
            </Button>
          )}
        </div>
      </div>
    </Dialog>
  )
}
