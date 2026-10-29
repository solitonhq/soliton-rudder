import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Application } from '../lib/types'
import { AppHistory } from './AppHistory'

const app = {
  id: 'helm|shop|shop',
  name: 'shop',
  groupedBy: 'helm',
  namespaces: ['shop'],
  status: 'ok',
  counts: { workloads: 1, workloadsHealthy: 1, pods: 1, podsReady: 1, restarts: 0 },
  version: '',
  chart: '',
  chartVersion: '',
  appVersion: '',
  images: [],
  urls: [],
  links: [],
  members: [],
  lastChange: '',
  issues: [],
  helm: {
    release: 'shop',
    namespace: 'shop',
    revision: 3,
    status: 'deployed',
    updated: '',
    revisions: 3,
  },
} as Application

const rev = (n: number) => ({
  revision: n,
  status: n === 3 ? 'deployed' : 'superseded',
  chartVersion: `1.${n}.0`,
  appVersion: `2.${n}`,
  description: `Upgrade complete ${n}`,
  updated: new Date(Date.now() - n * 86400_000).toISOString(),
})
const detail = (n: number) => ({
  name: 'shop',
  namespace: 'shop',
  revision: n,
  status: 'deployed',
  chart: 'shop',
  chartVersion: `1.${n}.0`,
  appVersion: `2.${n}`,
  description: '',
  updated: '',
  storage: 'secret',
  notes: '',
  values: `replicas: ${n}\n`,
  allValues: '',
  manifest: `kind: Deployment\nreplicas: ${n}\n`,
  objects: [],
  history: [rev(3), rev(2), rev(1)],
})

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetHelmRelease: vi
      .fn()
      .mockImplementation(async (_c: string, _ns: string, _n: string, r: number) => detail(r || 3)),
    HelmCLIAvailable: vi.fn().mockResolvedValue(true),
    RollbackHelmRelease: vi.fn().mockResolvedValue('Rollback was a success'),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('AppHistory', () => {
  it('lists the revisions with their status, chart and description', async () => {
    render(<AppHistory clusterId="c1" app={app} />)
    expect(await screen.findByText('Upgrade complete 3')).toBeTruthy()
    expect(screen.getByText('deployed')).toBeTruthy()
    expect(screen.getAllByText('superseded').length).toBe(2)
    expect(screen.getByText('Upgrade complete 2')).toBeTruthy()
    expect(screen.getByText('1.3.0')).toBeTruthy()
  })

  it('compares the previous revision with the latest by default and follows the selectors', async () => {
    render(<AppHistory clusterId="c1" app={app} />)
    await screen.findByText('Upgrade complete 3')
    await waitFor(() => {
      expect(App.GetHelmRelease).toHaveBeenCalledWith('c1', 'shop', 'shop', 2)
      expect(App.GetHelmRelease).toHaveBeenCalledWith('c1', 'shop', 'shop', 3)
    })
    expect((screen.getByLabelText('From revision') as HTMLSelectElement).value).toBe('2')
    expect((screen.getByLabelText('To revision') as HTMLSelectElement).value).toBe('3')
    fireEvent.change(screen.getByLabelText('From revision'), { target: { value: '1' } })
    await waitFor(() => expect(App.GetHelmRelease).toHaveBeenCalledWith('c1', 'shop', 'shop', 1))
    fireEvent.change(screen.getByLabelText('To revision'), { target: { value: '1' } })
    expect(await screen.findByText('Pick two different revisions.')).toBeTruthy()
  })

  it('says so when two revisions do not differ', async () => {
    App.GetHelmRelease.mockImplementation(
      async (_c: string, _ns: string, _n: string, r: number) => ({
        ...detail(r || 3),
        values: 'same: 1\n',
        manifest: 'same: 1\n',
      }),
    )
    render(<AppHistory clusterId="c1" app={app} />)
    expect(await screen.findByText(/No difference in the values/)).toBeTruthy()
    fireEvent.click(screen.getByText('manifest'))
    expect(await screen.findByText(/No difference in the manifest/)).toBeTruthy()
  })

  it('shows a rollback preview and only rolls back after confirmation', async () => {
    render(<AppHistory clusterId="c1" app={app} />)
    await screen.findByText('Upgrade complete 3')
    expect(screen.getAllByText('Roll back…').length).toBe(2) // not offered for the current revision
    fireEvent.click(screen.getAllByText('Roll back…')[0])
    expect(await screen.findByText(/Helm redeploys revision 2/)).toBeTruthy()
    expect(screen.getByText(/Roll back shop to revision 2/)).toBeTruthy()
    expect(App.RollbackHelmRelease).not.toHaveBeenCalled()
    const confirm = screen.getByRole('button', { name: 'Roll back to 2' }) as HTMLButtonElement
    await waitFor(() => expect(confirm.disabled).toBe(false))
    fireEvent.click(confirm)
    await waitFor(() =>
      expect(App.RollbackHelmRelease).toHaveBeenCalledWith('c1', 'shop', 'shop', 2),
    )
  })

  it('shows the rollback error and stays open', async () => {
    App.RollbackHelmRelease.mockRejectedValue(new Error('helm: another operation is in progress'))
    render(<AppHistory clusterId="c1" app={app} />)
    await screen.findByText('Upgrade complete 3')
    fireEvent.click(screen.getAllByText('Roll back…')[0])
    const confirm = (await screen.findByRole('button', {
      name: 'Roll back to 2',
    })) as HTMLButtonElement
    await waitFor(() => expect(confirm.disabled).toBe(false))
    fireEvent.click(confirm)
    expect(await screen.findByText(/another operation is in progress/)).toBeTruthy()
  })

  it('does not offer rollback without the helm command, and says why', async () => {
    App.HelmCLIAvailable.mockResolvedValue(false)
    render(<AppHistory clusterId="c1" app={app} />)
    await screen.findByText('Upgrade complete 3')
    await waitFor(() => expect(screen.getByText(/was not found/)).toBeTruthy())
    expect(screen.queryByText('Roll back…')).toBeNull()
  })

  it('shows the error when the release cannot be read', async () => {
    App.GetHelmRelease.mockRejectedValue(new Error('release shop/shop not found'))
    render(<AppHistory clusterId="c1" app={app} />)
    expect(await screen.findByText(/not found/)).toBeTruthy()
  })
})
