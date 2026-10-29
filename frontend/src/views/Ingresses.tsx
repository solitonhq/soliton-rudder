import clsx from 'clsx'
import {
  AlertTriangle,
  ArrowRight,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  Globe,
  Lock,
  LockOpen,
  Network,
  Search,
  XCircle,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { NamespacePicker } from '../components/NamespacePicker'
import { IngressDetails, certTone, isIP, openLink, ruleURL } from '../components/IngressDetails'
import { insight } from '../lib/backend'
import { formatAge } from '../lib/format'
import type { IngressInfo, IngressPath } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'

function PathRow({
  p,
  host,
  secure,
  fallback,
  onService,
}: {
  p: IngressPath
  host: string
  secure: boolean
  fallback: string
  onService: (name: string) => void
}) {
  const bad = !!p.issue
  const url = ruleURL(host, secure, p.path, fallback)
  return (
    <div className="flex items-center gap-3 py-1.5 text-[13px]">
      <div className="flex w-56 shrink-0 items-center gap-1.5">
        {url ? (
          <button
            onClick={() => openLink(url)}
            title={`Open ${url} in the browser`}
            className="min-w-0 cursor-pointer truncate rounded bg-surface-2 px-1.5 py-0.5 text-left font-mono text-xs hover:bg-accent/15 hover:text-accent"
          >
            {p.path}
          </button>
        ) : (
          <code
            className="truncate rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs"
            title={`${p.path} (${p.pathType})`}
          >
            {p.path}
          </code>
        )}
        {p.pathType && (
          <span className="text-[10px] uppercase text-muted">
            {p.pathType === 'ImplementationSpecific' ? 'impl' : p.pathType}
          </span>
        )}
        {url && (
          <button
            onClick={() => openLink(url)}
            className="cursor-pointer text-muted hover:text-accent"
            title={`Open ${url}`}
            aria-label={`Open ${url}`}
          >
            <ExternalLink size={12} />
          </button>
        )}
      </div>
      <ArrowRight size={14} className="shrink-0 text-muted" />
      {p.resource ? (
        <span className="rounded-md border border-border px-2 py-1 font-mono text-xs">
          {p.resource}
        </span>
      ) : (
        <button
          onClick={() => onService(p.service)}
          className={clsx(
            'min-w-0 cursor-pointer truncate rounded-md border px-2 py-1 text-left font-mono text-xs hover:border-accent',
            p.found ? 'border-border' : 'border-danger/60 text-danger',
          )}
          title="Show service"
        >
          {p.service}
          <span className="text-muted">:{p.port}</span>
        </button>
      )}
      {!p.resource && p.found && (
        <>
          <ArrowRight size={14} className="shrink-0 text-muted" />
          <div className="flex min-w-0 items-center gap-2">
            <div
              className="flex flex-wrap gap-1"
              title={p.pods
                .map(
                  (x) =>
                    `${x.name}${x.ip && x.ip !== x.name ? ` (${x.ip})` : ''} ${x.ready ? 'ready' : 'not ready'}`,
                )
                .join('\n')}
            >
              {p.pods.slice(0, 12).map((x, i) => (
                <span
                  key={i}
                  className={clsx('size-2.5 rounded-full', x.ready ? 'bg-success' : 'bg-danger')}
                />
              ))}
              {p.pods.length > 12 && (
                <span className="text-xs text-muted">+{p.pods.length - 12}</span>
              )}
            </div>
            <span
              className={clsx(
                'text-xs',
                p.total > 0 && p.ready === p.total
                  ? 'text-muted'
                  : bad
                    ? 'text-danger'
                    : 'text-warning',
              )}
            >
              {p.total > 0 ? `${p.ready}/${p.total} ready` : 'no endpoints'}
            </span>
          </div>
        </>
      )}
      {bad && (
        <span className="ml-auto truncate text-xs text-danger" title={p.issue}>
          {p.issue}
        </span>
      )}
    </div>
  )
}

function IngressCard({ ing, onService }: { ing: IngressInfo; onService: (name: string) => void }) {
  const [open, setOpen] = useState(false)
  const errors = ing.issues.filter((i) => i.severity === 'error').length
  const warns = ing.issues.filter((i) => i.severity === 'warn').length
  const fallback = ing.addresses[0] ?? ''
  const toggle = () => setOpen((o) => !o)
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-surface">
      <div
        role="button"
        tabIndex={0}
        aria-expanded={open}
        aria-label={`${open ? 'Hide' : 'Show'} details of ${ing.name}`}
        onClick={toggle}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            toggle()
          }
        }}
        className="flex cursor-pointer flex-wrap items-center gap-x-3 gap-y-1 border-b border-border px-4 py-2.5 hover:bg-surface-2"
      >
        {open ? (
          <ChevronDown size={14} className="text-muted" />
        ) : (
          <ChevronRight size={14} className="text-muted" />
        )}
        <span className="text-sm font-semibold">{ing.name}</span>
        <span className="text-xs text-muted">{ing.namespace}</span>
        {ing.class && (
          <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[11px] font-medium">
            {ing.class}
          </span>
        )}
        {ing.addresses.map((a) => (
          <span
            key={a}
            className="flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 font-mono text-[11px] text-muted"
          >
            {isIP(a) ? <Network size={10} /> : <Globe size={10} />}
            {a}
          </span>
        ))}
        <div className="ml-auto flex items-center gap-2">
          {ing.tls.map((t, i) => (
            <span
              key={i}
              className={clsx(
                'flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium',
                certTone(t),
              )}
              title={t.error || `${t.secretName} · ${t.issuer} · expires ${t.notAfter}`}
            >
              <Lock size={11} />{' '}
              {t.error ? 'TLS error' : t.daysLeft < 0 ? 'expired' : `${t.daysLeft}d`}
            </span>
          ))}
          {errors > 0 && (
            <span className="flex items-center gap-1 text-xs font-medium text-danger">
              <XCircle size={13} /> {errors}
            </span>
          )}
          {warns > 0 && (
            <span className="flex items-center gap-1 text-xs font-medium text-warning">
              <AlertTriangle size={13} /> {warns}
            </span>
          )}
          <span className="text-xs text-muted">{formatAge(ing.created)}</span>
        </div>
      </div>
      {open && <IngressDetails ing={ing} />}
      {ing.issues.length > 0 && (
        <div className="space-y-1 border-b border-border bg-bg px-4 py-2">
          {ing.issues.map((i, k) => (
            <div
              key={k}
              className={clsx(
                'flex gap-2 text-xs',
                i.severity === 'error'
                  ? 'text-danger'
                  : i.severity === 'warn'
                    ? 'text-warning'
                    : 'text-muted',
              )}
            >
              {i.severity === 'error' ? (
                <XCircle size={13} className="mt-px shrink-0" />
              ) : (
                <AlertTriangle size={13} className="mt-px shrink-0" />
              )}
              <span>{i.message}</span>
            </div>
          ))}
        </div>
      )}
      <div className="px-4 py-2">
        {ing.rules.map((r, i) => (
          <div key={i} className="py-1">
            <div className="flex items-center gap-1.5 text-[13px] font-medium">
              {r.secure ? (
                <Lock size={12} className="text-success" />
              ) : (
                <LockOpen size={12} className="text-muted" />
              )}
              {r.host ? (
                ruleURL(r.host, r.secure, '/') ? (
                  <button
                    onClick={() => openLink(ruleURL(r.host, r.secure, '/'))}
                    title={`Open ${ruleURL(r.host, r.secure, '/')} in the browser`}
                    className="min-w-0 cursor-pointer truncate text-left hover:text-accent hover:underline"
                  >
                    {r.host}
                  </button>
                ) : (
                  <span title="A wildcard host has no single address to open">{r.host}</span>
                )
              ) : (
                <span className="text-muted">any host</span>
              )}
            </div>
            <div className="ml-4 divide-y divide-border/40">
              {r.paths.map((p, j) => (
                <PathRow
                  key={j}
                  p={p}
                  host={r.host}
                  secure={r.secure}
                  fallback={fallback}
                  onService={onService}
                />
              ))}
            </div>
          </div>
        ))}
        {ing.rules.length === 0 && <div className="py-2 text-xs text-muted">No rules</div>}
      </div>
    </div>
  )
}

export function Ingresses({ clusterId }: { clusterId: string }) {
  const namespaces = useExplorer((s) => s.namespaces)
  const jump = useExplorer((s) => s.jump)
  const [q, setQ] = useState('')
  const [issuesOnly, setIssuesOnly] = useState(false)
  const { data, error, loading } = usePoll(
    () => insight.ingressMap(clusterId, ''),
    clusterId,
    15_000,
  )

  const list = useMemo(() => {
    const ql = q.trim().toLowerCase()
    return (data?.ingresses ?? []).filter((i) => {
      if (namespaces.length && !namespaces.includes(i.namespace)) return false
      if (issuesOnly && !i.issues.some((x) => x.severity !== 'info')) return false
      if (!ql) return true
      return (
        i.name.toLowerCase().includes(ql) ||
        i.rules.some(
          (r) =>
            r.host.toLowerCase().includes(ql) ||
            r.paths.some(
              (p) => p.service.toLowerCase().includes(ql) || p.path.toLowerCase().includes(ql),
            ),
        )
      )
    })
  }, [data, namespaces, q, issuesOnly])

  const totals = useMemo(() => {
    let e = 0
    let w = 0
    for (const i of list) {
      e += i.issues.filter((x) => x.severity === 'error').length
      w += i.issues.filter((x) => x.severity === 'warn').length
    }
    return { e, w }
  }, [list])

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
        <h1 className="text-sm font-semibold">Ingress Map</h1>
        <span className="font-mono text-xs text-muted">{list.length}</span>
        {totals.e > 0 && (
          <span className="flex items-center gap-1 text-xs font-medium text-danger">
            <XCircle size={13} /> {totals.e}
          </span>
        )}
        {totals.w > 0 && (
          <span className="flex items-center gap-1 text-xs font-medium text-warning">
            <AlertTriangle size={13} /> {totals.w}
          </span>
        )}
        <div className="flex-1" />
        <NamespacePicker />
        <div className="relative">
          <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Host, path, service…"
            className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
          />
        </div>
        <button
          onClick={() => setIssuesOnly(!issuesOnly)}
          className={clsx(
            'no-drag h-8 cursor-pointer rounded-md border px-3 text-[13px] font-medium',
            issuesOnly
              ? 'border-accent bg-accent text-accent-fg'
              : 'border-border bg-surface-2 hover:bg-surface-hover',
          )}
        >
          Issues only
        </button>
      </div>
      <div className="min-h-0 flex-1 space-y-3 overflow-auto p-4">
        {error && !data && <div className="text-[13px] text-danger">{error}</div>}
        {loading && <div className="text-[13px] text-muted">Analyzing ingresses…</div>}
        {!loading && !error && list.length === 0 && (
          <div className="mt-16 text-center text-[13px] text-muted">
            {data?.ingresses.length
              ? 'No ingresses match the filters.'
              : 'No ingresses in this cluster.'}
          </div>
        )}
        {list.map((i) => (
          <IngressCard
            key={i.namespace + '/' + i.name}
            ing={i}
            onService={(name) => void jump('core/v1/services', name)}
          />
        ))}
      </div>
    </div>
  )
}
