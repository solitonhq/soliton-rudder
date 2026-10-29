import clsx from 'clsx'
import { AlertTriangle, Bug } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button, Dialog, Field, Input } from '../components/ui'
import { debug, tools } from '../lib/backend'
import type { DebugContainer, LogTarget } from '../lib/types'
import { useDock } from '../store/dock'
import { useUI } from '../store/ui'

const PRESETS: { image: string; label: string; hint: string }[] = [
  {
    image: 'nicolaka/netshoot',
    label: 'netshoot',
    hint: 'curl, dig, tcpdump, netstat, ss, iperf, nmap…',
  },
  { image: 'busybox:stable', label: 'busybox', hint: 'tiny: sh, wget, nslookup, ps' },
  { image: 'alpine:3', label: 'alpine', hint: 'small, apk add whatever you need' },
  { image: 'ubuntu:24.04', label: 'ubuntu', hint: 'full userland, apt-get' },
]

const IMAGE_KEY = 'soliton-rudder.debug.image'
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

function loadImage(): string {
  try {
    return localStorage.getItem(IMAGE_KEY) || PRESETS[0].image
  } catch {
    return PRESETS[0].image
  }
}

/** Injects an ephemeral debug container into a running pod and opens a shell in it. */
export function DebugDialog({
  clusterId,
  ns,
  pod,
  onClose,
}: {
  clusterId: string
  ns: string
  pod: string
  onClose: () => void
}) {
  const toast = useUI((s) => s.toast)
  const openTab = useDock((s) => s.openTab)
  const [image, setImage] = useState(loadImage)
  const [target, setTarget] = useState('')
  const [caps, setCaps] = useState(true)
  const [containers, setContainers] = useState<LogTarget | null>(null)
  const [existing, setExisting] = useState<DebugContainer[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    void tools
      .logTargets(clusterId, 'core/v1/pods', ns, pod)
      .then((t) => {
        if (cancelled) return
        setContainers(t[0] ?? null)
        setTarget(t[0]?.containers[0] ?? '')
      })
      .catch(() => {})
    void debug
      .list(clusterId, ns, pod)
      .then((d) => !cancelled && setExisting(d))
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [clusterId, ns, pod])

  const attach = (container: string) => {
    openTab({
      kind: 'terminal',
      clusterId,
      title: `Debug: ${pod}`,
      pod: { ns, name: pod, container },
    })
    onClose()
  }

  const start = async () => {
    setBusy(true)
    setError('')
    try {
      try {
        localStorage.setItem(IMAGE_KEY, image.trim())
      } catch {
        /* ignore */
      }
      const dc = await debug.start(clusterId, ns, pod, { image: image.trim(), target, caps })
      toast('success', `Debug container ${dc.name} is running`)
      attach(dc.name)
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  return (
    <Dialog title={`Debug ${pod}`} onClose={onClose} width={560}>
      <div className="space-y-4 text-[13px]">
        <p className="text-muted">
          Adds a temporary container with debugging tools to this running pod, even if the app image
          has no shell (distroless). Use it to inspect the network, DNS, processes and ports of the
          pod.
        </p>

        {existing.length > 0 && (
          <section>
            <h4 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
              Already in this pod
            </h4>
            {existing.map((d) => (
              <div
                key={d.name}
                className="flex items-center justify-between rounded-md border border-border px-3 py-1.5"
              >
                <div className="min-w-0">
                  <div className="font-mono text-xs">{d.name}</div>
                  <div className="truncate text-xs text-muted">
                    {d.image} · {d.state}
                    {d.reason && ` (${d.reason})`}
                  </div>
                </div>
                <Button
                  variant="secondary"
                  disabled={d.state !== 'running'}
                  onClick={() => attach(d.name)}
                >
                  Attach
                </Button>
              </div>
            ))}
          </section>
        )}

        <section>
          <h4 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
            Image
          </h4>
          <div className="mb-2 flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <button
                key={p.image}
                onClick={() => setImage(p.image)}
                title={p.hint}
                className={clsx(
                  'cursor-pointer rounded-md border px-2.5 py-1 text-xs',
                  image === p.image
                    ? 'border-accent bg-accent/15 text-accent'
                    : 'border-border hover:border-muted',
                )}
              >
                {p.label}
              </button>
            ))}
          </div>
          <Input
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="registry/image:tag"
            spellCheck={false}
            className="font-mono"
          />
          <p className="mt-1 text-xs text-muted">
            {PRESETS.find((p) => p.image === image)?.hint ??
              'The cluster must be able to pull this image (private registries / air-gapped clusters need a mirror).'}
          </p>
        </section>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Share processes of container">
            <select
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              className="h-8 w-full rounded-md border border-border bg-bg px-2 text-[13px]"
            >
              {(containers?.containers ?? []).map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
              <option value="">None</option>
            </select>
          </Field>
          <label className="flex cursor-pointer items-start gap-2 pt-5">
            <input
              type="checkbox"
              checked={caps}
              onChange={(e) => setCaps(e.target.checked)}
              className="mt-0.5 accent-[var(--accent)]"
            />
            <span>
              Add NET_ADMIN and SYS_PTRACE
              <span className="block text-xs text-muted">for tcpdump, strace…</span>
            </span>
          </label>
        </div>

        <div className="flex gap-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
          <AlertTriangle size={14} className="mt-px shrink-0" />
          <span>
            Kubernetes cannot remove a debug container: it stays (idle) until the pod is recreated.
            Needs Kubernetes 1.23+ and the <span className="font-mono">patch</span> permission on{' '}
            <span className="font-mono">pods/ephemeralcontainers</span>. Admission policies (Pod
            Security) may reject the extra capabilities.
          </span>
        </div>

        {error && <p className="select-text break-words text-xs text-danger">{error}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={busy || !image.trim()} onClick={() => void start()}>
            <Bug size={14} /> {busy ? 'Starting…' : 'Start debug container'}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
