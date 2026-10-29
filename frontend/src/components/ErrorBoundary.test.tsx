import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ErrorBoundary } from './ErrorBoundary'

afterEach(cleanup)

function Bomb({ explode }: { explode: boolean }) {
  if (explode) throw new Error('pods is null')
  return <div>fine</div>
}

describe('ErrorBoundary', () => {
  it('shows the error instead of blanking, and recovers when the view changes', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    function Host() {
      const [view, setView] = useState('a')
      return (
        <div>
          <button onClick={() => setView('b')}>switch</button>
          <ErrorBoundary resetKey={view}>
            <Bomb explode={view === 'a'} />
          </ErrorBoundary>
        </div>
      )
    }
    render(<Host />)
    expect(screen.getByRole('alert').textContent).toContain('pods is null')
    expect(screen.getByText('switch')).toBeTruthy() // the rest of the UI survives
    fireEvent.click(screen.getByText('switch'))
    expect(screen.getByText('fine')).toBeTruthy()
  })
})
