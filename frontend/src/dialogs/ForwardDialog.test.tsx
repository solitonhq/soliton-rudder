import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ForwardRuntime } from '../lib/types'
import { ForwardDialog } from './ForwardDialog'

const rt = (o: Partial<ForwardRuntime> = {}): ForwardRuntime => ({
  id: 'f1',
  clusterId: 'c1',
  namespace: 'shop',
  kind: 'pod',
  name: 'web-1',
  remotePort: 8080,
  localPort: 0,
  auto: false,
  createdAt: 0,
  status: 'connecting',
  activePort: 0,
  pod: '',
  message: '',
  reconnects: 0,
  lastHealedReason: '',
  lastHealedAt: 0,
  ...o,
})

let App: Record<string, ReturnType<typeof vi.fn>>
let opened: ReturnType<typeof vi.fn>
beforeEach(() => {
  opened = vi.fn()
  ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
  App = {
    GetResourcePorts: vi
      .fn()
      .mockResolvedValue([{ name: 'http', port: 8080, protocol: 'TCP', target: 'web' }]),
    AddPortForward: vi.fn().mockResolvedValue(rt()),
    ListPortForwards: vi.fn().mockResolvedValue([rt({ status: 'active', activePort: 54321 })]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
  delete (window as unknown as { runtime?: unknown }).runtime
})

const open = () =>
  render(
    <ForwardDialog
      clusterId="c1"
      kind="pod"
      ns="shop"
      name="web-1"
      resourceKey="core/v1/pods"
      onClose={() => {}}
    />,
  )

describe('ForwardDialog opens the browser', () => {
  it('opens the forwarded port in the browser once it is ready, for a web port', async () => {
    open()
    const box = (await screen.findByLabelText('Open in the browser when ready')) as HTMLInputElement
    await waitFor(() => expect(box.checked).toBe(true)) // 8080 looks like a web port
    fireEvent.click(screen.getByRole('button', { name: 'Forward' }))
    await waitFor(() => expect(opened).toHaveBeenCalledWith('http://localhost:54321'), {
      timeout: 4000,
    })
  })

  it('does not open a browser for a database port unless asked', async () => {
    open()
    const remote = await screen.findByDisplayValue('8080')
    fireEvent.change(remote, { target: { value: '5432' } })
    const box = screen.getByLabelText('Open in the browser when ready') as HTMLInputElement
    expect(box.checked).toBe(false)
    App.AddPortForward.mockResolvedValue(rt({ remotePort: 5432 }))
    App.ListPortForwards.mockResolvedValue([
      rt({ remotePort: 5432, status: 'active', activePort: 40000 }),
    ])
    fireEvent.click(screen.getByRole('button', { name: 'Forward' }))
    await waitFor(() => expect(App.AddPortForward).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 700))
    expect(opened).not.toHaveBeenCalled()
  })

  it('respects the user turning it off', async () => {
    open()
    const box = (await screen.findByLabelText('Open in the browser when ready')) as HTMLInputElement
    await waitFor(() => expect(box.checked).toBe(true))
    fireEvent.click(box)
    expect(box.checked).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Forward' }))
    await waitFor(() => expect(App.AddPortForward).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 700))
    expect(opened).not.toHaveBeenCalled()
  })
})
