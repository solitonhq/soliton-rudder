import clsx from 'clsx'
import { Minus, Plus } from 'lucide-react'
import { useState } from 'react'
import { Button, Dialog, Input } from '../components/ui'

const MAX = 10_000
const PRESETS = [0, 1, 2, 3, 5, 10]

const stepBtn =
  'inline-flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-md border border-border text-muted hover:border-accent hover:text-accent disabled:cursor-not-allowed disabled:opacity-40'

/**
 * Sets a replica count. `initial` is the current desired count when it is known; otherwise the field starts empty so
 * nothing is scaled by a stray Enter. `ready` is shown next to it so you can see what is actually running now.
 */
export function ScaleDialog({
  name,
  kind = 'workload',
  initial,
  ready,
  onClose,
  onScale,
}: {
  name: string
  kind?: string
  initial?: number | null
  ready?: number | null
  onClose: () => void
  onScale: (n: number) => Promise<void>
}) {
  const [n, setN] = useState(initial == null ? '' : String(initial))
  const valid = /^\d+$/.test(n) && Number(n) <= MAX
  const target = valid ? Number(n) : null
  const unchanged = target !== null && initial != null && target === initial
  const delta = target !== null && initial != null ? target - initial : null

  const set = (v: number) => setN(String(Math.min(MAX, Math.max(0, v))))
  const submit = () => {
    if (target === null || unchanged) return
    onClose()
    void onScale(target)
  }

  return (
    <Dialog title={`Scale ${name}`} onClose={onClose} width={420}>
      <p className="mb-4 text-xs text-muted">
        Set how many replicas of this {kind} should run. Kubernetes adds or removes pods to match.
      </p>

      {initial != null && (
        <div className="mb-4 grid grid-cols-2 gap-2 text-xs">
          <div className="rounded-lg border border-border px-3 py-2">
            <div className="text-[10px] uppercase tracking-wide text-muted">Desired now</div>
            <div className="text-lg font-semibold tabular-nums">{initial}</div>
          </div>
          <div className="rounded-lg border border-border px-3 py-2">
            <div className="text-[10px] uppercase tracking-wide text-muted">Ready now</div>
            <div
              className={clsx(
                'text-lg font-semibold tabular-nums',
                ready != null && ready < initial && 'text-warning',
              )}
            >
              {ready ?? '—'}
            </div>
          </div>
        </div>
      )}

      <label className="block text-xs font-medium text-muted" htmlFor="scale-replicas">
        Replicas
      </label>
      <div className="mt-1 flex items-center gap-2">
        <button
          type="button"
          aria-label="Decrease"
          className={stepBtn}
          disabled={target === null || target <= 0}
          onClick={() => set((target ?? 0) - 1)}
        >
          <Minus size={14} aria-hidden />
        </button>
        <Input
          id="scale-replicas"
          value={n}
          onChange={(e) => setN(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && submit()}
          autoFocus
          inputMode="numeric"
          className="h-9 flex-1 text-center text-base font-semibold tabular-nums"
        />
        <button
          type="button"
          aria-label="Increase"
          className={stepBtn}
          disabled={target !== null && target >= MAX}
          onClick={() => set((target ?? initial ?? 0) + 1)}
        >
          <Plus size={14} aria-hidden />
        </button>
      </div>

      <div className="mt-2 flex flex-wrap gap-1.5">
        {PRESETS.map((p) => (
          <button
            key={p}
            type="button"
            onClick={() => set(p)}
            className={clsx(
              'cursor-pointer rounded-md border px-2.5 py-0.5 font-mono text-xs hover:border-accent',
              target === p ? 'border-accent text-accent' : 'border-border text-muted',
            )}
          >
            {p}
          </button>
        ))}
      </div>

      <div className="mt-3 min-h-5 text-xs">
        {!valid && n !== '' && (
          <span className="text-danger">
            Enter a whole number from 0 to {MAX.toLocaleString()}.
          </span>
        )}
        {valid && delta !== null && delta !== 0 && (
          <span className="text-muted">
            {initial} → <span className="font-semibold text-fg">{target}</span>{' '}
            <span className={delta > 0 ? 'text-success' : 'text-warning'}>
              ({delta > 0 ? `+${delta}` : delta} {Math.abs(delta) === 1 ? 'pod' : 'pods'})
            </span>
          </span>
        )}
        {unchanged && <span className="text-muted">Already at {initial} replicas.</span>}
      </div>
      {n === '0' && (
        <p className="mt-1 text-xs text-warning">Scaling to 0 stops every replica of {name}.</p>
      )}

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" disabled={target === null || unchanged} onClick={submit}>
          {target === null ? 'Scale' : `Scale to ${target}`}
        </Button>
      </div>
    </Dialog>
  )
}
