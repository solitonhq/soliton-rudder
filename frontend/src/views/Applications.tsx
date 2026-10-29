import clsx from 'clsx'
import { ExternalLink, Link2, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { AppDetail, sourceLabel, statusDot } from '../components/AppDetail'
import { openLink } from '../components/IngressDetails'
import { NamespacePicker } from '../components/NamespacePicker'
import { Button } from '../components/ui'
import { AppLinksDialog } from '../dialogs/AppLinksDialog'
import { apps } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { Application } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'

const GROUP_KEY = (clusterId: string) => `soliton-rudder.apps.group-by-namespace.${clusterId}`

function loadGroup(clusterId: string): boolean {
  try {
    return localStorage.getItem(GROUP_KEY(clusterId)) === '1'
  } catch {
    return false
  }
}

export const matchesApp = (a: Application, q: string): boolean => {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return true
  const hay =
    `${a.name} ${a.namespaces.join(' ')} ${a.version} ${a.chart} ${a.images.join(' ')} ${sourceLabel[a.groupedBy]}`.toLowerCase()
  return words.every((w) => hay.includes(w))
}

function AppRow({ a, onOpen }: { a: Application; onOpen: () => void }) {
  const c = a.counts
  const bad = a.issues.filter((i) => i.severity !== 'info').length
  return (
    <li>
      <div className="flex items-center gap-3 px-4 py-2.5 hover:bg-surface-2">
        <button
          className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left"
          onClick={onOpen}
          aria-label={`Open ${a.name}`}
        >
          <span className={clsx('size-2.5 shrink-0 rounded-full', statusDot[a.status])} />
          <div className="min-w-0 flex-1">
            <div className="flex items-baseline gap-2">
              <span className="truncate text-[13px] font-semibold">{a.name}</span>
              <span className="truncate text-xs text-muted">{a.namespaces.join(', ')}</span>
            </div>
            <div className="truncate text-xs text-muted">
              {c.workloads > 0
                ? `${c.workloadsHealthy}/${c.workloads} workloads · ${c.podsReady}/${c.pods} pods${c.restarts > 0 ? ` · ${c.restarts} restarts` : ''}`
                : 'no workloads'}
              {bad > 0 && (
                <span className="text-warning">
                  {' '}
                  · {bad} issue{bad > 1 ? 's' : ''}
                </span>
              )}
            </div>
          </div>
          <span className="hidden shrink-0 rounded bg-surface-2 px-1.5 py-0.5 text-[11px] text-muted md:inline">
            {sourceLabel[a.groupedBy]}
          </span>
          <span className="w-28 shrink-0 truncate text-right font-mono text-xs" title={a.version}>
            {a.version}
          </span>
          <span className="w-20 shrink-0 text-right text-xs text-muted">
            {a.lastChange ? `${formatAge(a.lastChange)} ago` : ''}
          </span>
        </button>
        {a.urls[0] ? (
          <button
            className="grid size-7 shrink-0 cursor-pointer place-items-center rounded text-muted hover:bg-surface-hover hover:text-accent"
            onClick={() => openLink(a.urls[0])}
            aria-label={`Open ${a.urls[0]}`}
            title={a.urls[0]}
          >
            <ExternalLink size={13} />
          </button>
        ) : (
          <span className="size-7 shrink-0" />
        )}
      </div>
    </li>
  )
}

export function Applications({ clusterId }: { clusterId: string }) {
  const namespaces = useExplorer((s) => s.namespaces)
  const jump = useExplorer((s) => s.jump)
  const setView = useExplorer((s) => s.setView)
  const [groupNs, setGroupNsState] = useState(() => loadGroup(clusterId))
  const [q, setQ] = useState('')
  const [only, setOnly] = useState<'all' | 'problems'>('all')
  const [selected, setSelected] = useState<string | null>(null)
  const [linksOpen, setLinksOpen] = useState(false)
  const [linksTick, setLinksTick] = useState(0)

  const setGroupNs = (v: boolean) => {
    setGroupNsState(v)
    try {
      localStorage.setItem(GROUP_KEY(clusterId), v ? '1' : '0')
    } catch {
      /* not remembered */
    }
  }

  const data = usePoll(() => apps.list(clusterId, groupNs), `${clusterId}|${groupNs}`, 30_000)
  const templates = usePoll(() => apps.links(clusterId), `${clusterId}|${linksTick}`, 0)
  const list = useMemo(
    () =>
      (data.data?.apps ?? []).filter((a) => {
        if (namespaces.length && !a.namespaces.some((n) => namespaces.includes(n))) return false
        if (only === 'problems' && a.status !== 'error' && a.status !== 'warn') return false
        return matchesApp(a, q)
      }),
    [data.data, namespaces, q, only],
  )
  const current = selected ? data.data?.apps.find((a) => a.id === selected) : undefined
  if (selected && current) {
    return (
      <AppDetail
        clusterId={clusterId}
        app={current}
        templates={templates.data ?? []}
        onBack={() => setSelected(null)}
      />
    )
  }

  const all = data.data?.apps ?? []
  const problems = all.filter((a) => a.status === 'error' || a.status === 'warn').length
  const unassigned = data.data?.unassigned ?? []
  return (
    <div className="flex h-full flex-col">
      <div className="flex min-h-12 shrink-0 flex-wrap items-center gap-2 border-b border-border px-4 py-1.5">
        <h1 className="text-sm font-semibold">Applications</h1>
        {data.data && (
          <span className="font-mono text-xs text-muted">
            {list.length === all.length ? all.length : `${list.length} of ${all.length}`}
          </span>
        )}
        {problems > 0 && (
          <span className="text-xs font-medium text-warning">{problems} need attention</span>
        )}
        <div className="flex-1" />
        <NamespacePicker />
        <div className="relative">
          <Search size={13} className="pointer-events-none absolute left-2.5 top-2.5 text-muted" />
          <input
            aria-label="Search applications"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Name, version, image…"
            className="no-drag h-8 w-52 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
          />
        </div>
        <button
          aria-pressed={only === 'problems'}
          onClick={() => setOnly(only === 'problems' ? 'all' : 'problems')}
          className={clsx(
            'no-drag h-8 cursor-pointer rounded-md border px-3 text-[13px] font-medium',
            only === 'problems'
              ? 'border-accent bg-accent text-accent-fg'
              : 'border-border bg-surface-2 hover:bg-surface-hover',
          )}
        >
          Problems only
        </button>
        <label
          className="flex cursor-pointer items-center gap-1.5 text-xs text-muted"
          title="Group objects that carry no application labels by their namespace"
        >
          <input
            type="checkbox"
            checked={groupNs}
            onChange={(e) => setGroupNs(e.target.checked)}
            className="accent-[var(--accent)]"
          />
          Group the rest by namespace
        </label>
        <Button onClick={() => setLinksOpen(true)}>
          <Link2 size={13} /> Links…
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {data.error && !data.data && (
          <div className="select-text p-4 text-[13px] text-danger">{data.error}</div>
        )}
        {data.loading && (
          <div className="p-4 text-[13px] text-muted">Looking for applications…</div>
        )}
        {data.data && list.length === 0 && (
          <div className="mx-auto mt-16 max-w-md px-4 text-center text-[13px] text-muted">
            {all.length === 0
              ? 'No applications were recognised. They are found from Helm releases and the app.kubernetes.io/part-of, instance and app labels. Turn on “Group the rest by namespace” to treat each namespace as one.'
              : 'No application matches these filters.'}
          </div>
        )}
        {list.length > 0 && (
          <ul className="m-4 divide-y divide-border overflow-hidden rounded-xl border border-border bg-surface">
            {list.map((a) => (
              <AppRow key={a.id} a={a} onOpen={() => setSelected(a.id)} />
            ))}
          </ul>
        )}
        {unassigned.length > 0 && only === 'all' && !q && (
          <details className="mx-4 mb-4 rounded-xl border border-border bg-surface">
            <summary className="cursor-pointer px-4 py-2.5 text-[13px]">
              <span className="font-medium">Not part of any application</span>{' '}
              <span className="text-muted">
                ({data.data?.unassignedTotal} workloads without application labels)
              </span>
            </summary>
            <ul className="divide-y divide-border border-t border-border">
              {unassigned.map((m) => (
                <li
                  key={`${m.kind}/${m.namespace}/${m.name}`}
                  className="flex items-center gap-2 px-4 py-1.5 text-xs"
                >
                  <span className={clsx('size-2 rounded-full', statusDot[m.status])} />
                  <span className="w-24 shrink-0 text-[10px] font-semibold uppercase text-muted">
                    {m.kind}
                  </span>
                  <button
                    className="min-w-0 flex-1 cursor-pointer truncate text-left font-mono hover:text-accent hover:underline"
                    onClick={() => {
                      setView('resources')
                      void jump(m.resourceKey, m.name)
                    }}
                  >
                    {m.namespace}/{m.name}
                  </button>
                  <span className="text-muted">{m.detail}</span>
                </li>
              ))}
            </ul>
            <p className="border-t border-border px-4 py-2 text-xs text-muted">
              Label them with <span className="font-mono">app.kubernetes.io/part-of</span> to group
              them, or turn on grouping by namespace.
            </p>
          </details>
        )}
        {(data.data?.notes ?? []).map((n) => (
          <p key={n} className="px-5 pb-2 text-xs text-muted">
            {n}
          </p>
        ))}
      </div>
      {linksOpen && (
        <AppLinksDialog
          clusterId={clusterId}
          onClose={() => setLinksOpen(false)}
          onSaved={() => setLinksTick((t) => t + 1)}
        />
      )}
    </div>
  )
}
