/** Millicores → "250m" or "1.5" cores. */
export function formatCPU(milli: number): string {
  if (milli < 1000) return `${Math.round(milli)}m`
  const c = milli / 1000
  return `${c >= 10 ? c.toFixed(0) : c.toFixed(2).replace(/\.?0+$/, '')}`
}

/** Bytes → "512Mi" / "1.5Gi" (binary units, like Kubernetes). */
export function formatBytes(b: number): string {
  const units = ['B', 'Ki', 'Mi', 'Gi', 'Ti']
  let v = b
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const s = i === 0 || v >= 100 ? v.toFixed(0) : v.toFixed(1).replace(/\.0$/, '')
  return `${s}${units[i]}`
}

export function formatPercent(part: number, total: number): string {
  if (total <= 0) return ''
  return `${Math.round((part / total) * 100)}%`
}

/** US dollars: "$1,234" for large amounts, "$12.34" for small ones. */
export function formatUSD(v: number): string {
  const a = Math.abs(v)
  if (a >= 1000) return `${v < 0 ? '-' : ''}$${Math.round(a).toLocaleString('en-US')}`
  return `${v < 0 ? '-' : ''}$${a.toFixed(a >= 100 ? 0 : 2)}`
}
