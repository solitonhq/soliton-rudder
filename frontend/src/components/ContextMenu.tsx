import { useEffect } from 'react'
import { createPortal } from 'react-dom'

export interface MenuItem {
  label: string
  onClick: () => void
  danger?: boolean
}

export function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number
  y: number
  items: MenuItem[]
  onClose: () => void
}) {
  useEffect(() => {
    const close = () => onClose()
    window.addEventListener('mousedown', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', close)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', close)
    }
  }, [onClose])
  return createPortal(
    <div
      className="fixed z-50 min-w-40 rounded-lg border border-border bg-surface p-1 shadow-xl"
      style={{
        left: Math.min(x, window.innerWidth - 180),
        top: Math.min(y, window.innerHeight - items.length * 32 - 16),
      }}
      onMouseDown={(e) => e.stopPropagation()}
    >
      {items.map((it) => (
        <button
          key={it.label}
          onClick={() => {
            onClose()
            it.onClick()
          }}
          className={`block w-full cursor-pointer rounded-md px-2.5 py-1.5 text-left text-[13px] hover:bg-surface-hover ${it.danger ? 'text-danger' : ''}`}
        >
          {it.label}
        </button>
      ))}
    </div>,
    document.body,
  )
}
