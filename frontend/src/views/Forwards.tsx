import clsx from 'clsx'
import { ExternalLink, HeartPulse, Play, Plus, Square, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button } from '../components/ui'
import { ForwardDialog } from '../dialogs/ForwardDialog'
import { openURL, tools } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { ForwardRuntime } from '../lib/types'
import { useForwards } from '../store/forwards'
import { useUI } from '../store/ui'

const dot: Record<ForwardRuntime['status'], string> = {
  active: 'bg-success',
  connecting: 'bg-warning animate-pulse',
  healing: 'bg-warning animate-pulse',
  degraded: 'bg-warning',
  error: 'bg-danger',
  stopped: 'bg-muted/50',
}

const label: Record<ForwardRuntime['status'], string> = {
  active: 'Active',
  connecting: 'Connecting',
  healing: 'Healing',
  degraded: 'Degraded',
  error: 'Error',
  stopped: 'Stopped',
}

const HEALED_BADGE_SECS = 120

/** "Auto-healed" stays visible for two minutes after a repair, so a quiet rebind is still noticeable. */
export const recentlyHealed = (f: ForwardRuntime, now = Date.now()) =>
  f.status === 'active' && f.reconnects > 0 && now / 1000 - f.lastHealedAt < HEALED_BADGE_SECS

export function Forwards({ clusterId }: { clusterId: string }) {
  const items = useForwards((s) => s.items)
  const toast = useUI((s) => s.toast)
  const [adding, setAdding] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    void useForwards.getState().load(clusterId)
    const t = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(t)
  }, [clusterId])

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      await useForwards.getState().load(clusterId)
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e))
    }
  }

  const healed = items.reduce((n, f) => n + f.reconnects, 0)

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
        <h1 className="text-sm font-semibold">
          Port Forwards{' '}
          <span className="ml-1 font-mono text-xs font-normal text-muted">{items.length}</span>
          {healed > 0 && (
            <span
              className="ml-3 inline-flex items-center gap-1 text-xs font-normal text-muted"
              title="Total automatic repairs since the forwards were started"
            >
              <HeartPulse size={12} className="text-success" /> {healed} auto-repair
              {healed === 1 ? '' : 's'}
            </span>
          )}
        </h1>
        <Button variant="primary" onClick={() => setAdding(true)}>
          <Plus size={14} /> Add forward
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        {items.length === 0 ? (
          <div className="mx-auto mt-16 max-w-sm text-center text-[13px] text-muted">
            No port forwards yet. Hover a Pod, Service or workload in any list and click{' '}
            <span className="font-medium text-fg">Forward</span> for a one-click tunnel on a free
            local port. Forwards keep their local port and rebind to a healthy replica when a pod
            restarts or rolls.
          </div>
        ) : (
          <div className="overflow-hidden rounded-xl border border-border">
            {items.map((f) => (
              <div
                key={f.id}
                className="flex items-center gap-4 border-b border-border bg-surface px-4 py-3 last:border-0"
              >
                <span
                  className={clsx('size-2.5 shrink-0 rounded-full', dot[f.status])}
                  title={label[f.status]}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 truncate text-[13px] font-medium">
                    <span className="truncate">
                      <span className="text-muted">{f.kind}/</span>
                      {f.name} <span className="font-normal text-muted">· {f.namespace}</span>
                    </span>
                    {recentlyHealed(f, now) && (
                      <span
                        className="shrink-0 rounded-full bg-success/15 px-2 py-0.5 text-[10px] font-semibold uppercase text-success"
                        title={f.lastHealedReason}
                      >
                        auto-healed
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted">
                    {f.status === 'error' || f.status === 'degraded' || f.status === 'healing' ? (
                      <span className={f.status === 'error' ? 'text-danger' : 'text-warning'}>
                        {label[f.status]}: {f.message}
                      </span>
                    ) : f.status === 'active' ? (
                      f.pod ? (
                        `via ${f.pod}`
                      ) : (
                        'active'
                      )
                    ) : (
                      label[f.status].toLowerCase()
                    )}
                  </div>
                  {f.reconnects > 0 && (
                    <div
                      className="mt-0.5 truncate text-[11px] text-muted"
                      title={f.lastHealedReason}
                    >
                      healed {f.reconnects}× · last{' '}
                      {formatAge(new Date(f.lastHealedAt * 1000).toISOString())} ago:{' '}
                      {f.lastHealedReason}
                    </div>
                  )}
                </div>
                <div className="font-mono text-[13px]">
                  {f.activePort || f.localPort || 'auto'} <span className="text-muted">→</span>{' '}
                  {f.remotePort}
                </div>
                <label
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted"
                  title="Start when the cluster connects"
                >
                  <input
                    type="checkbox"
                    checked={f.auto}
                    className="accent-[var(--accent)]"
                    onChange={(e) => void act(() => tools.forwards.setAuto(f.id, e.target.checked))}
                  />{' '}
                  Auto
                </label>
                <div className="flex gap-1">
                  {f.activePort > 0 && f.status !== 'stopped' && (
                    <Button
                      variant="ghost"
                      className="px-2"
                      title="Open in browser"
                      onClick={() => openURL(`http://localhost:${f.activePort}`)}
                    >
                      <ExternalLink size={14} />
                    </Button>
                  )}
                  {f.status === 'stopped' ? (
                    <Button
                      variant="ghost"
                      className="px-2"
                      title="Start"
                      onClick={() => void act(() => tools.forwards.start(f.id))}
                    >
                      <Play size={14} />
                    </Button>
                  ) : (
                    <Button
                      variant="ghost"
                      className="px-2"
                      title="Stop"
                      onClick={() => void act(async () => tools.forwards.stop(f.id))}
                    >
                      <Square size={14} />
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    className="px-2 text-danger hover:text-danger"
                    title="Remove"
                    onClick={() => void act(() => tools.forwards.remove(f.id))}
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      {adding && <ForwardDialog clusterId={clusterId} onClose={() => setAdding(false)} />}
    </div>
  )
}
