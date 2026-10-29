import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApplyResult, Fix } from '../lib/types'
import { FixDialog } from './FixDialog'

const fix = (o: Partial<Fix> = {}): Fix => ({
  id: 'f',
  title: 'Add resource requests',
  description: 'Sets the missing requests.',
  action: 'apply',
  manifest: 'kind: Deployment\n',
  risk: 'medium',
  notes: ['Sized from observed usage.'],
  managedBy: '',
  target: {
    kind: 'Deployment',
    namespace: 'shop',
    name: 'web',
    resourceKey: 'apps/v1/deployments',
  },
  ...o,
})
const result = (o: Partial<ApplyResult> = {}): ApplyResult => ({
  index: 0,
  kind: 'Deployment',
  name: 'web',
  namespace: 'shop',
  action: 'configure',
  before: 'a: 1',
  after: 'a: 2',
  error: '',
  ...o,
})

let app: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  app = {
    ProposeFixes: vi.fn().mockResolvedValue([fix()]),
    PreviewManifest: vi.fn().mockResolvedValue([result()]),
    ApplyManifest: vi.fn().mockResolvedValue([result()]),
    DeleteResource: vi.fn().mockResolvedValue(undefined),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App: app } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

const open = (onClose = vi.fn()) =>
  render(
    <FixDialog
      clusterId="c1"
      ruleId="no-requests"
      resourceKey="apps/v1/deployments"
      ns="shop"
      name="web"
      onClose={onClose}
    />,
  )

describe('FixDialog', () => {
  it('shows the proposal with its risk and notes, and dry-runs before anything is applied', async () => {
    open()
    expect(await screen.findByText('Add resource requests')).toBeTruthy()
    expect(screen.getByText('medium risk')).toBeTruthy()
    expect(screen.getByText('Sized from observed usage.')).toBeTruthy()
    await waitFor(() =>
      expect(app.PreviewManifest).toHaveBeenCalledWith('c1', 'kind: Deployment\n', 'shop', false),
    )
    expect(app.ApplyManifest).not.toHaveBeenCalled()
  })

  it('applies after the dry-run succeeded', async () => {
    const onClose = vi.fn()
    open(onClose)
    const btn = await screen.findByRole('button', { name: 'Apply fix' })
    await waitFor(() => expect((btn as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(btn)
    await waitFor(() =>
      expect(app.ApplyManifest).toHaveBeenCalledWith('c1', 'kind: Deployment\n', 'shop', false),
    )
    await waitFor(() => expect(onClose).toHaveBeenCalled())
  })

  it('requires explicit acknowledgement for high-risk fixes', async () => {
    app.ProposeFixes.mockResolvedValue([
      fix({ risk: 'high', notes: ['Pods fail to start if the image runs as root.'] }),
    ])
    open()
    const btn = (await screen.findByRole('button', { name: 'Apply fix' })) as HTMLButtonElement
    await waitFor(() => expect(app.PreviewManifest).toHaveBeenCalled())
    expect(btn.disabled).toBe(true)
    fireEvent.click(screen.getByLabelText(/I have read the warnings/))
    await waitFor(() => expect(btn.disabled).toBe(false))
  })

  it('cannot apply when the dry-run fails, and hints at force on ownership conflicts', async () => {
    app.PreviewManifest.mockResolvedValue([
      result({ action: 'error', error: 'Apply failed with 1 conflict: conflict with "kubectl"' }),
    ])
    open()
    const btn = (await screen.findByRole('button', { name: 'Apply fix' })) as HTMLButtonElement
    await waitFor(() => expect(screen.getByText(/conflict with "kubectl"/)).toBeTruthy())
    expect(btn.disabled).toBe(true)
    expect(screen.getByText(/Another tool owns some of these fields/)).toBeTruthy()
    fireEvent.click(screen.getByLabelText(/Take over fields/))
    await waitFor(() =>
      expect(app.PreviewManifest).toHaveBeenLastCalledWith(
        'c1',
        'kind: Deployment\n',
        'shop',
        true,
      ),
    )
  })

  it('does not offer to apply a change that is already in place', async () => {
    app.PreviewManifest.mockResolvedValue([result({ action: 'unchanged' })])
    open()
    const btn = (await screen.findByRole('button', { name: 'Apply fix' })) as HTMLButtonElement
    await waitFor(() => expect(screen.getByText(/already matches/)).toBeTruthy())
    expect(btn.disabled).toBe(true)
  })

  it('warns when a GitOps tool manages the workload', async () => {
    app.ProposeFixes.mockResolvedValue([
      fix({
        managedBy: 'Argo CD',
        notes: ['This workload is managed by Argo CD: it will be reverted.'],
      }),
    ])
    open()
    expect(await screen.findByText('managed by Argo CD')).toBeTruthy()
  })

  it('deletes through the delete API for delete fixes', async () => {
    app.ProposeFixes.mockResolvedValue([
      fix({
        action: 'delete',
        manifest: '',
        title: 'Delete the failed pod',
        risk: 'low',
        target: { kind: 'Pod', namespace: 'shop', name: 'dead', resourceKey: 'core/v1/pods' },
      }),
    ])
    open()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(app.DeleteResource).toHaveBeenCalledWith('c1', 'core/v1/pods', 'shop', 'dead'),
    )
    expect(app.PreviewManifest).not.toHaveBeenCalled()
  })

  it('shows the backend error when no fix can be built', async () => {
    app.ProposeFixes.mockRejectedValue(new Error('right-sizing needs usage data'))
    open()
    expect(await screen.findByText(/right-sizing needs usage data/)).toBeTruthy()
  })
})
