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
import type { ContainerEnv, EnvFromRef, EnvVar, PodEnv, PodVolume, RefKey } from '../lib/types'
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
  const hasSecrets =
    containers.some(
      (c) => c.env.some((e) => e.source === 'secret') || c.envFrom.some((f) => f.kind === 'secret'),
    ) || volumes.some((v) => v.kind === 'secret')
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
            {volumes.length > 0 && !q && (
              <Volumes volumes={volumes} containers={containers} clusterId={clusterId} ns={ns} />
            )}
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
                <EnvFromRow key={`${f.kind}/${f.name}/${i}`} f={f} clusterId={clusterId} ns={ns} />
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

function EnvFromRow({ f, clusterId, ns }: { f: EnvFromRef; clusterId: string; ns: string }) {
  return (
    <RefKeysBlock
      clusterId={clusterId}
      ns={ns}
      kind={f.kind}
      name={f.name}
      prefix={f.prefix}
      optional={f.optional}
      noun="All keys"
    />
  )
}

/**
 * Everything a Secret or ConfigMap brings into the pod (envFrom, or files of a volume), key by key. The key list is
 * fetched when the block is opened; Secret values stay masked behind an eye, ConfigMap values are shown.
 */
function RefKeysBlock({
  clusterId,
  ns,
  kind,
  name,
  prefix = '',
  optional = false,
  noun,
}: {
  clusterId: string
  ns: string
  kind: 'secret' | 'configmap'
  name: string
  prefix?: string
  optional?: boolean
  noun: string
}) {
  const [open, setOpen] = useState(false)
  const [keys, setKeys] = useState<RefKey[] | null>(null)
  const [error, setError] = useState('')
  const loaded = useRef(false)
  const Chevron = open ? ChevronDown : ChevronRight

  const load = useCallback(async () => {
    loaded.current = true
    try {
      setKeys(await kube.refKeys(clusterId, ns, kind, name))
      setError('')
    } catch (e) {
      setError(errText(e))
    }
  }, [clusterId, ns, kind, name])

  const toggle = () => {
    const next = !open
    setOpen(next)
    if (next && !loaded.current) void load()
  }

  // "Show secrets" in the section header opens the Secret key lists too
  const bulk = useContext(BulkContext)
  useEffect(() => {
    if (kind !== 'secret' || bulk.seq === 0 || bulk.action !== 'show') return
    const t = setTimeout(() => {
      setOpen(true)
      if (!loaded.current) void load()
    }, 0)
    return () => clearTimeout(t)
  }, [kind, bulk, load])

  return (
    <div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-1.5 text-xs">
        <button
          onClick={toggle}
          aria-expanded={open}
          aria-label={`${open ? 'Collapse' : 'Expand'} ${kind} ${name}`}
          className="inline-flex cursor-pointer items-center gap-1 text-muted hover:text-fg"
        >
          <Chevron size={13} aria-hidden />
          {noun}
          {prefix && (
            <>
              {' '}
              with prefix <span className="font-mono">{prefix}</span>
            </>
          )}
        </button>
        <span className="text-muted">{kind}</span>
        <RefLink kind={kind} name={name} />
        {optional && <span className="text-muted">optional</span>}
        {keys && (
          <span className="text-muted">
            · {keys.length} key{keys.length === 1 ? '' : 's'}
          </span>
        )}
      </div>
      {open && (
        <div className="border-t border-border/60 bg-bg/40">
          {error ? (
            <div className="px-3 py-1.5 text-[11px] text-danger">{error}</div>
          ) : keys === null ? (
            <div className="px-3 py-1.5 text-[11px] text-muted">Loading…</div>
          ) : keys.length === 0 ? (
            <div className="px-3 py-1.5 text-[11px] text-muted">No keys.</div>
          ) : (
            <div className="divide-y divide-border/50">
              {keys.map((k) => (
                <div
                  key={k.name}
                  className="grid grid-cols-[minmax(90px,2fr)_minmax(0,3fr)] items-start gap-3 py-1.5 pl-8 pr-3 text-xs"
                >
                  <div className="min-w-0">
                    <div className="break-all font-mono font-semibold">{prefix + k.name}</div>
                    <div className="mt-0.5 text-[10px] text-muted">{formatBytes(k.size)}</div>
                  </div>
                  <div className="min-w-0">
                    {k.binary ? (
                      <span className="text-muted">binary data, not shown</span>
                    ) : (
                      <RefValue
                        bare
                        kind={kind}
                        clusterId={clusterId}
                        ns={ns}
                        name={name}
                        keyName={k.name}
                        optional={false}
                        auto={kind === 'configmap' && k.size <= 4096}
                      />
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

const formatBytes = (n: number) =>
  n < 1024
    ? `${n} B`
    : n < 1024 * 1024
      ? `${(n / 1024).toFixed(1)} KiB`
      : `${(n / 1048576).toFixed(1)} MiB`

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
  bare = false,
  auto,
}: {
  kind: 'secret' | 'configmap'
  clusterId: string
  ns: string
  name: string
  keyName: string
  optional: boolean
  /** Only the value and an eye, for key lists where the Secret and key are already named by the row. */
  bare?: boolean
  /** Fetch the value as soon as it is shown. Defaults to true for a ConfigMap and false for a Secret. */
  auto?: boolean
}) {
  const toast = useUI((s) => s.toast)
  const [value, setValue] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const secret = kind === 'secret'
  const autoLoad = auto ?? !secret

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
    if (!autoLoad) return
    const t = setTimeout(() => void show(), 0)
    return () => clearTimeout(t)
  }, [autoLoad, show])

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

  if (bare) {
    const iconBtn =
      'inline-flex size-6 shrink-0 cursor-pointer items-center justify-center rounded border border-border text-muted hover:border-accent hover:text-accent disabled:opacity-50'
    return (
      <div>
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            {value !== null ? (
              <pre className="max-h-40 select-text overflow-auto whitespace-pre-wrap break-all font-mono text-xs">
                {value || <span className="text-muted">(empty)</span>}
              </pre>
            ) : secret ? (
              <span
                className="font-mono tracking-widest text-muted"
                aria-label="hidden secret value"
              >
                ••••••••
              </span>
            ) : (
              <span className="text-muted">{busy ? 'Loading…' : '—'}</span>
            )}
          </div>
          {value !== null ? (
            <>
              <button
                onClick={() => void copy()}
                className={iconBtn}
                title="Copy the value"
                aria-label="Copy value"
              >
                {copied ? <Check size={12} aria-hidden /> : <Copy size={12} aria-hidden />}
              </button>
              {secret && (
                <button onClick={hide} className={iconBtn} title="Hide" aria-label="Hide value">
                  <EyeOff size={12} aria-hidden />
                </button>
              )}
            </>
          ) : (
            <button
              onClick={() => void show()}
              disabled={busy}
              className={iconBtn}
              title="Show the value"
              aria-label="Show value"
            >
              <Eye size={12} aria-hidden />
            </button>
          )}
        </div>
        {error && <div className="mt-1 text-[11px] text-danger">{error}</div>}
      </div>
    )
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

function Volumes({
  volumes,
  containers,
  clusterId,
  ns,
}: {
  volumes: PodVolume[]
  containers: ContainerEnv[]
  clusterId: string
  ns: string
}) {
  const [open, setOpen] = useState(true)
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
          {volumes.map((v) => {
            const users = containers.flatMap((c) =>
              c.mounts
                .filter((m) => m.volume === v.name)
                .map((m) => ({ container: c.name, init: c.init, ...m })),
            )
            return (
              <div key={v.name} className="text-xs">
                <div className="grid grid-cols-[minmax(90px,2fr)_minmax(0,3fr)] items-start gap-3 px-3 py-1.5">
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
                    {users.length === 0 ? (
                      <div className="text-[11px] text-muted">not mounted by any container</div>
                    ) : (
                      users.map((u) => (
                        <div
                          key={`${u.container}:${u.path}`}
                          className="flex flex-wrap items-baseline gap-x-1.5 text-[11px]"
                        >
                          <span className="text-muted">mounted in</span>
                          <span className="font-semibold">{u.container}</span>
                          {u.init && (
                            <span className={clsx(chip, 'bg-surface-hover text-muted')}>init</span>
                          )}
                          <span className="text-muted">at</span>
                          <span className="break-all font-mono">
                            {u.path}
                            {u.subPath && <span className="text-muted"> ({u.subPath})</span>}
                          </span>
                          <span className="text-muted">
                            {u.readOnly ? 'read-only' : 'read-write'}
                          </span>
                        </div>
                      ))
                    )}
                  </div>
                </div>
                {(v.kind === 'secret' || v.kind === 'configmap') && v.ref && (
                  <div className="border-t border-border/60">
                    <RefKeysBlock
                      clusterId={clusterId}
                      ns={ns}
                      kind={v.kind}
                      name={v.ref}
                      noun="Files"
                    />
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
