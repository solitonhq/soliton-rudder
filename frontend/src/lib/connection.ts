import type { HealthStatus } from './types'

export interface ConnectionLook {
  /** Tailwind classes for the ring (border) around the cluster icon */
  ring: string
  label: string
}

/**
 * How a cluster looks on the hotbar: the ring is green when it is connected and healthy, red when it is closed
 * (not connected) or not reachable, and amber while the state is still being worked out.
 */
export function connectionLook(
  connected: boolean,
  status?: HealthStatus,
  error?: string,
): ConnectionLook {
  if (!connected) return { ring: 'border-danger/70', label: 'Disconnected' }
  switch (status) {
    case 'unreachable':
    case 'error':
      return {
        ring: 'border-danger',
        label: error ? `Connected, but not reachable: ${error}` : 'Connected, but not reachable',
      }
    case 'unauthorized':
      return { ring: 'border-warning', label: 'Connected, but not authorized' }
    case 'checking':
      return { ring: 'border-warning animate-pulse', label: 'Checking…' }
    default:
      return { ring: 'border-success', label: 'Connected' }
  }
}
