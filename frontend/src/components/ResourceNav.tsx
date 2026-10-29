import clsx from 'clsx'
import {
  Activity,
  BadgeCheck,
  Boxes,
  Cable,
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  GitBranch,
  GitCompare,
  LayoutDashboard,
  Globe,
  HardDrive,
  History,
  Network,
  Package,
  Puzzle,
  RefreshCcw,
  Search,
  Server,
  Shapes,
  ShieldCheck,
  SlidersHorizontal,
  FlaskConical,
  type LucideIcon,
  Workflow,
} from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { categorize, CUSTOM_ID, resourceTitle, type Category } from '../lib/categories'
import type { Resource } from '../lib/types'
import { useExplorer, type View } from '../store/explorer'
import { useForwards } from '../store/forwards'
import { usePanels } from '../store/plugins'
import { useUI } from '../store/ui'

const TOOL_VIEWS: View[] = [
  'reports',
  'topology',
  'blackbox',
  'simulator',
  'ingress',
  'certificates',
  'forwards',
]
const INTEGRATION_VIEWS: View[] = ['argocd', 'flux', 'helm', 'prometheus']

const CATEGORY_ICON: Record<string, LucideIcon> = {
  workloads: Boxes,
  network: Network,
  config: SlidersHorizontal,
  storage: HardDrive,
  access: ShieldCheck,
  cluster: Server,
  [CUSTOM_ID]: Shapes,
}

const SECTIONS_KEY = 'soliton-rudder.nav.collapsed-sections'

function loadCollapsedSections(): Set<string> {
  try {
    const v = JSON.parse(localStorage.getItem(SECTIONS_KEY) ?? '[]')
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
  } catch {
    return new Set() // storage unavailable or corrupt: everything starts open
  }
}

function saveCollapsedSections(s: Set<string>) {
  try {
    localStorage.setItem(SECTIONS_KEY, JSON.stringify([...s]))
  } catch {
    /* not remembered */
  }
}

/**
 * A collapsible top-level section (Tools / Integrations / Plugins / Resources). When it is collapsed, a dot on the header
 * shows that the page you are on lives inside it.
 */
function SectionHeader({
  id,
  label,
  open,
  onToggle,
  hasActive,
}: {
  id: string
  label: string
  open: boolean
  onToggle: () => void
  hasActive: boolean
}) {
  return (
    <button
      onClick={onToggle}
      aria-expanded={open}
      aria-controls={`nav-section-${id}`}
      className={clsx(
        'flex w-full cursor-pointer items-center gap-1 px-2 pb-1 pt-3 text-left text-[10px] font-semibold uppercase tracking-[0.1em] text-muted hover:text-fg',
        'mt-2 border-t border-border',
      )}
    >
      {open ? (
        <ChevronDown size={12} className="shrink-0" />
      ) : (
        <ChevronRight size={12} className="shrink-0" />
      )}
      <span className="flex-1">{label}</span>
      {!open && hasActive && (
        <span className="size-1.5 rounded-full bg-accent" aria-label="contains the current page" />
      )}
    </button>
  )
}

function ToolItem({
  active,
  onClick,
  icon: Icon,
  children,
  badge,
}: {
  active: boolean
  onClick: () => void
  icon: LucideIcon
  children: ReactNode
  badge?: ReactNode
}) {
  return (
    <button
      onClick={onClick}
      aria-current={active}
      className={clsx(
        'flex w-full cursor-pointer items-center justify-between rounded-md px-3 py-1.5 text-left text-[13px]',
        active ? 'bg-accent/15 font-medium text-accent' : 'text-fg/90 hover:bg-surface-hover',
      )}
    >
      <span className="flex min-w-0 items-center gap-2">
        <Icon size={14} className="shrink-0" />
        <span className="truncate">{children}</span>
      </span>
      {badge}
    </button>
  )
}

function KindItem({ r, active, onClick }: { r: Resource; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      title={r.key}
      aria-current={active}
      className={clsx(
        'block w-full cursor-pointer truncate rounded-md px-2.5 py-1 text-left text-[13px]',
        active
          ? 'bg-accent/15 font-medium text-accent'
          : 'text-muted hover:bg-surface-hover hover:text-fg',
      )}
    >
      {resourceTitle(r)}
    </button>
  )
}

export function ResourceNav() {
  const catalog = useExplorer((s) => s.catalog)
  const selectedKey = useExplorer((s) => s.selectedKey)
  const select = useExplorer((s) => s.select)
  const view = useExplorer((s) => s.view)
  const setView = useExplorer((s) => s.setView)
  const integrations = useExplorer((s) => s.integrations)
  const pluginPanels = usePanels('cluster-nav')
  const pluginPanel = useExplorer((s) => s.pluginPanel)
  const openPluginPanel = useExplorer((s) => s.openPluginPanel)
  const activeForwards = useForwards((s) => s.items.filter((f) => f.status === 'active').length)
  const hasIngress = catalog.some((r) => r.key === 'networking.k8s.io/v1/ingresses')
  const [filter, setFilter] = useState('')
  const [collapsedSections, setCollapsedSections] = useState<Set<string>>(loadCollapsedSections)
  const sectionOpen = (id: string) => !collapsedSections.has(id)
  const toggleSection = (id: string) =>
    setCollapsedSections((s) => {
      const n = new Set(s)
      if (!n.delete(id)) n.add(id)
      saveCollapsedSections(n)
      return n
    })
  // Custom Resources start collapsed (they can be hundreds of kinds), unless the open kind lives there.
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    const inCustom = categorize(catalog)
      .find((c) => c.id === CUSTOM_ID)
      ?.items.some((r) => r.key === selectedKey)
    return new Set(inCustom ? [] : [CUSTOM_ID])
  })

  const cats: Category[] = useMemo(() => {
    const f = filter.trim().toLowerCase()
    const match = (r: Resource) =>
      r.kind.toLowerCase().includes(f) ||
      r.name.includes(f) ||
      r.group.includes(f) ||
      r.shortNames?.some((s) => s === f)
    return categorize(catalog)
      .map((c) => {
        if (!f) return c
        const groups = c.groups
          ?.map((g) => ({ ...g, items: g.items.filter(match) }))
          .filter((g) => g.items.length)
        return { ...c, items: c.items.filter(match), groups }
      })
      .filter((c) => c.items.length)
  }, [catalog, filter])

  const toggle = (id: string) =>
    setCollapsed((s) => {
      const n = new Set(s)
      if (!n.delete(id)) n.add(id)
      return n
    })

  const isActive = (v: View) => view === v
  // a search always shows what it found, wherever it lives
  const resourcesOpen = filter ? true : sectionOpen('resources')
  const kindActive = (r: Resource) => view === 'resources' && r.key === selectedKey

  return (
    <nav
      className="flex w-[232px] shrink-0 flex-col border-r border-border bg-surface"
      aria-label="Cluster navigation"
    >
      <div className="p-2">
        <div className="relative">
          <Search size={13} className="absolute left-2 top-2.5 text-muted" />
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter kinds…"
            className="h-8 w-full select-text rounded-md border border-border bg-bg pl-7 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-1.5 pb-3">
        {/* the cluster's front pages: always visible, never folded away */}
        <div className="pb-1 pt-0.5">
          <ToolItem
            active={isActive('overview')}
            onClick={() => setView('overview')}
            icon={LayoutDashboard}
          >
            Overview
          </ToolItem>
          <ToolItem
            active={isActive('applications')}
            onClick={() => setView('applications')}
            icon={Boxes}
          >
            Applications
          </ToolItem>
        </div>
        <SectionHeader
          id="tools"
          label="Tools"
          open={sectionOpen('tools')}
          onToggle={() => toggleSection('tools')}
          hasActive={TOOL_VIEWS.includes(view)}
        />
        {sectionOpen('tools') && (
          <div id="nav-section-tools">
            <ToolItem
              active={isActive('reports')}
              onClick={() => setView('reports')}
              icon={ClipboardCheck}
            >
              Assessments
            </ToolItem>
            <ToolItem
              active={isActive('topology')}
              onClick={() => setView('topology')}
              icon={Workflow}
            >
              Topology
            </ToolItem>
            <ToolItem
              active={false}
              onClick={() => useUI.getState().open({ kind: 'compare' })}
              icon={GitCompare}
            >
              Compare clusters
            </ToolItem>
            <ToolItem
              active={isActive('blackbox')}
              onClick={() => setView('blackbox')}
              icon={History}
            >
              Blackbox
            </ToolItem>
            <ToolItem
              active={isActive('simulator')}
              onClick={() => setView('simulator')}
              icon={FlaskConical}
            >
              Simulator
            </ToolItem>
            {hasIngress && (
              <ToolItem
                active={isActive('ingress')}
                onClick={() => setView('ingress')}
                icon={Globe}
              >
                Ingress Map
              </ToolItem>
            )}
            <ToolItem
              active={isActive('certificates')}
              onClick={() => setView('certificates')}
              icon={BadgeCheck}
            >
              Certificates
            </ToolItem>
            <ToolItem
              active={isActive('forwards')}
              onClick={() => setView('forwards')}
              icon={Cable}
              badge={
                activeForwards > 0 ? (
                  <span className="rounded-full bg-success/20 px-1.5 text-[11px] font-medium text-success">
                    {activeForwards}
                  </span>
                ) : undefined
              }
            >
              Port Forwards
            </ToolItem>
          </div>
        )}

        <SectionHeader
          id="integrations"
          label="Integrations"
          open={sectionOpen('integrations')}
          onToggle={() => toggleSection('integrations')}
          hasActive={INTEGRATION_VIEWS.includes(view)}
        />
        {sectionOpen('integrations') && (
          <div id="nav-section-integrations">
            {(
              [
                ['argocd', 'Argo CD', GitBranch, integrations?.argocd],
                ['flux', 'Flux', RefreshCcw, integrations?.flux],
                ['helm', 'Helm', Package, integrations?.helm],
                ['prometheus', 'Prometheus', Activity, true],
              ] as const
            )
              .filter(([, , , show]) => show)
              .map(([id, label, Icon]) => (
                <ToolItem key={id} active={isActive(id)} onClick={() => setView(id)} icon={Icon}>
                  {label}
                </ToolItem>
              ))}
          </div>
        )}

        {pluginPanels.length > 0 && (
          <>
            <SectionHeader
              id="plugins"
              label="Plugins"
              open={sectionOpen('plugins')}
              onToggle={() => toggleSection('plugins')}
              hasActive={view === 'plugin'}
            />
            {sectionOpen('plugins') && (
              <div id="nav-section-plugins">
                {pluginPanels.map(({ plugin, panel }) => (
                  <ToolItem
                    key={plugin.manifest.id + panel.id}
                    active={
                      view === 'plugin' &&
                      pluginPanel?.pluginId === plugin.manifest.id &&
                      pluginPanel.panelId === panel.id
                    }
                    onClick={() => openPluginPanel(plugin.manifest.id, panel.id)}
                    icon={Puzzle}
                  >
                    {panel.title}
                  </ToolItem>
                ))}
              </div>
            )}
          </>
        )}

        <SectionHeader
          id="resources"
          label="Resources"
          open={resourcesOpen}
          onToggle={() => toggleSection('resources')}
          hasActive={view === 'resources' && cats.some((c) => c.items.some(kindActive))}
        />
        {resourcesOpen && (
          <div id="nav-section-resources" className="mt-1">
            {cats.map((c) => {
              const open = filter ? true : !collapsed.has(c.id)
              const Icon = CATEGORY_ICON[c.id] ?? Shapes
              const hasActive = c.items.some(kindActive)
              return (
                <div key={c.id} className="mb-0.5">
                  <button
                    onClick={() => toggle(c.id)}
                    aria-expanded={open}
                    className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-[12.5px] font-semibold text-fg hover:bg-surface-hover"
                  >
                    {open ? (
                      <ChevronDown size={13} className="shrink-0 text-muted" />
                    ) : (
                      <ChevronRight size={13} className="shrink-0 text-muted" />
                    )}
                    <Icon
                      size={14}
                      className={clsx('shrink-0', hasActive ? 'text-accent' : 'text-muted')}
                    />
                    <span className="min-w-0 flex-1 truncate">{c.label}</span>
                    <span className="font-mono text-[10.5px] font-normal text-muted">
                      {c.items.length}
                    </span>
                  </button>
                  {open && (
                    <div className="mb-1.5 ml-[19px] mt-0.5 border-l border-border pl-1.5">
                      {c.groups
                        ? c.groups.map((g) => (
                            <div key={g.label}>
                              <div
                                className="truncate px-2.5 pb-0.5 pt-1.5 font-mono text-[10.5px] text-muted/80"
                                title={g.label}
                              >
                                {g.label}
                              </div>
                              {g.items.map((r) => (
                                <KindItem
                                  key={r.key}
                                  r={r}
                                  active={kindActive(r)}
                                  onClick={() => void select(r.key)}
                                />
                              ))}
                            </div>
                          ))
                        : c.items.map((r) => (
                            <KindItem
                              key={r.key}
                              r={r}
                              active={kindActive(r)}
                              onClick={() => void select(r.key)}
                            />
                          ))}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </nav>
  )
}
