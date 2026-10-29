import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Dialog } from './ui'

afterEach(cleanup)

describe('Dialog accessibility', () => {
  it('is a labelled modal dialog and closes on Escape', () => {
    const onClose = vi.fn()
    render(
      <Dialog title="Delete thing" onClose={onClose}>
        <button>OK</button>
      </Dialog>,
    )
    const d = screen.getByRole('dialog', { name: 'Delete thing' })
    expect(d.getAttribute('aria-modal')).toBe('true')
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('traps Tab focus inside the dialog', () => {
    render(
      <Dialog title="T" onClose={() => {}}>
        <button>one</button>
        <button>two</button>
      </Dialog>,
    )
    const [close, one, two] = [
      screen.getByLabelText('Close'),
      screen.getByText('one'),
      screen.getByText('two'),
    ]
    two.focus()
    fireEvent.keyDown(window, { key: 'Tab' })
    expect(document.activeElement).toBe(close) // wraps forward to the first control
    close.focus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(two) // and backward to the last
    expect(one).toBeTruthy()
  })

  it('restores focus to the opener when closed', () => {
    function Host() {
      const [open, setOpen] = useState(false)
      return (
        <>
          <button onClick={() => setOpen(true)}>opener</button>
          {open && (
            <Dialog title="T" onClose={() => setOpen(false)}>
              <span>body</span>
            </Dialog>
          )}
        </>
      )
    }
    render(<Host />)
    const opener = screen.getByText('opener')
    opener.focus()
    fireEvent.click(opener)
    expect(screen.getByRole('dialog')).toBeTruthy()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(opener)
  })

  it('does not steal focus from a field when the parent re-renders with a new onClose', () => {
    function Host() {
      const [n, setN] = useState(0)
      return (
        <>
          <button onClick={() => setN(n + 1)}>tick</button>
          <Dialog title="T" onClose={() => void n}>
            <input aria-label="name" />
          </Dialog>
        </>
      )
    }
    render(<Host />)
    const input = screen.getByLabelText('name')
    input.focus()
    fireEvent.click(screen.getByText('tick')) // re-render with a brand-new onClose
    input.focus()
    fireEvent.click(screen.getByText('tick'))
    expect(document.activeElement).toBe(input)
  })
})
