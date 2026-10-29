import type { FzCategory, Severity } from './types'

export const SEVERITIES: Severity[] = ['critical', 'high', 'medium', 'low', 'info']

export const sevRank: Record<Severity, number> = {
  critical: 4,
  high: 3,
  medium: 2,
  low: 1,
  info: 0,
}

export const sevChip: Record<Severity, string> = {
  critical: 'bg-danger text-white',
  high: 'bg-danger/15 text-danger',
  medium: 'bg-warning/15 text-warning',
  low: 'bg-accent/15 text-accent',
  info: 'bg-surface-2 text-muted',
}

export const catLabel: Record<FzCategory, string> = {
  resources: 'Resources',
  reliability: 'Reliability',
  security: 'Security',
  maintenance: 'Maintenance',
}

/** Score → semantic colour token. */
export function scoreColor(score: number): string {
  if (score >= 85) return 'var(--success)'
  if (score >= 60) return 'var(--warning)'
  return 'var(--danger)'
}

export function scoreLabel(score: number): string {
  if (score >= 90) return 'Excellent'
  if (score >= 75) return 'Good'
  if (score >= 60) return 'Needs attention'
  if (score >= 40) return 'At risk'
  return 'Critical'
}
