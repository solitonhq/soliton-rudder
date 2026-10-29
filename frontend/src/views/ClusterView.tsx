import { useClusters } from '../store/clusters'
import { Explorer } from './Explorer'

export function ClusterView({ id }: { id: string }) {
  const exists = useClusters((s) => s.clusters.some((c) => c.id === id))
  if (!exists) return null
  return <Explorer clusterId={id} />
}
