import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Incident, Runbook } from '../lib/types'
import { RunbookDialog } from './RunbookDialog'

const inc: Incident = {
  kind: 'Pod',
  namespace: 'shop',
  name: 'web-1',
  reason: 'CrashLoopBackOff · 7 restarts',
  status: 'error',
}
const step = (o: Partial<Runbook['steps'][0]>) => ({
  title: 't',
  do: 'manual',
  applicable: true,
  readOnly: false,
  ...o,
})
const rb: Runbook = {
  id: 'crashloop',
  title: 'Container keeps crashing',
  description: 'web-1 starts, exits and is restarted.',
  builtin: true,
  score: 4,
  steps: [
    step({
      title: 'Read the previous logs',
      do: 'previous-logs',
      readOnly: true,
      text: 'The previous run holds the error.',
    }),
    step({ title: 'Recreate the pod', do: 'delete' }),
    step({ title: 'Raise the limit', do: 'manual', text: 'Edit the Deployment.' }),
    step({ title: 'Scale the node', do: 'restart', applicable: false }),
  ],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    MatchRunbooks: vi.fn().mockResolvedValue([rb]),
    AllRunbooksFor: vi.fn().mockResolvedValue([rb]),
    RunRunbookCheck: vi
      .fn()
      .mockResolvedValue({ title: 'Previous logs · app', text: 'panic: cannot open /etc/config' }),
    DeleteResource: vi.fn().mockResolvedValue(undefined),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('RunbookDialog', () => {
  it('shows the matched runbook, runs a read-only check and prints its output', async () => {
    render(<RunbookDialog clusterId="c1" incident={inc} onClose={() => {}} />)
    expect(await screen.findByText('Container keeps crashing')).toBeTruthy()
    expect(App.MatchRunbooks).toHaveBeenCalledWith(inc)
    fireEvent.click(screen.getByRole('button', { name: /Run check/ }))
    await waitFor(() =>
      expect(App.RunRunbookCheck).toHaveBeenCalledWith(
        'c1',
        'previous-logs',
        'core/v1/pods',
        'shop',
        'web-1',
      ),
    )
    expect(await screen.findByText('panic: cannot open /etc/config')).toBeTruthy()
    expect(screen.getByText('read-only')).toBeTruthy()
  })

  it('never changes the cluster without a second, explicit confirmation', async () => {
    render(<RunbookDialog clusterId="c1" incident={inc} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: /Delete pod…/ }))
    expect(App.DeleteResource).not.toHaveBeenCalled()
    expect(screen.getByText('changes the cluster')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Yes, delete pod web-1/ }))
    await waitFor(() =>
      expect(App.DeleteResource).toHaveBeenCalledWith('c1', 'core/v1/pods', 'shop', 'web-1'),
    )
    expect(await screen.findByText('Done.')).toBeTruthy()
  })

  it('cancelling the confirmation does nothing', async () => {
    render(<RunbookDialog clusterId="c1" incident={inc} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: /Delete pod…/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(App.DeleteResource).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /Delete pod…/ })).toBeTruthy()
  })

  it('marks steps that do not apply to this kind and gives them no buttons', async () => {
    render(<RunbookDialog clusterId="c1" incident={inc} onClose={() => {}} />)
    expect(await screen.findByText('Does not apply to a Pod.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Restart/ })).toBeNull()
  })

  it('shows a failing check as an error, not as output', async () => {
    App.RunRunbookCheck.mockRejectedValue(new Error('forbidden: pods/log'))
    render(<RunbookDialog clusterId="c1" incident={inc} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: /Run check/ }))
    expect(await screen.findByText(/forbidden: pods\/log/)).toBeTruthy()
  })

  it('offers all runbooks when none match', async () => {
    App.MatchRunbooks.mockResolvedValue([])
    render(
      <RunbookDialog clusterId="c1" incident={{ ...inc, reason: 'Weird' }} onClose={() => {}} />,
    )
    expect(await screen.findByText(/No runbook matches/)).toBeTruthy()
    fireEvent.click(screen.getByText('Browse all runbooks'))
    await waitFor(() => expect(App.AllRunbooksFor).toHaveBeenCalled())
    expect(await screen.findByText('Container keeps crashing')).toBeTruthy()
  })
})
