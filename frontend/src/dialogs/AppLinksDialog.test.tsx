import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { expandLink } from '../lib/applinks'
import type { Application } from '../lib/types'
import { AppLinksDialog } from './AppLinksDialog'

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetAppLinks: vi
      .fn()
      .mockResolvedValue([{ title: 'CI', url: 'https://ci.example.com/job/{release}' }]),
    SetAppLinks: vi.fn().mockResolvedValue([]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('expandLink', () => {
  const app = {
    name: 'my app',
    namespaces: ['team a'],
    version: '1.0/rc',
    helm: { release: 'rel-1' },
  } as unknown as Application
  it('fills the placeholders and encodes them', () => {
    expect(expandLink('https://x/{release}/{namespace}/{name}?v={version}', app)).toBe(
      'https://x/rel-1/team%20a/my%20app?v=1.0%2Frc',
    )
  })
  it('uses the name as the release for applications that are not Helm releases', () => {
    expect(
      expandLink('https://x/{release}', { ...app, helm: null } as unknown as Application),
    ).toBe('https://x/my%20app')
  })
})

describe('AppLinksDialog', () => {
  it('edits, adds and removes links and saves them', async () => {
    const onSaved = vi.fn()
    const onClose = vi.fn()
    render(<AppLinksDialog clusterId="c1" onClose={onClose} onSaved={onSaved} />)
    expect(await screen.findByDisplayValue('CI')).toBeTruthy()
    fireEvent.click(screen.getByText('Add a link'))
    fireEvent.change(screen.getByLabelText('Title 2'), { target: { value: 'Dash' } })
    fireEvent.change(screen.getByLabelText('Address 2'), {
      target: { value: 'https://g.example.com/{name}' },
    })
    fireEvent.click(screen.getByLabelText('Remove link 1'))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(App.SetAppLinks).toHaveBeenCalledWith('c1', [
        { title: 'Dash', url: 'https://g.example.com/{name}' },
      ]),
    )
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(onSaved).toHaveBeenCalled()
  })

  it('shows the validation error and stays open', async () => {
    App.SetAppLinks.mockRejectedValue(
      new Error('link 1 (CI): {cluster} is not a known placeholder'),
    )
    const onClose = vi.fn()
    render(<AppLinksDialog clusterId="c1" onClose={onClose} onSaved={() => {}} />)
    await screen.findByDisplayValue('CI')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/not a known placeholder/)).toBeTruthy()
    expect(onClose).not.toHaveBeenCalled()
  })
})
