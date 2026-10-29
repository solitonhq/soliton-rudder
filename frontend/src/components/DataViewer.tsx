import clsx from 'clsx'
import { Check, Copy, Eye, EyeOff } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { insight } from '../lib/backend'
import { usePoll } from '../lib/usePoll'
import type { ConfigData, DataEntry, Resource } from '../lib/types'
import type { RowRef } from '../store/explorer'
import { useUI } from '../store/ui'
import { CertCard } from './CertCard'
import { formatBytes } from '../lib/units'

const YamlEditor = lazy(() => import('./YamlEditor').then((m) => ({ default: m.YamlEditor })))

const fmtBadge: Record<DataEntry['format'], string> = {
  json: 'bg-accent/15 text-accent',
  yaml: 'bg-accent/15 text-accent',
  env: 'bg-warning/15 text-warning',
  cert: 'bg-success/15 text-success',
  text: 'bg-surface-2 text-muted',
  binary: 'bg-surface-2 text-muted',
}

function parseEnv(v: string): [string, string][] {
  return v
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith('#'))
    .map((l) => {
      const s = l.replace(/^export\s+/, '')
      const i = s.indexOf('=')
      let val = s.slice(i + 1)
      if (/^(".*"|'.*')$/.test(val)) val = val.slice(1, -1)
      return [s.slice(0, i), val] as [string, string]
    })
}

export function DataViewer({
  clusterId,
  resource,
  target,
}: {
  clusterId: string
  resource: Resource
  target: RowRef
}) {
  const { data, error } = usePoll<ConfigData>(
    () => insight.configData(clusterId, resource.key, target.ns, target.name),
    `${clusterId}/${resource.key}/${target.ns}/${target.name}`,
    0,
  )
  if (error) return <div className="p-4 text-[13px] text-danger">{error}</div>
  if (!data) return <div className="p-4 text-[13px] text-muted">Loading…</div>
  const secret = data.kind === 'Secret'
  return (
    <div className="h-full select-text space-y-3 overflow-auto p-4">
      {secret && data.type && (
        <div className="text-xs text-muted">
          Type <span className="font-mono text-fg">{data.type}</span> · values are hidden until you
          reveal them
        </div>
      )}
      {data.entries.length === 0 && <div className="text-[13px] text-muted">No data keys.</div>}
      {data.entries.map((e) => (
        <Entry key={e.key} entry={e} secret={secret} clusterId={clusterId} target={target} />
      ))}
    </div>
  )
}

function Entry({
  entry,
  secret,
  clusterId,
  target,
}: {
  entry: DataEntry
  secret: boolean
  clusterId: string
  target: RowRef
}) {
  const toast = useUI((s) => s.toast)
  const [revealed, setRevealed] = useState<DataEntry | null>(null)
  const [copied, setCopied] = useState(false)
  const [loading, setLoading] = useState(false)
  const shown = revealed ?? (entry.masked ? null : entry)

  const reveal = async () => {
    if (revealed) return setRevealed(null)
    setLoading(true)
    try {
      setRevealed(await insight.revealSecret(clusterId, target.ns, target.name, entry.key))
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  const copy = async () => {
    let value = shown?.value
    if (value === undefined && secret) {
      try {
        value = (await insight.revealSecret(clusterId, target.ns, target.name, entry.key)).value
      } catch (e) {
        return toast('error', e instanceof Error ? e.message : String(e))
      }
    }
    await navigator.clipboard?.writeText(value ?? '')
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const body = () => {
    if (entry.format === 'binary')
      return (
        <div className="px-3 py-2 text-xs text-muted">Binary data · {formatBytes(entry.size)}</div>
      )
    if (!shown)
      return (
        <div className="px-3 py-2 font-mono text-xs tracking-widest text-muted">
          {'•'.repeat(Math.min(24, Math.max(8, entry.size)))}
        </div>
      )
    if (entry.format === 'env') {
      return (
        <table className="w-full text-[13px]">
          <tbody>
            {parseEnv(shown.value).map(([k, v], i) => (
              <tr key={i} className="border-t border-border/60">
                <td className="whitespace-nowrap px-3 py-1 font-mono text-xs text-accent">{k}</td>
                <td className="break-all px-3 py-1 font-mono text-xs">{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )
    }
    const lines = shown.value.split('\n').length
    if (entry.format === 'json' || entry.format === 'yaml') {
      return (
        <div style={{ height: Math.min(Math.max(lines, 2), 18) * 18 + 16 }}>
          <Suspense fallback={<div className="p-3 text-xs text-muted">Loading…</div>}>
            <YamlEditor value={shown.value} readOnly />
          </Suspense>
        </div>
      )
    }
    return (
      <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all px-3 py-2 font-mono text-xs">
        {shown.value}
      </pre>
    )
  }

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-bg">
      <div className="flex items-center gap-2 border-b border-border px-3 py-1.5">
        <span className="truncate font-mono text-[13px] font-semibold">{entry.key}</span>
        <span
          className={clsx(
            'rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase',
            fmtBadge[entry.format],
          )}
        >
          {entry.format}
        </span>
        <span className="text-xs text-muted">{formatBytes(entry.size)}</span>
        {entry.info && (
          <span className="truncate text-xs text-muted" title={entry.info}>
            · {entry.info}
          </span>
        )}
        <div className="ml-auto flex shrink-0 gap-0.5">
          {secret && entry.format !== 'binary' && (
            <button
              onClick={() => void reveal()}
              disabled={loading}
              className="cursor-pointer rounded p-1 text-muted hover:bg-surface-hover hover:text-fg"
              title={revealed ? 'Hide' : 'Reveal'}
            >
              {revealed ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          )}
          {entry.format !== 'binary' && (
            <button
              onClick={() => void copy()}
              className="cursor-pointer rounded p-1 text-muted hover:bg-surface-hover hover:text-fg"
              title="Copy value"
            >
              {copied ? <Check size={14} className="text-success" /> : <Copy size={14} />}
            </button>
          )}
        </div>
      </div>
      {entry.cert && <CertCard cert={entry.cert} />}
      {body()}
      {shown?.truncated && (
        <div className="border-t border-border px-3 py-1 text-xs text-warning">
          Value truncated for display (over 1 MiB)
        </div>
      )}
    </div>
  )
}
