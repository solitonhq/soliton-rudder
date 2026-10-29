import clsx from 'clsx'
import { Check, Copy, ExternalLink, Globe, Lock, LockOpen, Network } from 'lucide-react'
import { Fragment, useState, type ReactNode } from 'react'
import { openURL } from '../lib/backend'
import type { IngressInfo, IngressTLS } from '../lib/types'
import { useUI } from '../store/ui'

export function certTone(t: IngressTLS): string {
  if (t.error) return 'bg-danger/15 text-danger'
  if (t.daysLeft < 14) return 'bg-danger/15 text-danger'
  if (t.daysLeft < 30) return 'bg-warning/15 text-warning'
  return 'bg-success/15 text-success'
}

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/
const IPV6 = /^[0-9a-f:]+$/i
const HOSTNAME = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$/

export const isIP = (s: string) => IPV4.test(s) || (s.includes(':') && IPV6.test(s))

/**
 * The part of an Ingress path that can be opened. nginx-style regex paths such as `/api(/|$)(.*)` are cut at the first
 * regular-expression character (`/api`), and `(default)` has no path at all.
 */
export function literalPath(path: string): string {
  if (!path.startsWith('/')) return ''
  const cut = path.search(/[()[\]{}*+?^$|\\]/)
  let lit = cut < 0 ? path : path.slice(0, cut)
  if (cut >= 0) lit = lit.replace(/\.+$/, '') // `/api.*` leaves a dangling dot
  return encodeURI(lit || '/')
}

/**
 * The address to open for a rule: its host, or the load balancer address when the rule has no host.
 * Empty when there is nothing safe to open (wildcard hosts such as `*.example.com`, odd characters).
 */
export function ruleURL(host: string, secure: boolean, path: string, fallback = ''): string {
  const target = host || fallback
  if (!target) return ''
  const ip = isIP(target)
  if (!ip && !HOSTNAME.test(target)) return ''
  const hostPart = ip && target.includes(':') ? `[${target}]` : target
  return `${secure ? 'https' : 'http'}://${hostPart}${literalPath(path)}`
}

// Wails refuses (silently!) URLs with shell metacharacters, so check first and tell the user instead.
const UNSAFE = /[;|`$\\<>*{}[\]()~!\s]/

export function canOpen(url: string): boolean {
  try {
    const u = new URL(url)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== '' && !UNSAFE.test(url)
  } catch {
    return false
  }
}

/** Opens an address in the default browser; explains instead of failing silently when it cannot be opened. */
export function openLink(url: string) {
  if (!canOpen(url)) {
    useUI.getState().toast('error', `Cannot open “${url}”: the address is not a valid web address.`)
    return
  }
  openURL(url)
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false)
  return (
    <button
      aria-label={`Copy ${label}`}
      title={done ? 'Copied' : `Copy ${label}`}
      onClick={() => {
        void Promise.resolve(navigator.clipboard?.writeText(text)).catch(() => {})
        setDone(true)
        setTimeout(() => setDone(false), 1200)
      }}
      className="grid size-6 shrink-0 cursor-pointer place-items-center rounded text-muted hover:bg-surface-hover hover:text-fg"
    >
      {done ? <Check size={12} className="text-success" /> : <Copy size={12} />}
    </button>
  )
}

function UrlLink({ url, children }: { url: string; children: ReactNode }) {
  return (
    <button
      onClick={() => openLink(url)}
      title={`Open ${url} in the browser`}
      className="min-w-0 cursor-pointer truncate text-left hover:text-accent hover:underline"
    >
      {children}
    </button>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h4 className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted">
        {title}
      </h4>
      {children}
    </section>
  )
}

/** Everything about where traffic enters and where it goes: addresses, URLs, Service and pod IPs, TLS. */
export function IngressDetails({ ing, compact = false }: { ing: IngressInfo; compact?: boolean }) {
  const fallback = ing.addresses[0] ?? ''
  const urls: { url: string; secure: boolean }[] = []
  for (const r of ing.rules)
    for (const p of r.paths) {
      const url = ruleURL(r.host, r.secure, p.path, fallback)
      if (url && !urls.some((u) => u.url === url)) urls.push({ url, secure: r.secure })
    }
  const backends = ing.rules.flatMap((r) =>
    r.paths.filter((p) => !p.resource).map((p) => ({ host: r.host, p })),
  )
  return (
    <div
      className={clsx(
        'grid gap-5 bg-bg text-[13px]',
        compact ? 'px-3 py-3' : 'border-t border-border px-4 py-3',
      )}
    >
      <Section title="URLs">
        {urls.length === 0 ? (
          <p className="text-xs text-muted">No routable URLs.</p>
        ) : (
          <ul className="space-y-1">
            {urls.map(({ url, secure }) => (
              <li key={url} className="flex items-center gap-1.5">
                {secure ? (
                  <Lock size={12} className="shrink-0 text-success" />
                ) : (
                  <LockOpen size={12} className="shrink-0 text-muted" />
                )}
                <span className="min-w-0 flex-1 truncate font-mono text-xs">
                  <UrlLink url={url}>{url}</UrlLink>
                </span>
                <CopyButton text={url} label={url} />
              </li>
            ))}
          </ul>
        )}
      </Section>

      {backends.length > 0 && (
        <div>
          <Section title="Backends">
            <div className="overflow-hidden rounded-lg border border-border">
              <table className="w-full text-xs">
                <thead className="bg-surface text-left text-[10px] uppercase text-muted">
                  <tr>
                    <th className="px-2.5 py-1.5">Route</th>
                    <th className="px-2.5 py-1.5">Service</th>
                    <th className="px-2.5 py-1.5">Service IP</th>
                    <th className="px-2.5 py-1.5">Pod IPs</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {backends.map(({ host, p }, i) => (
                    <tr key={i} className="align-top">
                      <td className="select-text px-2.5 py-1.5 font-mono">
                        {host || '*'}
                        {p.path}
                      </td>
                      <td className="select-text px-2.5 py-1.5 font-mono">
                        {p.service}
                        <span className="text-muted">:{p.port}</span>
                        {p.serviceType && p.serviceType !== 'ClusterIP' && (
                          <span className="ml-1.5 rounded bg-surface-2 px-1 py-px font-sans text-[9px] font-semibold uppercase text-muted">
                            {p.serviceType}
                          </span>
                        )}
                      </td>
                      <td className="select-text px-2.5 py-1.5 font-mono">
                        {p.clusterIP && p.clusterIP !== 'None' ? (
                          p.clusterIP
                        ) : (
                          <span className="text-muted">
                            {p.clusterIP === 'None' ? 'headless' : '—'}
                          </span>
                        )}
                      </td>
                      <td className="px-2.5 py-1.5">
                        {p.pods.length === 0 ? (
                          <span className="text-danger">
                            {p.found ? 'no endpoints' : 'service missing'}
                          </span>
                        ) : (
                          <div className="flex flex-wrap gap-1">
                            {p.pods.map((x, k) => (
                              <span
                                key={k}
                                title={`${x.name} · ${x.ready ? 'ready' : 'not ready'}`}
                                className={clsx(
                                  'flex select-text items-center gap-1 rounded-full border px-2 py-0.5 font-mono',
                                  x.ready ? 'border-success/40' : 'border-danger/50 text-danger',
                                )}
                              >
                                <span
                                  className={clsx(
                                    'size-1.5 rounded-full',
                                    x.ready ? 'bg-success' : 'bg-danger',
                                  )}
                                />
                                {x.ip || x.name}
                              </span>
                            ))}
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Section>
        </div>
      )}

      {ing.tls.length > 0 && (
        <div>
          <Section title="TLS">
            <ul className="space-y-1 text-xs">
              {ing.tls.map((t, i) => (
                <li key={i} className="flex flex-wrap items-center gap-x-3 gap-y-0.5">
                  <span
                    className={clsx(
                      'rounded-full px-2 py-0.5 text-[11px] font-medium',
                      certTone(t),
                    )}
                  >
                    {t.error ? 'error' : t.daysLeft < 0 ? 'expired' : `${t.daysLeft} days left`}
                  </span>
                  <span className="font-mono">{t.secretName || '(controller default)'}</span>
                  {t.issuer && <span className="text-muted">issued by {t.issuer}</span>}
                  {t.hosts.length > 0 && <span className="text-muted">{t.hosts.join(', ')}</span>}
                  {t.error && <span className="text-danger">{t.error}</span>}
                </li>
              ))}
            </ul>
          </Section>
        </div>
      )}
      <Section title="Addresses">
        {ing.addresses.length === 0 ? (
          <p className="text-xs text-muted">
            No address assigned yet. The ingress controller has not reported one.
          </p>
        ) : (
          <ul className="space-y-1.5">
            {ing.addresses.map((a) => {
              const ip = isIP(a)
              return (
                <li
                  key={a}
                  className="flex items-center gap-2 rounded-lg border border-border bg-surface px-2.5 py-1.5"
                >
                  {ip ? (
                    <Network size={14} className="shrink-0 text-accent" />
                  ) : (
                    <Globe size={14} className="shrink-0 text-accent" />
                  )}
                  <span className="min-w-0 flex-1 select-text truncate font-mono text-xs">{a}</span>
                  <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-muted">
                    {ip ? 'IP' : 'Hostname'}
                  </span>
                  <CopyButton text={a} label={a} />
                  <button
                    aria-label={`Open ${a}`}
                    title={`Open http://${a}`}
                    onClick={() => openLink(`http://${a}`)}
                    className="grid size-6 shrink-0 cursor-pointer place-items-center rounded text-muted hover:bg-surface-hover hover:text-accent"
                  >
                    <ExternalLink size={12} />
                  </button>
                </li>
              )
            })}
          </ul>
        )}
        {ing.class && (
          <p className="mt-2 text-xs text-muted">
            Served by the <span className="font-mono">{ing.class}</span> controller.
          </p>
        )}
      </Section>
    </div>
  )
}

/**
 * The Hosts / Address cell of the Ingresses table: every entry opens in the browser. `secure` picks https (the table
 * only knows the ports, so the caller passes whether 443 is among them). Entries with no single address (wildcard
 * hosts, `<none>`) stay plain text.
 */
export function IngressCell({ text, secure }: { text: string; secure: boolean }) {
  const items = text.split(/[,\s]+/).filter(Boolean)
  return (
    <span>
      {items.map((item, i) => {
        const url = ruleURL(item, secure, '/')
        return (
          <Fragment key={i}>
            {i > 0 && ', '}
            {url ? (
              <button
                onClick={(e) => {
                  e.stopPropagation() // do not also select the row
                  openLink(url)
                }}
                title={`Open ${url} in the browser`}
                className="cursor-pointer hover:text-accent hover:underline"
              >
                {item}
              </button>
            ) : (
              <span>{item}</span>
            )}
          </Fragment>
        )
      })}
    </span>
  )
}
