import { Check, ChevronDown } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useRowStoreVersion } from '../lib/useRowStore'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { Button } from './ui'

export function NamespacePicker() {
  const { nsStore, namespaces, setNamespaces, access, clusterId } = useExplorer()
  const version = useRowStoreVersion(nsStore)
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const h = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    window.addEventListener('mousedown', h)
    return () => window.removeEventListener('mousedown', h)
  }, [open])

  const listed = useMemo(
    () => [...nsStore.rows.values()].map((r) => r.name).sort(),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- version signals in-place store mutation
    [nsStore, version],
  )
  // credentials that cannot list namespaces still know which ones they can use
  const limited = !!access && !access.clusterWide
  const all = listed.length > 0 ? listed : limited ? access.namespaces : []
  const shown = all.filter((n) => n.includes(q.toLowerCase()))
  const label =
    namespaces.length === 0
      ? limited
        ? 'My namespaces'
        : 'All namespaces'
      : namespaces.length === 1
        ? namespaces[0]
        : `${namespaces.length} namespaces`

  const toggle = (n: string) =>
    setNamespaces(namespaces.includes(n) ? namespaces.filter((x) => x !== n) : [...namespaces, n])

  return (
    <div ref={ref} className="relative">
      <Button onClick={() => setOpen((o) => !o)} className="min-w-40 justify-between">
        <span className="truncate">{label}</span>
        <ChevronDown size={13} className="text-muted" />
      </Button>
      {open && (
        <div className="absolute left-0 top-9 z-30 w-64 rounded-lg border border-border bg-surface p-2 shadow-xl">
          <input
            autoFocus
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search namespaces…"
            className="mb-2 h-8 w-full select-text rounded-md border border-border bg-bg px-2.5 text-[13px] outline-none focus:border-accent"
          />
          <button
            onClick={() => setNamespaces([])}
            className="flex w-full cursor-pointer items-center justify-between rounded-md px-2 py-1.5 text-left text-[13px] hover:bg-surface-hover"
          >
            {limited ? 'All my namespaces' : 'All namespaces'}{' '}
            {namespaces.length === 0 && <Check size={14} className="text-accent" />}
          </button>
          <div className="mt-1 max-h-64 overflow-y-auto">
            {shown.map((n) => (
              <button
                key={n}
                onClick={() => toggle(n)}
                className="flex w-full cursor-pointer items-center justify-between rounded-md px-2 py-1.5 text-left text-[13px] hover:bg-surface-hover"
              >
                <span className="truncate">{n}</span>
                {namespaces.includes(n) && <Check size={14} className="text-accent" />}
              </button>
            ))}
            {shown.length === 0 && (
              <div className="px-2 py-3 text-xs text-muted">No namespaces</div>
            )}
          </div>
          {limited && clusterId && (
            <button
              onClick={() => {
                setOpen(false)
                useUI.getState().open({ kind: 'access', clusterId })
              }}
              className="mt-1 w-full cursor-pointer border-t border-border px-2 pb-1 pt-2 text-left text-xs text-accent hover:underline"
            >
              Choose which namespaces to use…
            </button>
          )}
        </div>
      )}
    </div>
  )
}
