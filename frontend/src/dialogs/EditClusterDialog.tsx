import clsx from 'clsx'
import { useState } from 'react'
import { ClusterBadge } from '../components/ClusterBadge'
import { Button, Dialog, Field, Input } from '../components/ui'
import { useClusters } from '../store/clusters'

const COLORS = [
  '#5b8def',
  '#3fb97f',
  '#e0a63a',
  '#e5584f',
  '#a77bf3',
  '#2fc4c4',
  '#ef7bb0',
  '#8b95a5',
]

export function EditClusterDialog({ id, onClose }: { id: string; onClose: () => void }) {
  const cluster = useClusters((s) => s.clusters.find((c) => c.id === id))
  const { update, remove } = useClusters()
  const [name, setName] = useState(cluster?.name ?? '')
  const [color, setColor] = useState(cluster?.color ?? COLORS[0])
  const [icon, setIcon] = useState(cluster?.icon ?? '')
  const [pinned, setPinned] = useState(cluster?.pinned ?? false)
  const [error, setError] = useState('')
  if (!cluster) return null

  const save = async () => {
    try {
      await update(id, { name, color, icon: icon.trim().slice(0, 2), pinned })
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <Dialog title="Edit cluster" onClose={onClose} width={440}>
      <div className="space-y-4">
        <div className="flex items-center gap-3">
          <ClusterBadge
            cluster={{ ...cluster, name: name || cluster.name, color, icon }}
            size={48}
          />
          <div className="min-w-0 text-xs text-muted">
            <div className="truncate font-mono">{cluster.context}</div>
            <div className="truncate">
              {cluster.source === 'file' ? cluster.kubeconfigPath : 'Stored in Keychain'}
            </div>
          </div>
        </div>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Icon (1–2 characters, optional)">
          <Input
            value={icon}
            onChange={(e) => setIcon(e.target.value)}
            maxLength={2}
            placeholder="auto"
          />
        </Field>
        <Field label="Color">
          <div className="flex gap-2">
            {COLORS.map((c) => (
              <button
                key={c}
                onClick={() => setColor(c)}
                aria-label={c}
                className={clsx(
                  'size-6 cursor-pointer rounded-full',
                  color === c && 'ring-2 ring-fg ring-offset-2 ring-offset-surface',
                )}
                style={{ background: c }}
              />
            ))}
          </div>
        </Field>
        <label className="flex cursor-pointer items-center gap-2 text-[13px]">
          <input
            type="checkbox"
            checked={pinned}
            onChange={(e) => setPinned(e.target.checked)}
            className="accent-[var(--accent)]"
          />
          Pin to the hotbar
        </label>
        {error && <p className="text-xs text-danger">{error}</p>}
        <div className="flex items-center justify-between pt-1">
          <Button
            variant="ghost"
            className="text-danger hover:text-danger"
            onClick={() => {
              if (
                confirm(
                  `Remove "${cluster.name}" from Soliton Rudder? Your kubeconfig is not modified.`,
                )
              ) {
                void remove(id).then(onClose)
              }
            }}
          >
            Delete
          </Button>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" onClick={save} disabled={!name.trim()}>
              Save
            </Button>
          </div>
        </div>
      </div>
    </Dialog>
  )
}
