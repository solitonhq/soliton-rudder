import { useVirtualizer } from '@tanstack/react-virtual'
import clsx from 'clsx'
import { ArrowDown, ArrowUp, Cable } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  cellText,
  formatAge,
  isStatusColumn,
  readyTone,
  statusTone,
  type Tone,
} from '../lib/format'
import { openURL } from '../lib/backend'
import { FORWARD_KIND, FORWARDABLE, quickForward } from '../lib/quickforward'
import { computeRows, moveSelection, usageKey } from '../lib/tablerows'
import type { Column, Row } from '../lib/types'
import { formatBytes, formatCPU } from '../lib/units'
import { useRowStoreVersion } from '../lib/useRowStore'
import { useExplorer, type RowRef } from '../store/explorer'
import { ForwardDialog, type ForwardTargetProps } from '../dialogs/ForwardDialog'
import { useUI } from '../store/ui'
import { ContextMenu, type MenuItem } from './ContextMenu'
import { IngressCell } from './IngressDetails'

const ROW_H = 30

interface DisplayCol {
  id: string
  label: string
  index: number // index into row.cells; -1 = namespace, -2 = age
  type: string
  width: number
}

const toneCls: Record<Tone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  neutral: 'text-fg',
}

export function ResourceTable({
  namespaced,
  resourceName,
  onAction,
}: {
  namespaced: boolean
  resourceName: string
  onAction: (action: 'edit' | 'delete', ref: RowRef) => void
}) {
  const {
    main,
    namespaces,
    search,
    sort,
    wide,
    selected,
    toggleSort,
    setSelected,
    usage,
    setNamespaces,
    access,
  } = useExplorer()
  const isNode = resourceName === 'nodes'
  const clusterId = useExplorer((s) => s.clusterId)
  const selectedKey = useExplorer((s) => s.selectedKey)
  const toast = useUI((s) => s.toast)
  const canForward = FORWARDABLE.has(resourceName) && !!clusterId && !!selectedKey
  const [fwdDialog, setFwdDialog] = useState<ForwardTargetProps | null>(null)

  /** One click: free local port, reuse an existing identical forward; asks only when several ports exist. */
  const forwardRow = async (r: Row) => {
    if (!clusterId || !selectedKey) return
    try {
      const out = await quickForward(clusterId, selectedKey, r.ns, r.name)
      if (out.kind === 'choose') {
        setFwdDialog({
          clusterId,
          kind: FORWARD_KIND[resourceName],
          ns: r.ns,
          name: r.name,
          resourceKey: selectedKey,
        })
      } else {
        toast('success', out.text)
        if (out.url) openURL(out.url) // web ports open in the browser right away
      }
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e))
    }
  }
  const hasUsage = usage !== null && (resourceName === 'pods' || isNode)
  const version = useRowStoreVersion(main)
  const scrollRef = useRef<HTMLDivElement>(null)
  const [now, setNow] = useState(() => Date.now())
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null)

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 15_000)
    return () => clearInterval(t)
  }, [])

  const columns: Column[] = main.columns
  const hasRows = main.rows.size > 0
  const showNs = namespaced && namespaces.length !== 1

  const cols: DisplayCol[] = useMemo(() => {
    const sample = [...main.rows.values()].slice(0, 300)
    const out: DisplayCol[] = []
    columns.forEach((c, i) => {
      if (c.priority > 0 && !wide) return
      const isAge = c.name === 'Age'
      let max = c.name.length + 3
      if (!isAge) for (const r of sample) max = Math.max(max, cellText(r.cells[i]).length)
      out.push({
        id: c.name + i,
        label: c.name,
        index: isAge ? -2 : i,
        type: c.type,
        width: isAge ? 70 : Math.min(Math.max(max * 7.4 + 28, 80), i === 0 ? 420 : 340),
      })
    })
    if (hasUsage) {
      const at = out.findIndex((c) => c.index === -2)
      const extra: DisplayCol[] = [
        { id: 'cpu', label: 'CPU', index: -3, type: 'usage', width: 84 },
        { id: 'mem', label: 'Memory', index: -4, type: 'usage', width: 96 },
      ]
      out.splice(at < 0 ? out.length : at, 0, ...extra)
    }
    if (showNs) {
      const nsW = Math.min(Math.max(...sample.map((r) => r.ns.length), 9) * 7.4 + 28, 240)
      out.splice(Math.min(1, out.length), 0, {
        id: 'ns',
        label: 'Namespace',
        index: -1,
        type: 'string',
        width: nsW,
      })
    }
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [columns, wide, showNs, hasRows, hasUsage])

  const rows = useMemo(
    () => computeRows(main.rows, { namespaced, namespaces, search, sort, cols, usage, isNode }),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `version` signals in-place mutation of main.rows
    [main, version, namespaces, search, sort, cols, namespaced, usage, isNode],
  )

  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual is not memoized by the compiler; harmless here
  const virt = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_H,
    overscan: 14,
  })
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.target !== e.currentTarget || e.metaKey || e.ctrlKey || e.altKey) return
    if (!['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const cur = rows.findIndex((r) => r.uid === selected?.uid)
    const next = moveSelection(cur, rows.length, e.key)
    if (next >= 0 && next !== cur) {
      const r = rows[next]
      setSelected({ uid: r.uid, ns: r.ns, name: r.name })
      virt.scrollToIndex(next, { align: 'auto' })
    }
  }

  const totalW = cols.reduce((n, c) => n + c.width, 0)
  const grid = cols.map((c) => `${c.width}px`).join(' ')

  const renderCell = (r: Row, c: DisplayCol, i: number) => {
    if (c.index === -3 || c.index === -4) {
      const u = usage?.[usageKey(r, isNode)]
      if (!u) return <span className="text-muted">—</span>
      return (
        <span className="font-mono text-xs">
          {c.index === -3 ? formatCPU(u.cpu) : formatBytes(u.mem)}
        </span>
      )
    }
    if (c.index === -1)
      return (
        <button
          onClick={(e) => {
            e.stopPropagation() // don't also select the row
            setNamespaces([r.ns])
          }}
          title={`Show only namespace ${r.ns}`}
          className="max-w-full cursor-pointer truncate text-left text-muted hover:text-accent hover:underline"
        >
          {r.ns}
        </button>
      )
    if (c.index === -2) return <span className="text-muted">{formatAge(r.created, now)}</span>
    const text = cellText(r.cells[c.index])
    if (c.index === 0) return <span className="font-medium">{text}</span>
    if (resourceName === 'ingresses' && /^(hosts?|address(es)?)$/i.test(c.label)) {
      const ports = columns.findIndex((x) => /^ports?$/i.test(x.name))
      return <IngressCell text={text} secure={/\b(443|8443)\b/.test(cellText(r.cells[ports]))} />
    }
    if (isStatusColumn(c.label))
      return <span className={clsx('font-medium', toneCls[statusTone(text)])}>{text}</span>
    if (/^ready$/i.test(c.label)) return <span className={toneCls[readyTone(text)]}>{text}</span>
    void i
    return (
      <span className={c.type === 'integer' || c.type === 'number' ? 'font-mono' : ''}>{text}</span>
    )
  }

  if (main.status === 'error' && !hasRows) {
    return (
      <div className="grid h-full place-items-center p-6 text-center text-sm">
        <div>
          <div className="select-text text-danger">{main.message}</div>
          {access && !access.clusterWide && clusterId && (
            <button
              onClick={() => useUI.getState().open({ kind: 'access', clusterId })}
              className="mt-3 cursor-pointer text-accent hover:underline"
            >
              Choose which namespaces to use…
            </button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div
      ref={scrollRef}
      className="relative h-full overflow-auto focus-visible:outline-offset-[-2px]"
      tabIndex={0}
      role="grid"
      aria-label="Resources"
      aria-rowcount={rows.length}
      onKeyDown={onKeyDown}
    >
      <div style={{ width: Math.max(totalW, 0), minWidth: '100%' }}>
        <div
          className="sticky top-0 z-10 grid border-b border-border bg-surface text-[11px] font-semibold uppercase tracking-wide text-muted"
          style={{ gridTemplateColumns: grid, height: 30 }}
        >
          {cols.map((c) => (
            <button
              key={c.id}
              onClick={() => toggleSort(c.id)}
              aria-label={`Sort by ${c.label}`}
              className="flex cursor-pointer items-center gap-1 truncate px-3 text-left uppercase hover:text-fg"
            >
              <span className="truncate">{c.label}</span>
              {sort?.col === c.id &&
                (sort.dir === 'asc' ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
            </button>
          ))}
        </div>
        <div style={{ height: virt.getTotalSize(), position: 'relative' }}>
          {virt.getVirtualItems().map((v) => {
            const r = rows[v.index]
            const active = selected?.uid === r.uid
            const ref = { uid: r.uid, ns: r.ns, name: r.name }
            return (
              <div
                key={r.uid}
                role="row"
                aria-selected={active}
                aria-rowindex={v.index + 1}
                onClick={() => setSelected(active ? null : ref)}
                onContextMenu={(e) => {
                  e.preventDefault()
                  setSelected(ref)
                  setMenu({
                    x: e.clientX,
                    y: e.clientY,
                    items: [
                      ...(canForward
                        ? [{ label: 'Forward (auto port)', onClick: () => void forwardRow(r) }]
                        : []),
                      { label: 'Edit YAML', onClick: () => onAction('edit', ref) },
                      {
                        label: 'Copy name',
                        onClick: () => void navigator.clipboard?.writeText(r.name),
                      },
                      { label: 'Delete…', danger: true, onClick: () => onAction('delete', ref) },
                    ],
                  })
                }}
                className={clsx(
                  'group absolute left-0 grid cursor-pointer items-center border-b border-border/50 text-[13px]',
                  active ? 'bg-accent/15' : 'hover:bg-surface-hover',
                )}
                style={{
                  top: 0,
                  transform: `translateY(${v.start}px)`,
                  height: ROW_H,
                  gridTemplateColumns: grid,
                  width: '100%',
                }}
              >
                {cols.map((c, i) => (
                  <div key={c.id} className="truncate px-3">
                    {renderCell(r, c, i)}
                  </div>
                ))}
                {canForward && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation() // forward without also selecting the row
                      void forwardRow(r)
                    }}
                    title="Forward: one click, free local port"
                    aria-label={`Forward ${r.name}`}
                    style={{ left: Math.max(0, (cols[0]?.width ?? 200) - 30) }}
                    className="absolute top-1/2 grid size-6 -translate-y-1/2 cursor-pointer place-items-center rounded-md border border-border bg-surface text-muted opacity-0 hover:border-accent hover:text-accent focus-visible:opacity-100 group-hover:opacity-100"
                  >
                    <Cable size={13} />
                  </button>
                )}
              </div>
            )
          })}
        </div>
        {rows.length === 0 && (
          <div className="p-10 text-center text-sm text-muted">
            {main.status === 'connecting' && !hasRows
              ? 'Loading…'
              : search || namespaces.length
                ? 'No matching resources'
                : 'No resources found'}
          </div>
        )}
      </div>
      {menu && <ContextMenu {...menu} onClose={() => setMenu(null)} />}
      {fwdDialog && <ForwardDialog {...fwdDialog} onClose={() => setFwdDialog(null)} />}
    </div>
  )
}
