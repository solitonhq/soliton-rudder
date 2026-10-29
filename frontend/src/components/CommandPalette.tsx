import clsx from 'clsx'
import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useRowStoreVersion } from '../lib/useRowStore'
import { resourceTitle } from '../lib/categories'
import { useClusters } from '../store/clusters'
import { useDock } from '../store/dock'
import { usePlugins } from '../store/plugins'
import { plugins as pluginApi } from '../lib/backend'
import { useExplorer } from '../store/explorer'
import { useSettings } from '../store/settings'
import { useUI } from '../store/ui'
import { THEMES, THEME_META } from '../themes/themes'

interface Item {
  id: string
  group: string
  label: string
  hint?: string
  run: () => void
}

/** Subsequence fuzzy score; higher is better, -1 means no match. */
export function fuzzy(q: string, s: string): number {
  if (!q) return 0
  const t = s.toLowerCase()
  let score = 0
  let pos = 0
  let last = -1
  for (const ch of q.toLowerCase()) {
    const i = t.indexOf(ch, pos)
    if (i < 0) return -1
    score += i === last + 1 ? 3 : 1
    if (i === 0 || /[\s\-_/.]/.test(t[i - 1])) score += 2
    last = i
    pos = i + 1
  }
  return score - t.length * 0.01
}

export function CommandPalette({ onClose }: { onClose: () => void }) {
  const [q, setQ] = useState('')
  const [idx, setIdx] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)
  const clusters = useClusters((s) => s.clusters)
  const setActive = useClusters((s) => s.setActive)
  const activeId = useClusters((s) => s.activeId)
  const { catalog, select, nsStore, setNamespaces } = useExplorer()
  const nsVersion = useRowStoreVersion(nsStore)
  const pluginList = usePlugins((s) => s.list)
  const setTheme = useSettings((s) => s.setTheme)
  const openDialog = useUI((s) => s.open)

  const items: Item[] = useMemo(() => {
    const out: Item[] = []
    if (activeId) {
      for (const r of catalog) {
        out.push({
          id: 'r:' + r.key,
          group: 'Resources',
          label: resourceTitle(r),
          hint: r.group === 'core' ? r.name : `${r.name}.${r.group}`,
          run: () => void select(r.key),
        })
      }
      out.push({
        id: 'ns:all',
        group: 'Namespaces',
        label: 'All namespaces',
        run: () => setNamespaces([]),
      })
      for (const n of [...nsStore.rows.values()].map((r) => r.name).sort()) {
        out.push({
          id: 'ns:' + n,
          group: 'Namespaces',
          label: n,
          hint: 'namespace',
          run: () => setNamespaces([n]),
        })
      }
    }
    for (const c of clusters)
      out.push({
        id: 'c:' + c.id,
        group: 'Clusters',
        label: c.name,
        hint: 'switch cluster',
        run: () => setActive(c.id),
      })
    out.push({ id: 'home', group: 'Go to', label: 'Home', run: () => setActive(null) })
    if (activeId) {
      const name = clusters.find((c) => c.id === activeId)?.name ?? ''
      const open = useDock.getState().openTab
      out.push({
        id: 'term',
        group: 'Tools',
        label: 'New terminal',
        hint: name,
        run: () => open({ kind: 'terminal', clusterId: activeId, title: `Terminal: ${name}` }),
      })
      out.push({
        id: 'apply',
        group: 'Tools',
        label: 'Apply manifest…',
        hint: name,
        run: () => open({ kind: 'apply', clusterId: activeId, title: `Apply: ${name}`, ns: '' }),
      })
      if (catalog.some((r) => r.key === 'networking.k8s.io/v1/ingresses'))
        out.push({
          id: 'ing',
          group: 'Tools',
          label: 'Ingress map',
          hint: name,
          run: () => useExplorer.getState().setView('ingress'),
        })
      for (const pl of pluginList) {
        if (!pl.enabled) continue
        for (const c of pl.manifest.commands ?? []) {
          const panel = c.panel ? pl.manifest.ui?.panels?.find((x) => x.id === c.panel) : undefined
          out.push({
            id: `plug-${pl.manifest.id}-${c.id}`,
            group: 'Plugins',
            label: c.title,
            hint: pl.manifest.name,
            run: () => {
              if (panel) {
                if (panel.placement === 'dock')
                  useDock.getState().openTab({
                    kind: 'plugin',
                    clusterId: activeId,
                    title: panel.title,
                    pluginId: pl.manifest.id,
                    panelId: panel.id,
                    entry: panel.entry,
                  })
                else useExplorer.getState().openPluginPanel(pl.manifest.id, panel.id)
              } else {
                const ns = useExplorer.getState().namespaces
                void pluginApi
                  .runCommand(pl.manifest.id, c.id, {
                    clusterId: activeId,
                    clusterName: name,
                    namespaces: ns,
                  })
                  .then((msg) => useUI.getState().toast('success', msg || `${c.title}: done`))
                  .catch((e) =>
                    useUI.getState().toast('error', e instanceof Error ? e.message : String(e)),
                  )
              }
            },
          })
        }
        for (const panel of pl.manifest.ui?.panels ?? []) {
          out.push({
            id: `panel-${pl.manifest.id}-${panel.id}`,
            group: 'Plugins',
            label: panel.title,
            hint: pl.manifest.name,
            run: () => {
              if (panel.placement === 'dock')
                useDock.getState().openTab({
                  kind: 'plugin',
                  clusterId: activeId,
                  title: panel.title,
                  pluginId: pl.manifest.id,
                  panelId: panel.id,
                  entry: panel.entry,
                })
              else useExplorer.getState().openPluginPanel(pl.manifest.id, panel.id)
            },
          })
        }
      }
      const ig = useExplorer.getState().integrations
      for (const [id, label, show] of [
        ['argocd', 'Argo CD applications', ig?.argocd],
        ['flux', 'Flux', ig?.flux],
        ['helm', 'Helm releases', ig?.helm],
        ['prometheus', 'Prometheus', true],
      ] as const) {
        if (show)
          out.push({
            id: 'int-' + id,
            group: 'Tools',
            label,
            hint: name,
            run: () => useExplorer.getState().setView(id),
          })
      }
      out.push({
        id: 'applications',
        group: 'Tools',
        label: 'Applications',
        hint: name,
        run: () => useExplorer.getState().setView('applications'),
      })
      out.push({
        id: 'overview',
        group: 'Tools',
        label: 'Cluster overview',
        hint: name,
        run: () => useExplorer.getState().setView('overview'),
      })
      out.push({
        id: 'blackbox',
        group: 'Tools',
        label: 'Blackbox (time travel) and runbooks',
        hint: name,
        run: () => useExplorer.getState().setView('blackbox'),
      })
      out.push({
        id: 'simulator',
        group: 'Tools',
        label: 'Simulator (NetworkPolicy, node drain)',
        hint: name,
        run: () => useExplorer.getState().setView('simulator'),
      })
      out.push({
        id: 'topology',
        group: 'Tools',
        label: 'Topology map',
        hint: name,
        run: () => useExplorer.getState().setView('topology'),
      })
      out.push({
        id: 'certs',
        group: 'Tools',
        label: 'Certificates',
        hint: name,
        run: () => useExplorer.getState().setView('certificates'),
      })
      out.push({
        id: 'assess',
        group: 'Tools',
        label: 'Cluster assessments',
        hint: name,
        run: () => useExplorer.getState().setView('reports'),
      })
      out.push({
        id: 'fwd',
        group: 'Tools',
        label: 'Port forwards',
        hint: name,
        run: () => useExplorer.getState().setView('forwards'),
      })
    }
    out.push({
      id: 'add',
      group: 'Actions',
      label: 'Add cluster…',
      run: () => openDialog({ kind: 'add' }),
    })
    out.push({
      id: 'compare',
      group: 'Actions',
      label: 'Compare clusters / namespaces…',
      run: () => openDialog({ kind: 'compare' }),
    })
    out.push({
      id: 'settings',
      group: 'Actions',
      label: 'Open settings',
      run: () => openDialog({ kind: 'settings' }),
    })
    for (const t of THEMES)
      out.push({
        id: 'th:' + t,
        group: 'Theme',
        label: `Theme: ${THEME_META[t].label}`,
        run: () => void setTheme(t),
      })
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps -- nsVersion signals in-place store mutation
  }, [
    activeId,
    catalog,
    clusters,
    nsStore,
    nsVersion,
    select,
    setActive,
    setNamespaces,
    setTheme,
    openDialog,
  ])

  const results = useMemo(() => {
    if (!q) return items.filter((i) => i.group !== 'Namespaces').slice(0, 60)
    return items
      .map((i) => ({ i, s: Math.max(fuzzy(q, i.label), fuzzy(q, i.hint ?? '') - 1) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => b.s - a.s)
      .slice(0, 60)
      .map((x) => x.i)
  }, [items, q])

  useEffect(() => {
    listRef.current?.querySelector(`[data-i="${idx}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [idx])

  const exec = (i?: Item) => {
    if (!i) return
    onClose()
    i.run()
  }

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex justify-center bg-black/50 pt-[14vh]"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div className="h-fit w-[560px] max-w-[90vw] overflow-hidden rounded-xl border border-border bg-surface shadow-2xl">
        <input
          autoFocus
          value={q}
          onChange={(e) => {
            setQ(e.target.value)
            setIdx(0)
          }}
          placeholder="Search resources, namespaces, clusters, actions…"
          className="h-12 w-full select-text border-b border-border bg-transparent px-4 text-sm outline-none placeholder:text-muted"
          onKeyDown={(e) => {
            if (e.key === 'Escape') onClose()
            else if (e.key === 'ArrowDown') {
              e.preventDefault()
              setIdx((i) => Math.min(i + 1, results.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setIdx((i) => Math.max(i - 1, 0))
            } else if (e.key === 'Enter') exec(results[idx])
          }}
        />
        <div ref={listRef} className="max-h-[50vh] overflow-y-auto p-1.5">
          {results.map((r, i) => (
            <button
              key={r.id}
              data-i={i}
              onMouseMove={() => setIdx(i)}
              onClick={() => exec(r)}
              className={clsx(
                'flex w-full cursor-pointer items-center justify-between rounded-md px-3 py-2 text-left text-[13px]',
                i === idx && 'bg-accent/15',
              )}
            >
              <span className="truncate">{r.label}</span>
              <span className="ml-3 shrink-0 text-xs text-muted">{r.hint ?? r.group}</span>
            </button>
          ))}
          {results.length === 0 && (
            <div className="px-3 py-6 text-center text-sm text-muted">No results</div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}
