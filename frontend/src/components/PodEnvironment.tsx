import { Check, Copy, Eye, EyeOff, KeyRound, SquareArrowOutUpRight } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { kube } from '../lib/backend'
import type { ContainerEnv, EnvFromRef, EnvVar, PodEnv, PodVolume } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const REVEAL_MS = 30_000
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** Jump to the Secret or ConfigMap list, filtered to the one referenced. */
async function openRef(kind: 'secret' | 'configmap', name: string) {
  const ex = useExplorer.getState()
  await ex.select(kind === 'secret' ? 'core/v1/secrets' : 'core/v1/configmaps')
  useExplorer.getState().setSearch(name)
}

/**
 * Where a pod gets its environment and files from. It lists references only; the value of a Secret or ConfigMap key
 * is fetched when you press Show, hidden again after 30 seconds, and never written to a log.
 */
export function PodEnvironment({
  clusterId,
  ns,
  pod,
}: {
  clusterId: string
  ns: string
  pod: string
}) {
  const [env, setEnv] = useState<PodEnv | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    kube
      .podEnv(clusterId, ns, pod)
      .then((e) => !cancelled && setEnv(e))
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [clusterId, ns, pod])

  const containers =
    env?.containers.filter((c) => c.env.length || c.envFrom.length || c.mounts.length) ?? []
  const volumes = env?.volumes ?? []

  return (
    <section className="mb-4 mt-5" aria-label="Environment and mounts">
      <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">
        Environment &amp; mounts
      </h3>
      {error ? (
        <div className="text-xs text-muted">{error}</div>
      ) : !env ? (
        <div className="text-xs text-muted">Loading…</div>
      ) : containers.length === 0 && volumes.length === 0 ? (
        <div className="text-xs text-muted">
          This pod sets no environment variables and mounts no volumes.
        </div>
      ) : (
        <div className="space-y-3">
          {containers.map((c) => (
            <ContainerBlock key={`${c.init}/${c.name}`} c={c} clusterId={clusterId} ns={ns} />
          ))}
          {volumes.length > 0 && <Volumes volumes={volumes} />}
        </div>
      )}
    </section>
  )
}

function ContainerBlock({ c, clusterId, ns }: { c: ContainerEnv; clusterId: string; ns: string }) {
  return (
    <div className="rounded-lg border border-border">
      <div className="flex items-center gap-2 border-b border-border px-3 py-1.5 text-xs font-semibold">
        {c.name}
        {c.init && (
          <span className="rounded bg-surface-hover px-1.5 py-0.5 text-[10px] font-normal text-muted">
            init
          </span>
        )}
      </div>
      <div className="divide-y divide-border">
        {c.env.map((e) => (
          <EnvRow key={e.name} e={e} clusterId={clusterId} ns={ns} />
        ))}
        {c.envFrom.map((f, i) => (
          <EnvFromRow key={`${f.kind}/${f.name}/${i}`} f={f} />
        ))}
      </div>
      {c.mounts.length > 0 && (
        <div className="border-t border-border px-3 py-1.5 text-xs text-muted">
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide">Mounts</div>
          {c.mounts.map((m) => (
            <div key={`${m.volume}:${m.path}`} className="font-mono">
              {m.path} <span className="text-muted">← {m.volume}</span>
              {m.subPath && <span> ({m.subPath})</span>}
              {m.readOnly && (
                <span className="ml-1 rounded bg-surface-hover px-1 text-[10px]">ro</span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function EnvRow({ e, clusterId, ns }: { e: EnvVar; clusterId: string; ns: string }) {
  const refKind = e.source === 'secret' || e.source === 'configmap' ? e.source : null
  return (
    <div className="flex items-start gap-3 px-3 py-1.5 text-xs">
      <span className="w-2/5 shrink-0 break-all font-mono font-semibold">{e.name}</span>
      <div className="min-w-0 flex-1">
        {e.source === 'literal' ? (
          <span className="break-all font-mono">
            {e.value || <span className="text-muted">(empty)</span>}
          </span>
        ) : refKind ? (
          <RefValue
            kind={refKind}
            clusterId={clusterId}
            ns={ns}
            name={e.ref}
            keyName={e.key}
            optional={e.optional}
          />
        ) : (
          <span className="text-muted">
            {e.source === 'field' ? 'pod field' : 'container resource'}{' '}
            <span className="font-mono">{e.detail}</span>
          </span>
        )}
      </div>
    </div>
  )
}

function EnvFromRow({ f }: { f: EnvFromRef }) {
  return (
    <div className="flex items-start gap-3 px-3 py-1.5 text-xs">
      <span className="w-2/5 shrink-0 text-muted">
        All keys
        {f.prefix && (
          <>
            {' '}
            with prefix <span className="font-mono">{f.prefix}</span>
          </>
        )}
      </span>
      <div className="min-w-0 flex-1">
        <RefLink kind={f.kind} name={f.name} />
        {f.optional && <span className="ml-2 text-muted">optional</span>}
      </div>
    </div>
  )
}

function RefLink({ kind, name }: { kind: 'secret' | 'configmap'; name: string }) {
  return (
    <button
      onClick={() => void openRef(kind, name)}
      title={`Open ${kind} ${name}`}
      className="inline-flex cursor-pointer items-center gap-1 text-accent hover:underline"
    >
      {kind === 'secret' && <KeyRound size={11} aria-hidden />}
      <span className="break-all font-mono">{name}</span>
      <SquareArrowOutUpRight size={10} aria-hidden />
    </button>
  )
}

const smallBtn =
  'inline-flex cursor-pointer items-center gap-1 rounded border border-border px-1.5 py-0.5 text-[11px] text-muted hover:border-accent hover:text-accent disabled:opacity-50'

function RefValue({
  kind,
  clusterId,
  ns,
  name,
  keyName,
  optional,
}: {
  kind: 'secret' | 'configmap'
  clusterId: string
  ns: string
  name: string
  keyName: string
  optional: boolean
}) {
  const toast = useUI((s) => s.toast)
  const [value, setValue] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const hide = useCallback(() => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = null
    setValue(null)
  }, [])
  // never leave a revealed value behind when the panel goes away
  useEffect(() => hide, [hide])

  const show = async () => {
    setBusy(true)
    setError('')
    try {
      setValue(await kube.refValue(clusterId, ns, kind, name, keyName))
      timer.current = setTimeout(hide, REVEAL_MS)
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const copy = async () => {
    if (value === null) return
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast('error', 'Could not copy to the clipboard')
    }
  }

  return (
    <div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <RefLink kind={kind} name={name} />
        <span className="font-mono text-muted">/ {keyName}</span>
        {optional && <span className="text-muted">optional</span>}
        {value === null ? (
          <button onClick={() => void show()} disabled={busy} className={smallBtn}>
            <Eye size={11} aria-hidden /> {busy ? 'Loading…' : 'Show'}
          </button>
        ) : (
          <>
            <button onClick={hide} className={smallBtn}>
              <EyeOff size={11} aria-hidden /> Hide
            </button>
            <button onClick={() => void copy()} className={smallBtn}>
              {copied ? <Check size={11} aria-hidden /> : <Copy size={11} aria-hidden />}{' '}
              {copied ? 'Copied' : 'Copy'}
            </button>
          </>
        )}
      </div>
      {value !== null && (
        <pre className="mt-1 max-h-40 select-text overflow-auto whitespace-pre-wrap break-all rounded bg-bg px-2 py-1 font-mono text-[11px]">
          {value || '(empty)'}
        </pre>
      )}
      {error && <div className="mt-1 text-[11px] text-danger">{error}</div>}
    </div>
  )
}

function Volumes({ volumes }: { volumes: PodVolume[] }) {
  return (
    <div className="rounded-lg border border-border">
      <div className="border-b border-border px-3 py-1.5 text-xs font-semibold">Volumes</div>
      <div className="divide-y divide-border">
        {volumes.map((v) => (
          <div key={v.name} className="flex items-start gap-3 px-3 py-1.5 text-xs">
            <span className="w-2/5 shrink-0 break-all font-mono font-semibold">{v.name}</span>
            <div className="min-w-0 flex-1 space-y-0.5">
              {v.kind === 'secret' || v.kind === 'configmap' ? (
                <div>
                  <span className="mr-1 text-muted">{v.kind}</span>
                  <RefLink kind={v.kind} name={v.ref} />
                </div>
              ) : v.kind === 'projected' ? (
                <div className="text-muted">
                  projected
                  {v.sources.map((s, i) => (
                    <div key={i}>
                      {s.kind === 'secret' || s.kind === 'configmap' ? (
                        <>
                          {s.kind} <RefLink kind={s.kind} name={s.name} />
                        </>
                      ) : (
                        s.kind
                      )}
                    </div>
                  ))}
                </div>
              ) : (
                <span className="text-muted">
                  {v.kind === 'pvc' ? 'claim' : v.kind}
                  {v.ref && <span className="ml-1 break-all font-mono text-fg">{v.ref}</span>}
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
