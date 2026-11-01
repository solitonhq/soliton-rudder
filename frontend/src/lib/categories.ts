import type { Resource } from './types'

export interface CategoryGroup {
  label: string
  items: Resource[]
}

export interface Category {
  id: string
  label: string
  /** Every resource in the category (for counts and filtering). */
  items: Resource[]
  /** Present for "Custom Resources": items sub-grouped by API group. */
  groups?: CategoryGroup[]
}

export const CUSTOM_ID = 'custom'
export const OTHER_GROUP_LABEL = 'Other built-in'

// Order within a category follows these lists; anything else falls to "Other" / "Custom Resources".
const ORDER: [string, string, string[]][] = [
  // the cluster itself first, then what runs on it, then how it is wired, configured and stored
  ['cluster', 'Cluster', ['nodes', 'namespaces', 'events']],
  [
    'workloads',
    'Workloads',
    [
      'pods',
      'deployments',
      'statefulsets',
      'daemonsets',
      'replicasets',
      'jobs',
      'cronjobs',
      'replicationcontrollers',
    ],
  ],
  [
    'network',
    'Network',
    ['services', 'ingresses', 'ingressclasses', 'endpoints', 'endpointslices', 'networkpolicies'],
  ],
  [
    'config',
    'Config',
    [
      'configmaps',
      'secrets',
      'resourcequotas',
      'limitranges',
      'horizontalpodautoscalers',
      'poddisruptionbudgets',
      'priorityclasses',
      'runtimeclasses',
    ],
  ],
  [
    'storage',
    'Storage',
    [
      'persistentvolumeclaims',
      'persistentvolumes',
      'storageclasses',
      'volumeattachments',
      'csidrivers',
      'csinodes',
    ],
  ],
  [
    'access',
    'Access Control',
    ['serviceaccounts', 'roles', 'rolebindings', 'clusterroles', 'clusterrolebindings'],
  ],
]

export function isCustomGroup(group: string): boolean {
  return group.includes('.') && !group.endsWith('.k8s.io') && group !== 'core'
}

/** Groups discovered resources into the sidebar's categories. Core-API matches only. */
export function categorize(resources: Resource[]): Category[] {
  const used = new Set<string>()
  const out: Category[] = []

  for (const [id, label, names] of ORDER) {
    const items: Resource[] = []
    for (const n of names) {
      const r = resources.find((x) => x.name === n && !isCustomGroup(x.group) && !used.has(x.key))
      if (r) {
        items.push(r)
        used.add(r.key)
      }
    }
    if (items.length) out.push({ id, label, items })
  }

  // Everything that is not a well-known built-in goes under ONE header, sub-grouped by API group.
  const byGroup = new Map<string, Resource[]>()
  const other: Resource[] = []
  for (const r of resources) {
    if (used.has(r.key)) continue
    if (isCustomGroup(r.group)) {
      if (!byGroup.has(r.group)) byGroup.set(r.group, [])
      byGroup.get(r.group)!.push(r)
    } else other.push(r)
  }
  const groups: CategoryGroup[] = [...byGroup.keys()]
    .sort()
    .map((g) => ({ label: g, items: byGroup.get(g)! }))
  if (other.length) groups.push({ label: OTHER_GROUP_LABEL, items: other })
  if (groups.length)
    out.push({
      id: CUSTOM_ID,
      label: 'Custom Resources',
      items: groups.flatMap((g) => g.items),
      groups,
    })
  return out
}

const words = (kind: string) => kind.replace(/([a-z])([A-Z])/g, '$1 $2').split(' ')
const capital = (w: string) => w.charAt(0).toUpperCase() + w.slice(1)

/** English plural of a single word, for kinds whose API plural does not line up with their name. */
function pluralize(w: string): string {
  const lower = w.toLowerCase()
  if (lower.endsWith('s') && !/(ss|us)$/.test(lower)) return w // already plural: Endpoints
  if (/(s|x|z|ch|sh)$/.test(lower)) return w + 'es'
  if (/[^aeiou]y$/.test(lower)) return w.slice(0, -1) + 'ies'
  return w + 's'
}

/**
 * The plural display name of a kind ("Ingresses", "Network Policies", "Ingress Classes"). The API already knows the
 * plural (`networkpolicies`), so it is taken from there; only when that does not line up with the kind is it guessed.
 */
export function resourceTitle(r: Resource): string {
  const w = words(r.kind)
  const prefix = w.slice(0, -1).join('').toLowerCase()
  const name = r.name.toLowerCase()
  const last = w[w.length - 1]!.toLowerCase()
  const rest = name.startsWith(prefix) ? name.slice(prefix.length) : ''
  // the API's plural is trusted only when it is recognisably the plural of this kind's last word
  const lastPlural = rest.slice(0, 3) === last.slice(0, 3) ? rest : pluralize(last).toLowerCase()
  return [...w.slice(0, -1), capital(lastPlural)].join(' ')
}
