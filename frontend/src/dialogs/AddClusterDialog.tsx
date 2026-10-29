import clsx from 'clsx'
import { useEffect, useState } from 'react'
import { Dialog, Button, Input } from '../components/ui'
import { backend } from '../lib/backend'
import type { ContextInfo } from '../lib/types'
import { useClusters } from '../store/clusters'

type Mode = 'file' | 'paste'

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function AddClusterDialog({ onClose }: { onClose: () => void }) {
  const addImported = useClusters((s) => s.addImported)
  const [mode, setMode] = useState<Mode>('file')
  const [path, setPath] = useState('')
  const [text, setText] = useState('')
  const [contexts, setContexts] = useState<ContextInfo[] | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    void backend.clusters.defaultPath().then(setPath)
  }, [])

  const reset = () => {
    setContexts(null)
    setSelected(new Set())
    setError('')
  }

  const preview = async (p = path) => {
    reset()
    setBusy(true)
    try {
      const infos =
        mode === 'file'
          ? await backend.clusters.previewFile(p)
          : await backend.clusters.previewText(text)
      setContexts(infos)
      setSelected(new Set(infos.filter((i) => !i.exists).map((i) => i.name)))
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  const browse = async () => {
    const p = await backend.clusters.pickFile()
    if (p) {
      setPath(p)
      void preview(p)
    }
  }

  const doImport = async () => {
    setBusy(true)
    setError('')
    try {
      const names = [...selected]
      const added =
        mode === 'file'
          ? await backend.clusters.importFile(path, names)
          : await backend.clusters.importText(text, names)
      await addImported(added)
      onClose()
    } catch (e) {
      setError(errMsg(e))
      setBusy(false)
    }
  }

  const toggle = (name: string) =>
    setSelected((s) => {
      const n = new Set(s)
      if (!n.delete(name)) n.add(name)
      return n
    })

  return (
    <Dialog title="Add cluster" onClose={onClose} width={560}>
      <div className="mb-3 inline-flex rounded-md border border-border p-0.5">
        {(['file', 'paste'] as const).map((m) => (
          <button
            key={m}
            onClick={() => {
              setMode(m)
              reset()
            }}
            className={clsx(
              'cursor-pointer rounded px-3 py-1 text-xs font-medium',
              mode === m ? 'bg-surface-hover text-fg' : 'text-muted hover:text-fg',
            )}
          >
            {m === 'file' ? 'From file' : 'Paste kubeconfig'}
          </button>
        ))}
      </div>

      {mode === 'file' ? (
        <div className="flex gap-2">
          <Input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="~/.kube/config"
            spellCheck={false}
          />
          <Button onClick={browse}>Browse…</Button>
          <Button variant="primary" onClick={() => void preview()} disabled={!path || busy}>
            Scan
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="apiVersion: v1&#10;kind: Config&#10;…"
            spellCheck={false}
            className="h-40 w-full select-text resize-none rounded-md border border-border bg-bg p-2.5 font-mono text-xs outline-none focus:border-accent"
          />
          <Button variant="primary" onClick={() => void preview()} disabled={!text.trim() || busy}>
            Scan
          </Button>
          <p className="text-xs text-muted">
            Credentials are stored in the macOS Keychain, never on disk.
          </p>
        </div>
      )}

      {error && <p className="mt-3 text-xs text-danger">{error}</p>}

      {contexts && (
        <div className="mt-4">
          <div className="mb-1.5 text-xs font-medium text-muted">Contexts ({contexts.length})</div>
          <div className="max-h-56 overflow-auto rounded-md border border-border">
            {contexts.map((c) => (
              <label
                key={c.name}
                className={clsx(
                  'flex items-center gap-3 border-b border-border px-3 py-2 last:border-0',
                  c.exists ? 'opacity-50' : 'cursor-pointer hover:bg-surface-hover',
                )}
              >
                <input
                  type="checkbox"
                  disabled={c.exists}
                  checked={selected.has(c.name)}
                  onChange={() => toggle(c.name)}
                  className="accent-[var(--accent)]"
                />
                <div className="min-w-0">
                  <div className="truncate text-[13px] font-medium">
                    {c.name}{' '}
                    {c.current && <span className="ml-1 text-xs text-muted">(current)</span>}
                    {c.exists && <span className="ml-1 text-xs text-muted">already added</span>}
                  </div>
                  <div className="truncate font-mono text-xs text-muted">{c.server}</div>
                </div>
              </label>
            ))}
          </div>
        </div>
      )}

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" disabled={selected.size === 0 || busy} onClick={doImport}>
          Add {selected.size > 0 ? `${selected.size} cluster${selected.size > 1 ? 's' : ''}` : ''}
        </Button>
      </div>
    </Dialog>
  )
}
