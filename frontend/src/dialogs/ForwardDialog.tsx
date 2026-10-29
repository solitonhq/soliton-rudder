import { useEffect, useState } from 'react'
import { Button, Dialog, Field, Input } from '../components/ui'
import { tools } from '../lib/backend'
import type { ForwardKind, ForwardRuntime, PortInfo } from '../lib/types'
import { forwardURL, looksLikeWeb, openForwardInBrowser, waitForPort } from '../lib/quickforward'
import { useForwards } from '../store/forwards'
import { useUI } from '../store/ui'

export interface ForwardTargetProps {
  clusterId: string
  kind?: ForwardKind
  ns?: string
  name?: string
  resourceKey?: string // core/v1/pods | core/v1/services — enables port discovery
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** After the dialog is gone: wait for the local port, then tell the user where it is and optionally open it. */
async function followUp(created: ForwardRuntime, open: boolean) {
  const toast = useUI.getState().toast
  try {
    const cur = open
      ? await openForwardInBrowser(created.clusterId, created)
      : await waitForPort(created.clusterId, created)
    if (cur.status === 'error') toast('error', cur.message || 'The port forward could not start')
    else if (cur.activePort)
      toast(
        'success',
        `${forwardURL(cur.activePort, cur.remotePort).replace(/^https?:\/\//, '')} → ${cur.name}:${cur.remotePort}`,
      )
  } catch {
    /* the Port Forwards tab shows the state */
  }
}

export function ForwardDialog({
  clusterId,
  kind: k0,
  ns: ns0,
  name: n0,
  resourceKey,
  onClose,
}: ForwardTargetProps & { onClose: () => void }) {
  const toast = useUI((s) => s.toast)
  const [kind, setKind] = useState<ForwardKind>(k0 ?? 'service')
  const [ns, setNs] = useState(ns0 ?? 'default')
  const [name, setName] = useState(n0 ?? '')
  const [remote, setRemote] = useState('')
  const [local, setLocal] = useState('')
  const [auto, setAuto] = useState(false)
  // null = follow the port: web-looking ports open a browser, databases and caches do not
  const [openChoice, setOpenChoice] = useState<boolean | null>(null)
  const [ports, setPorts] = useState<PortInfo[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const locked = !!n0

  useEffect(() => {
    if (!resourceKey || !ns0 || !n0) return
    let cancelled = false
    tools
      .ports(clusterId, resourceKey, ns0, n0)
      .then((p) => {
        if (cancelled) return
        setPorts(p)
        if (p[0]) setRemote(String(p[0].port))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [clusterId, resourceKey, ns0, n0])

  const rp = Number(remote)
  const lp = local === '' ? 0 : Number(local)
  const valid =
    name.trim() &&
    ns.trim() &&
    Number.isInteger(rp) &&
    rp > 0 &&
    rp < 65536 &&
    Number.isInteger(lp) &&
    (lp === 0 || (lp >= 1024 && lp < 65536))

  const openBrowser = openChoice ?? looksLikeWeb(rp)

  const submit = async () => {
    setBusy(true)
    try {
      const created = await tools.forwards.add(
        {
          clusterId,
          kind,
          namespace: ns.trim(),
          name: name.trim(),
          remotePort: rp,
          localPort: lp,
          auto,
        },
        true,
      )
      await useForwards.getState().load(clusterId)
      toast('success', 'Port forward started')
      onClose()
      void followUp(created, openBrowser)
    } catch (e) {
      setError(errText(e))
      setBusy(false)
    }
  }

  return (
    <Dialog title="Port forward" onClose={onClose} width={460}>
      <div className="space-y-3">
        {!locked && (
          <div className="grid grid-cols-[110px_1fr] gap-3">
            <Field label="Kind">
              <select
                value={kind}
                onChange={(e) => setKind(e.target.value as ForwardKind)}
                className="h-8 w-full rounded-md border border-border bg-bg px-2 text-[13px]"
              >
                <option value="service">Service</option>
                <option value="pod">Pod</option>
                <option value="deployment">Deployment</option>
                <option value="statefulset">StatefulSet</option>
                <option value="daemonset">DaemonSet</option>
              </select>
            </Field>
            <Field label="Namespace">
              <Input value={ns} onChange={(e) => setNs(e.target.value)} />
            </Field>
          </div>
        )}
        <Field label={locked ? `${kind} · ${ns}` : 'Name'}>
          <Input value={name} onChange={(e) => setName(e.target.value)} disabled={locked} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Remote port">
            <Input
              value={remote}
              onChange={(e) => setRemote(e.target.value)}
              inputMode="numeric"
              list="fwd-ports"
              autoFocus
            />
            <datalist id="fwd-ports">
              {ports.map((p) => (
                <option key={p.port + p.name} value={p.port}>
                  {p.name || p.target}
                </option>
              ))}
            </datalist>
          </Field>
          <Field label="Local port (blank = auto)">
            <Input
              value={local}
              onChange={(e) => setLocal(e.target.value)}
              inputMode="numeric"
              placeholder="auto"
            />
          </Field>
        </div>
        {ports.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {ports.map((p) => (
              <button
                key={p.port + p.name}
                onClick={() => setRemote(String(p.port))}
                className="cursor-pointer rounded-md border border-border px-2 py-0.5 font-mono text-xs hover:border-accent"
              >
                {p.port}
                {p.name && <span className="ml-1 text-muted">{p.name}</span>}
              </button>
            ))}
          </div>
        )}
        <label className="flex cursor-pointer items-center gap-2 text-[13px]">
          <input
            type="checkbox"
            checked={auto}
            onChange={(e) => setAuto(e.target.checked)}
            className="accent-[var(--accent)]"
          />
          Start automatically when this cluster connects
        </label>
        <label className="flex cursor-pointer items-center gap-2 text-[13px]">
          <input
            type="checkbox"
            checked={openBrowser}
            onChange={(e) => setOpenChoice(e.target.checked)}
            className="accent-[var(--accent)]"
          />
          Open in the browser when ready
        </label>
        {error && <p className="text-xs text-danger">{error}</p>}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!valid || busy} onClick={() => void submit()}>
            Forward
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
