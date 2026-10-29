import clsx from 'clsx'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import type { Resource } from '../lib/types'
import { useExplorer } from '../store/explorer'

export type PillTone = 'success' | 'warning' | 'danger' | 'neutral' | 'accent'

const toneCls: Record<PillTone, string> = {
  success: 'bg-success/15 text-success',
  warning: 'bg-warning/15 text-warning',
  danger: 'bg-danger/15 text-danger',
  accent: 'bg-accent/15 text-accent',
  neutral: 'bg-surface-2 text-muted',
}

export function Pill({
  tone = 'neutral',
  children,
  title,
}: {
  tone?: PillTone
  children: ReactNode
  title?: string
}) {
  return (
    <span
      title={title}
      className={clsx(
        'inline-block max-w-full truncate rounded-full px-2 py-0.5 text-[11px] font-semibold',
        toneCls[tone],
      )}
    >
      {children}
    </span>
  )
}

export const argoSyncTone = (s: string): PillTone =>
  s === 'Synced' ? 'success' : s === 'OutOfSync' ? 'warning' : 'neutral'
export const argoHealthTone = (s: string): PillTone =>
  s === 'Healthy'
    ? 'success'
    : s === 'Degraded' || s === 'Missing'
      ? 'danger'
      : s === 'Progressing' || s === 'Suspended'
        ? 'warning'
        : 'neutral'
export const fluxTone = (ready: string, suspended: boolean): PillTone =>
  suspended ? 'warning' : ready === 'True' ? 'success' : ready === 'False' ? 'danger' : 'neutral'
export const helmTone = (s: string): PillTone =>
  s === 'deployed'
    ? 'success'
    : s === 'failed'
      ? 'danger'
      : s.startsWith('pending') || s === 'uninstalling'
        ? 'warning'
        : 'neutral'

/** Right-hand detail drawer with a header, optional actions and tab strip. */
export function Drawer({
  title,
  subtitle,
  badges,
  actions,
  tabs,
  tab,
  onTab,
  onClose,
  children,
}: {
  title: string
  subtitle?: string
  badges?: ReactNode
  actions?: ReactNode
  tabs: { id: string; label: string }[]
  tab: string
  onTab: (id: string) => void
  onClose: () => void
  children: ReactNode
}) {
  return (
    <aside className="flex w-[600px] max-w-[62%] shrink-0 flex-col border-l border-border bg-surface">
      <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{title}</div>
          {subtitle && <div className="truncate text-xs text-muted">{subtitle}</div>}
          {badges && <div className="mt-1.5 flex flex-wrap gap-1.5">{badges}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {actions}
          <button
            onClick={onClose}
            aria-label="Close"
            className="cursor-pointer rounded p-1.5 text-muted hover:bg-surface-hover hover:text-fg"
          >
            <X size={15} />
          </button>
        </div>
      </div>
      <div className="flex gap-1 border-b border-border px-3">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => onTab(t.id)}
            className={clsx(
              '-mb-px cursor-pointer border-b-2 px-3 py-2 text-[13px] font-medium',
              tab === t.id
                ? 'border-accent text-fg'
                : 'border-transparent text-muted hover:text-fg',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">{children}</div>
    </aside>
  )
}

export function ViewHeader({
  title,
  count,
  children,
}: {
  title: string
  count?: number
  children?: ReactNode
}) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
      <h1 className="text-sm font-semibold">{title}</h1>
      {count !== undefined && <span className="font-mono text-xs text-muted">{count}</span>}
      <div className="flex-1" />
      {children}
    </div>
  )
}

export function KV({ k, children }: { k: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted">{k}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  )
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <div className="mx-auto mt-16 max-w-sm text-center text-[13px] text-muted">{children}</div>
}

/** Finds the explorer resource key for a group/kind (e.g. "apps", "Deployment"). */
export function findResourceKey(
  catalog: Resource[],
  group: string,
  kind: string,
): string | undefined {
  const g = group || 'core'
  return catalog.find((r) => r.kind === kind && r.group === g)?.key
}

/** Opens an object in the resource explorer when its kind is known on this cluster. */
export function useJumpTo() {
  const catalog = useExplorer((s) => s.catalog)
  const jump = useExplorer((s) => s.jump)
  return (group: string, kind: string, name: string) => {
    const key = findResourceKey(catalog, group, kind)
    if (key) void jump(key, name)
    return !!key
  }
}

export function canJump(catalog: Resource[], group: string, kind: string) {
  return !!findResourceKey(catalog, group, kind)
}

export const shortRepo = (u: string) => u.replace(/^https?:\/\//, '').replace(/\.git$/, '')
