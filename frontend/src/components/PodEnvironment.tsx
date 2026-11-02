import clsx from 'clsx'
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  Search,
  SquareArrowOutUpRight,
} from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { kube } from '../lib/backend'
import type { ContainerEnv, EnvFromRef, EnvVar, PodEnv, PodVolume } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

const REVEAL_MS = 30_000
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** "Show all" / "Hide all" for secrets from the section header; each value reacts to a new sequence number. */
type Bulk = { seq: number; action: 'show' | 'hide' }
const BulkContext = createContext<Bulk>({ seq: 0, action: 'hide' })

/** Jump to the Secret or ConfigMap list, filtered to the one referenced. */
async function openRef(kind: 'secret' | 'configmap', name: string) {
  const ex = useExplorer.getState()
  await ex.select(kind === 'secret' ? 'core/v1/secrets' : 'core/v1/configmaps')
  useExplorer.getState().setSearch(name)
}

const smallBtn =
  'inline-flex cursor-pointer items-center gap-1 rounded border border-border px-1.5 py-0.5 text-[11px] text-muted hover:border-accent hover:text-accent disabled:opacity-50'

const chip = 'shrink-0 rounded px-1.5 py-px text-[10px] font-medium'

const matches = (q: string, ...parts: string[]) =>
  !q || parts.some((p) => p.toLowerCase().includes(q))

/**
 * Where a pod gets its environment and files from, per container, like Lens but with the value in reach: literals and
 * ConfigMap values are shown straight away, Secret values stay masked until you press Show (or Show secrets for all of
 * them), are hidden again after 30 seconds and are never written to a log.
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
  const [bulk, setBulk] = useState<Bulk>({ seq: 0, action: 'hide' })
  const [filter, setFilter] = useState('')

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

  const containers = useMemo(
    () => env?.containers.filter((c) => c.env.length || c.envFrom.length || c.mounts.length) ?? [],
    [env],
  )
  const volumes = env?.volumes ?? []
  const vars = containers.reduce((n, c) => n + c.env.length + c.envFrom.length, 0)
  const mounts = containers.reduce((n, c) => n + c.mounts.length, 0)
  const hasSecrets = containers.some(
    (c) => c.env.some((e) => e.source === 'secret') || c.envFrom.some((f) => f.kind === 'secret'),
  )
  const revealed = bulk.action === 'show'
  const q = filter.trim().toLowerCase()

  // the values hide themselves; keep the button label honest
  useEffect(() => {
    if (!revealed) return
    const t = setTimeout(() => setBulk((b) => ({ seq: b.seq + 1, action: 'hide' })), REVEAL_MS)
    return () => clearTimeout(t)
  }, [revealed, bulk.seq])

  return (
    <section className="mb-4 mt-6" aria-label="Environment and mounts">
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted">
          Environment &amp; mounts
          {env && (vars > 0 || mounts > 0) && (
            <span className="ml-2 font-normal normal-case tracking-normal">
              {vars} variable{vars === 1 ? '' : 's'} · {mounts} mount{mounts === 1 ? '' : 's'}
            </span>
          )}
        </h3>
        {hasSecrets && (
          <button
            onClick={() => setBulk((b) => ({ seq: b.seq + 1, action: revealed ? 'hide' : 'show' }))}
            title="Fetch every Secret value referenced below. They hide again after 30 seconds."
            className={smallBtn}
          >
            {revealed ? <EyeOff size={11} aria-hidden /> : <Eye size={11} aria-hidden />}{' '}
            {revealed ? 'Hide secrets' : 'Show secrets'}
          </button>
        )}
      </div>

      {error ? (
        <div className="text-xs text-muted">{error}</div>
      ) : !env ? (
        <div className="text-xs text-muted">Loading…</div>
      ) : containers.length === 0 && volumes.length === 0 ? (
        <div className="text-xs text-muted">
          This pod sets no environment variables and mounts no volumes.
        </div>
      ) : (
        <BulkContext.Provider value={bulk}>
          {vars + mounts > 8 && (
            <label className="relative mb-2 block">
              <Search
                size={12}
                aria-hidden
                className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-muted"
              />
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter variables and mounts"
                aria-label="Filter variables and mounts"
                className="h-7 w-full rounded-md border border-border bg-bg pl-7 pr-2 text-xs outline-none focus:border-accent"
              />
            </label>
          )}
          <div className="space-y-3">
            {containers.map((c) => (
              <ContainerBlock
                key={`${c.init}/${c.name}`}
                c={c}
                volumes={volumes}
                q={q}
                clusterId={clusterId}
                ns={ns}
                startOpen={containers.length === 1 || !c.init}
              />
            ))}
            {volumes.length > 0 && !q && <Volumes volumes={volumes} />}
          </div>
        </BulkContext.Provider>
      )}
    </section>
  )
}

function ContainerBlock({
  c,
  volumes,
  q,
  clusterId,
  ns,
  startOpen,
}: {
  c: ContainerEnv
  volumes: PodVolume[]
  q: string
  clusterId: string
  ns: string
  startOpen: boolean
}) {
  const [open, setOpen] = useState(startOpen)
  const envs = c.env.filter((e) => matches(q, e.name, e.ref, e.key, e.value))
  const froms = c.envFrom.filter((f) => matches(q, f.name, f.prefix))
  const mnts = c.mounts.filter((m) => matches(q, m.path, m.volume, m.subPath))
  if (q && envs.length + froms.length + mnts.length === 0) return null
  const shown = open || !!q
  const Chevron = shown ? ChevronDown : ChevronRight

  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <button
        onClick={() => setOpen(!open)}
        aria-expanded={shown}
        className="flex w-full cursor-pointer items-center gap-2 bg-surface-hover/40 px-3 py-1.5 text-left text-xs font-semibold hover:bg-surface-hover"
      >
        <Chevron size={13} aria-hidden className="text-muted" />
        <span className="min-w-0 truncate">{c.name}</span>
        {c.init && (
          <span className={clsx(chip, 'bg-surface-hover font-normal text-muted')}>init</span>
        )}
        <span className="ml-auto shrink-0 text-[11px] font-normal text-muted">
          {c.env.length + c.envFrom.length} vars · {c.mounts.length} mounts
        </span>
      </button>
      {shown && (
        <>
          {(envs.length > 0 || froms.length > 0) && (
            <div className="divide-y divide-border border-t border-border">
              {envs.map((e) => (
                <EnvRow key={e.name} e={e} clusterId={clusterId} ns={ns} />
              ))}
              {froms.map((f, i) => (
                <EnvFromRow key={`${f.kind}/${f.name}/${i}`} f={f} />
              ))}
            </div>
          )}
          {mnts.length > 0 && (
            <div className="border-t border-border">
              <div className="px-3 pb-0.5 pt-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted">
                Mounts
              </div>
              <div className="divide-y divide-border/60">
                {mnts.map((m) => {
                  const vol = volumes.find((v) => v.name === m.volume)
                  return (
                    <div
                      key={`${m.volume}:${m.path}`}
                      className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-1.5 text-xs"
                    >
                      <span className="min-w-0 break-all font-mono font-semibold">
                        {m.path}
                        {m.subPath && (
                          <span className="font-normal text-muted"> ({m.subPath})</span>
                        )}
                      </span>
                      <span className="text-muted">
                        ←{' '}
                        {vol && (vol.kind === 'secret' || vol.kind === 'configmap') ? (
                          <>
                            {vol.kind} <RefLink kind={vol.kind} name={vol.ref} />
                          </>
                        ) : (
                          <>
                            {vol ? `${vol.kind === 'pvc' ? 'claim' : vol.kind} ` : ''}
                            <span className="break-all font-mono text-fg">
                              {vol?.ref || m.volume}
                            </span>
                          </>
                        )}
                      </span>
                      <span
                        className={clsx(
                          chip,
                          m.readOnly ? 'bg-surface-hover text-muted' : 'bg-warning/15 text-warning',
                        )}
                      >
                        {m.readOnly ? 'read-only' : 'read-write'}
                      </span>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

const SOURCE_LABEL: Record<EnvVar['source'], string> = {
  literal: '',
  secret: 'secret',
  configmap: 'configmap',
  field: 'pod field',
  resource: 'resource',
}

function EnvRow({ e, clusterId, ns }: { e: EnvVar; clusterId: string; ns: string }) {
  const refKind = e.source === 'secret' || e.source === 'configmap' ? e.source : null
  return (
    <div className="grid grid-cols-[minmax(90px,2fr)_minmax(0,3fr)] items-start gap-3 px-3 py-1.5 text-xs">
      <div className="min-w-0">
        <div className="break-all font-mono font-semibold">{e.name}</div>
        {SOURCE_LABEL[e.source] && (
          <div className="mt-0.5 text-[10px] text-muted">{SOURCE_LABEL[e.source]}</div>
        )}
      </div>
      <div className="min-w-0">
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
          <span className="break-all font-mono text-muted">{e.detail}</span>
        )}
      </div>
    </div>
  )
}

function EnvFromRow({ f }: { f: EnvFromRef }) {
  return (
    <div className="grid grid-cols-[minmax(90px,2fr)_minmax(0,3fr)] items-start gap-3 px-3 py-1.5 text-xs">
      <div className="text-muted">
        All keys
        {f.prefix && (
          <>
            {' '}
            with prefix <span className="font-mono">{f.prefix}</span>
          </>
        )}
      </div>
      <div className="min-w-0">
        <span className="mr-1.5 text-muted">{f.kind}</span>
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

/**
 * The value behind a Secret or ConfigMap key. A ConfigMap value is fetched as soon as the row is shown; a Secret value
 * only on request, and hidden again after 30 seconds.
 */
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
  const secret = kind === 'secret'

  const hide = useCallback(() => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = null
    setValue(null)
  }, [])
  // never leave a revealed value behind when the panel goes away
  useEffect(() => hide, [hide])

  const show = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      setValue(await kube.refValue(clusterId, ns, kind, name, keyName))
      if (secret) timer.current = setTimeout(hide, REVEAL_MS)
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }, [clusterId, ns, kind, name, keyName, secret, hide])

  // ConfigMap values load by themselves; deferred so it is not a synchronous state cascade
  useEffect(() => {
    if (secret) return
    const t = setTimeout(() => void show(), 0)
    return () => clearTimeout(t)
  }, [secret, show])

  const bulk = useContext(BulkContext)
  useEffect(() => {
    if (!secret || bulk.seq === 0) return
    const t = setTimeout(() => (bulk.action === 'show' ? void show() : hide()), 0)
    return () => clearTimeout(t)
  }, [secret, bulk, show, hide])

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
      {value !== null ? (
        <pre className="max-h-40 select-text overflow-auto whitespace-pre-wrap break-all font-mono text-xs">
          {value || <span className="text-muted">(empty)</span>}
        </pre>
      ) : secret ? (
        <span className="font-mono tracking-widest text-muted" aria-label="hidden secret value">
          ••••••••
        </span>
      ) : (
        !error && <span className="text-muted">{busy ? 'Loading…' : ''}</span>
      )}
      <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted">
        <RefLink kind={kind} name={name} />
        <span className="font-mono">/ {keyName}</span>
        {optional && <span>optional</span>}
        {secret && value === null && (
          <button onClick={() => void show()} disabled={busy} className={smallBtn}>
            <Eye size={11} aria-hidden /> {busy ? 'Loading…' : 'Show'}
          </button>
        )}
        {value !== null && secret && (
          <button onClick={hide} className={smallBtn}>
            <EyeOff size={11} aria-hidden /> Hide
          </button>
        )}
        {value !== null && (
          <button onClick={() => void copy()} className={smallBtn}>
            {copied ? <Check size={11} aria-hidden /> : <Copy size={11} aria-hidden />}{' '}
            {copied ? 'Copied' : 'Copy'}
          </button>
        )}
      </div>
      {error && <div className="mt-1 text-[11px] text-danger">{error}</div>}
    </div>
  )
}

function Volumes({ volumes }: { volumes: PodVolume[] }) {
  const [open, setOpen] = useState(false)
  const Chevron = open ? ChevronDown : ChevronRight
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <button
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="flex w-full cursor-pointer items-center gap-2 bg-surface-hover/40 px-3 py-1.5 text-left text-xs font-semibold hover:bg-surface-hover"
      >
        <Chevron size={13} aria-hidden className="text-muted" />
        Volumes
        <span className="ml-auto text-[11px] font-normal text-muted">{volumes.length}</span>
      </button>
      {open && (
        <div className="divide-y divide-border border-t border-border">
          {volumes.map((v) => (
            <div
              key={v.name}
              className="grid grid-cols-[minmax(90px,2fr)_minmax(0,3fr)] items-start gap-3 px-3 py-1.5 text-xs"
            >
              <span className="break-all font-mono font-semibold">{v.name}</span>
              <div className="min-w-0 space-y-0.5">
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
      )}
    </div>
  )
}
