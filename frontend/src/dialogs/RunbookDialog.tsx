import clsx from 'clsx'
import { BookOpen, ChevronRight, Play } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { kube, runbooks } from '../lib/backend'
import type { CheckOutput, Incident, Runbook, RunbookStep } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { DebugDialog } from './DebugDialog'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const KIND_KEYS: Record<string, string> = {
  Pod: 'core/v1/pods',
  Node: 'core/v1/nodes',
  Deployment: 'apps/v1/deployments',
  StatefulSet: 'apps/v1/statefulsets',
  DaemonSet: 'apps/v1/daemonsets',
}

/** Resource key for a kind: the well-known ones directly, anything else through the cluster's catalog. */
export function useKindKey(kind: string): string {
  const catalog = useExplorer((s) => s.catalog)
  return KIND_KEYS[kind] ?? catalog.find((r) => r.kind === kind)?.key ?? ''
}

const verb: Record<string, string> = {
  restart: 'Restart',
  scale: 'Scale',
  delete: 'Delete',
}

function StepRow({
  n,
  step,
  clusterId,
  incident,
  resourceKey,
  onDebug,
  onOpen,
}: {
  n: number
  step: RunbookStep
  clusterId: string
  incident: Incident
  resourceKey: string
  onDebug: () => void
  onOpen: () => void
}) {
  const toast = useUI((s) => s.toast)
  const [out, setOut] = useState<CheckOutput | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [done, setDone] = useState(false)

  const run = async () => {
    setBusy(true)
    setError('')
    try {
      setOut(
        await runbooks.check(clusterId, step.do, resourceKey, incident.namespace, incident.name),
      )
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const act = async () => {
    setBusy(true)
    setError('')
    try {
      if (step.do === 'restart')
        await kube.restart(clusterId, resourceKey, incident.namespace, incident.name)
      else if (step.do === 'scale')
        await kube.scale(
          clusterId,
          resourceKey,
          incident.namespace,
          incident.name,
          step.replicas ?? 0,
        )
      else if (step.do === 'delete')
        await kube.remove(clusterId, resourceKey, incident.namespace, incident.name)
      toast('success', `${verb[step.do]} requested for ${incident.name}`)
      setDone(true)
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  const mutating = step.do === 'restart' || step.do === 'scale' || step.do === 'delete'
  const label =
    step.do === 'scale'
      ? `Scale to ${step.replicas ?? 0}`
      : mutating
        ? `${verb[step.do]} ${incident.kind.toLowerCase()}`
        : ''

  return (
    <li className={clsx('rounded-lg border border-border p-3', !step.applicable && 'opacity-50')}>
      <div className="flex items-start gap-2">
        <span className="mt-px grid h-5 w-5 shrink-0 place-items-center rounded-full bg-surface-2 text-[11px] font-semibold">
          {n}
        </span>
        <div className="min-w-0 flex-1">
          <div className="font-medium">{step.title}</div>
          {step.text && <p className="select-text whitespace-pre-wrap text-muted">{step.text}</p>}
          {!step.applicable && (
            <p className="text-xs text-muted">Does not apply to a {incident.kind}.</p>
          )}
          {step.applicable && step.readOnly && (
            <div className="mt-2">
              <Button disabled={busy} onClick={() => void run()}>
                <Play size={12} /> {busy ? 'Running…' : out ? 'Run again' : 'Run check'}
              </Button>
              <span className="ml-2 text-[11px] text-muted">read-only</span>
            </div>
          )}
          {step.applicable && mutating && !done && (
            <div className="mt-2 flex items-center gap-2">
              {!confirm ? (
                <Button onClick={() => setConfirm(true)}>{label}…</Button>
              ) : (
                <>
                  <Button variant="danger" disabled={busy} onClick={() => void act()}>
                    {busy ? 'Working…' : `Yes, ${label.toLowerCase()} ${incident.name}`}
                  </Button>
                  <Button variant="ghost" onClick={() => setConfirm(false)}>
                    Cancel
                  </Button>
                </>
              )}
              <span className="text-[11px] text-warning">changes the cluster</span>
            </div>
          )}
          {done && <p className="mt-2 text-xs text-success">Done.</p>}
          {step.applicable && step.do === 'debug' && (
            <div className="mt-2">
              <Button onClick={onDebug}>Open debug container…</Button>
            </div>
          )}
          {step.applicable && step.do === 'open' && (
            <div className="mt-2">
              <Button onClick={onOpen}>
                Open in table <ChevronRight size={12} />
              </Button>
            </div>
          )}
          {error && (
            <div className="mt-2 select-text rounded-md bg-danger/10 px-3 py-2 text-xs text-danger">
              {error}
            </div>
          )}
          {out && (
            <div className="mt-2">
              <div className="mb-1 text-[10px] font-semibold uppercase text-muted">{out.title}</div>
              <pre className="max-h-64 select-text overflow-auto whitespace-pre-wrap rounded-md border border-border bg-bg p-2 font-mono text-[11px]">
                {out.text}
              </pre>
            </div>
          )}
        </div>
      </div>
    </li>
  )
}

/** Step-by-step guide for a failing object. Read-only checks run in place; anything that changes the cluster asks first. */
export function RunbookDialog({
  clusterId,
  incident,
  onClose,
}: {
  clusterId: string
  incident: Incident
  onClose: () => void
}) {
  const resourceKey = useKindKey(incident.kind)
  const setView = useExplorer((s) => s.setView)
  const jump = useExplorer((s) => s.jump)
  const [list, setList] = useState<Runbook[] | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [sel, setSel] = useState(0)
  const [error, setError] = useState('')
  const [debug, setDebug] = useState(false)

  useEffect(() => {
    let cancelled = false
    ;(showAll ? runbooks.all(incident) : runbooks.match(incident))
      .then((l) => {
        if (cancelled) return
        setList(l)
        setSel(0)
      })
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [incident, showAll])

  const rb = list?.[sel]
  return (
    <Dialog title="Runbook" onClose={onClose} width={780}>
      <div className="space-y-3 text-[13px]">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded bg-surface-2 px-1.5 py-0.5 font-semibold uppercase text-muted">
            {incident.kind}
          </span>
          <span className="font-mono text-sm font-semibold">{incident.name}</span>
          {incident.namespace && <span className="text-muted">{incident.namespace}</span>}
          {incident.reason && (
            <span className="rounded bg-danger/10 px-1.5 py-0.5 text-danger">
              {incident.reason}
            </span>
          )}
        </div>
        {error && (
          <div className="select-text rounded-md bg-danger/10 px-3 py-2 text-danger">{error}</div>
        )}
        {!list && !error && <div className="text-muted">Finding runbooks…</div>}
        {list && list.length === 0 && (
          <div className="rounded-md bg-surface-2 px-3 py-3 text-muted">
            No runbook matches this incident.{' '}
            <button
              className="cursor-pointer text-accent hover:underline"
              onClick={() => setShowAll(true)}
            >
              Browse all runbooks
            </button>{' '}
            or add your own under <em>Runbooks…</em>.
          </div>
        )}
        {list && list.length > 1 && (
          <div className="flex flex-wrap gap-1.5">
            {list.map((r, i) => (
              <button
                key={r.id}
                onClick={() => setSel(i)}
                className={clsx(
                  'cursor-pointer rounded-md border px-2.5 py-1 text-xs',
                  i === sel
                    ? 'border-accent bg-accent/15 text-accent'
                    : 'border-border hover:border-muted',
                )}
              >
                {r.title}
              </button>
            ))}
          </div>
        )}
        {rb && (
          <div>
            <div className="flex items-center gap-2">
              <BookOpen size={15} className="text-accent" />
              <span className="font-semibold">{rb.title}</span>
              <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[10px] font-bold uppercase text-muted">
                {rb.builtin ? 'built-in' : 'yours'}
              </span>
            </div>
            {rb.description && <p className="mt-1 text-muted">{rb.description}</p>}
            <ol className="mt-3 space-y-2">
              {rb.steps.map((s, i) => (
                <StepRow
                  key={`${rb.id}-${i}`}
                  n={i + 1}
                  step={s}
                  clusterId={clusterId}
                  incident={incident}
                  resourceKey={resourceKey}
                  onDebug={() => setDebug(true)}
                  onOpen={() => {
                    setView('resources')
                    void jump(resourceKey, incident.name)
                    onClose()
                  }}
                />
              ))}
            </ol>
            {!showAll && list && list.length > 0 && (
              <button
                className="mt-3 cursor-pointer text-xs text-muted hover:text-accent hover:underline"
                onClick={() => setShowAll(true)}
              >
                None of these fit? Browse all runbooks
              </button>
            )}
          </div>
        )}
        <div className="flex justify-end pt-1">
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
      {debug && (
        <DebugDialog
          clusterId={clusterId}
          ns={incident.namespace}
          pod={incident.name}
          onClose={() => setDebug(false)}
        />
      )}
    </Dialog>
  )
}
