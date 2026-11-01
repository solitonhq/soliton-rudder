import clsx from 'clsx'
import { GitCompare, Home, Plus, Settings, SquareTerminal } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useDock } from '../store/dock'
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
  const dockTabs = useDock((s) => s.tabs)
  const [dragId, setDragId] = useState<string | null>(null)
  const [overId, setOverId] = useState<string | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null)

  // The hotbar holds the clusters you pinned plus the ones that are connected; Home lists all of them.
  const shown = clusters.filter((c) => c.pinned || connected[c.id] || c.id === activeId)
  // Pinned clusters come first; clusters that are only open (not pinned) follow after a divider.
  const pinnedShown = shown.filter((c) => c.pinned)
  const ordered = [...pinnedShown, ...shown.filter((c) => !c.pinned)]

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

      <div className="no-drag flex min-h-0 w-full flex-1 flex-col items-center gap-2 overflow-y-auto overflow-x-hidden py-1">
        {ordered.map((c, i) => {
          const look = connectionLook(!!connected[c.id], health[c.id]?.status, health[c.id]?.error)
          const active = activeId === c.id
          return (
            <Fragment key={c.id}>
              {i === pinnedShown.length && pinnedShown.length > 0 && (
                <div
                  role="separator"
                  aria-label="Open clusters that are not pinned"
                  title="Open, not pinned: disconnect to remove from the hotbar"
                  className="my-0.5 h-px w-8 shrink-0 bg-fg/25"
                />
              )}
              <div className="relative flex w-full shrink-0 justify-center">
                {active && (
                  <span
                    data-testid="active-marker"
                    className="absolute left-0 top-1/2 h-7 w-[3px] -translate-y-1/2 rounded-r-full bg-accent"
                  />
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
                {(() => {
                  // shells and logs are kept per cluster: show that this one still has some open
                  const n = dockTabs.filter((t) => t.clusterId === c.id).length
                  return n > 0 ? (
                    <span
                      className="pointer-events-none absolute -bottom-0.5 right-1 flex items-center gap-0.5 rounded-full border border-border bg-surface px-1 text-[9px] leading-[14px] text-muted"
                      title={`${n} open terminal or log tab${n === 1 ? '' : 's'}`}
                      aria-label={`${n} open terminal or log tabs`}
                    >
                      <SquareTerminal size={8} />
                      {n}
                    </span>
                  ) : null
                })()}
              </div>
            </Fragment>
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
