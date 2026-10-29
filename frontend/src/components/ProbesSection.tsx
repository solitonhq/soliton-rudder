import clsx from 'clsx'
import { Copy, ExternalLink, Play } from 'lucide-react'
import { useState } from 'react'
import { probes } from '../lib/backend'
import { waitForPort } from '../lib/quickforward'
import type { ContainerState, ProbeInfo, ProbeResult } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useUI } from '../store/ui'
import { openLink } from './IngressDetails'

const kindLabel: Record<ProbeInfo['kind'], string> = {
  liveness: 'Liveness',
  readiness: 'Readiness',
  startup: 'Startup',
}
const kindTone: Record<ProbeInfo['kind'], string> = {
  liveness: 'bg-warning/15 text-warning',
  readiness: 'bg-success/15 text-success',
  startup: 'bg-accent/15 text-accent',
}

/** What the probe does, in the words people use: `GET /healthz :8080 (HTTPS)`. */
export function describeProbe(p: ProbeInfo): string {
  const port = p.port ? `:${p.port}` : p.portName ? `:${p.portName} (undeclared)` : ''
  switch (p.type) {
    case 'http':
      return `GET ${p.path} ${port}${p.scheme === 'HTTPS' ? ' (HTTPS)' : ''}`.replace(/\s+/g, ' ')
    case 'tcp':
      return `TCP ${port}`
    case 'grpc':
      return `gRPC ${port}${p.grpcService ? ` (${p.grpcService})` : ''}`
    default:
      return `exec ${p.command.join(' ')}`
  }
}

/** A command that reproduces the probe from inside the cluster (the pod IP is only reachable there). */
export function probeCommand(p: ProbeInfo, podIP: string): string {
  const host = p.host || podIP || '<pod-ip>'
  if (p.type === 'http') {
    const scheme = p.scheme === 'HTTPS' ? 'https' : 'http'
    const hdrs = p.headers.map((h) => ` -H '${h.name}: ${h.value.replace(/'/g, "'\\''")}'`).join('')
    return `curl -is${scheme === 'https' ? 'k' : ''}${hdrs} ${scheme}://${host}:${p.port}${p.path}`
  }
  if (p.type === 'tcp') return `nc -vz ${host} ${p.port}`
  if (p.type === 'grpc') return `grpcurl -plaintext ${host}:${p.port} grpc.health.v1.Health/Check`
  return p.command.join(' ')
}

function Result({ r }: { r: ProbeResult }) {
  return (
    <div className="mt-1.5">
      <div
        className={clsx(
          'flex flex-wrap items-center gap-2 text-xs',
          r.ok ? 'text-success' : 'text-danger',
        )}
      >
        <span className="font-semibold">{r.ok ? 'Success' : 'Failure'}</span>
        <span className="select-text">{r.detail}</span>
        <span className="font-mono text-muted">{r.durationMs} ms</span>
      </div>
      {r.body && (
        <pre className="mt-1 max-h-28 select-text overflow-auto whitespace-pre-wrap rounded-md border border-border bg-bg p-2 font-mono text-[11px]">
          {r.body}
        </pre>
      )}
    </div>
  )
}

function ProbeRow({
  p,
  clusterId,
  ns,
  pod,
  podIP,
}: {
  p: ProbeInfo
  clusterId: string
  ns: string
  pod: string
  podIP: string
}) {
  const toast = useUI((s) => s.toast)
  const [result, setResult] = useState<ProbeResult | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const runnable = (p.type === 'http' || p.type === 'tcp') && p.port > 0

  const check = async () => {
    setBusy(true)
    setError('')
    try {
      setResult(
        await probes.check(clusterId, ns, pod, {
          type: p.type,
          scheme: p.scheme,
          port: p.port,
          path: p.path,
          headers: p.headers,
        }),
      )
    } catch (e) {
      setResult(null)
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const open = async () => {
    setBusy(true)
    try {
      const fwd = await probes.forward(clusterId, ns, pod, p.port)
      const cur = await waitForPort(clusterId, fwd, 8000)
      if (cur.status === 'error' || !cur.activePort)
        throw new Error(cur.message || 'The port forward did not start')
      openLink(`${p.scheme === 'HTTPS' ? 'https' : 'http'}://localhost:${cur.activePort}${p.path}`)
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const btn =
    'inline-flex h-6 cursor-pointer items-center gap-1 rounded border border-border bg-surface-2 px-2 text-[11px] font-medium hover:border-accent disabled:cursor-not-allowed disabled:opacity-50'
  return (
    <li className="px-3 py-2">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span
          className={clsx(
            'rounded px-1.5 py-0.5 text-[10px] font-bold uppercase',
            kindTone[p.kind],
          )}
        >
          {kindLabel[p.kind]}
        </span>
        <span className="min-w-0 select-text break-all font-mono text-xs">{describeProbe(p)}</span>
        <div className="ml-auto flex gap-1">
          {runnable && (
            <button
              className={btn}
              disabled={busy}
              onClick={() => void check()}
              title="Run this probe now, from here, through a temporary port-forward"
            >
              <Play size={11} /> {busy ? 'Running…' : 'Check'}
            </button>
          )}
          {p.type === 'http' && p.port > 0 && (
            <button
              className={btn}
              disabled={busy}
              onClick={() => void open()}
              title="Forward the port and open this endpoint in the browser"
            >
              <ExternalLink size={11} /> Open
            </button>
          )}
          <button
            className={btn}
            aria-label={`Copy command for ${kindLabel[p.kind]} probe`}
            title={probeCommand(p, podIP)}
            onClick={() => {
              void Promise.resolve(navigator.clipboard?.writeText(probeCommand(p, podIP))).catch(
                () => {},
              )
              toast('success', 'Command copied')
            }}
          >
            <Copy size={11} /> Copy
          </button>
        </div>
      </div>
      <div className="mt-0.5 text-[11px] text-muted">
        every {p.period}s · timeout {p.timeout}s · fails after {p.failureThreshold}
        {p.initialDelay > 0 && ` · starts after ${p.initialDelay}s`}
        {p.successThreshold > 1 && ` · needs ${p.successThreshold} successes`}
        {p.headers.length > 0 && ` · headers: ${p.headers.map((h) => h.name).join(', ')}`}
      </div>
      {!runnable && p.type === 'exec' && (
        <div className="mt-0.5 text-[11px] text-muted">
          Runs inside the container: use Shell to try it.
        </div>
      )}
      {!runnable && p.type !== 'exec' && p.port === 0 && (
        <div className="mt-0.5 text-[11px] text-warning">
          The named port is not declared by the container.
        </div>
      )}
      {error && <div className="mt-1.5 select-text text-xs text-danger">{error}</div>}
      {result && <Result r={result} />}
    </li>
  )
}

function containerLine(c: ContainerState): { text: string; tone: string } {
  if (c.state.startsWith('waiting') || c.state.startsWith('terminated'))
    return { text: c.state, tone: 'text-danger' }
  if (!c.ready) return { text: 'running, not ready', tone: 'text-warning' }
  return { text: 'running, ready', tone: 'text-success' }
}

/** The probes of a pod: where Kubernetes checks health, how often, and a way to try and open each endpoint. */
export function ProbesSection({
  clusterId,
  ns,
  pod,
}: {
  clusterId: string
  ns: string
  pod: string
}) {
  const { data, error } = usePoll(
    () => probes.list(clusterId, ns, pod),
    `${clusterId}|${ns}|${pod}`,
    10_000,
  )
  if (error && !data) return null
  if (!data) return null
  const byContainer = data.containers.map((c) => ({
    c,
    probes: data.probes.filter((p) => p.container === c.name),
  }))
  return (
    <section className="mb-4">
      <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
        Probes
      </h3>
      <div className="divide-y divide-border overflow-hidden rounded-lg border border-border">
        {byContainer.map(({ c, probes: ps }) => {
          const line = containerLine(c)
          return (
            <div key={c.name}>
              <div className="flex flex-wrap items-center gap-x-2 bg-surface-2 px-3 py-1.5 text-xs">
                <span className="font-mono font-semibold">{c.name}</span>
                <span className={line.tone}>{line.text}</span>
                {c.restarts > 0 && (
                  <span className="text-warning">
                    {c.restarts} restart{c.restarts > 1 ? 's' : ''}
                  </span>
                )}
              </div>
              {ps.length === 0 ? (
                <div className="px-3 py-2 text-xs text-muted">
                  No probes: Kubernetes treats this container as ready as soon as it starts.
                </div>
              ) : (
                <ul className="divide-y divide-border/60">
                  {ps.map((p) => (
                    <ProbeRow
                      key={p.kind}
                      p={p}
                      clusterId={clusterId}
                      ns={ns}
                      pod={pod}
                      podIP={data.podIP}
                    />
                  ))}
                </ul>
              )}
            </div>
          )
        })}
      </div>
    </section>
  )
}
