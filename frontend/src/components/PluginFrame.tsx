import { useCallback, useEffect, useRef } from 'react'
import { plugins } from '../lib/backend'
import { useClusters } from '../store/clusters'
import { useExplorer } from '../store/explorer'
import { onPluginEvent } from '../store/plugins'
import { useSettings } from '../store/settings'

const THEME_VARS = [
  '--bg',
  '--surface',
  '--surface-2',
  '--surface-hover',
  '--border',
  '--fg',
  '--fg-muted',
  '--accent',
  '--accent-fg',
  '--success',
  '--warning',
  '--danger',
  '--font-sans',
  '--font-mono',
]

const MAX_PARAMS = 1 << 20

function themeSnapshot(name: string) {
  const cs = getComputedStyle(document.documentElement)
  const vars: Record<string, string> = {}
  for (const v of THEME_VARS) vars[v] = cs.getPropertyValue(v).trim()
  return { name, vars }
}

/**
 * Hosts a plugin's UI in a sandboxed iframe (scripts only, opaque origin, no network). All data access goes
 * through this bridge to the host API, where the plugin's granted permissions are enforced.
 */
export function PluginFrame({
  pluginId,
  entry,
  panelId,
}: {
  pluginId: string
  entry: string
  panelId: string
}) {
  const ref = useRef<HTMLIFrameElement>(null)
  const clusterId = useClusters((s) => s.activeId)
  const clusterName = useClusters((s) => s.clusters.find((c) => c.id === s.activeId)?.name ?? '')
  const namespaces = useExplorer((s) => s.namespaces)
  const theme = useSettings((s) => s.theme)
  const ready = useRef(false)

  const post = useCallback((msg: unknown) => ref.current?.contentWindow?.postMessage(msg, '*'), [])
  const sendContext = useCallback(
    () =>
      post({
        type: 'context',
        context: {
          clusterId,
          clusterName,
          namespaces,
          panel: panelId,
          theme: themeSnapshot(theme),
        },
      }),
    [post, clusterId, clusterName, namespaces, panelId, theme],
  )

  // the message listener below is long-lived; it reads the freshest sender through this ref
  const sendRef = useRef(sendContext)
  useEffect(() => {
    sendRef.current = sendContext
    if (ready.current) sendContext() // cluster / namespace / theme changed after the page loaded
  }, [sendContext])

  useEffect(() => {
    const onMessage = async (e: MessageEvent) => {
      if (e.source !== ref.current?.contentWindow) return
      const m = e.data as { type?: string; id?: number; method?: string; params?: unknown }
      if (m?.type === 'ready') {
        ready.current = true
        sendRef.current()
      } else if (m?.type === 'rpc' && typeof m.id === 'number' && typeof m.method === 'string') {
        try {
          if (JSON.stringify(m.params ?? {}).length > MAX_PARAMS)
            throw new Error('request too large')
          const res = await plugins.uiCall(pluginId, m.method, m.params)
          post({ type: 'rpc-result', id: m.id, result: res ? JSON.parse(res) : null })
        } catch (err) {
          post({
            type: 'rpc-result',
            id: m.id,
            error: err instanceof Error ? err.message : String(err),
          })
        }
      }
    }
    window.addEventListener('message', onMessage)
    const off = onPluginEvent(
      (p, name, data) => p === pluginId && post({ type: 'event', name, data }),
    )
    return () => {
      window.removeEventListener('message', onMessage)
      off()
    }
  }, [pluginId, post])

  return (
    <iframe
      ref={ref}
      title={`Plugin ${pluginId}`}
      src={`/plugins/${pluginId}/${entry}`}
      sandbox="allow-scripts"
      referrerPolicy="no-referrer"
      className="h-full w-full border-0 bg-bg"
    />
  )
}
