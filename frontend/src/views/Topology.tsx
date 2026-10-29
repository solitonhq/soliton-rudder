import clsx from 'clsx'
import { AlertTriangle, ExternalLink, RefreshCw, Search, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { NamespacePicker } from '../components/NamespacePicker'
import { insight } from '../lib/backend'
import { edgePath, layoutTopology, NODE_H, NODE_W, problemSet, relatedTo } from '../lib/topology'
import type { TopoNode, TopoStatus } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'

const TIER_LABEL: Record<number, string> = {
  0: 'Ingress',
  1: 'Services',
  2: 'Workloads',
  3: 'Pods',
  4: 'Nodes & config',
}

const STATUS_VAR: Record<TopoStatus, string> = {
  ok: 'var(--color-success)',
  warn: 'var(--color-warning)',
  error: 'var(--color-danger)',
  unknown: 'var(--color-muted)',
}

const CONFIG_KINDS = new Set(['ConfigMap', 'Secret', 'PersistentVolumeClaim'])

function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: string }) {
  return (
    <button
      onClick={onClick}
      className={clsx(
        'no-drag h-8 cursor-pointer rounded-md border px-3 text-[13px] font-medium',
        on
          ? 'border-accent bg-accent text-accent-fg'
          : 'border-border bg-surface-2 hover:bg-surface-hover',
      )}
    >
      {children}
    </button>
  )
}

export function Topology({ clusterId }: { clusterId: string }) {
  const namespaces = useExplorer((s) => s.namespaces)
  const jump = useExplorer((s) => s.jump)
  const setView = useExplorer((s) => s.setView)
  const [q, setQ] = useState('')
  const [problemsOnly, setProblemsOnly] = useState(false)
  const [showConfig, setShowConfig] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  // The graph is fetched for a single namespace when one is picked, otherwise for all of them.
  const ns = namespaces.length === 1 ? namespaces[0] : ''
  const { data, error, loading } = usePoll(
    () => insight.topology(clusterId, ns),
    `${clusterId}|${ns}`,
    5_000,
  )

  const view = useMemo(() => {
    if (!data) return null
    const nsSet = namespaces.length > 1 ? new Set(namespaces) : null
    const ql = q.trim().toLowerCase()
    let nodes = data.nodes.filter((n) => {
      if (!showConfig && CONFIG_KINDS.has(n.kind)) return false
      if (nsSet && n.namespace && !nsSet.has(n.namespace)) return false
      return true
    })
    let ids = new Set(nodes.map((n) => n.id))
    let edges = data.edges.filter((e) => ids.has(e.from) && ids.has(e.to))
    if (ql || problemsOnly) {
      // search and problems-only both narrow the graph; with both on, a node must satisfy both
      const bySearch = new Set<string>()
      const byProblem = problemsOnly ? problemSet(nodes, edges) : null
      if (ql)
        for (const n of nodes)
          if (`${n.kind} ${n.name} ${n.namespace} ${n.detail}`.toLowerCase().includes(ql))
            for (const r of relatedTo(n.id, edges)) bySearch.add(r)
      nodes = nodes.filter(
        (n) => (!ql || bySearch.has(n.id)) && (!byProblem || byProblem.has(n.id)),
      )
      ids = new Set(nodes.map((n) => n.id))
      edges = edges.filter((e) => ids.has(e.from) && ids.has(e.to))
    }
    const layout = layoutTopology(nodes, edges)
    const counts = { error: 0, warn: 0 }
    for (const n of data.nodes) {
      if (n.status === 'error') counts.error++
      else if (n.status === 'warn') counts.warn++
    }
    return { nodes, edges, layout, counts }
  }, [data, namespaces, q, problemsOnly, showConfig])

  const focus = useMemo(
    () => (selected && view ? relatedTo(selected, view.edges) : null),
    [selected, view],
  )
  const sel: TopoNode | undefined = view?.nodes.find((n) => n.id === selected)

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
        <h1 className="text-sm font-semibold">Topology</h1>
        {view && <span className="font-mono text-xs text-muted">{view.nodes.length} objects</span>}
        {view && view.counts.error > 0 && (
          <span className="flex items-center gap-1 text-xs font-medium text-danger">
            <XCircle size={13} /> {view.counts.error}
          </span>
        )}
        {view && view.counts.warn > 0 && (
          <span className="flex items-center gap-1 text-xs font-medium text-warning">
            <AlertTriangle size={13} /> {view.counts.warn}
          </span>
        )}
        {data && (
          <span
            className="flex items-center gap-1 text-[11px] text-muted"
            title="Refreshes every 5 seconds"
          >
            <RefreshCw size={11} /> live
          </span>
        )}
        <div className="flex-1" />
        <NamespacePicker />
        <div className="relative">
          <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Name, kind, status…"
            className="no-drag h-8 w-52 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
          />
        </div>
        <Chip on={problemsOnly} onClick={() => setProblemsOnly(!problemsOnly)}>
          Problems only
        </Chip>
        <Chip on={showConfig} onClick={() => setShowConfig(!showConfig)}>
          Config
        </Chip>
      </div>

      {data && (data.truncated || data.notes.length > 0) && (
        <div className="shrink-0 border-b border-border bg-surface-2 px-4 py-1.5 text-xs text-muted">
          {data.notes.join(' · ')}
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto" onClick={() => setSelected(null)}>
          {error && !data && <div className="p-4 text-[13px] text-danger">{error}</div>}
          {loading && <div className="p-4 text-[13px] text-muted">Building the graph…</div>}
          {view && view.nodes.length === 0 && !loading && (
            <div className="mt-16 text-center text-[13px] text-muted">
              {problemsOnly ? 'No problems found.' : 'Nothing to show for these filters.'}
            </div>
          )}
          {view && view.nodes.length > 0 && (
            <svg
              width={view.layout.width}
              height={view.layout.height + 28}
              className="select-none"
              role="img"
              aria-label="Cluster topology"
            >
              <g transform="translate(0,24)">
                {view.layout.columns.map((t, i) => (
                  <text
                    key={t}
                    x={24 + i * (NODE_W + 110)}
                    y={-8}
                    className="fill-muted text-[10px] font-semibold uppercase tracking-wide"
                  >
                    {TIER_LABEL[t]}
                  </text>
                ))}
                {view.edges.map((e) => {
                  const a = view.layout.placed.get(e.from)
                  const b = view.layout.placed.get(e.to)
                  if (!a || !b) return null
                  const dim = focus && !(focus.has(e.from) && focus.has(e.to))
                  const bad = a.node.status === 'error' || b.node.status === 'error'
                  return (
                    <path
                      key={`${e.from}>${e.to}>${e.kind}`}
                      d={edgePath(a, b)}
                      fill="none"
                      stroke={bad ? 'var(--color-danger)' : 'var(--color-muted)'}
                      strokeWidth={focus && !dim ? 2 : 1.25}
                      strokeDasharray={
                        e.kind === 'mounts' || e.kind === 'runs-on' ? '4 3' : undefined
                      }
                      opacity={dim ? 0.08 : focus ? 0.9 : 0.45}
                    />
                  )
                })}
                {[...view.layout.placed.values()].map(({ node: n, x, y }) => {
                  const dim = focus && !focus.has(n.id)
                  return (
                    <g
                      key={n.id}
                      transform={`translate(${x},${y})`}
                      opacity={dim ? 0.18 : 1}
                      className="cursor-pointer"
                      onClick={(ev) => {
                        ev.stopPropagation()
                        setSelected(n.id === selected ? null : n.id)
                      }}
                    >
                      <title>{`${n.kind} ${n.namespace ? n.namespace + '/' : ''}${n.name}\n${n.detail}`}</title>
                      <rect
                        width={NODE_W}
                        height={NODE_H}
                        rx={8}
                        fill="var(--color-surface-2)"
                        stroke={n.id === selected ? 'var(--color-accent)' : STATUS_VAR[n.status]}
                        strokeWidth={n.id === selected ? 2.5 : n.status === 'error' ? 2 : 1.25}
                        strokeDasharray={n.missing ? '5 3' : undefined}
                      />
                      <rect width={5} height={NODE_H} rx={2} fill={STATUS_VAR[n.status]} />
                      <text x={14} y={18} className="fill-muted text-[10px] uppercase">
                        {n.kind === 'PersistentVolumeClaim'
                          ? 'PVC'
                          : n.kind === 'HorizontalPodAutoscaler'
                            ? 'HPA'
                            : n.kind}
                      </text>
                      <text x={14} y={33} className="fill-fg text-[12px] font-medium">
                        {n.name.length > 28 ? n.name.slice(0, 27) + '…' : n.name}
                      </text>
                    </g>
                  )
                })}
              </g>
            </svg>
          )}
        </div>

        {sel && (
          <aside className="w-72 shrink-0 space-y-3 overflow-y-auto border-l border-border p-4 text-[13px]">
            <div>
              <div className="text-[10px] font-semibold uppercase text-muted">{sel.kind}</div>
              <div className="select-text break-all font-mono text-sm font-semibold">
                {sel.name}
              </div>
              {sel.namespace && <div className="text-xs text-muted">{sel.namespace}</div>}
            </div>
            <div className="flex items-center gap-2">
              <span
                className="inline-block h-2.5 w-2.5 rounded-full"
                style={{ background: STATUS_VAR[sel.status] }}
              />
              <span className="capitalize">{sel.status}</span>
            </div>
            {sel.detail && <div className="select-text text-muted">{sel.detail}</div>}
            {sel.missing && (
              <div className="rounded-md bg-danger/10 p-2 text-xs text-danger">
                This object is referenced but does not exist.
              </div>
            )}
            {view && (
              <div className="text-xs text-muted">
                {view.edges.filter((e) => e.to === sel.id).length} upstream ·{' '}
                {view.edges.filter((e) => e.from === sel.id).length} downstream
              </div>
            )}
            {sel.resourceKey && !sel.missing && (
              <button
                onClick={() => {
                  setView('resources')
                  void jump(sel.resourceKey, sel.name)
                }}
                className="flex h-8 w-full cursor-pointer items-center justify-center gap-1.5 rounded-md border border-border bg-surface-2 text-[13px] font-medium hover:bg-surface-hover"
              >
                <ExternalLink size={13} /> Open in table
              </button>
            )}
          </aside>
        )}
      </div>
    </div>
  )
}
