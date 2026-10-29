import clsx from 'clsx'
import { Copy, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  certTone,
  issuerLabel,
  remaining,
  toneBg,
  toneChip,
  toneText,
} from '../components/CertCard'
import { Drawer, EmptyState, KV, Pill, ViewHeader } from '../components/GitOps'
import { NamespacePicker } from '../components/NamespacePicker'
import { certs } from '../lib/backend'
import type { CertItem } from '../lib/types'
import { usePoll } from '../lib/usePoll'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'

type Filter = 'all' | 'expired' | 'soon' | 'errors'

const keyOf = (c: CertItem) => c.namespace + '/' + c.name
const bucket = (c: CertItem): Filter =>
  c.error ? 'errors' : c.expired || c.daysLeft < 30 ? 'soon' : 'all'

export function Certificates({ clusterId }: { clusterId: string }) {
  const namespaces = useExplorer((s) => s.namespaces)
  // one namespace → ask the API for just that namespace; otherwise fetch all and filter here
  const scope = namespaces.length === 1 ? namespaces[0] : ''
  const { data, error, loading } = usePoll(
    () => certs.list(clusterId, scope),
    `${clusterId}/${scope}`,
    60_000,
  )
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [sel, setSel] = useState<string | null>(null)

  const items = useMemo(() => {
    const ql = q.trim().toLowerCase()
    return (data?.items ?? []).filter((c) => {
      if (namespaces.length > 1 && !namespaces.includes(c.namespace)) return false
      if (filter === 'expired' && !c.expired) return false
      if (filter === 'soon' && bucket(c) !== 'soon') return false
      if (filter === 'errors' && !c.error) return false
      return (
        !ql ||
        `${c.name} ${c.namespace} ${c.subject} ${c.issuer} ${c.sans.join(' ')} ${c.usedBy.join(' ')}`
          .toLowerCase()
          .includes(ql)
      )
    })
  }, [data, namespaces, q, filter])

  const counts = data?.counts
  const chips: [Filter, string, number | undefined, string][] = [
    ['all', 'All', data?.items.length, ''],
    ['expired', 'Expired', counts?.expired, 'text-danger'],
    ['soon', 'Expiring < 30d', counts ? counts.soon14 + counts.soon30 : undefined, 'text-warning'],
    ['errors', 'Unreadable', counts?.errors, 'text-danger'],
  ]
  const selected = items.find((c) => keyOf(c) === sel) ?? data?.items.find((c) => keyOf(c) === sel)

  return (
    <div className="flex h-full">
      <div className="flex min-w-0 flex-1 flex-col">
        <ViewHeader title="Certificates" count={data?.items.length}>
          <NamespacePicker />
          <div className="relative">
            <Search size={13} className="absolute left-2.5 top-2.5 text-muted" />
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Name, domain, issuer…"
              className="no-drag h-8 w-56 select-text rounded-md border border-border bg-bg pl-8 pr-2 text-[13px] outline-none placeholder:text-muted focus:border-accent"
            />
          </div>
        </ViewHeader>
        <div className="flex flex-wrap gap-1 border-b border-border px-4 py-2">
          {chips.map(([id, label, n, cls]) => (
            <button
              key={id}
              onClick={() => setFilter(id)}
              aria-pressed={filter === id}
              className={clsx(
                'cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium',
                filter === id ? 'bg-accent/20 text-accent' : 'text-muted hover:text-fg',
              )}
            >
              {label} <span className={clsx('font-mono', n ? cls : '')}>{n ?? '–'}</span>
            </button>
          ))}
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {error && !data && (
            <div className="p-4 text-[13px] text-danger">
              {error}
              <div className="mt-1 text-xs text-muted">
                Listing TLS secrets needs <span className="font-mono">list</span> on{' '}
                <span className="font-mono">secrets</span>.
              </div>
            </div>
          )}
          {loading && <div className="p-4 text-[13px] text-muted">Reading TLS secrets…</div>}
          {data && items.length === 0 && (
            <EmptyState>
              {data.items.length === 0
                ? 'No TLS secrets found in this cluster.'
                : 'Nothing matches the filters.'}
            </EmptyState>
          )}
          {items.length > 0 && (
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-surface text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                <tr>
                  {['Expires', 'Secret', 'Domains', 'Issuer', 'Used by'].map((h) => (
                    <th key={h} className="border-b border-border px-3 py-2">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {items.map((c) => {
                  const tone = certTone(c)
                  return (
                    <tr
                      key={keyOf(c)}
                      onClick={() => setSel(sel === keyOf(c) ? null : keyOf(c))}
                      className={clsx(
                        'cursor-pointer border-b border-border/50',
                        sel === keyOf(c) ? 'bg-accent/15' : 'hover:bg-surface-hover',
                      )}
                    >
                      <td className="w-44 px-3 py-2">
                        {c.error ? (
                          <Pill tone="danger" title={c.error}>
                            unreadable
                          </Pill>
                        ) : (
                          <>
                            <div className={clsx('text-xs font-semibold', toneText[tone])}>
                              {remaining(c)}
                            </div>
                            <div className="mt-1 h-1 w-28 overflow-hidden rounded-full bg-surface-2">
                              <div
                                className={clsx('h-full rounded-full', toneBg[tone])}
                                style={{
                                  width: `${c.expired ? 100 : Math.min(100, Math.max(4, 100 - (c.daysLeft / 90) * 100))}%`,
                                }}
                              />
                            </div>
                            <div className="mt-0.5 text-[11px] text-muted">
                              {new Date(c.notAfter).toLocaleDateString()}
                            </div>
                          </>
                        )}
                      </td>
                      <td className="px-3 py-2">
                        <div className="font-medium">{c.name}</div>
                        <div className="text-xs text-muted">{c.namespace}</div>
                      </td>
                      <td className="max-w-64 px-3 py-2">
                        <div className="truncate font-mono text-xs" title={c.sans.join(', ')}>
                          {c.sans[0] ?? c.error}
                        </div>
                        {c.sans.length > 1 && (
                          <div className="text-[11px] text-muted">+{c.sans.length - 1} more</div>
                        )}
                      </td>
                      <td className="px-3 py-2">
                        {!c.error && (
                          <div className="max-w-52 truncate text-xs" title={c.issuer}>
                            {c.issuer}
                          </div>
                        )}
                        <div className="mt-0.5 flex gap-1">
                          {!c.error && c.issuerKind !== 'other' && (
                            <Pill tone={c.issuerKind === 'self-signed' ? 'warning' : 'neutral'}>
                              {issuerLabel[c.issuerKind]}
                            </Pill>
                          )}
                          {c.source === 'cert-manager' && (
                            <Pill
                              tone={c.cmReady === 'False' ? 'danger' : 'accent'}
                              title={c.cmMessage}
                            >
                              cert-manager
                            </Pill>
                          )}
                        </div>
                      </td>
                      <td
                        className="max-w-48 truncate px-3 py-2 text-xs text-muted"
                        title={c.usedBy.join(', ')}
                      >
                        {c.usedBy.join(', ') || '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
      {selected && <CertDrawer item={selected} onClose={() => setSel(null)} />}
    </div>
  )
}

function CertDrawer({ item: c, onClose }: { item: CertItem; onClose: () => void }) {
  const toast = useUI((s) => s.toast)
  const jump = useExplorer((s) => s.jump)
  const tone = certTone(c)
  const copy = (text: string, what: string) =>
    void navigator.clipboard
      ?.writeText(text)
      .then(() => toast('success', `${what} copied`))
      .catch(() => toast('error', 'Could not copy'))
  return (
    <Drawer
      title={c.name}
      subtitle={c.namespace}
      badges={
        !c.error && (
          <span
            className={clsx('rounded-full px-2 py-0.5 text-[11px] font-semibold', toneChip[tone])}
          >
            {remaining(c)}
          </span>
        )
      }
      actions={null}
      tabs={[{ id: 'details', label: 'Details' }]}
      tab="details"
      onTab={() => {}}
      onClose={onClose}
    >
      <div className="select-text space-y-4 p-4 text-[13px]">
        {c.error ? (
          <div className="rounded-md bg-danger/10 px-3 py-2 text-danger">{c.error}</div>
        ) : (
          <dl className="grid grid-cols-[110px_1fr] gap-y-1.5">
            <KV k="Subject">{c.subject}</KV>
            <KV k="Issuer">
              {c.issuer} <span className="text-muted">· {issuerLabel[c.issuerKind]}</span>
            </KV>
            <KV k="Valid from">{new Date(c.notBefore).toLocaleString()}</KV>
            <KV k="Valid until">
              <span className={toneText[tone]}>{new Date(c.notAfter).toLocaleString()}</span>
            </KV>
            <KV k="Key">{c.keyAlg}</KV>
            <KV k="Serial">
              <span className="break-all font-mono text-xs">{c.serial}</span>
            </KV>
            <KV k="Chain">
              {c.chainLen} certificate{c.chainLen === 1 ? '' : 's'} in the bundle
              {c.isCA && ' · is a CA'}
            </KV>
          </dl>
        )}
        {c.sans.length > 0 && (
          <section>
            <div className="mb-1.5 flex items-center justify-between">
              <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted">
                Domains &amp; addresses ({c.sans.length})
              </h3>
              <button
                onClick={() => copy(c.sans.join('\n'), 'Domains')}
                className="flex cursor-pointer items-center gap-1 text-xs text-accent hover:underline"
              >
                <Copy size={12} /> Copy
              </button>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {c.sans.map((s) => (
                <span
                  key={s}
                  className="rounded border border-border bg-bg px-1.5 py-0.5 font-mono text-xs"
                >
                  {s}
                </span>
              ))}
            </div>
          </section>
        )}
        {c.source === 'cert-manager' && (
          <section>
            <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
              cert-manager
            </h3>
            <dl className="grid grid-cols-[110px_1fr] gap-y-1.5">
              <KV k="Certificate">{c.managed}</KV>
              {c.cmReady && (
                <KV k="Ready">
                  <span className={c.cmReady === 'True' ? 'text-success' : 'text-danger'}>
                    {c.cmReady}
                  </span>
                </KV>
              )}
              {c.renewal && <KV k="Renews at">{new Date(c.renewal).toLocaleString()}</KV>}
            </dl>
            {c.cmMessage && (
              <div className="mt-2 rounded-md bg-danger/10 px-3 py-2 text-xs text-danger">
                {c.cmMessage}
              </div>
            )}
          </section>
        )}
        {c.usedBy.length > 0 && (
          <section>
            <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
              Used by
            </h3>
            {c.usedBy.map((u) => (
              <div key={u} className="font-mono text-xs">
                {u}
              </div>
            ))}
          </section>
        )}
        <button
          onClick={() => void jump('core/v1/secrets', c.name)}
          className="cursor-pointer text-xs text-accent hover:underline"
        >
          Open the Secret (Data tab shows the certificate)
        </button>
      </div>
    </Drawer>
  )
}
