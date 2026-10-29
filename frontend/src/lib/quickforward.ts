import { openURL, tools } from './backend'
import type { ForwardKind, ForwardRuntime, PortInfo } from './types'

/** Resource plural → forward target kind (mirrors kube.ForwardKinds in Go). */
export const FORWARD_KIND: Record<string, ForwardKind> = {
  pods: 'pod',
  services: 'service',
  deployments: 'deployment',
  statefulsets: 'statefulset',
  daemonsets: 'daemonset',
  replicasets: 'replicaset',
}
export const FORWARDABLE = new Set(Object.keys(FORWARD_KIND))

export type QuickOutcome =
  { kind: 'started'; text: string; url?: string } | { kind: 'choose'; ports: PortInfo[] }

/** True for ports that usually speak HTTP, so opening a browser on them makes sense (not databases or caches). */
export function looksLikeWeb(remotePort: number): boolean {
  return (
    remotePort === 80 ||
    remotePort === 443 ||
    [3000, 3001, 3030, 3100, 3333].includes(remotePort) ||
    remotePort === 4200 ||
    remotePort === 5000 ||
    remotePort === 5173 ||
    (remotePort >= 8000 && remotePort <= 8999) ||
    remotePort === 9000 ||
    remotePort === 9090 ||
    remotePort === 9093
  )
}

/** The address to open for a forward: https for the usual TLS ports. */
export function forwardURL(localPort: number, remotePort: number): string {
  const scheme = remotePort === 443 || remotePort === 8443 ? 'https' : 'http'
  return `${scheme}://localhost:${localPort}`
}

/** Polls until the forward has a local port (or failed, or the wait ran out) and returns its latest state. */
export async function waitForPort(
  clusterId: string,
  initial: ForwardRuntime,
  wait = 4000,
): Promise<ForwardRuntime> {
  const deadline = Date.now() + wait
  let cur = initial
  while (cur.activePort === 0 && cur.status !== 'error' && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 250))
    const list = await tools.forwards.list(clusterId)
    cur = list.find((f) => f.id === initial.id) ?? cur
  }
  return cur
}

/** Waits for a just-started forward and opens it in the browser. Returns the state it ended up in. */
export async function openForwardInBrowser(
  clusterId: string,
  initial: ForwardRuntime,
): Promise<ForwardRuntime> {
  const cur = await waitForPort(clusterId, initial, 8000)
  if (cur.activePort > 0 && cur.status !== 'error')
    openURL(forwardURL(cur.activePort, cur.remotePort))
  return cur
}

/**
 * One-click forward: the backend picks a free local port and reuses an identical saved forward if there is one.
 * Waits briefly for the tunnel so the user sees the actual `localhost:<port>` to use.
 */
export async function quickForward(
  clusterId: string,
  resourceKey: string,
  ns: string,
  name: string,
  wait = 4000,
): Promise<QuickOutcome> {
  const res = await tools.quickForward(clusterId, resourceKey, ns, name)
  if (res.needChoice || !res.forward) return { kind: 'choose', ports: res.ports }
  const cur = await waitForPort(clusterId, res.forward, wait)
  if (cur.status === 'error') throw new Error(cur.message || 'Could not start the forward')
  return {
    kind: 'started',
    text: cur.activePort
      ? `localhost:${cur.activePort} → ${name}:${cur.remotePort}`
      : `Forward to ${name}:${cur.remotePort} is starting (see Port Forwards)`,
    url:
      cur.activePort && looksLikeWeb(cur.remotePort)
        ? forwardURL(cur.activePort, cur.remotePort)
        : undefined,
  }
}
