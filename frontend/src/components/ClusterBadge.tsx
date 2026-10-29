import type { Cluster } from '../lib/types'

export function initials(name: string) {
  const parts = name.split(/[\s\-_/.:@]+/).filter(Boolean)
  const s = parts.length > 1 ? parts[0][0] + parts[1][0] : name.slice(0, 2)
  return s.toUpperCase()
}

export function ClusterBadge({ cluster, size = 40 }: { cluster: Cluster; size?: number }) {
  return (
    <span
      className="grid shrink-0 place-items-center rounded-xl font-semibold text-white"
      style={{ width: size, height: size, background: cluster.color, fontSize: size * 0.38 }}
    >
      {cluster.icon || initials(cluster.name)}
    </span>
  )
}
