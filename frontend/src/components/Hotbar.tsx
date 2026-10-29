import clsx from 'clsx'
import { GitCompare, Home, Plus, Settings } from 'lucide-react'
import { useState } from 'react'
import { useClusters } from '../store/clusters'
import { useUI } from '../store/ui'
import { ClusterBadge } from './ClusterBadge'
import { ContextMenu, type MenuItem } from './ContextMenu'
import { connectionLook } from '../lib/connection'
import { Logo } from './Logo'
import { ThemeSwitcher } from './ThemeSwitcher'

export function Hotbar() {
  const {
    clusters,
    health,
    connected,
    activeId,
    setActive,
    update,
    reorder,
    remove,
    connect,
    disconnect,
  } = useClusters()
  const open = useUI((s) => s.open)
  const [dragId, setDragId] = useState<string | null>(null)
  const [overId, setOverId] = useState<string | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null)

  // The hotbar holds the clusters you pinned plus the ones that are connected; Home lists all of them.
  const shown = clusters.filter((c) => c.pinned || connected[c.id] || c.id === activeId)

  const drop = (targetId: string) => {
    if (!dragId || dragId === targetId) return
    const ids = clusters.map((c) => c.id).filter((i) => i !== dragId)
    ids.splice(ids.indexOf(targetId), 0, dragId)
    void reorder(ids)
  }

  return (
    <aside className="drag flex w-[68px] shrink-0 flex-col items-center border-r border-border bg-hotbar pb-3 pt-9">
      <button
        title="Home"
        aria-label="Home"
        onClick={() => setActive(null)}
        className="no-drag mb-2 cursor-pointer"
      >
        <Logo size={38} />
      </button>
      <RailButton title="Home" active={activeId === null} onClick={() => setActive(null)}>
        <Home size={18} />
      </RailButton>
      <div className="my-2 h-px w-8 bg-border" />

      <div className="no-drag flex min-h-0 flex-1 flex-col items-center gap-2 overflow-y-auto overflow-x-hidden py-1">
        {shown.map((c) => {
          const look = connectionLook(!!connected[c.id], health[c.id]?.status, health[c.id]?.error)
          const active = activeId === c.id
          return (
            <div key={c.id} className="relative flex w-full shrink-0 justify-center">
              {active && (
                <span className="absolute left-0 top-1/2 h-6 w-[3px] -translate-y-1/2 rounded-r bg-accent" />
              )}
              <button
                title={`${c.name}\n${c.context}\n${look.label}${c.pinned ? '' : '\nNot pinned: disconnect to remove it from the hotbar'}`}
                aria-label={`Cluster ${c.name}`}
                draggable
                onDragStart={() => setDragId(c.id)}
                onDragOver={(e) => {
                  e.preventDefault()
                  setOverId(c.id)
                }}
                onDragEnd={() => {
                  setDragId(null)
                  setOverId(null)
                }}
                onDrop={() => {
                  drop(c.id)
                  setDragId(null)
                  setOverId(null)
                }}
                onClick={() => setActive(c.id)}
                onContextMenu={(e) => {
                  e.preventDefault()
                  setMenu({
                    x: e.clientX,
                    y: e.clientY,
                    items: [
                      { label: 'Edit…', onClick: () => open({ kind: 'edit', id: c.id }) },
                      {
                        label: c.pinned ? 'Unpin from hotbar' : 'Pin to hotbar',
                        onClick: () => void update(c.id, { pinned: !c.pinned }),
                      },
                      {
                        label: 'Access…',
                        onClick: () => open({ kind: 'access', clusterId: c.id }),
                      },
                      connected[c.id]
                        ? { label: 'Disconnect', onClick: () => void disconnect(c.id) }
                        : { label: 'Connect', onClick: () => void connect(c.id) },
                      {
                        label: 'Delete',
                        danger: true,
                        onClick: () => confirm(`Remove "${c.name}"?`) && void remove(c.id),
                      },
                    ],
                  })
                }}
                className={clsx(
                  'cursor-pointer rounded-[14px] border-2 p-0.5 transition-all',
                  look.ring,
                  !active && 'opacity-80 hover:opacity-100',
                  overId === c.id && dragId !== c.id && 'outline outline-2 outline-fg/40',
                  dragId === c.id && 'opacity-40',
                )}
              >
                <ClusterBadge cluster={c} size={36} />
              </button>
            </div>
          )
        })}
        {shown.length === 0 && (
          <p className="px-1 text-center text-[10px] leading-tight text-muted">
            Pin clusters on Home to keep them here
          </p>
        )}
        <button
          title="Add cluster"
          aria-label="Add cluster"
          onClick={() => open({ kind: 'add' })}
          className="grid size-[38px] shrink-0 cursor-pointer place-items-center rounded-xl border border-dashed border-border text-muted hover:border-accent hover:text-accent"
        >
          <Plus size={18} />
        </button>
      </div>

      <div className="mt-2 flex flex-col items-center gap-2">
        <ThemeSwitcher vertical />
        <RailButton title="Compare clusters" onClick={() => open({ kind: 'compare' })}>
          <GitCompare size={18} />
        </RailButton>
        <RailButton title="Settings" onClick={() => open({ kind: 'settings' })}>
          <Settings size={18} />
        </RailButton>
      </div>
      {menu && <ContextMenu {...menu} onClose={() => setMenu(null)} />}
    </aside>
  )
}

function RailButton({
  title,
  active,
  onClick,
  children,
}: {
  title: string
  active?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      title={title}
      aria-label={title}
      onClick={onClick}
      className={clsx(
        'no-drag grid size-9 cursor-pointer place-items-center rounded-lg hover:bg-surface-hover hover:text-fg',
        active ? 'bg-surface-hover text-fg' : 'text-muted',
      )}
    >
      {children}
    </button>
  )
}
