import clsx from 'clsx'
import { ArrowRight } from 'lucide-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { drift, tools, type DriftSide } from '../lib/backend'
import type { ApplyResult, Promotion } from '../lib/types'
import { useUI } from '../store/ui'

const DiffView = lazy(() => import('../components/DiffView').then((m) => ({ default: m.DiffView })))
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

/**
 * Carries the chosen fields (or a whole missing object) to the target cluster. The change is shown as a
 * server-side dry-run on the TARGET first; nothing is written until the user presses the apply button.
 */
export function PromoteDialog({
  src,
  dst,
  srcName,
  dstName,
  resourceKey,
  name,
  paths,
  onClose,
  onApplied,
}: {
  src: DriftSide
  dst: DriftSide
  srcName: string
  dstName: string
  resourceKey: string
  name: string
  paths: string[]
  onClose: () => void
  onApplied: () => void
}) {
  const toast = useUI((s) => s.toast)
  const [promo, setPromo] = useState<Promotion | null>(null)
  const [loadError, setError] = useState('')
  const [force, setForce] = useState(false)
  const [tab, setTab] = useState<'diff' | 'yaml'>('diff')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    drift
      .promotion(src, dst, resourceKey, name, paths)
      .then((p) => !cancelled && setPromo(p))
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src.cluster, src.ns, dst.cluster, dst.ns, resourceKey, name, paths.join('\n')])

  // a dry-run result is only valid for the request it was made for; anything else counts as "loading"
  const reqKey = promo ? `${dst.cluster}|${dst.ns}|${force}|${promo.manifest}` : ''
  const [dry, setDry] = useState<{ key: string; result: ApplyResult | null; error: string }>({
    key: '',
    result: null,
    error: '',
  })
  useEffect(() => {
    if (!promo) return
    let cancelled = false
    tools
      .previewManifest(dst.cluster, promo.manifest, dst.ns, force)
      .then((r) => !cancelled && setDry({ key: reqKey, result: r[0] ?? null, error: '' }))
      .catch((e) => !cancelled && setDry({ key: reqKey, result: null, error: errText(e) }))
    return () => {
      cancelled = true
    }
  }, [promo, dst.cluster, dst.ns, force, reqKey])
  const loading = !!promo && dry.key !== reqKey
  const result = loading ? null : dry.result
  const dryError = loading ? '' : dry.error

  const failed = result?.action === 'error' ? result.error : ''
  const error = loadError || dryError
  const conflict = /conflict/i.test(failed || error)
  const canApply =
    !!promo &&
    !!result &&
    !busy &&
    !loading &&
    result.action !== 'error' &&
    result.action !== 'unchanged'

  const apply = async () => {
    if (!promo) return
    setBusy(true)
    try {
      const res = await tools.applyManifest(dst.cluster, promo.manifest, dst.ns, force)
      const bad = res.find((r) => r.action === 'error')
      if (bad) throw new Error(bad.error)
      toast('success', `${name} updated in ${dstName}`)
      onApplied()
      onClose()
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  return (
    <Dialog title="Promote" onClose={onClose} width={780}>
      <div className="space-y-4 text-[13px]">
        <div className="flex flex-wrap items-center gap-2">
          <span className="rounded bg-surface-2 px-2 py-0.5 font-medium">
            {srcName}
            {src.ns && <span className="text-muted"> / {src.ns}</span>}
          </span>
          <ArrowRight size={14} className="text-muted" />
          <span className="rounded bg-danger/15 px-2 py-0.5 font-semibold text-danger">
            {dstName}
            {dst.ns && <span className="font-normal"> / {dst.ns}</span>}
          </span>
          <span className="text-muted">·</span>
          <span className="font-mono">
            {resourceKey.split('/').pop()}/{name}
          </span>
        </div>
        <p className="text-muted">
          {promo?.create
            ? 'The object does not exist in the target: the whole object will be created there.'
            : `Only the ${paths.length} selected field${paths.length === 1 ? '' : 's'} will be written to the target; everything else in it is left alone.`}
        </p>

        {!promo && !error && <div className="text-muted">Preparing the change…</div>}
        {promo && (
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
                  {t === 'diff' ? `Changes in ${dstName} (dry-run)` : 'Manifest'}
                </button>
              ))}
              {result && (
                <span className="ml-auto rounded bg-surface-2 px-1.5 py-0.5 text-[10px] font-bold uppercase">
                  {result.action}
                </span>
              )}
            </div>
            <div className="h-72 overflow-hidden rounded-md border border-border">
              {tab === 'yaml' ? (
                <pre className="h-full select-text overflow-auto whitespace-pre-wrap bg-bg p-3 font-mono text-xs">
                  {promo.manifest}
                </pre>
              ) : loading ? (
                <div className="p-4 text-muted">Running a server-side dry-run on {dstName}…</div>
              ) : error ? (
                <div className="select-text p-4 text-danger">{error}</div>
              ) : failed ? (
                <div className="select-text p-4 text-danger">{failed}</div>
              ) : result?.action === 'unchanged' ? (
                <div className="p-4 text-muted">The target already has these values.</div>
              ) : result ? (
                <Suspense fallback={<div className="p-4 text-muted">Loading diff…</div>}>
                  <DiffView before={result.before} after={result.after} />
                </Suspense>
              ) : null}
            </div>
            {conflict && (
              <p className="mt-2 text-xs text-warning">
                Another tool (for example Helm, Argo CD or kubectl) owns some of these fields in the
                target. Enable “Take over fields” to override it, or change them in that tool
                instead.
              </p>
            )}
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
        {!promo && error && (
          <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!canApply} onClick={() => void apply()}>
            {busy ? 'Applying…' : `Apply to ${dstName}`}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
