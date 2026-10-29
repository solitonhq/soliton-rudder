import clsx from 'clsx'
import type { HealthStatus } from '../lib/types'

const cls: Record<HealthStatus, string> = {
  connected: 'bg-success',
  checking: 'bg-warning animate-pulse',
  unreachable: 'bg-danger',
  unauthorized: 'bg-warning',
  error: 'bg-danger',
}

export const statusLabel: Record<HealthStatus, string> = {
  connected: 'Connected',
  checking: 'Checking…',
  unreachable: 'Unreachable',
  unauthorized: 'Unauthorized',
  error: 'Error',
}

export function StatusDot({
  status,
  className,
  title,
}: {
  status?: HealthStatus
  className?: string
  title?: string
}) {
  return (
    <span
      title={title}
      className={clsx(
        'inline-block size-2 rounded-full',
        status ? cls[status] : 'bg-muted/50',
        className,
      )}
    />
  )
}
