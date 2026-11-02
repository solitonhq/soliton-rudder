import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RestartDialog } from './RestartDialog'
import { ScaleDialog } from './ScaleDialog'

afterEach(cleanup)

describe('ScaleDialog', () => {
  it('prefills the current replica count and scales on Enter', () => {
    const onScale = vi.fn().mockResolvedValue(undefined)
    const onClose = vi.fn()
    render(<ScaleDialog name="web" initial={3} onClose={onClose} onScale={onScale} />)
    const input = screen.getByLabelText('Replicas') as HTMLInputElement
    expect(input.value).toBe('3')
    fireEvent.change(input, { target: { value: '5' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onScale).toHaveBeenCalledWith(5)
    expect(onClose).toHaveBeenCalled()
  })

  it('starts empty when the count is unknown, so a stray Enter scales nothing', () => {
    const onScale = vi.fn()
    render(<ScaleDialog name="rs" initial={null} onClose={vi.fn()} onScale={onScale} />)
    const input = screen.getByLabelText('Replicas') as HTMLInputElement
    expect(input.value).toBe('')
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onScale).not.toHaveBeenCalled()
    expect((screen.getByRole('button', { name: 'Scale' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows desired and ready counts, steps with +/- and presets, and previews the change', () => {
    const onScale = vi.fn().mockResolvedValue(undefined)
    render(
      <ScaleDialog
        name="web"
        kind="deployment"
        initial={3}
        ready={2}
        onClose={vi.fn()}
        onScale={onScale}
      />,
    )
    expect(screen.getByText('Desired now').nextSibling?.textContent).toBe('3')
    expect(screen.getByText('Ready now').nextSibling?.textContent).toBe('2')
    // nothing to do while the count is unchanged
    expect(
      (screen.getByRole('button', { name: /^Scale to 3$/ }) as HTMLButtonElement).disabled,
    ).toBe(true)
    expect(screen.getByText(/Already at 3 replicas/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Increase' }))
    expect(screen.getByText(/\+1 pod\b/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '10' }))
    expect(screen.getByText(/\+7 pods/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Decrease' }))
    fireEvent.click(screen.getByRole('button', { name: 'Scale to 9' }))
    expect(onScale).toHaveBeenCalledWith(9)
  })

  it('rejects non-numbers and warns that 0 stops everything', () => {
    render(<ScaleDialog name="web" initial={1} onClose={vi.fn()} onScale={vi.fn()} />)
    const input = screen.getByLabelText('Replicas')
    fireEvent.change(input, { target: { value: '-2' } })
    expect((screen.getByRole('button', { name: 'Scale' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/whole number from 0/)).toBeTruthy()
    fireEvent.change(input, { target: { value: '0' } })
    expect(screen.getByText(/stops every replica of web/)).toBeTruthy()
  })
})

describe('RestartDialog', () => {
  it('asks before restarting and runs only on confirm', () => {
    const onRestart = vi.fn().mockResolvedValue(undefined)
    const onClose = vi.fn()
    render(<RestartDialog name="web" kind="deployment" onClose={onClose} onRestart={onRestart} />)
    expect(screen.getByText(/rolling restart of the deployment/)).toBeTruthy()
    expect(onRestart).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }))
    expect(onRestart).toHaveBeenCalledTimes(1)
    expect(onClose).toHaveBeenCalled()
  })

  it('does nothing on Cancel', () => {
    const onRestart = vi.fn()
    render(<RestartDialog name="web" kind="deployment" onClose={vi.fn()} onRestart={onRestart} />)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onRestart).not.toHaveBeenCalled()
  })
})
