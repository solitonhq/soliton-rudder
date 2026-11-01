/** Kinds that have a replica count, and kinds that can be rolled (kubectl rollout restart). */
export const SCALABLE = new Set([
  'deployments',
  'statefulsets',
  'replicasets',
  'replicationcontrollers',
])
export const RESTARTABLE = new Set(['deployments', 'statefulsets', 'daemonsets'])

/** The desired replica count from a "ready/desired" table cell such as "2/3", or null when there is none. */
export function desiredReplicas(cells: unknown[]): number | null {
  for (const c of cells) {
    const m = typeof c === 'string' ? /^(\d+)\/(\d+)$/.exec(c) : null
    if (m) return Number(m[2])
  }
  return null
}
