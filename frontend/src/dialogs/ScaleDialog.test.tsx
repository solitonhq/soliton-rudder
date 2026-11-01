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

  it('rejects non-numbers and warns that 0 stops everything', () => {
    render(<ScaleDialog name="web" initial={1} onClose={vi.fn()} onScale={vi.fn()} />)
    const input = screen.getByLabelText('Replicas')
    fireEvent.change(input, { target: { value: '-2' } })
    expect((screen.getByRole('button', { name: 'Scale' }) as HTMLButtonElement).disabled).toBe(true)
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
