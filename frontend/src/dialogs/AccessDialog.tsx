import { useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { insight } from '../lib/backend'
import type { AccessInfo } from '../lib/types'
import { useClusters } from '../store/clusters'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const sourceText: Record<AccessInfo['source'], string> = {
  cluster: 'Your credentials can read the whole cluster.',
  configured: 'Using the namespaces you chose below.',
  discovered: 'Using the namespaces that these credentials were found to be able to read.',
  kubeconfig: 'Using the namespace of this kubeconfig context. Add the others you work with below.',
  none: 'No namespace could be found that these credentials can read.',
}

/** For credentials that cannot read the whole cluster: which namespaces the app should work with. */
export function AccessDialog({ clusterId, onClose }: { clusterId: string; onClose: () => void }) {
  const toast = useUI((s) => s.toast)
  const name = useClusters((s) => s.clusters.find((c) => c.id === clusterId)?.name ?? '')
  const [info, setInfo] = useState<AccessInfo | null>(null)
  const [text, setText] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    insight
      .access(clusterId)
      .then((a) => {
        if (cancelled) return
        setInfo(a)
        setText(a.configured.join(', '))
      })
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [clusterId])

  const save = async (list: string[]) => {
    setBusy(true)
    setError('')
    try {
      await insight.setAccessNamespaces(clusterId, list)
      toast('success', list.length ? 'Namespaces saved' : 'Back to detecting namespaces')
      onClose()
      // tables, the overview and the namespace list all depend on this: reload the cluster view
      if (useExplorer.getState().clusterId === clusterId)
        void useExplorer.getState().open(clusterId)
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  const parse = (t: string) => t.split(/[\s,]+/).filter(Boolean)

  return (
    <Dialog title={`Access${name ? ` · ${name}` : ''}`} onClose={onClose} width={560}>
      <div className="space-y-3 text-[13px]">
        {!info && !error && <div className="text-muted">Checking permissions…</div>}
        {info && (
          <>
            <div className="rounded-md bg-surface-2 px-3 py-2">
              <div className="font-medium">
                {info.clusterWide ? 'Full access' : 'Limited access'}
              </div>
              <div className="text-muted">{sourceText[info.source]}</div>
              {!info.clusterWide && info.namespaces.length > 0 && (
                <div className="mt-1.5 flex flex-wrap gap-1">
                  {info.namespaces.map((n) => (
                    <span key={n} className="rounded bg-bg px-1.5 py-0.5 font-mono text-xs">
                      {n}
                    </span>
                  ))}
                </div>
              )}
              {info.note && <div className="mt-1.5 text-xs text-warning">{info.note}</div>}
            </div>
            {info.clusterWide ? (
              <p className="text-muted">
                Nothing to configure: every page can read all namespaces. If your rights are limited
                later, the app detects it and works with the namespaces you can read.
              </p>
            ) : (
              <>
                <p className="text-muted">
                  The app cannot list pods across the whole cluster with these credentials, so every
                  page works namespace by namespace.{' '}
                  {info.canListNamespaces ? 'The namespaces above were found automatically; ' : ''}
                  List the namespaces you want to use to override that. Leave it empty to detect
                  them again.
                </p>
                <label className="block">
                  <span className="mb-1 block text-[11px] font-semibold uppercase text-muted">
                    Namespaces (comma or space separated)
                  </span>
                  <textarea
                    aria-label="Namespaces"
                    value={text ?? ''}
                    onChange={(e) => setText(e.target.value)}
                    spellCheck={false}
                    placeholder="team-a, team-b"
                    className="h-20 w-full resize-none select-text rounded-md border border-border bg-bg p-2 font-mono text-xs outline-none focus:border-accent"
                  />
                </label>
              </>
            )}
          </>
        )}
        {error && (
          <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          {info && !info.clusterWide && (
            <>
              {info.configured.length > 0 && (
                <Button disabled={busy} onClick={() => void save([])}>
                  Detect again
                </Button>
              )}
              <Button
                variant="primary"
                disabled={busy}
                onClick={() => void save(parse(text ?? ''))}
              >
                {busy ? 'Saving…' : 'Save'}
              </Button>
            </>
          )}
        </div>
      </div>
    </Dialog>
  )
}
