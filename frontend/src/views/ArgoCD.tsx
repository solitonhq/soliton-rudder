import clsx from 'clsx'
import { RefreshCw, RotateCcw, Search, Zap } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  canJump,
  Drawer,
  EmptyState,
  KV,
  Pill,
  argoHealthTone,
  argoSyncTone,
  shortRepo,
  useJumpTo,
  ViewHeader,
} from '../components/GitOps'
import { Button, Dialog, Input } from '../components/ui'
import { integ } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { ArgoApp } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function ArgoCD({ clusterId }: { clusterId: string }) {
  const { data, error, loading } = usePoll(() => integ.argo.list(clusterId), clusterId, 5_000)
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<'all' | 'outofsync' | 'unhealthy'>('all')
  const [sel, setSel] = useState<{ ns: string; name: string } | null>(null)

  const apps = useMemo(() => {
    const ql = q.trim().toLowerCase()
    return (data ?? []).filter((a) => {
      if (filter === 'outofsync' && a.sync === 'Synced') return false
      if (filter === 'unhealthy' && (a.health === 'Healthy' || a.health === 'Progressing'))
        return false
      return !ql || `${a.name} ${a.project} ${a.repo} ${a.destNamespace}`.toLowerCase().includes(ql)
    })
  }, [data, q, filter])

  const counts = useMemo(() => {
    const all = data ?? []
    return {
      out: all.filter((a) => a.sync !== 'Synced').length,
      bad: all.filter((a) => a.health !== 'Healthy' && a.health !== 'Progressing').length,
    }
  }, [data])

  return (
    <div className="flex h-full">
      <div className="flex min-w-0 flex-1 flex-col">
        <ViewHeader title="Argo CD" count={data?.length}>
          <div className="flex gap-1">
            {(
              [
                ['all', 'All'],
                ['outofsync', `Out of sync ${counts.out}`],
                ['unhealthy', `Unhealthy ${counts.bad}`],
              ] as const
            ).map(([id, label]) => (
              <button
                key={id}
                onClick={() => setFilter(id)}
                className={clsx(
                  'no-drag cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium',
                  filter === id ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <div className="relative">
            <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search applications…"
              className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
            />
          </div>
        </ViewHeader>
        <div className="min-h-0 flex-1 overflow-auto">
          {error && !data && <div className="p-4 text-[13px] text-danger">{error}</div>}
          {loading && <div className="p-4 text-[13px] text-muted">Loading applications…</div>}
          {data && apps.length === 0 && (
            <EmptyState>
              {data.length === 0
                ? 'No Argo CD applications found.'
                : 'No applications match the filters.'}
            </EmptyState>
          )}
          {apps.length > 0 && (
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-surface text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                <tr>
                  {['Application', 'Sync', 'Health', 'Source', 'Destination', 'Last sync', ''].map(
                    (h) => (
                      <th key={h} className="border-b border-border px-3 py-2">
                        {h}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {apps.map((a) => (
                  <tr
                    key={a.namespace + a.name}
                    onClick={() =>
                      setSel(
                        sel?.name === a.name && sel.ns === a.namespace
                          ? null
                          : { ns: a.namespace, name: a.name },
                      )
                    }
                    className={clsx(
                      'cursor-pointer border-b border-border/50',
                      sel?.name === a.name && sel.ns === a.namespace
                        ? 'bg-accent/15'
                        : 'hover:bg-surface-hover',
                    )}
                  >
                    <td className="px-3 py-2">
                      <div className="font-medium">{a.name}</div>
                      <div className="text-xs text-muted">{a.project}</div>
                    </td>
                    <td className="px-3 py-2">
                      <Pill tone={argoSyncTone(a.sync)}>{a.sync}</Pill>
                      {a.outOfSync > 0 && (
                        <span className="ml-1.5 text-xs text-muted">{a.outOfSync}</span>
                      )}
                    </td>
                    <td className="px-3 py-2">
                      <Pill tone={argoHealthTone(a.health)} title={a.healthMessage}>
                        {a.health}
                      </Pill>
                      {a.unhealthy > 0 && (
                        <span className="ml-1.5 text-xs text-muted">{a.unhealthy}</span>
                      )}
                    </td>
                    <td className="max-w-64 px-3 py-2">
                      <div className="truncate font-mono text-xs" title={a.repo}>
                        {shortRepo(a.repo)}
                      </div>
                      <div className="truncate text-xs text-muted">
                        {a.path} @ {a.targetRevision}
                      </div>
                    </td>
                    <td className="max-w-48 px-3 py-2">
                      <div className="truncate text-xs">{a.destNamespace || '—'}</div>
                      <div className="truncate text-xs text-muted">
                        {a.destName ||
                          (a.destServer.includes('kubernetes.default')
                            ? 'in-cluster'
                            : a.destServer)}
                      </div>
                    </td>
                    <td className="px-3 py-2 text-xs">
                      {a.phase ? (
                        <span
                          className={
                            a.phase === 'Succeeded'
                              ? 'text-success'
                              : a.phase === 'Running'
                                ? 'text-warning'
                                : 'text-danger'
                          }
                        >
                          {a.phase}
                        </span>
                      ) : (
                        <span className="text-muted">—</span>
                      )}
                      {a.opFinished && (
                        <span className="text-muted"> · {formatAge(a.opFinished)} ago</span>
                      )}
                    </td>
                    <td className="px-3 py-2">
                      {a.auto && (
                        <Pill tone="accent" title={`prune: ${a.prune}, self-heal: ${a.selfHeal}`}>
                          auto
                        </Pill>
                      )}
                      {a.errors > 0 && <Pill tone="danger">error</Pill>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
      {sel && (
        <AppDrawer
          key={sel.ns + sel.name}
          clusterId={clusterId}
          ns={sel.ns}
          name={sel.name}
          onClose={() => setSel(null)}
        />
      )}
    </div>
  )
}

function AppDrawer({
  clusterId,
  ns,
  name,
  onClose,
}: {
  clusterId: string
  ns: string
  name: string
  onClose: () => void
}) {
  const toast = useUI((s) => s.toast)
  const catalog = useExplorer((s) => s.catalog)
  const jumpTo = useJumpTo()
  const jump = useExplorer((s) => s.jump)
  const [tab, setTab] = useState('overview')
  const [sync, setSync] = useState(false)
  const [rollback, setRollback] = useState<number | null>(null)
  const { data: d, error } = usePoll(
    () => integ.argo.get(clusterId, ns, name),
    `${clusterId}/${ns}/${name}`,
    3_000,
  )

  const act = async (label: string, fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast('success', label)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  if (error && !d)
    return (
      <Drawer title={name} tabs={[]} tab="" onTab={() => {}} onClose={onClose}>
        <div className="p-4 text-[13px] text-danger">{error}</div>
      </Drawer>
    )
  if (!d)
    return (
      <Drawer title={name} tabs={[]} tab="" onTab={() => {}} onClose={onClose}>
        <div className="p-4 text-[13px] text-muted">Loading…</div>
      </Drawer>
    )

  return (
    <Drawer
      title={d.name}
      subtitle={`${d.project} · ${d.namespace}`}
      badges={
        <>
          <Pill tone={argoSyncTone(d.sync)}>{d.sync}</Pill>
          <Pill tone={argoHealthTone(d.health)}>{d.health}</Pill>
          {d.auto && <Pill tone="accent">auto-sync</Pill>}
        </>
      }
      actions={
        <>
          <Button
            variant="ghost"
            title="Re-compare with git"
            onClick={() =>
              void act('Refresh requested', () => integ.argo.refresh(clusterId, ns, name, false))
            }
          >
            <RefreshCw size={14} /> Refresh
          </Button>
          <Button
            variant="ghost"
            title="Hard refresh (drops caches)"
            onClick={() =>
              void act('Hard refresh requested', () =>
                integ.argo.refresh(clusterId, ns, name, true),
              )
            }
          >
            Hard
          </Button>
          <Button variant="primary" onClick={() => setSync(true)}>
            <Zap size={13} /> Sync
          </Button>
        </>
      }
      tabs={[
        { id: 'overview', label: 'Overview' },
        { id: 'resources', label: `Resources (${d.items.length})` },
        { id: 'history', label: `History (${d.history.length})` },
      ]}
      tab={tab}
      onTab={setTab}
      onClose={onClose}
    >
      {tab === 'overview' && (
        <div className="select-text space-y-5 p-4 text-[13px]">
          {d.conditions.length > 0 && (
            <div className="space-y-1.5">
              {d.conditions.map((c, i) => (
                <div
                  key={i}
                  className={clsx(
                    'rounded-md px-3 py-2',
                    c.type.endsWith('Error')
                      ? 'bg-danger/10 text-danger'
                      : 'bg-warning/10 text-warning',
                  )}
                >
                  <div className="text-xs font-semibold">{c.type}</div>
                  <div className="break-words">{c.message}</div>
                </div>
              ))}
            </div>
          )}
          <dl className="grid grid-cols-[120px_1fr] gap-y-1.5">
            {d.sources.map((s, i) => (
              <KV key={i} k={d.sources.length > 1 ? `Source ${i + 1}` : 'Source'}>
                <span className="font-mono text-xs">{s}</span>
              </KV>
            ))}
            <KV k="Destination">
              {d.destNamespace} <span className="text-muted">on {d.destName || d.destServer}</span>
            </KV>
            <KV k="Revision">
              <span className="font-mono text-xs">{d.revision.slice(0, 12) || '—'}</span>
            </KV>
            <KV k="Health">
              {d.health}
              {d.healthMessage && <span className="text-muted"> — {d.healthMessage}</span>}
            </KV>
            <KV k="Auto-sync">
              {d.auto ? `on${d.prune ? ', prune' : ''}${d.selfHeal ? ', self-heal' : ''}` : 'off'}{' '}
              <button
                className="cursor-pointer text-accent hover:underline"
                onClick={() =>
                  void act(d.auto ? 'Auto-sync disabled' : 'Auto-sync enabled', () =>
                    integ.argo.autoSync(clusterId, ns, name, !d.auto, d.prune, d.selfHeal),
                  )
                }
              >
                {d.auto ? 'disable' : 'enable'}
              </button>
            </KV>
            {d.syncOptions.length > 0 && (
              <KV k="Sync options">
                <span className="font-mono text-xs">{d.syncOptions.join(', ')}</span>
              </KV>
            )}
            <KV k="Reconciled">{d.reconciledAt ? `${formatAge(d.reconciledAt)} ago` : '—'}</KV>
          </dl>
          {d.phase && (
            <section>
              <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
                Last operation
              </h3>
              <div
                className={clsx(
                  'font-medium',
                  d.phase === 'Succeeded'
                    ? 'text-success'
                    : d.phase === 'Running'
                      ? 'text-warning'
                      : 'text-danger',
                )}
              >
                {d.phase}
              </div>
              {d.opMessage && <div className="mt-0.5 break-words text-muted">{d.opMessage}</div>}
              {d.opResults
                .filter((r) => r.status !== 'Synced' || r.message)
                .slice(0, 15)
                .map((r, i) => (
                  <div key={i} className="mt-1 text-xs">
                    <span className="font-mono">
                      {r.kind}/{r.name}
                    </span>{' '}
                    <span className="text-muted">
                      {r.status} — {r.message}
                    </span>
                  </div>
                ))}
            </section>
          )}
          {d.images.length > 0 && (
            <section>
              <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
                Images
              </h3>
              {d.images.map((i) => (
                <div key={i} className="break-all font-mono text-xs">
                  {i}
                </div>
              ))}
            </section>
          )}
          <button
            onClick={() => void jump('argoproj.io/v1alpha1/applications', d.name)}
            className="cursor-pointer text-xs text-accent hover:underline"
          >
            View / edit YAML
          </button>
        </div>
      )}
      {tab === 'resources' && (
        <div className="divide-y divide-border/50">
          {d.items.map((r, i) => {
            const link = canJump(catalog, r.group, r.kind)
            return (
              <div key={i} className="flex items-center gap-3 px-4 py-2 text-[13px]">
                <div className="min-w-0 flex-1">
                  {link ? (
                    <button
                      onClick={() => jumpTo(r.group, r.kind, r.name)}
                      className="cursor-pointer truncate text-left font-mono text-xs text-accent hover:underline"
                    >
                      {r.kind}/{r.name}
                    </button>
                  ) : (
                    <span className="truncate font-mono text-xs">
                      {r.kind}/{r.name}
                    </span>
                  )}
                  <div className="truncate text-xs text-muted">
                    {r.namespace}
                    {r.hook && ' · hook'}
                    {r.message && ` · ${r.message}`}
                  </div>
                </div>
                {r.sync && <Pill tone={argoSyncTone(r.sync)}>{r.sync}</Pill>}
                {r.health && <Pill tone={argoHealthTone(r.health)}>{r.health}</Pill>}
              </div>
            )
          })}
          {d.items.length === 0 && <EmptyState>No resources reported yet.</EmptyState>}
        </div>
      )}
      {tab === 'history' && (
        <div className="divide-y divide-border/50">
          {d.history.map((h, i) => (
            <div key={h.id} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
              <span className="w-8 font-mono text-xs text-muted">#{h.id}</span>
              <div className="min-w-0 flex-1">
                <div className="font-mono text-xs">
                  {h.revision.slice(0, 12)}
                  {i === 0 && <span className="ml-2 font-sans text-success">current</span>}
                </div>
                <div className="truncate text-xs text-muted">
                  {formatAge(h.deployedAt)} ago{h.source && ` · ${h.source}`}
                </div>
              </div>
              {i > 0 && (
                <Button variant="ghost" onClick={() => setRollback(h.id)}>
                  <RotateCcw size={13} /> Roll back
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
      {sync && (
        <SyncDialog
          app={d}
          onClose={() => setSync(false)}
          onSync={(o) => act('Sync started', () => integ.argo.sync(clusterId, ns, name, o))}
        />
      )}
      {rollback !== null && (
        <RollbackDialog
          app={d}
          id={rollback}
          onClose={() => setRollback(null)}
          onRun={(prune, dis) =>
            act('Rollback started', () =>
              integ.argo.rollback(clusterId, ns, name, rollback, prune, dis),
            )
          }
        />
      )}
    </Drawer>
  )
}

function SyncDialog({
  app,
  onClose,
  onSync,
}: {
  app: ArgoApp
  onClose: () => void
  onSync: (o: {
    revision: string
    prune: boolean
    force: boolean
    dryRun: boolean
  }) => Promise<void>
}) {
  const [revision, setRevision] = useState('')
  const [prune, setPrune] = useState(false)
  const [force, setForce] = useState(false)
  const [dry, setDry] = useState(false)
  return (
    <Dialog title={`Sync ${app.name}`} onClose={onClose} width={440}>
      <div className="space-y-3 text-[13px]">
        <div>
          <label className="mb-1 block text-xs font-medium text-muted">
            Revision (blank = {app.targetRevision || 'target revision'})
          </label>
          <Input
            value={revision}
            onChange={(e) => setRevision(e.target.value)}
            placeholder={app.targetRevision}
          />
        </div>
        {(
          [
            ['Prune resources that are no longer in git', prune, setPrune],
            ['Force (replace instead of patch)', force, setForce],
            ['Dry run (report only)', dry, setDry],
          ] as const
        ).map(([label, v, set]) => (
          <label key={label} className="flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              checked={v}
              onChange={(e) => set(e.target.checked)}
              className="accent-[var(--accent)]"
            />{' '}
            {label}
          </label>
        ))}
        {prune && (
          <p className="text-xs text-warning">
            Pruning deletes live resources that are not in git.
          </p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={() => {
              onClose()
              void onSync({ revision: revision.trim(), prune, force, dryRun: dry })
            }}
          >
            Sync
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

function RollbackDialog({
  app,
  id,
  onClose,
  onRun,
}: {
  app: ArgoApp
  id: number
  onClose: () => void
  onRun: (prune: boolean, disableAuto: boolean) => Promise<void>
}) {
  const [prune, setPrune] = useState(false)
  const [disable, setDisable] = useState(app.auto)
  return (
    <Dialog title={`Roll back ${app.name} to #${id}`} onClose={onClose} width={460}>
      <div className="space-y-3 text-[13px]">
        {app.auto && (
          <label className="flex cursor-pointer items-start gap-2 rounded-md bg-warning/10 p-2.5">
            <input
              type="checkbox"
              checked={disable}
              onChange={(e) => setDisable(e.target.checked)}
              className="mt-0.5 accent-[var(--accent)]"
            />
            <span>
              Disable auto-sync first. Otherwise Argo CD immediately re-applies git and undoes the
              rollback. Re-enable it after fixing git.
            </span>
          </label>
        )}
        <label className="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            checked={prune}
            onChange={(e) => setPrune(e.target.checked)}
            className="accent-[var(--accent)]"
          />{' '}
          Prune resources that did not exist then
        </label>
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="danger"
            disabled={app.auto && !disable}
            onClick={() => {
              onClose()
              void onRun(prune, disable)
            }}
          >
            Roll back
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
