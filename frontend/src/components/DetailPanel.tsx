import clsx from 'clsx'
import { Bug, Cable, RotateCw, Scaling, ScrollText, SquareTerminal, Trash2, X } from 'lucide-react'
import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { insight, kube } from '../lib/backend'
import { formatAge } from '../lib/format'
import { usePoll } from '../lib/usePoll'
import type { ObjectMeta, Resource, TimelineEvent } from '../lib/types'
import type { RowRef } from '../store/explorer'
import { useDock } from '../store/dock'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { DataViewer } from './DataViewer'
import { IngressDetails } from './IngressDetails'
import { ProbesSection } from './ProbesSection'
import {
  EventsPanel,
  EventsSummary,
  HealthCard,
  isWarning,
  MetricsSection,
  useTimeline,
} from './Insights'
import { PodEnvironment } from './PodEnvironment'
import { FORWARD_KIND, FORWARDABLE } from '../lib/quickforward'
import { RESTARTABLE, SCALABLE } from '../lib/workloads'
import { DebugDialog } from '../dialogs/DebugDialog'
import { ScaleDialog } from '../dialogs/ScaleDialog'
import { ForwardDialog } from '../dialogs/ForwardDialog'
import { Button, Dialog } from './ui'

const YamlEditor = lazy(() => import('./YamlEditor').then((m) => ({ default: m.YamlEditor })))

const LOGGABLE = new Set([
  'pods',
  'deployments',
  'statefulsets',
  'daemonsets',
  'replicasets',
  'jobs',
  'services',
  'replicationcontrollers',
])

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export type DetailTab = 'overview' | 'events' | 'data' | 'yaml'

export function DetailPanel({
  clusterId,
  resource,
  target,
  tab: requestedTab,
  onTab,
  onClose,
  deleteRequest = 0,
}: {
  clusterId: string
  resource: Resource
  target: RowRef
  tab: DetailTab
  onTab: (t: DetailTab) => void
  onClose: () => void
  /** Panel is re-keyed per request, so a non-zero value opens the delete confirmation on mount. */
  deleteRequest?: number
}) {
  const toast = useUI((s) => s.toast)
  const [confirmDelete, setConfirmDelete] = useState(deleteRequest > 0)
  const [scaleOpen, setScaleOpen] = useState(false)
  const [forwardOpen, setForwardOpen] = useState(false)
  const [debugOpen, setDebugOpen] = useState(false)
  const openTab = useDock((s) => s.openTab)
  const hasData = resource.name === 'configmaps' || resource.name === 'secrets'
  const tab: DetailTab =
    (requestedTab === 'data' && !hasData) ||
    (requestedTab === 'events' && resource.name === 'events')
      ? 'overview'
      : requestedTab
  // an Event has no events of its own
  const hasEvents = resource.name !== 'events'
  const tl = useTimeline(clusterId, resource, target)
  const warnings = tl.events.filter(isWarning).length
  const tabs: DetailTab[] = [
    'overview',
    ...(hasEvents ? (['events'] as const) : []),
    ...(hasData ? (['data'] as const) : []),
    'yaml',
  ]

  const run = async (label: string, fn: () => Promise<void>) => {
    try {
      await fn()
      toast('success', label)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <aside className="flex w-[560px] max-w-[60%] shrink-0 flex-col border-l border-border bg-surface">
      <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{target.name}</div>
          <div className="truncate text-xs text-muted">
            {resource.kind}
            {target.ns && ` · ${target.ns}`}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {resource.name === 'pods' && (
            <>
              <Button
                variant="ghost"
                title="Open a shell in this pod"
                onClick={() =>
                  openTab({
                    kind: 'terminal',
                    clusterId,
                    title: `Shell: ${target.name}`,
                    pod: { ns: target.ns, name: target.name, container: '' },
                  })
                }
              >
                <SquareTerminal size={14} /> Shell
              </Button>
              <Button
                variant="ghost"
                title="Add a debug container with network/process tools (kubectl debug)"
                onClick={() => setDebugOpen(true)}
              >
                <Bug size={14} /> Debug
              </Button>
            </>
          )}
          {LOGGABLE.has(resource.name) && (
            <Button
              variant="ghost"
              title="Stream logs"
              onClick={() =>
                openTab({
                  kind: 'logs',
                  clusterId,
                  title: `Logs: ${target.name}`,
                  key: resource.key,
                  ns: target.ns,
                  name: target.name,
                  resourceKind: resource.kind,
                })
              }
            >
              <ScrollText size={14} /> Logs
            </Button>
          )}
          {FORWARDABLE.has(resource.name) && (
            <Button variant="ghost" title="Forward a port" onClick={() => setForwardOpen(true)}>
              <Cable size={14} /> Forward
            </Button>
          )}
          {SCALABLE.has(resource.name) && (
            <Button variant="ghost" onClick={() => setScaleOpen(true)} title="Scale">
              <Scaling size={14} /> Scale
            </Button>
          )}
          {RESTARTABLE.has(resource.name) && (
            <Button
              variant="ghost"
              title="Rolling restart"
              onClick={() =>
                void run('Restart triggered', () =>
                  kube.restart(clusterId, resource.key, target.ns, target.name),
                )
              }
            >
              <RotateCw size={14} /> Restart
            </Button>
          )}
          <Button
            variant="ghost"
            className="text-danger hover:text-danger"
            onClick={() => setConfirmDelete(true)}
            title="Delete"
          >
            <Trash2 size={14} />
          </Button>
          <Button variant="ghost" onClick={onClose} aria-label="Close">
            <X size={15} />
          </Button>
        </div>
      </div>

      <div className="flex gap-1 border-b border-border px-3">
        {tabs.map((t) => (
          <button
            key={t}
            onClick={() => onTab(t)}
            className={clsx(
              '-mb-px cursor-pointer border-b-2 px-3 py-2 text-[13px] font-medium capitalize',
              tab === t ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg',
            )}
          >
            {t === 'yaml' ? 'YAML' : t === 'data' ? 'Data' : t === 'events' ? 'Events' : 'Overview'}
            {t === 'events' && warnings > 0 && (
              <span
                className="ml-1.5 rounded-full bg-warning/20 px-1.5 text-[10px] text-warning"
                title={`${warnings} warning${warnings === 1 ? '' : 's'}`}
                aria-label={`${warnings} warnings`}
              >
                {warnings}
              </span>
            )}
          </button>
        ))}
      </div>

      <div className="min-h-0 flex-1">
        {tab === 'overview' ? (
          <Overview
            key={target.uid}
            clusterId={clusterId}
            resource={resource}
            target={target}
            events={hasEvents ? tl : null}
            onOpenEvents={() => onTab('events')}
          />
        ) : tab === 'events' ? (
          <EventsPanel
            events={tl.events}
            loaded={tl.loaded}
            error={tl.error}
            objectName={target.name}
          />
        ) : tab === 'data' && hasData ? (
          <DataViewer key={target.uid} clusterId={clusterId} resource={resource} target={target} />
        ) : (
          <YamlTab key={target.uid} clusterId={clusterId} resource={resource} target={target} />
        )}
      </div>

      {confirmDelete && (
        <Dialog
          title={`Delete ${resource.kind}`}
          onClose={() => setConfirmDelete(false)}
          width={420}
        >
          <p className="text-[13px]">
            Delete <span className="font-mono font-medium">{target.name}</span>
            {target.ns && (
              <>
                {' '}
                from namespace <span className="font-mono">{target.ns}</span>
              </>
            )}
            ? This cannot be undone.
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirmDelete(false)
                void run('Deleted', async () => {
                  await kube.remove(clusterId, resource.key, target.ns, target.name)
                  onClose()
                })
              }}
            >
              Delete
            </Button>
          </div>
        </Dialog>
      )}
      {debugOpen && (
        <DebugDialog
          clusterId={clusterId}
          ns={target.ns}
          pod={target.name}
          onClose={() => setDebugOpen(false)}
        />
      )}
      {forwardOpen && (
        <ForwardDialog
          clusterId={clusterId}
          kind={FORWARD_KIND[resource.name]}
          ns={target.ns}
          name={target.name}
          resourceKey={resource.key}
          onClose={() => setForwardOpen(false)}
        />
      )}
      {scaleOpen && (
        <ScaleDialog
          name={target.name}
          onClose={() => setScaleOpen(false)}
          onScale={(n) =>
            run(`Scaled to ${n}`, () =>
              kube.scale(clusterId, resource.key, target.ns, target.name, n),
            )
          }
        />
      )}
    </aside>
  )
}

/** Where an Ingress receives traffic and where it sends it: addresses, clickable URLs, Service and pod IPs, TLS. */
function IngressSection({ clusterId, target }: { clusterId: string; target: RowRef }) {
  const { data, error } = usePoll(
    () => insight.ingressMap(clusterId, target.ns),
    `${clusterId}|${target.ns}|${target.name}`,
    10_000,
  )
  const ing = data?.ingresses.find((i) => i.name === target.name && i.namespace === target.ns)
  return (
    <section className="mb-4">
      <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
        Routing
      </h3>
      <div className="overflow-hidden rounded-lg border border-border">
        {ing ? (
          <IngressDetails ing={ing} compact />
        ) : (
          <div className={`px-3 py-3 text-xs ${error ? 'text-danger' : 'text-muted'}`}>
            {error || (data ? 'Not found' : 'Loading…')}
          </div>
        )}
      </div>
    </section>
  )
}

function Overview({
  clusterId,
  resource,
  target,
  events,
  onOpenEvents,
}: {
  clusterId: string
  resource: Resource
  target: RowRef
  events: { events: TimelineEvent[]; loaded: boolean } | null
  onOpenEvents: () => void
}) {
  const [meta, setMeta] = useState<ObjectMeta | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      setMeta(await kube.meta(clusterId, resource.key, target.ns, target.name))
      setError('')
    } catch (e) {
      setError(errText(e))
    }
  }, [clusterId, resource.key, target.ns, target.name])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch-on-mount; state is set after awaiting
    void load()
    const t = setInterval(() => void load(), 10_000)
    return () => clearInterval(t)
  }, [load])

  if (error) return <div className="p-4 text-[13px] text-danger">{error}</div>
  if (!meta) return <div className="p-4 text-[13px] text-muted">Loading…</div>

  return (
    <div className="h-full select-text overflow-auto p-4 text-[13px]">
      <HealthCard clusterId={clusterId} resource={resource} target={target} />
      {events && (
        <EventsSummary events={events.events} loaded={events.loaded} onOpen={onOpenEvents} />
      )}
      {resource.name === 'pods' && (
        <ProbesSection clusterId={clusterId} ns={target.ns} pod={target.name} />
      )}
      {resource.name === 'pods' && (
        <PodEnvironment clusterId={clusterId} ns={target.ns} pod={target.name} />
      )}
      {resource.name === 'ingresses' && <IngressSection clusterId={clusterId} target={target} />}
      <MetricsSection clusterId={clusterId} resource={resource} target={target} />
      <dl className="grid grid-cols-[110px_1fr] gap-y-1.5">
        <dt className="text-muted">Created</dt>
        <dd>
          {new Date(meta.created).toLocaleString()}{' '}
          <span className="text-muted">({formatAge(meta.created)} ago)</span>
        </dd>
        {target.ns && (
          <>
            <dt className="text-muted">Namespace</dt>
            <dd>
              <button
                onClick={() => useExplorer.getState().setNamespaces([target.ns])}
                title="Show only this namespace"
                className="cursor-pointer text-accent hover:underline"
              >
                {target.ns}
              </button>
            </dd>
          </>
        )}
        {meta.owners?.length ? (
          <>
            <dt className="text-muted">Owner</dt>
            <dd className="break-all">{meta.owners.join(', ')}</dd>
          </>
        ) : null}
        {meta.finalizers?.length ? (
          <>
            <dt className="text-muted">Finalizers</dt>
            <dd className="break-all">{meta.finalizers.join(', ')}</dd>
          </>
        ) : null}
        <dt className="text-muted">UID</dt>
        <dd className="break-all font-mono text-xs">{meta.uid}</dd>
      </dl>

      <Section title="Labels" empty={!meta.labels || !Object.keys(meta.labels).length}>
        <div className="flex flex-wrap gap-1.5">
          {Object.entries(meta.labels ?? {}).map(([k, v]) => (
            <span
              key={k}
              className="max-w-full truncate rounded-md border border-border bg-bg px-2 py-0.5 font-mono text-xs"
              title={`${k}=${v}`}
            >
              {k}=<span className="text-accent">{v}</span>
            </span>
          ))}
        </div>
      </Section>

      <Section
        title="Annotations"
        empty={!meta.annotations || !Object.keys(meta.annotations).length}
      >
        <div className="space-y-1">
          {Object.entries(meta.annotations ?? {}).map(([k, v]) => (
            <div key={k} className="break-all font-mono text-xs">
              <span className="text-muted">{k}:</span> {v.length > 160 ? v.slice(0, 160) + '…' : v}
            </div>
          ))}
        </div>
      </Section>
    </div>
  )
}

function Section({
  title,
  children,
  empty,
  emptyText = 'None',
}: {
  title: string
  children: React.ReactNode
  empty: boolean
  emptyText?: string
}) {
  return (
    <section className="mt-5">
      <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">{title}</h3>
      {empty ? <div className="text-xs text-muted">{emptyText}</div> : children}
    </section>
  )
}

function YamlTab({
  clusterId,
  resource,
  target,
}: {
  clusterId: string
  resource: Resource
  target: RowRef
}) {
  const toast = useUI((s) => s.toast)
  const [orig, setOrig] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      const y = await kube.getYAML(clusterId, resource.key, target.ns, target.name)
      setOrig(y)
      setText(y)
      setError('')
    } catch (e) {
      setError(errText(e))
    }
  }, [clusterId, resource.key, target.ns, target.name])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch-on-mount; state is set after awaiting
    void load()
  }, [load])

  const dirty = orig !== null && text !== orig
  const readOnly = resource.name === 'events'

  // let the explorer ask before the panel is closed or another object is selected
  useEffect(() => {
    useExplorer.getState().setDetailDirty(dirty)
    return () => useExplorer.getState().setDetailDirty(false)
  }, [dirty])

  const save = async () => {
    setSaving(true)
    try {
      const y = await kube.updateYAML(clusterId, resource.key, target.ns, target.name, text)
      setOrig(y)
      setText(y)
      setError('')
      toast('success', 'Saved')
    } catch (e) {
      setError(errText(e))
    } finally {
      setSaving(false)
    }
  }

  if (orig === null && !error) return <div className="p-4 text-[13px] text-muted">Loading…</div>

  return (
    <div className="flex h-full flex-col">
      <div className="min-h-0 flex-1">
        <Suspense fallback={<div className="p-4 text-[13px] text-muted">Loading editor…</div>}>
          <YamlEditor value={text} onChange={setText} readOnly={readOnly} />
        </Suspense>
      </div>
      {error && (
        <div className="max-h-28 select-text overflow-auto border-t border-border bg-danger/10 px-4 py-2 text-xs text-danger">
          {error}
        </div>
      )}
      <div className="flex items-center justify-between border-t border-border px-4 py-2">
        <span className="text-xs text-muted">
          {dirty ? 'Unsaved changes' : readOnly ? 'Read-only' : 'Up to date'}
        </span>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => void load()} disabled={saving}>
            {dirty ? 'Revert' : 'Reload'}
          </Button>
          <Button
            variant="primary"
            onClick={() => void save()}
            disabled={!dirty || saving || readOnly}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </div>
    </div>
  )
}
