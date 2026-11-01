/** User-set column widths of a resource table, remembered per resource kind. Columns without an entry keep their automatic width. */
export type Widths = Record<string, number>

export const MIN_COL = 48
export const MAX_COL = 900

const key = (kind: string) => `soliton-rudder.colwidths.${kind}`

export const clampWidth = (n: number) => Math.round(Math.min(MAX_COL, Math.max(MIN_COL, n)))

export function loadWidths(kind: string): Widths {
  try {
    const raw = JSON.parse(localStorage.getItem(key(kind)) ?? '{}') as unknown
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {}
    const out: Widths = {}
    for (const [id, w] of Object.entries(raw)) {
      if (typeof w === 'number' && Number.isFinite(w)) out[id] = clampWidth(w)
    }
    return out
  } catch {
    return {}
  }
}

export function saveWidths(kind: string, widths: Widths) {
  try {
    if (Object.keys(widths).length === 0) localStorage.removeItem(key(kind))
    else localStorage.setItem(key(kind), JSON.stringify(widths))
  } catch {
    /* storage unavailable: the widths simply do not persist */
  }
}

/** The columns with the user's widths applied. */
export function applyWidths<T extends { id: string; width: number }>(
  cols: T[],
  widths: Widths,
): T[] {
  return cols.map((c) => (widths[c.id] === undefined ? c : { ...c, width: widths[c.id]! }))
}
