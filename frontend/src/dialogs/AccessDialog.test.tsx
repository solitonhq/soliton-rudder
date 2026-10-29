import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AccessInfo } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { AccessDialog } from './AccessDialog'

const limited: AccessInfo = {
  clusterWide: false,
  canListNamespaces: true,
  namespaces: ['team-a', 'team-b'],
  source: 'discovered',
  note: '',
  configured: [],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetAccess: vi.fn().mockResolvedValue(limited),
    SetAccessNamespaces: vi
      .fn()
      .mockResolvedValue({ ...limited, source: 'configured', configured: ['team-a'] }),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('AccessDialog', () => {
  it('explains where the namespaces come from and lists them', async () => {
    render(<AccessDialog clusterId="c1" onClose={() => {}} />)
    expect(await screen.findByText('Limited access')).toBeTruthy()
    expect(screen.getByText(/found to be able to read/)).toBeTruthy()
    expect(screen.getByText('team-a')).toBeTruthy()
    expect(screen.getByText('team-b')).toBeTruthy()
  })

  it('saves the chosen namespaces (any separators) and reloads the cluster', async () => {
    const open = vi.fn().mockResolvedValue(undefined)
    useExplorer.setState({ clusterId: 'c1', open } as never)
    const onClose = vi.fn()
    render(<AccessDialog clusterId="c1" onClose={onClose} />)
    const box = await screen.findByLabelText('Namespaces')
    fireEvent.change(box, { target: { value: 'team-a, team-c\nteam-d  team-e' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(App.SetAccessNamespaces).toHaveBeenCalledWith('c1', [
        'team-a',
        'team-c',
        'team-d',
        'team-e',
      ]),
    )
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(open).toHaveBeenCalledWith('c1')
  })

  it('can go back to detecting namespaces after overriding them', async () => {
    App.GetAccess.mockResolvedValue({ ...limited, source: 'configured', configured: ['team-a'] })
    render(<AccessDialog clusterId="c1" onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Detect again' }))
    await waitFor(() => expect(App.SetAccessNamespaces).toHaveBeenCalledWith('c1', []))
  })

  it('shows the validation error from the backend and stays open', async () => {
    App.SetAccessNamespaces.mockRejectedValue(new Error('"Bad_Name" is not a valid namespace name'))
    const onClose = vi.fn()
    render(<AccessDialog clusterId="c1" onClose={onClose} />)
    fireEvent.change(await screen.findByLabelText('Namespaces'), { target: { value: 'Bad_Name' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/not a valid namespace name/)).toBeTruthy()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('has nothing to configure with full access', async () => {
    App.GetAccess.mockResolvedValue({
      clusterWide: true,
      canListNamespaces: true,
      namespaces: [],
      source: 'cluster',
      note: '',
      configured: [],
    })
    render(<AccessDialog clusterId="c1" onClose={() => {}} />)
    expect(await screen.findByText('Full access')).toBeTruthy()
    expect(screen.queryByLabelText('Namespaces')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
  })

  it('says when nothing could be found', async () => {
    App.GetAccess.mockResolvedValue({
      clusterWide: false,
      canListNamespaces: false,
      namespaces: [],
      source: 'none',
      note: "No namespace could be found that these credentials can read. Choose the namespaces to work with in the cluster's access settings.",
      configured: [],
    })
    render(<AccessDialog clusterId="c1" onClose={() => {}} />)
    expect(
      await screen.findByText(
        /No namespace could be found that these credentials can read\. Choose/,
      ),
    ).toBeTruthy()
  })
})
