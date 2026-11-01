import { useState } from 'react'
import { Button, Dialog, Input } from '../components/ui'

/** Asks for a replica count. `initial` prefills the current desired count when it is known; otherwise it starts empty so nothing is scaled by a stray Enter. */
export function ScaleDialog({
  name,
  initial,
  onClose,
  onScale,
}: {
  name: string
  initial?: number | null
  onClose: () => void
  onScale: (n: number) => Promise<void>
}) {
  const [n, setN] = useState(initial == null ? '' : String(initial))
  const valid = /^\d+$/.test(n) && Number(n) <= 10_000
  const submit = () => {
    if (!valid) return
    onClose()
    void onScale(Number(n))
  }
  return (
    <Dialog title={`Scale ${name}`} onClose={onClose} width={360}>
      <label className="block text-xs font-medium text-muted" htmlFor="scale-replicas">
        Replicas
      </label>
      <Input
        id="scale-replicas"
        value={n}
        onChange={(e) => setN(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        autoFocus
        inputMode="numeric"
        className="mt-1"
      />
      {n === '0' && (
        <p className="mt-2 text-xs text-warning">Scaling to 0 stops every replica of {name}.</p>
      )}
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" disabled={!valid} onClick={submit}>
          Scale
        </Button>
      </div>
    </Dialog>
  )
}
