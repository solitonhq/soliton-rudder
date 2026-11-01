import { Columns3, FileCode2, RefreshCw, Search, SquareTerminal } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { DetailPanel, type DetailTab } from '../components/DetailPanel'
import { NamespacePicker } from '../components/NamespacePicker'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { ResourceNav } from '../components/ResourceNav'
import { ResourceTable } from '../components/ResourceTable'
import { Button } from '../components/ui'
import { resourceTitle } from '../lib/categories'
import { useRowStoreVersion } from '../lib/useRowStore'
import { insight } from '../lib/backend'
import { useClusters } from '../store/clusters'
import { useDock } from '../store/dock'
import { useExplorer } from '../store/explorer'
import { PluginFrame } from '../components/PluginFrame'
import { usePlugins } from '../store/plugins'
import { ArgoCD } from './ArgoCD'
import { Blackbox } from './Blackbox'
import { Applications } from './Applications'
import { Overview } from './Overview'
import { Simulator } from './Simulator'
import { Topology } from './Topology'
import { Certificates } from './Certificates'
import { Flux } from './Flux'
import { Forwards } from './Forwards'
import { Helm } from './Helm'
import { Prometheus } from './Prometheus'
import { Ingresses } from './Ingresses'
import { Reports } from './Reports'

export function Explorer({ clusterId }: { clusterId: string }) {
  const ex = useExplorer()
  const {
    phase,
    error,
    catalog,
    selectedKey,
    main,
    search,
    setSearch,
    wide,
    setWide,
    selected,
    setSelected,
    namespaces,
    view,
  } = ex
  const openTab = useDock((s) => s.openTab)
  const clusterName = useClusters((s) => s.clusters.find((c) => c.id === clusterId)?.name ?? '')
  const version = useRowStoreVersion(main)
  const [tab, setTab] = useState<DetailTab>('overview')
  const [deleteRequest, setDeleteRequest] = useState(0)
  const searchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    void useExplorer.getState().open(clusterId)
    return () => useExplorer.getState().close()
  }, [clusterId])

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      const typing = el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.closest('.cm-editor')
      if (e.key === '/' && !typing) {
        e.preventDefault()
        searchRef.current?.focus()
      } else if (e.key === 'Escape' && selected && !typing) setSelected(null)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [selected, setSelected])

  const resource = catalog.find((r) => r.key === selectedKey)

  // CPU/Memory columns for pods and nodes (silently absent without metrics-server).
  const usageKind =
    view === 'resources' && (resource?.name === 'pods' || resource?.name === 'nodes')
      ? resource.name
      : null
  useEffect(() => {
    if (!usageKind) return
    let cancelled = false
    const run = async () => {
      try {
        const u = await insight.usage(clusterId, usageKind, '')
        if (!cancelled) useExplorer.getState().setUsage(u)
      } catch {
        if (!cancelled) useExplorer.getState().setUsage(null)
      }
    }
    void run()
    const t = setInterval(() => !document.hidden && void run(), 15_000)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [clusterId, usageKind])

  if (phase === 'connecting' || phase === 'idle') {
    return <div className="grid h-full place-items-center text-sm text-muted">Connecting…</div>
  }
  if (phase === 'error') {
    return (
      <div className="grid h-full place-items-center p-6">
        <div className="max-w-md text-center">
          <h2 className="text-base font-semibold">Could not connect</h2>
          <p className="mt-2 select-text break-words text-[13px] text-danger">{error}</p>
          <Button className="mt-4" onClick={() => void useExplorer.getState().open(clusterId)}>
            <RefreshCw size={13} /> Retry
          </Button>
        </div>
      </div>
    )
  }

  const total = main.rows.size
  const live = main.status === 'live'

  return (
    <div className="flex h-full">
      <ResourceNav />
      <ErrorBoundary resetKey={`${clusterId}|${view}|${selectedKey ?? ''}`}>
        {view === 'overview' ? (
          <div className="min-w-0 flex-1">
            <Overview clusterId={clusterId} />
          </div>
        ) : view === 'applications' ? (
          <div className="min-w-0 flex-1">
            <Applications clusterId={clusterId} />
          </div>
        ) : view === 'certificates' ? (
          <div className="min-w-0 flex-1">
            <Certificates clusterId={clusterId} />
          </div>
        ) : view === 'blackbox' ? (
          <div className="min-w-0 flex-1">
            <Blackbox clusterId={clusterId} />
          </div>
        ) : view === 'simulator' ? (
          <div className="min-w-0 flex-1">
            <Simulator clusterId={clusterId} />
          </div>
        ) : view === 'topology' ? (
          <div className="min-w-0 flex-1">
            <Topology clusterId={clusterId} />
          </div>
        ) : view === 'plugin' ? (
          <div className="min-w-0 flex-1">
            <PluginView />
          </div>
        ) : view === 'argocd' || view === 'helm' || view === 'flux' || view === 'prometheus' ? (
          <div className="min-w-0 flex-1">
            {view === 'argocd' && <ArgoCD clusterId={clusterId} />}
            {view === 'helm' && <Helm clusterId={clusterId} />}
            {view === 'flux' && <Flux clusterId={clusterId} />}
            {view === 'prometheus' && <Prometheus clusterId={clusterId} />}
          </div>
        ) : view === 'reports' ? (
          <div className="min-w-0 flex-1">
            <Reports clusterId={clusterId} />
          </div>
        ) : view === 'ingress' ? (
          <div className="min-w-0 flex-1">
            <Ingresses clusterId={clusterId} />
          </div>
        ) : view === 'forwards' ? (
          <div className="min-w-0 flex-1">
            <Forwards clusterId={clusterId} />
          </div>
        ) : (
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
              <h1 className="mr-1 truncate text-sm font-semibold">
                {resource ? resourceTitle(resource) : ''}
              </h1>
              <span className="font-mono text-xs text-muted" data-v={version}>
                {total}
              </span>
              <span
                title={live ? 'Live' : main.status === 'error' ? main.message : 'Connecting…'}
                className={`ml-1 inline-block size-2 rounded-full ${live ? 'bg-success' : main.status === 'error' ? 'bg-danger' : 'animate-pulse bg-warning'}`}
              />
              <div className="flex-1" />
              {resource?.namespaced && <NamespacePicker />}
              <div className="relative">
                <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
                <input
                  ref={searchRef}
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search  ( / )"
                  className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
                />
              </div>
              <Button
                variant={wide ? 'primary' : 'secondary'}
                onClick={() => setWide(!wide)}
                title="Show extra columns"
              >
                <Columns3 size={14} /> Wide
              </Button>
              <Button
                title="Open a terminal with kubectl pointed at this cluster"
                onClick={() =>
                  openTab({ kind: 'terminal', clusterId, title: `Terminal: ${clusterName}` })
                }
              >
                <SquareTerminal size={14} /> Terminal
              </Button>
              <Button
                title="Create or update resources from YAML"
                onClick={() =>
                  openTab({
                    kind: 'apply',
                    clusterId,
                    title: `Apply: ${clusterName}`,
                    ns: namespaces.length === 1 ? namespaces[0] : '',
                  })
                }
              >
                <FileCode2 size={14} /> Apply
              </Button>
            </div>
            <div className="flex min-h-0 flex-1">
              <div className="min-w-0 flex-1">
                {resource && (
                  <ResourceTable
                    key={resource.key} // widths are remembered per kind
                    namespaced={resource.namespaced}
                    resourceName={resource.name}
                    onAction={(action) => {
                      setTab(action === 'edit' ? 'yaml' : 'overview')
                      if (action === 'delete') setDeleteRequest((n) => n + 1)
                    }}
                  />
                )}
              </div>
              {resource && selected && (
                <DetailPanel
                  key={`${selected.uid}:${deleteRequest}`}
                  deleteRequest={deleteRequest}
                  clusterId={clusterId}
                  resource={resource}
                  target={selected}
                  tab={tab}
                  onTab={setTab}
                  onClose={() => setSelected(null)}
                />
              )}
            </div>
            {namespaces.length > 0 && resource?.namespaced && (
              <div className="border-t border-border px-4 py-1 text-xs text-muted">
                Namespaces: {namespaces.join(', ')}
              </div>
            )}
          </div>
        )}
      </ErrorBoundary>
    </div>
  )
}

function PluginView() {
  const sel = useExplorer((s) => s.pluginPanel)
  const list = usePlugins((s) => s.list)
  const p = list.find((x) => x.manifest.id === sel?.pluginId && x.enabled)
  const panel = p?.manifest.ui?.panels?.find((x) => x.id === sel?.panelId)
  if (!p || !panel)
    return (
      <div className="grid h-full place-items-center text-[13px] text-muted">
        This plugin panel is no longer available.
      </div>
    )
  return (
    <PluginFrame
      key={p.manifest.id + panel.id}
      pluginId={p.manifest.id}
      entry={panel.entry}
      panelId={panel.id}
    />
  )
}
