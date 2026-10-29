import { ChevronLeft, ChevronRight } from 'lucide-react'
import { canGoBack, canGoForward, useNav } from '../store/nav'
import { Dock } from './Dock'
import { Hotbar } from './Hotbar'

/** Frame: hotbar (left rail), draggable titlebar, content area. */
export function AppShell({ title, children }: { title?: string; children: React.ReactNode }) {
  const back = useNav((s) => canGoBack(s))
  const forward = useNav((s) => canGoForward(s))
  return (
    <div className="flex h-full">
      <Hotbar />
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="drag flex h-10 shrink-0 items-center border-b border-border bg-surface px-4 text-xs font-medium text-muted">
          <div className="no-drag mr-3 flex gap-0.5">
            <NavButton
              label="Back (⌘[)"
              enabled={back}
              onClick={() => void useNav.getState().back()}
            >
              <ChevronLeft size={16} />
            </NavButton>
            <NavButton
              label="Forward (⌘])"
              enabled={forward}
              onClick={() => void useNav.getState().forward()}
            >
              <ChevronRight size={16} />
            </NavButton>
          </div>
          {title}
        </header>
        <main className="min-h-0 flex-1 overflow-auto">{children}</main>
        <Dock />
      </div>
    </div>
  )
}

function NavButton({
  label,
  enabled,
  onClick,
  children,
}: {
  label: string
  enabled: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      aria-label={label.split(' ')[0]}
      title={label}
      disabled={!enabled}
      onClick={onClick}
      className="grid size-6 cursor-pointer place-items-center rounded text-muted hover:bg-surface-hover hover:text-fg disabled:cursor-default disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted"
    >
      {children}
    </button>
  )
}
