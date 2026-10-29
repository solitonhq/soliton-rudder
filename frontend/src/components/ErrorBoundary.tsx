import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  /** Changing this (for example the open view) clears a previous error. */
  resetKey?: string
  children: ReactNode
}

/**
 * A rendering error in one view must not blank the whole window: show what happened and let the user move on
 * (switching view, or pressing "Try again") instead of losing the sidebar and every open tab.
 */
export class ErrorBoundary extends Component<Props, { error: Error | null }> {
  state: { error: Error | null } = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('View crashed:', error, info.componentStack)
  }

  componentDidUpdate(prev: Props) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: null })
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="grid min-w-0 flex-1 place-items-center p-8" role="alert">
        <div className="max-w-md text-center">
          <h2 className="text-base font-semibold">This view hit a problem</h2>
          <p className="mt-2 text-[13px] text-muted">
            The rest of the app keeps working. You can switch to another view or try again.
          </p>
          <pre className="mt-3 max-h-40 select-text overflow-auto whitespace-pre-wrap rounded-md bg-danger/10 p-3 text-left text-xs text-danger">
            {this.state.error.message}
          </pre>
          <button
            onClick={() => this.setState({ error: null })}
            className="mt-4 cursor-pointer rounded-md border border-border bg-surface-2 px-3 py-1.5 text-[13px] font-medium hover:bg-surface-hover"
          >
            Try again
          </button>
        </div>
      </div>
    )
  }
}
