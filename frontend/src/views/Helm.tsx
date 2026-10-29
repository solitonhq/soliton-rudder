import clsx from 'clsx'
import { RotateCcw, Search, Trash2 } from 'lucide-react'
import { lazy, Suspense, useMemo, useState } from 'react'
import {
  canJump,
  Drawer,
  EmptyState,
  KV,
  Pill,
  helmTone,
  useJumpTo,
  ViewHeader,
} from '../components/GitOps'
import { NamespacePicker } from '../components/NamespacePicker'
import { Button, Dialog, Input } from '../components/ui'
import { integ } from '../lib/backend'
import { formatAge } from '../lib/format'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const YamlEditor = lazy(() =>
  import('../components/YamlEditor').then((m) => ({ default: m.YamlEditor })),
)
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Helm's own object kinds resolve to well-known API groups for explorer navigation.
const KIND_GROUP: Record<string, string> = {
  Deployment: 'apps',
  StatefulSet: 'apps',
  DaemonSet: 'apps',
  ReplicaSet: 'apps',
  Job: 'batch',
  CronJob: 'batch',
  Ingress: 'networking.k8s.io',
  NetworkPolicy: 'networking.k8s.io',
  HorizontalPodAutoscaler: 'autoscaling',
  PodDisruptionBudget: 'policy',
  Role: 'rbac.authorization.k8s.io',
  RoleBinding: 'rbac.authorization.k8s.io',
  ClusterRole: 'rbac.authorization.k8s.io',
  ClusterRoleBinding: 'rbac.authorization.k8s.io',
  StorageClass: 'storage.k8s.io',
  CustomResourceDefinition: 'apiextensions.k8s.io',
}

export function Helm({ clusterId }: { clusterId: string }) {
  const namespaces = useExplorer((s) => s.namespaces)
  const { data, error, loading } = usePoll(() => integ.helm.list(clusterId), clusterId, 15_000)
  const [q, setQ] = useState('')
  const [sel, setSel] = useState<{ ns: string; name: string } | null>(null)

  const list = useMemo(() => {
    const ql = q.trim().toLowerCase()
    return (data ?? []).filter(
      (r) =>
        (!namespaces.length || namespaces.includes(r.namespace)) &&
        (!ql || `${r.name} ${r.chart} ${r.namespace}`.toLowerCase().includes(ql)),
    )
  }, [data, namespaces, q])

  return (
    <div className="flex h-full">
      <div className="flex min-w-0 flex-1 flex-col">
        <ViewHeader title="Helm releases" count={data?.length}>
          <NamespacePicker />
          <div className="relative">
            <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search releases…"
              className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
            />
          </div>
        </ViewHeader>
        <div className="min-h-0 flex-1 overflow-auto">
          {error && !data && <div className="p-4 text-[13px] text-danger">{error}</div>}
          {loading && <div className="p-4 text-[13px] text-muted">Reading releases…</div>}
          {data && list.length === 0 && (
            <EmptyState>
              {data.length === 0
                ? 'No Helm releases found (or no permission to read Helm release secrets).'
                : 'No releases match the filters.'}
            </EmptyState>
          )}
          {list.length > 0 && (
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-surface text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                <tr>
                  {['Release', 'Namespace', 'Chart', 'App version', 'Rev', 'Status', 'Updated'].map(
                    (h) => (
                      <th key={h} className="border-b border-border px-3 py-2">
                        {h}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {list.map((r) => {
                  const active = sel?.name === r.name && sel.ns === r.namespace
                  return (
                    <tr
                      key={r.namespace + r.name}
                      onClick={() => setSel(active ? null : { ns: r.namespace, name: r.name })}
                      className={clsx(
                        'cursor-pointer border-b border-border/50',
                        active ? 'bg-accent/15' : 'hover:bg-surface-hover',
                      )}
                    >
                      <td className="px-3 py-2 font-medium">{r.name}</td>
                      <td className="px-3 py-2 text-muted">{r.namespace}</td>
                      <td className="px-3 py-2 font-mono text-xs">
                        {r.chart}-{r.chartVersion}
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{r.appVersion || '—'}</td>
                      <td className="px-3 py-2 font-mono text-xs">{r.revision}</td>
                      <td className="px-3 py-2">
                        <Pill tone={helmTone(r.status)}>{r.status}</Pill>
                      </td>
                      <td className="px-3 py-2 text-xs text-muted">{formatAge(r.updated)} ago</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
      {sel && (
        <ReleaseDrawer
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

function ReleaseDrawer({
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
  const [tab, setTab] = useState('overview')
  const [allValues, setAllValues] = useState(false)
  const [rollback, setRollback] = useState<number | null>(null)
  const [uninstall, setUninstall] = useState(false)
  const { data: d, error } = usePoll(
    () => integ.helm.get(clusterId, ns, name, 0),
    `${clusterId}/${ns}/${name}`,
    15_000,
  )
  const { data: hasCli } = usePoll(() => integ.helm.cli(), 'cli', 0)

  const run = async (label: string, fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast('success', label)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  const bare = (children: React.ReactNode) => (
    <Drawer title={name} tabs={[]} tab="" onTab={() => {}} onClose={onClose}>
      {children}
    </Drawer>
  )
  if (error && !d) return bare(<div className="p-4 text-[13px] text-danger">{error}</div>)
  if (!d) return bare(<div className="p-4 text-[13px] text-muted">Loading…</div>)

  const editor = (v: string) => (
    <Suspense fallback={<div className="p-4 text-[13px] text-muted">Loading editor…</div>}>
      <YamlEditor value={v} readOnly />
    </Suspense>
  )

  return (
    <Drawer
      title={d.name}
      subtitle={`${d.chart} ${d.chartVersion} · ${d.namespace}`}
      badges={
        <>
          <Pill tone={helmTone(d.status)}>{d.status}</Pill>
          <Pill>rev {d.revision}</Pill>
          {d.appVersion && <Pill>app {d.appVersion}</Pill>}
        </>
      }
      actions={
        <Button
          variant="ghost"
          className="text-danger hover:text-danger"
          title={hasCli ? 'Uninstall release' : 'Requires the helm CLI'}
          disabled={!hasCli}
          onClick={() => setUninstall(true)}
        >
          <Trash2 size={14} />
        </Button>
      }
      tabs={[
        { id: 'overview', label: 'Overview' },
        { id: 'values', label: 'Values' },
        { id: 'manifest', label: 'Manifest' },
        { id: 'resources', label: `Resources (${d.objects.length})` },
        { id: 'history', label: 'History' },
      ]}
      tab={tab}
      onTab={setTab}
      onClose={onClose}
    >
      {tab === 'overview' && (
        <div className="select-text space-y-4 p-4 text-[13px]">
          <dl className="grid grid-cols-[110px_1fr] gap-y-1.5">
            <KV k="Chart">
              {d.chart} {d.chartVersion}
            </KV>
            <KV k="App version">{d.appVersion || '—'}</KV>
            <KV k="Updated">
              {new Date(d.updated).toLocaleString()}{' '}
              <span className="text-muted">({formatAge(d.updated)} ago)</span>
            </KV>
            {d.description && <KV k="Description">{d.description}</KV>}
            <KV k="Storage">
              <span className="text-muted">{d.storage}</span>
            </KV>
          </dl>
          {d.notes && (
            <section>
              <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
                Notes
              </h3>
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-bg p-3 font-mono text-xs">
                {d.notes}
              </pre>
            </section>
          )}
        </div>
      )}
      {tab === 'values' && (
        <div className="flex h-full flex-col">
          <div className="flex gap-1 border-b border-border px-3 py-1.5">
            {(
              [
                [false, 'Your values'],
                [true, 'All values (with chart defaults)'],
              ] as const
            ).map(([v, label]) => (
              <button
                key={label}
                onClick={() => setAllValues(v)}
                className={clsx(
                  'cursor-pointer rounded px-2.5 py-1 text-xs font-medium',
                  allValues === v ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <div className="min-h-0 flex-1">
            {editor(
              allValues
                ? d.allValues
                : d.values || '# no custom values — chart defaults are in use\n',
            )}
          </div>
        </div>
      )}
      {tab === 'manifest' && <div className="h-full">{editor(d.manifest)}</div>}
      {tab === 'resources' && (
        <div className="divide-y divide-border/50">
          {d.objects.map((o, i) => {
            const g = KIND_GROUP[o.kind] ?? ''
            const link = canJump(catalog, g, o.kind)
            return (
              <div key={i} className="px-4 py-2 text-[13px]">
                {link ? (
                  <button
                    onClick={() => jumpTo(g, o.kind, o.name)}
                    className="cursor-pointer font-mono text-xs text-accent hover:underline"
                  >
                    {o.kind}/{o.name}
                  </button>
                ) : (
                  <span className="font-mono text-xs">
                    {o.kind}/{o.name}
                  </span>
                )}
                <span className="ml-2 text-xs text-muted">{o.namespace}</span>
              </div>
            )
          })}
        </div>
      )}
      {tab === 'history' && (
        <div className="divide-y divide-border/50">
          {d.history.map((h) => (
            <div key={h.revision} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
              <span className="w-8 font-mono text-xs text-muted">#{h.revision}</span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <Pill tone={helmTone(h.status)}>{h.status}</Pill>
                  <span className="font-mono text-xs">
                    {d.chart}-{h.chartVersion}
                  </span>
                </div>
                <div className="truncate text-xs text-muted">
                  {formatAge(h.updated)} ago{h.description && ` · ${h.description}`}
                </div>
              </div>
              {h.revision !== d.revision && (
                <Button
                  variant="ghost"
                  disabled={!hasCli}
                  title={hasCli ? '' : 'Requires the helm CLI'}
                  onClick={() => setRollback(h.revision)}
                >
                  <RotateCcw size={13} /> Roll back
                </Button>
              )}
            </div>
          ))}
          {!hasCli && (
            <p className="p-4 text-xs text-muted">
              Rollback and uninstall run your local <span className="font-mono">helm</span> CLI,
              which was not found. Install it (e.g.{' '}
              <span className="font-mono">brew install helm</span>) to enable them.
            </p>
          )}
        </div>
      )}
      {rollback !== null && (
        <Dialog
          title={`Roll back ${d.name} to revision ${rollback}`}
          onClose={() => setRollback(null)}
          width={420}
        >
          <p className="text-[13px]">
            Helm will redeploy revision {rollback} as a new revision. Workloads may restart.
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setRollback(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                const r = rollback
                setRollback(null)
                void run(`Rolled back to ${r}`, () => integ.helm.rollback(clusterId, ns, name, r))
              }}
            >
              Roll back
            </Button>
          </div>
        </Dialog>
      )}
      {uninstall && (
        <UninstallDialog
          name={d.name}
          onClose={() => setUninstall(false)}
          onRun={() =>
            run('Release uninstalled', async () => {
              await integ.helm.uninstall(clusterId, ns, name)
              onClose()
            })
          }
        />
      )}
    </Drawer>
  )
}

function UninstallDialog({
  name,
  onClose,
  onRun,
}: {
  name: string
  onClose: () => void
  onRun: () => Promise<void>
}) {
  const [typed, setTyped] = useState('')
  return (
    <Dialog title={`Uninstall ${name}`} onClose={onClose} width={440}>
      <p className="text-[13px]">
        This removes the release and the resources it created. Type{' '}
        <span className="font-mono font-semibold">{name}</span> to confirm.
      </p>
      <Input value={typed} onChange={(e) => setTyped(e.target.value)} className="mt-3" autoFocus />
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="danger"
          disabled={typed !== name}
          onClick={() => {
            onClose()
            void onRun()
          }}
        >
          Uninstall
        </Button>
      </div>
    </Dialog>
  )
}
