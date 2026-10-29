import clsx from 'clsx'
import { useUI } from '../store/ui'

export function Toasts() {
  const toasts = useUI((s) => s.toasts)
  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={clsx(
            'pointer-events-auto max-w-sm select-text rounded-lg border bg-surface px-3.5 py-2.5 text-[13px] shadow-xl',
            t.kind === 'error' ? 'border-danger/60 text-danger' : 'border-border text-fg',
          )}
        >
          {t.text}
        </div>
      ))}
    </div>
  )
}
