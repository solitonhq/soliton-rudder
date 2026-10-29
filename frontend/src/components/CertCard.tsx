import clsx from 'clsx'
import type { CertSummary } from '../lib/types'

export const issuerLabel: Record<CertSummary['issuerKind'], string> = {
  letsencrypt: "Let's Encrypt",
  'letsencrypt-staging': "Let's Encrypt (staging)",
  'self-signed': 'Self-signed',
  other: 'Issuer',
}

/** Colour by remaining validity: expired / < 14 days red, < 30 days amber, otherwise green. */
export function certTone(
  c: Pick<CertSummary, 'expired' | 'daysLeft'>,
): 'danger' | 'warning' | 'success' {
  if (c.expired || c.daysLeft < 14) return 'danger'
  if (c.daysLeft < 30) return 'warning'
  return 'success'
}

export const toneText = { danger: 'text-danger', warning: 'text-warning', success: 'text-success' }
export const toneBg = { danger: 'bg-danger', warning: 'bg-warning', success: 'bg-success' }
export const toneChip = {
  danger: 'bg-danger/15 text-danger',
  warning: 'bg-warning/15 text-warning',
  success: 'bg-success/15 text-success',
}

export function remaining(c: Pick<CertSummary, 'expired' | 'daysLeft'>): string {
  if (c.expired) return c.daysLeft === 0 ? 'expired today' : `expired ${-c.daysLeft}d ago`
  if (c.daysLeft === 0) return 'expires today'
  return `${c.daysLeft}d left`
}

/** Readable summary of a certificate: validity, issuer and every domain it covers. */
export function CertCard({ cert }: { cert: CertSummary }) {
  const tone = certTone(cert)
  return (
    <div className="border-b border-border px-3 py-2.5 text-[13px]">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span
          className={clsx('rounded-full px-2 py-0.5 text-[11px] font-semibold', toneChip[tone])}
        >
          {remaining(cert)}
        </span>
        <span className="text-xs text-muted">
          until {new Date(cert.notAfter).toLocaleDateString()}
        </span>
        <span className="text-xs text-muted">· {cert.keyAlg}</span>
        {cert.chainLen > 1 && (
          <span className="text-xs text-muted">· chain of {cert.chainLen}</span>
        )}
      </div>
      <dl className="mt-2 grid grid-cols-[64px_1fr] gap-y-0.5 text-xs">
        <dt className="text-muted">Subject</dt>
        <dd className="select-text break-words">{cert.subject}</dd>
        <dt className="text-muted">Issuer</dt>
        <dd className="select-text break-words">
          {cert.issuer} <span className="text-muted">· {issuerLabel[cert.issuerKind]}</span>
        </dd>
      </dl>
      {cert.sans.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {cert.sans.map((s) => (
            <span
              key={s}
              className="select-text rounded border border-border bg-bg px-1.5 py-0.5 font-mono text-[11px]"
            >
              {s}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
