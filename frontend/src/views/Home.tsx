import clsx from 'clsx'
import {
  GitCompare,
  LayoutGrid,
  List,
  Pin,
  Plug,
  Plus,
  RefreshCw,
  Search,
  Unplug,
  X,
} from 'lucide-react'
import { useState } from 'react'
import { ClusterBadge } from '../components/ClusterBadge'
import { StatusDot, statusLabel } from '../components/StatusDot'
import { Button } from '../components/ui'
import type { Cluster, Health } from '../lib/types'
import { useClusters } from '../store/clusters'
import { useUI } from '../store/ui'

type Layout = 'cards' | 'list'
const LAYOUT_KEY = 'soliton-rudder.home.layout'

function loadLayout(): Layout {
  try {
    return localStorage.getItem(LAYOUT_KEY) === 'list' ? 'list' : 'cards'
  } catch {
    return 'cards' // storage can be unavailable; the choice just is not remembered
  }
}

/** True when every word of the query appears in the cluster's name, context or status. */
export function matches(c: Cluster, h: Health | undefined, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const hay = `${c.name} ${c.context} ${h ? statusLabel[h.status] : ''}`.toLowerCase()
  return words.every((w) => hay.includes(w))
}

const showError = (h?: Health) =>
  !!h && !!h.error && h.status !== 'connected' && h.status !== 'checking'

function PinButton({ c, className }: { c: Cluster; className?: string }) {
  const update = useClusters((s) => s.update)
  return (
    <button
      aria-label={c.pinned ? `Unpin ${c.name} from the hotbar` : `Pin ${c.name} to the hotbar`}
      aria-pressed={c.pinned}
      title={c.pinned ? 'Pinned to the hotbar: click to unpin' : 'Pin to the hotbar'}
      onClick={() => void update(c.id, { pinned: !c.pinned })}
      className={clsx(
        'z-10 grid size-7 cursor-pointer place-items-center rounded-md transition-opacity hover:bg-surface-hover',
        c.pinned ? 'text-accent' : 'text-muted opacity-0 focus:opacity-100 group-hover:opacity-100',
        className,
      )}
    >
      <Pin size={14} className={c.pinned ? 'fill-current' : ''} />
    </button>
  )
}

function ConnectButton({ c, className }: { c: Cluster; className?: string }) {
  const isConnected = useClusters((s) => !!s.connected[c.id])
  const connect = useClusters((s) => s.connect)
  const disconnect = useClusters((s) => s.disconnect)
  return (
    <button
      aria-label={isConnected ? `Disconnect ${c.name}` : `Connect ${c.name}`}
      aria-pressed={isConnected}
      title={isConnected ? 'Connected: click to disconnect' : 'Disconnected: click to connect'}
      onClick={() => void (isConnected ? disconnect(c.id) : connect(c.id))}
      className={clsx(
        'z-10 grid size-7 cursor-pointer place-items-center rounded-md transition-opacity hover:bg-surface-hover',
        isConnected
          ? 'text-success'
          : 'text-muted opacity-0 focus:opacity-100 group-hover:opacity-100',
        className,
      )}
    >
      {isConnected ? <Plug size={14} /> : <Unplug size={14} />}
    </button>
  )
}

function ClusterCard({ c, h }: { c: Cluster; h?: Health }) {
  const setActive = useClusters((s) => s.setActive)
  const open = useUI((s) => s.open)
  return (
    <div className="group relative">
      <div className="absolute right-2.5 top-2.5 z-10 flex">
        <ConnectButton c={c} />
        <PinButton c={c} />
      </div>
      <button
        onClick={() => setActive(c.id)}
        onDoubleClick={() => open({ kind: 'edit', id: c.id })}
        className="w-full cursor-pointer rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:border-accent/60 hover:bg-surface-2"
      >
        <div className="flex items-center gap-3 pr-16">
          <ClusterBadge cluster={c} size={40} />
          <div className="min-w-0">
            <div className="truncate font-medium">{c.name}</div>
            <div className="truncate text-xs text-muted">{c.context}</div>
          </div>
        </div>
        <div className="mt-4 flex items-center justify-between text-xs">
          <span className="flex items-center gap-1.5 text-muted">
            <StatusDot status={h?.status} />
            {h ? statusLabel[h.status] : '—'}
          </span>
          <span className="font-mono text-muted">
            {h?.status === 'connected' ? `${h.version} · ${h.latencyMs}ms` : ''}
          </span>
        </div>
        {showError(h) && (
          <div className="mt-2 line-clamp-2 text-xs text-danger/90" title={h!.error}>
            {h!.error}
          </div>
        )}
      </button>
    </div>
  )
}

const ROW_COLS =
  'grid-cols-[32px_minmax(160px,1.3fr)_minmax(140px,1.1fr)_120px_130px_minmax(0,1.6fr)]'

function ClusterRow({ c, h }: { c: Cluster; h?: Health }) {
  const setActive = useClusters((s) => s.setActive)
  const open = useUI((s) => s.open)
  return (
    <div className="group relative">
      <PinButton c={c} className="absolute left-1.5 top-1/2 -translate-y-1/2" />
      <ConnectButton c={c} className="absolute right-2 top-1/2 -translate-y-1/2" />
      <button
        onClick={() => setActive(c.id)}
        onDoubleClick={() => open({ kind: 'edit', id: c.id })}
        className={clsx(
          'grid w-full cursor-pointer items-center gap-3 py-2 pl-3 pr-11 text-left text-[13px] hover:bg-surface-2',
          ROW_COLS,
        )}
      >
        <span />
        <span className="flex min-w-0 items-center gap-2.5">
          <ClusterBadge cluster={c} size={26} />
          <span className="truncate font-medium">{c.name}</span>
        </span>
        <span className="truncate text-xs text-muted">{c.context}</span>
        <span className="flex items-center gap-1.5 text-xs text-muted">
          <StatusDot status={h?.status} />
          {h ? statusLabel[h.status] : '—'}
        </span>
        <span className="truncate font-mono text-xs text-muted">
          {h?.status === 'connected' ? `${h.version} · ${h.latencyMs}ms` : ''}
        </span>
        <span
          className="truncate text-xs text-danger/90"
          title={showError(h) ? h!.error : undefined}
        >
          {showError(h) ? h!.error : ''}
        </span>
      </button>
    </div>
  )
}

export function Home() {
  const { clusters, health, loaded, setActive, checkAll } = useClusters()
  const open = useUI((s) => s.open)
  const [query, setQuery] = useState('')
  const [layout, setLayoutState] = useState<Layout>(loadLayout)

  const setLayout = (l: Layout) => {
    setLayoutState(l)
    try {
      localStorage.setItem(LAYOUT_KEY, l)
    } catch {
      /* not remembered */
    }
  }

  if (loaded && clusters.length === 0) {
    return (
      <div className="grid h-full place-items-center">
        <div className="max-w-sm text-center">
          <h1 className="text-xl font-semibold">Welcome to Soliton Rudder</h1>
          <p className="mt-2 text-muted">
            Add your first cluster from a kubeconfig to get started.
          </p>
          <Button variant="primary" className="mt-5" onClick={() => open({ kind: 'add' })}>
            <Plus size={14} /> Add cluster
          </Button>
        </div>
      </div>
    )
  }

  const shown = clusters.filter((c) => matches(c, health[c.id], query))
  const filtering = query.trim() !== ''

  return (
    <div className="p-6">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <h1 className="text-lg font-semibold">Clusters</h1>
        <span className="font-mono text-xs text-muted">
          {filtering ? `${shown.length} of ${clusters.length}` : clusters.length}
        </span>
        <div className="relative ml-2">
          <Search size={14} className="pointer-events-none absolute left-2.5 top-2.5 text-muted" />
          <input
            aria-label="Search clusters"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') setQuery('')
              if (e.key === 'Enter' && shown.length === 1) setActive(shown[0].id)
            }}
            placeholder="Search clusters…"
            className="no-drag h-8 w-64 select-text rounded-md border border-border bg-bg pl-8 pr-7 text-[13px] outline-none placeholder:text-muted focus:border-accent"
          />
          {filtering && (
            <button
              aria-label="Clear search"
              onClick={() => setQuery('')}
              className="absolute right-1.5 top-1.5 grid size-5 cursor-pointer place-items-center rounded text-muted hover:text-fg"
            >
              <X size={13} />
            </button>
          )}
        </div>
        <div className="flex-1" />
        <div
          className="flex gap-0.5 rounded-md border border-border p-0.5"
          role="group"
          aria-label="Layout"
        >
          {(
            [
              ['cards', LayoutGrid, 'Cards'],
              ['list', List, 'List'],
            ] as const
          ).map(([l, Icon, label]) => (
            <button
              key={l}
              aria-label={label}
              aria-pressed={layout === l}
              title={label}
              onClick={() => setLayout(l)}
              className={clsx(
                'grid size-7 cursor-pointer place-items-center rounded',
                layout === l ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
              )}
            >
              <Icon size={15} />
            </button>
          ))}
        </div>
        <div className="flex gap-2">
          <Button onClick={() => void checkAll()}>
            <RefreshCw size={13} /> Refresh
          </Button>
          {clusters.length > 0 && (
            <Button onClick={() => open({ kind: 'compare' })}>
              <GitCompare size={13} /> Compare clusters
            </Button>
          )}
          <Button variant="primary" onClick={() => open({ kind: 'add' })}>
            <Plus size={14} /> Add cluster
          </Button>
        </div>
      </div>

      {shown.length === 0 ? (
        <div className="mt-16 text-center text-[13px] text-muted">
          No clusters match “{query.trim()}”.{' '}
          <button
            className="cursor-pointer text-accent hover:underline"
            onClick={() => setQuery('')}
          >
            Clear search
          </button>
        </div>
      ) : layout === 'cards' ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3">
          {shown.map((c) => (
            <ClusterCard key={c.id} c={c} h={health[c.id]} />
          ))}
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-border bg-surface">
          <div
            className={clsx(
              'grid gap-3 border-b border-border py-1.5 pl-3 pr-11 text-[10px] font-semibold uppercase tracking-wide text-muted',
              ROW_COLS,
            )}
          >
            <span />
            <span>Name</span>
            <span>Context</span>
            <span>Status</span>
            <span>Version</span>
            <span>Details</span>
          </div>
          <div className="divide-y divide-border">
            {shown.map((c) => (
              <ClusterRow key={c.id} c={c} h={health[c.id]} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
