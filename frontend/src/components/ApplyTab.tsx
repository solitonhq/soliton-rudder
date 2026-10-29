import clsx from 'clsx'
import { FileUp, Link2 } from 'lucide-react'
import { useState } from 'react'
import { tools } from '../lib/backend'
import type { ApplyResult } from '../lib/types'
import type { DockTab } from '../store/dock'
import { useUI } from '../store/ui'
import { DiffView } from './DiffView'
import { Button, Input } from './ui'
import { YamlEditor } from './YamlEditor'

type ApplyTabT = Extract<DockTab, { kind: 'apply' }>

const STARTER = `# Paste one or more manifests (separate with ---), open a file, or load from a URL.
apiVersion: v1
kind: ConfigMap
metadata:
  name: example
data:
  hello: world
`

const badge: Record<ApplyResult['action'], string> = {
  create: 'bg-success/15 text-success',
  configure: 'bg-warning/15 text-warning',
  unchanged: 'bg-surface-2 text-muted',
  error: 'bg-danger/15 text-danger',
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function ApplyTabView({ tab }: { tab: ApplyTabT }) {
  const toast = useUI((s) => s.toast)
  const [text, setText] = useState(STARTER)
  const [force, setForce] = useState(false)
  const [urlOpen, setUrlOpen] = useState(false)
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [results, setResults] = useState<ApplyResult[] | null>(null)
  const [previewed, setPreviewed] = useState<string | null>(null)
  const [applied, setApplied] = useState(false)
  const [sel, setSel] = useState(0)
  const [error, setError] = useState('')

  const edit = (v: string) => {
    setText(v)
    setApplied(false)
  }

  const openFile = async () => {
    try {
      const c = await tools.pickManifestFile()
      if (c) edit(c)
    } catch (e) {
      setError(errText(e))
    }
  }

  const fetchURL = async () => {
    setBusy(true)
    try {
      edit(await tools.fetchManifestURL(url.trim()))
      setUrlOpen(false)
      setError('')
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const run = async (mode: 'preview' | 'apply') => {
    setBusy(true)
    setError('')
    try {
      const fn = mode === 'preview' ? tools.previewManifest : tools.applyManifest
      const r = await fn(tab.clusterId, text, tab.ns, force)
      setResults(r)
      setSel(0)
      if (mode === 'preview') {
        setPreviewed(text)
        setApplied(false)
      } else {
        setApplied(true)
        const failed = r.filter((x) => x.action === 'error').length
        toast(
          failed ? 'error' : 'success',
          failed
            ? `${failed} of ${r.length} failed`
            : `Applied ${r.length} resource${r.length === 1 ? '' : 's'}`,
        )
      }
    } catch (e) {
      setError(errText(e))
      setResults(null)
    } finally {
      setBusy(false)
    }
  }

  const ready = previewed === text && results !== null && !applied
  const changes =
    results?.filter((r) => r.action === 'create' || r.action === 'configure').length ?? 0
  const cur = results?.[sel]

  return (
    <div className="flex h-full">
      <div className="flex min-w-0 flex-1 flex-col border-r border-border">
        <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-1.5">
          <Button className="h-7 text-xs" onClick={() => void openFile()}>
            <FileUp size={13} /> Open file
          </Button>
          <Button className="h-7 text-xs" onClick={() => setUrlOpen((o) => !o)}>
            <Link2 size={13} /> From URL
          </Button>
          {urlOpen && (
            <>
              <Input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://…/manifest.yaml"
                className="h-7 w-72 text-xs"
              />
              <Button
                className="h-7 text-xs"
                disabled={!url.trim() || busy}
                onClick={() => void fetchURL()}
              >
                Load
              </Button>
            </>
          )}
          <label
            className="ml-auto flex cursor-pointer items-center gap-1.5 text-xs text-muted"
            title="Take ownership of fields managed by other tools"
          >
            <input
              type="checkbox"
              checked={force}
              onChange={(e) => setForce(e.target.checked)}
              className="accent-[var(--accent)]"
            />{' '}
            Force conflicts
          </label>
          <span className="text-xs text-muted">
            default ns: <span className="font-mono">{tab.ns || 'default'}</span>
          </span>
        </div>
        <div className="min-h-0 flex-1">
          <YamlEditor value={text} onChange={edit} />
        </div>
        {error && (
          <div className="max-h-24 select-text overflow-auto border-t border-border bg-danger/10 px-3 py-2 text-xs text-danger">
            {error}
          </div>
        )}
        <div className="flex items-center justify-end gap-2 border-t border-border px-3 py-2">
          <Button disabled={busy || !text.trim()} onClick={() => void run('preview')}>
            Preview changes
          </Button>
          <Button
            variant="primary"
            disabled={busy || !ready || changes === 0}
            onClick={() => void run('apply')}
          >
            {changes > 0 ? `Apply ${changes} change${changes === 1 ? '' : 's'}` : 'Apply'}
          </Button>
        </div>
      </div>

      <div className="flex w-[52%] min-w-0 flex-col">
        {!results ? (
          <div className="grid h-full place-items-center p-6 text-center text-[13px] text-muted">
            Preview runs a server-side dry-run and shows exactly what would change.
          </div>
        ) : (
          <>
            <div className="flex gap-1 overflow-x-auto border-b border-border px-2 py-1.5">
              {results.map((r, i) => (
                <button
                  key={i}
                  onClick={() => setSel(i)}
                  className={clsx(
                    'flex shrink-0 cursor-pointer items-center gap-2 rounded-md px-2.5 py-1 text-xs',
                    i === sel ? 'bg-surface-hover' : 'hover:bg-surface-hover',
                  )}
                >
                  <span
                    className={clsx(
                      'rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase',
                      badge[r.action],
                    )}
                  >
                    {applied && r.action !== 'error' ? 'done' : r.action}
                  </span>
                  <span className="font-medium">
                    {r.kind}/{r.name}
                  </span>
                </button>
              ))}
            </div>
            <div className="min-h-0 flex-1">
              {cur?.action === 'error' ? (
                <div className="select-text p-4 text-[13px] text-danger">{cur.error}</div>
              ) : cur ? (
                <DiffView key={`${sel}-${applied}`} before={cur.before} after={cur.after} />
              ) : null}
            </div>
          </>
        )}
      </div>
    </div>
  )
}
