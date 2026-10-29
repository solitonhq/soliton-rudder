import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { apps } from '../lib/backend'
import type { AppLinkTemplate } from '../lib/types'
import { useUI } from '../store/ui'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** Links shown on every application of a cluster: a CI job, a dashboard, a runbook… with {name}, {namespace}, {release}, {version}. */
export function AppLinksDialog({
  clusterId,
  onClose,
  onSaved,
}: {
  clusterId: string
  onClose: () => void
  onSaved: () => void
}) {
  const toast = useUI((s) => s.toast)
  const [rows, setRows] = useState<AppLinkTemplate[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    apps
      .links(clusterId)
      .then((l) => setRows(l.length ? l : [{ title: '', url: '' }]))
      .catch((e) => setError(errText(e)))
  }, [clusterId])

  const set = (i: number, patch: Partial<AppLinkTemplate>) =>
    setRows((r) => (r ?? []).map((x, k) => (k === i ? { ...x, ...patch } : x)))

  const save = async () => {
    setBusy(true)
    setError('')
    try {
      await apps.setLinks(clusterId, rows ?? [])
      toast('success', 'Links saved')
      onSaved()
      onClose()
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  const input =
    'h-8 select-text rounded-md border border-border bg-bg px-2 text-[13px] outline-none placeholder:text-muted focus:border-accent'
  return (
    <Dialog title="Application links" onClose={onClose} width={680}>
      <div className="space-y-3 text-[13px]">
        <p className="text-muted">
          Add links that appear on every application of this cluster, for example the CI job that
          deploys it or its dashboard. In the address you can use{' '}
          <code className="font-mono">{'{name}'}</code>,{' '}
          <code className="font-mono">{'{namespace}'}</code>,{' '}
          <code className="font-mono">{'{release}'}</code> and{' '}
          <code className="font-mono">{'{version}'}</code>. Objects can also declare their own links
          with <code className="font-mono">link.argocd.argoproj.io/&lt;title&gt;</code> annotations.
        </p>
        {rows === null && !error && <div className="text-muted">Loading…</div>}
        {rows && (
          <div className="space-y-2">
            {rows.map((r, i) => (
              <div key={i} className="flex gap-2">
                <input
                  aria-label={`Title ${i + 1}`}
                  className={`${input} w-40`}
                  placeholder="Title"
                  value={r.title}
                  onChange={(e) => set(i, { title: e.target.value })}
                />
                <input
                  aria-label={`Address ${i + 1}`}
                  className={`${input} min-w-0 flex-1 font-mono`}
                  placeholder="https://ci.example.com/job/{release}"
                  value={r.url}
                  onChange={(e) => set(i, { url: e.target.value })}
                />
                <button
                  aria-label={`Remove link ${i + 1}`}
                  className="grid size-8 shrink-0 cursor-pointer place-items-center rounded-md text-muted hover:bg-surface-hover hover:text-danger"
                  onClick={() => setRows((x) => (x ?? []).filter((_, k) => k !== i))}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
            <Button
              variant="ghost"
              onClick={() => setRows((x) => [...(x ?? []), { title: '', url: '' }])}
            >
              <Plus size={13} /> Add a link
            </Button>
          </div>
        )}
        {error && (
          <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={busy || rows === null} onClick={() => void save()}>
            {busy ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
