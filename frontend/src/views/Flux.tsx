import clsx from 'clsx'
import { Pause, Play, RefreshCw, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  canJump,
  Drawer,
  EmptyState,
  fluxTone,
  KV,
  Pill,
  shortRepo,
  useJumpTo,
  ViewHeader,
} from '../components/GitOps'
import { Button } from '../components/ui'
import { integ } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { FluxObject } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))
const status = (o: FluxObject) =>
  o.suspended
    ? 'Suspended'
    : o.ready === 'True'
      ? 'Ready'
      : o.ready === 'False'
        ? 'Failing'
        : 'Unknown'

export function Flux({ clusterId }: { clusterId: string }) {
  const { data, error, loading } = usePoll(() => integ.flux.list(clusterId), clusterId, 5_000)
  const [kind, setKind] = useState('all')
  const [q, setQ] = useState('')
  const [sel, setSel] = useState<FluxObject | null>(null)

  const kinds = useMemo(() => {
    const m = new Map<string, number>()
    for (const o of data ?? []) m.set(o.kind, (m.get(o.kind) ?? 0) + 1)
    return [...m.entries()].sort()
  }, [data])
  const list = useMemo(() => {
    const ql = q.trim().toLowerCase()
    return (data ?? []).filter(
      (o) =>
        (kind === 'all' || o.kind === kind) &&
        (!ql || `${o.name} ${o.namespace} ${o.source} ${o.path}`.toLowerCase().includes(ql)),
    )
  }, [data, kind, q])
  const failing = (data ?? []).filter((o) => !o.suspended && o.ready === 'False').length

  return (
    <div className="flex h-full">
      <div className="flex min-w-0 flex-1 flex-col">
        <ViewHeader title="Flux" count={data?.length}>
          {failing > 0 && (
            <span className="text-xs font-medium text-danger">{failing} failing</span>
          )}
          <div className="relative">
            <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search…"
              className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
            />
          </div>
        </ViewHeader>
        <div className="flex flex-wrap gap-1 border-b border-border px-4 py-2">
          {[['all', data?.length ?? 0] as const, ...kinds].map(([k, n]) => (
            <button
              key={k}
              onClick={() => setKind(k)}
              className={clsx(
                'cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium',
                kind === k ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
              )}
            >
              {k === 'all' ? 'All' : k} <span className="text-muted">{n}</span>
            </button>
          ))}
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {error && !data && <div className="p-4 text-[13px] text-danger">{error}</div>}
          {loading && <div className="p-4 text-[13px] text-muted">Loading…</div>}
          {data && list.length === 0 && <EmptyState>No Flux objects match.</EmptyState>}
          {list.length > 0 && (
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-surface text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                <tr>
                  {['Kind', 'Name', 'Status', 'Revision', 'Source', 'Interval', 'Since'].map(
                    (h) => (
                      <th key={h} className="border-b border-border px-3 py-2">
                        {h}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {list.map((o) => {
                  const active =
                    sel?.key === o.key && sel.name === o.name && sel.namespace === o.namespace
                  return (
                    <tr
                      key={o.key + o.namespace + o.name}
                      onClick={() => setSel(active ? null : o)}
                      className={clsx(
                        'cursor-pointer border-b border-border/50',
                        active ? 'bg-accent/15' : 'hover:bg-surface-hover',
                      )}
                    >
                      <td className="px-3 py-2 text-xs text-muted">{o.kind}</td>
                      <td className="px-3 py-2">
                        <div className="font-medium">{o.name}</div>
                        <div className="text-xs text-muted">{o.namespace}</div>
                      </td>
                      <td className="px-3 py-2">
                        <Pill tone={fluxTone(o.ready, o.suspended)} title={o.message}>
                          {status(o)}
                        </Pill>
                        {o.ready === 'False' && !o.suspended && (
                          <div
                            className="mt-0.5 max-w-56 truncate text-xs text-danger"
                            title={o.message}
                          >
                            {o.reason}
                          </div>
                        )}
                      </td>
                      <td
                        className="max-w-48 truncate px-3 py-2 font-mono text-xs"
                        title={o.revision}
                      >
                        {o.revision || '—'}
                      </td>
                      <td className="max-w-48 truncate px-3 py-2 text-xs" title={o.source}>
                        {o.source.startsWith('http') ? shortRepo(o.source) : o.source || '—'}
                        {o.path && <span className="text-muted"> {o.path}</span>}
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{o.interval || '—'}</td>
                      <td className="px-3 py-2 text-xs text-muted">
                        {o.lastTransition ? `${formatAge(o.lastTransition)} ago` : '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
      {sel && (
        <FluxDrawer
          key={sel.key + sel.namespace + sel.name}
          clusterId={clusterId}
          obj={sel}
          onClose={() => setSel(null)}
        />
      )}
    </div>
  )
}

function FluxDrawer({
  clusterId,
  obj,
  onClose,
}: {
  clusterId: string
  obj: FluxObject
  onClose: () => void
}) {
  const toast = useUI((s) => s.toast)
  const catalog = useExplorer((s) => s.catalog)
  const jump = useExplorer((s) => s.jump)
  const jumpTo = useJumpTo()
  const [tab, setTab] = useState('overview')
  const { data: d, error } = usePoll(
    () => integ.flux.get(clusterId, obj.key, obj.namespace, obj.name),
    `${obj.key}/${obj.namespace}/${obj.name}`,
    4_000,
  )
  const cur = d ?? obj

  const act = async (label: string, fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast('success', label)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <Drawer
      title={cur.name}
      subtitle={`${cur.kind} · ${cur.namespace}`}
      badges={<Pill tone={fluxTone(cur.ready, cur.suspended)}>{status(cur)}</Pill>}
      actions={
        <>
          <Button
            variant="ghost"
            onClick={() =>
              void act('Reconcile requested', () =>
                integ.flux.reconcile(clusterId, obj.key, obj.namespace, obj.name),
              )
            }
          >
            <RefreshCw size={14} /> Reconcile
          </Button>
          <Button
            variant="ghost"
            onClick={() =>
              void act(cur.suspended ? 'Resumed' : 'Suspended', () =>
                integ.flux.suspend(clusterId, obj.key, obj.namespace, obj.name, !cur.suspended),
              )
            }
          >
            {cur.suspended ? (
              <>
                <Play size={13} /> Resume
              </>
            ) : (
              <>
                <Pause size={13} /> Suspend
              </>
            )}
          </Button>
        </>
      }
      tabs={[
        { id: 'overview', label: 'Overview' },
        ...(d && d.inventory.length
          ? [{ id: 'inventory', label: `Inventory (${d.inventory.length})` }]
          : []),
      ]}
      tab={tab}
      onTab={setTab}
      onClose={onClose}
    >
      {error && !d && <div className="p-4 text-[13px] text-danger">{error}</div>}
      {tab === 'overview' && (
        <div className="select-text space-y-4 p-4 text-[13px]">
          {cur.ready === 'False' && !cur.suspended && (
            <div className="rounded-md bg-danger/10 px-3 py-2 text-danger">
              <div className="text-xs font-semibold">{cur.reason}</div>
              <div className="break-words">{cur.message}</div>
            </div>
          )}
          <dl className="grid grid-cols-[110px_1fr] gap-y-1.5">
            <KV k="Revision">
              <span className="break-all font-mono text-xs">{cur.revision || '—'}</span>
            </KV>
            {cur.source && (
              <KV k="Source">
                <span className="font-mono text-xs">{cur.source}</span>
              </KV>
            )}
            {cur.path && (
              <KV k="Path">
                <span className="font-mono text-xs">{cur.path}</span>
              </KV>
            )}
            <KV k="Interval">{cur.interval || '—'}</KV>
            <KV k="Created">{formatAge(cur.created)} ago</KV>
          </dl>
          {d && d.conditions.length > 0 && (
            <section>
              <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
                Conditions
              </h3>
              <div className="space-y-1.5">
                {d.conditions.map((c, i) => (
                  <div key={i} className="rounded-md border border-border bg-bg px-3 py-2">
                    <div className="flex items-center justify-between text-xs">
                      <span className="font-semibold">{c.type}</span>
                      <span
                        className={
                          c.status === 'True'
                            ? 'text-success'
                            : c.status === 'False'
                              ? 'text-danger'
                              : 'text-muted'
                        }
                      >
                        {c.status} · {c.reason}
                      </span>
                    </div>
                    {c.message && <div className="mt-0.5 break-words text-muted">{c.message}</div>}
                  </div>
                ))}
              </div>
            </section>
          )}
          <button
            onClick={() => void jump(obj.key, obj.name)}
            className="cursor-pointer text-xs text-accent hover:underline"
          >
            View / edit YAML
          </button>
        </div>
      )}
      {tab === 'inventory' && d && (
        <div className="divide-y divide-border/50">
          {d.inventory.map((it, i) => (
            <div key={i} className="px-4 py-2 text-[13px]">
              {canJump(catalog, it.group, it.kind) ? (
                <button
                  onClick={() => jumpTo(it.group, it.kind, it.name)}
                  className="cursor-pointer font-mono text-xs text-accent hover:underline"
                >
                  {it.kind}/{it.name}
                </button>
              ) : (
                <span className="font-mono text-xs">
                  {it.kind}/{it.name}
                </span>
              )}
              <span className="ml-2 text-xs text-muted">{it.namespace}</span>
            </div>
          ))}
        </div>
      )}
    </Drawer>
  )
}
