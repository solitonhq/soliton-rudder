import clsx from 'clsx'
import {
  ChevronDown,
  ChevronUp,
  FileCode2,
  Puzzle,
  ScrollText,
  SquareTerminal,
  X,
} from 'lucide-react'
import { PluginFrame } from './PluginFrame'
import { lazy, Suspense, useRef } from 'react'
import { useClusters } from '../store/clusters'
import { useDock, type DockTab } from '../store/dock'

const TerminalTab = lazy(() => import('./TerminalTab').then((m) => ({ default: m.TerminalTab })))
const LogsTabView = lazy(() => import('./LogsTab').then((m) => ({ default: m.LogsTabView })))
const ApplyTabView = lazy(() => import('./ApplyTab').then((m) => ({ default: m.ApplyTabView })))

const icon = (t: DockTab) =>
  t.kind === 'terminal' ? (
    <SquareTerminal size={13} />
  ) : t.kind === 'logs' ? (
    <ScrollText size={13} />
  ) : t.kind === 'plugin' ? (
    <Puzzle size={13} />
  ) : (
    <FileCode2 size={13} />
  )

/** Bottom dock hosting terminals, log viewers and manifest apply tabs. Tabs stay mounted to keep state. */
export function Dock() {
  const { tabs, activeId, expanded, height, setActive, closeTab, setExpanded, setHeight } =
    useDock()
  const clusters = useClusters((s) => s.clusters)
  const drag = useRef<{ y: number; h: number } | null>(null)

  if (tabs.length === 0) return null

  const onDown = (e: React.PointerEvent) => {
    drag.current = { y: e.clientY, h: height }
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
  }
  const onMove = (e: React.PointerEvent) => {
    if (drag.current) setHeight(drag.current.h + (drag.current.y - e.clientY))
  }

  return (
    <section
      className="flex shrink-0 flex-col border-t border-border bg-surface"
      style={{ height: expanded ? height : 34 }}
    >
      {expanded && (
        <div
          className="h-1 w-full shrink-0 cursor-row-resize hover:bg-accent/40"
          onPointerDown={onDown}
          onPointerMove={onMove}
          onPointerUp={() => (drag.current = null)}
        />
      )}
      <div className="flex h-[33px] shrink-0 items-center border-b border-border pr-2">
        <div className="flex min-w-0 flex-1 overflow-x-auto">
          {tabs.map((t) => {
            const c = clusters.find((x) => x.id === t.clusterId)
            return (
              <div
                key={t.id}
                onClick={() => setActive(t.id)}
                className={clsx(
                  'group flex h-[33px] shrink-0 cursor-pointer items-center gap-1.5 border-r border-border px-3 text-xs',
                  t.id === activeId && expanded ? 'bg-bg text-fg' : 'text-muted hover:text-fg',
                )}
              >
                {icon(t)}
                <span className="max-w-48 truncate">{t.title}</span>
                {c && (
                  <span
                    className="size-1.5 rounded-full"
                    style={{ background: c.color }}
                    title={c.name}
                  />
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    closeTab(t.id)
                  }}
                  className="cursor-pointer rounded p-0.5 opacity-60 hover:bg-surface-hover hover:opacity-100"
                  aria-label="Close tab"
                >
                  <X size={12} />
                </button>
              </div>
            )
          })}
        </div>
        <button
          onClick={() => setExpanded(!expanded)}
          className="cursor-pointer p-1.5 text-muted hover:text-fg"
          aria-label="Toggle dock"
        >
          {expanded ? <ChevronDown size={15} /> : <ChevronUp size={15} />}
        </button>
      </div>
      <div className={clsx('min-h-0 flex-1', !expanded && 'hidden')}>
        {tabs.map((t) => {
          const active = t.id === activeId && expanded
          return (
            <div key={t.id} className={clsx('h-full', t.id !== activeId && 'hidden')}>
              {t.kind === 'terminal' && <TerminalTab tab={t} active={active} />}
              {t.kind === 'logs' && <LogsTabView tab={t} active={active} />}
              {t.kind === 'plugin' && (
                <PluginFrame pluginId={t.pluginId} entry={t.entry} panelId={t.panelId} />
              )}
              {t.kind === 'apply' && (
                <Suspense
                  fallback={<div className="p-4 text-[13px] text-muted">Loading editor…</div>}
                >
                  <ApplyTabView tab={t} />
                </Suspense>
              )}
            </div>
          )
        })}
      </div>
    </section>
  )
}
