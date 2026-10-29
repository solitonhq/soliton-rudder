import { useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { runbooks } from '../lib/backend'
import { useUI } from '../store/ui'

const EXAMPLE = `# Your own runbooks. Built-in ones are matched first by how specific they are; an id equal to a built-in's replaces it.
- id: payments-down
  title: Payments service is failing
  description: "{{kind}} {{name}} in {{namespace}} is unhealthy."
  match:
    kinds: [Deployment]        # optional
    reasons: ["0/"]            # case-insensitive text found in the failure reason
    status: error              # optional: warn | error
  steps:
    - title: Look at the health summary
      do: health               # logs, previous-logs, events, health, yaml: read-only, run in the app
    - title: Restart the rollout
      do: restart              # restart, scale, delete: ask for confirmation first
    - title: Tell the on-call
      do: manual               # text only
      text: Page #payments-oncall and link this incident.
`

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function RunbooksEditor({ onClose }: { onClose: () => void }) {
  const toast = useUI((s) => s.toast)
  const [doc, setDoc] = useState<string | null>(null)
  const [builtin, setBuiltin] = useState<{ id: string; title: string }[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    runbooks
      .info()
      .then((i) => {
        setDoc(i.custom)
        setBuiltin(i.builtin)
        setError(i.error)
      })
      .catch((e) => setError(errText(e)))
  }, [])

  const save = async () => {
    if (doc === null) return
    setBusy(true)
    setError('')
    try {
      await runbooks.save(doc)
      toast('success', 'Runbooks saved')
      onClose()
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  return (
    <Dialog title="Runbooks" onClose={onClose} width={820}>
      <div className="space-y-3 text-[13px]">
        <p className="text-muted">
          Write your own troubleshooting guides in YAML. Steps that only read run inside the app;
          steps that change the cluster always ask for confirmation. Built-in:{' '}
          {builtin.map((b) => b.id).join(', ') || '…'}.
        </p>
        <textarea
          aria-label="Runbooks YAML"
          value={doc ?? ''}
          onChange={(e) => setDoc(e.target.value)}
          spellCheck={false}
          placeholder={EXAMPLE}
          className="h-80 w-full resize-none select-text rounded-md border border-border bg-bg p-3 font-mono text-xs outline-none focus:border-accent"
        />
        {error && (
          <div className="select-text whitespace-pre-wrap rounded-md bg-danger/10 px-3 py-2 text-danger">
            {error}
          </div>
        )}
        <div className="flex justify-between">
          <Button variant="ghost" onClick={() => setDoc(EXAMPLE)}>
            Insert example
          </Button>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" disabled={busy || doc === null} onClick={() => void save()}>
              {busy ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </div>
      </div>
    </Dialog>
  )
}
