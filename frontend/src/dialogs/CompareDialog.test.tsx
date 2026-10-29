import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Cluster, DriftObject, DriftReport } from '../lib/types'
import { useClusters } from '../store/clusters'
import { CompareDialog } from './CompareDialog'

const cl = (id: string, name: string): Cluster =>
  ({
    id,
    name,
    color: '#888',
    icon: '',
    pinned: false,
    position: 0,
    source: 'file',
    kubeconfigPath: '',
    context: '',
    createdAt: 0,
  }) as Cluster

const report: DriftReport = {
  groups: [
    {
      key: 'apps/v1/deployments',
      label: 'Deployments',
      error: '',
      items: [
        { name: 'web', namespace: 'staging', status: 'differs', changes: 2 },
        { name: 'new-svc', namespace: 'staging', status: 'onlySource', changes: 0 },
        { name: 'steady', namespace: 'staging', status: 'same', changes: 0 },
      ],
    },
  ],
  notes: ['Objects owned by a controller, and Secrets, are not compared.'],
}
const obj: DriftObject = {
  name: 'web',
  status: 'differs',
  srcExists: true,
  dstExists: true,
  srcYaml: 'a: 2',
  dstYaml: 'a: 1',
  fields: [
    {
      path: 'spec.template.spec.containers[name=web].image',
      op: 'changed',
      src: 'shop/web:2.0',
      dst: 'shop/web:1.0',
      promotable: true,
      envSpecific: false,
      note: '',
    },
    {
      path: 'spec.replicas',
      op: 'changed',
      src: 2,
      dst: 5,
      promotable: true,
      envSpecific: true,
      note: 'replica counts are usually sized per environment',
    },
    {
      path: 'spec.minReadySeconds',
      op: 'removed',
      src: undefined,
      dst: 5,
      promotable: false,
      envSpecific: false,
      note: 'present only in the target',
    },
  ],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetDriftKinds: vi.fn().mockResolvedValue([
      { key: 'apps/v1/deployments', label: 'Deployments' },
      { key: 'core/v1/configmaps', label: 'ConfigMaps' },
    ]),
    GetNamespaces: vi.fn().mockResolvedValue(['staging', 'prod']),
    CompareClusters: vi.fn().mockResolvedValue(report),
    CompareObject: vi.fn().mockResolvedValue(obj),
    BuildPromotion: vi
      .fn()
      .mockResolvedValue({ manifest: 'kind: Deployment\n', create: false, fields: 1 }),
    PreviewManifest: vi.fn().mockResolvedValue([
      {
        index: 0,
        kind: 'Deployment',
        name: 'web',
        namespace: 'prod',
        action: 'configure',
        before: 'a: 1',
        after: 'a: 2',
        error: '',
      },
    ]),
    ApplyManifest: vi.fn().mockResolvedValue([
      {
        index: 0,
        kind: 'Deployment',
        name: 'web',
        namespace: 'prod',
        action: 'configure',
        before: '',
        after: '',
        error: '',
      },
    ]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
  useClusters.setState({ clusters: [cl('s', 'Staging'), cl('p', 'Production')], activeId: 's' })
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

async function compare() {
  render(<CompareDialog onClose={() => {}} />)
  await waitFor(() => expect(screen.getAllByRole('option', { name: 'prod' }).length).toBe(2))
  fireEvent.change(screen.getByLabelText('Source namespace'), { target: { value: 'staging' } })
  fireEvent.change(screen.getByLabelText('Target namespace'), { target: { value: 'prod' } })
  fireEvent.click(screen.getByRole('button', { name: /^Compare$/ }))
  await screen.findByText('web')
}

describe('CompareDialog namespaces', () => {
  it('asks each cluster for its namespaces (connecting on demand) and shows a loading state', async () => {
    let release: (v: string[]) => void = () => {}
    App.GetNamespaces.mockImplementation((c: string) =>
      c === 'p' ? new Promise<string[]>((r) => (release = r)) : Promise.resolve(['staging']),
    )
    render(<CompareDialog onClose={() => {}} />)
    expect(await screen.findByRole('option', { name: 'Loading namespaces…' })).toBeTruthy()
    expect((screen.getByLabelText('Target namespace') as HTMLSelectElement).disabled).toBe(true)
    expect(App.GetNamespaces).toHaveBeenCalledWith('s')
    expect(App.GetNamespaces).toHaveBeenCalledWith('p')
    release(['prod'])
    await waitFor(() =>
      expect((screen.getByLabelText('Target namespace') as HTMLSelectElement).disabled).toBe(false),
    )
    expect(screen.getByRole('option', { name: 'prod' })).toBeTruthy()
  })

  it('shows why namespaces could not be loaded and lets the user retry, and reloads when the cluster changes', async () => {
    App.GetNamespaces.mockImplementation((c: string) =>
      c === 'p' ? Promise.reject(new Error('connection refused')) : Promise.resolve(['staging']),
    )
    render(<CompareDialog onClose={() => {}} />)
    expect(await screen.findByText(/connection refused/)).toBeTruthy()
    App.GetNamespaces.mockImplementation(() => Promise.resolve(['prod']))
    fireEvent.click(screen.getByText('Retry'))
    await waitFor(() => expect(screen.queryByText(/connection refused/)).toBeNull())
    expect(screen.getAllByRole('option', { name: 'prod' }).length).toBeGreaterThan(0)
    // switching the source cluster fetches that cluster's list, not the old one
    fireEvent.change(screen.getByLabelText('Source cluster'), { target: { value: 'p' } })
    await waitFor(() => expect(App.GetNamespaces).toHaveBeenLastCalledWith('p'))
  })
})

describe('CompareDialog', () => {
  it('preselects staging as source and another cluster as target, and needs both namespaces', async () => {
    render(<CompareDialog onClose={() => {}} />)
    expect((screen.getByLabelText('Source cluster') as HTMLSelectElement).value).toBe('s')
    expect((screen.getByLabelText('Target cluster') as HTMLSelectElement).value).toBe('p')
    expect((screen.getByRole('button', { name: /^Compare$/ }) as HTMLButtonElement).disabled).toBe(
      true,
    )
  })

  it('lists only differences by default and never offers Secrets', async () => {
    await compare()
    expect(App.CompareClusters).toHaveBeenCalledWith(
      's',
      'staging',
      'p',
      'prod',
      ['apps/v1/deployments', 'core/v1/configmaps'].filter(
        (k) => k === 'apps/v1/deployments' || k === 'core/v1/configmaps',
      ),
    )
    expect(screen.getByText('new-svc')).toBeTruthy()
    expect(screen.queryByText('steady')).toBeNull()
    fireEvent.click(screen.getByLabelText('Only differences'))
    expect(screen.getByText('steady')).toBeTruthy()
    expect(screen.queryByText('Secrets', { selector: 'button' })).toBeNull()
  })

  it('selects fields explicitly, keeps environment-specific ones out of "select all", and promotes only the chosen paths', async () => {
    await compare()
    fireEvent.click(screen.getByText('web'))
    const image = await screen.findByLabelText(
      'Promote spec.template.spec.containers[name=web].image',
    )
    const replicas = screen.getByLabelText('Promote spec.replicas') as HTMLInputElement
    const removed = screen.getByLabelText('Promote spec.minReadySeconds') as HTMLInputElement
    expect((image as HTMLInputElement).checked).toBe(false) // nothing is preselected
    expect(removed.disabled).toBe(true) // removals cannot be promoted
    const promote = screen.getByRole('button', { name: /Promote 0 fields/ }) as HTMLButtonElement
    expect(promote.disabled).toBe(true)

    fireEvent.click(screen.getByText('Select all except environment-specific'))
    expect((image as HTMLInputElement).checked).toBe(true)
    expect(replicas.checked).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: /Promote 1 field/ }))
    await waitFor(() =>
      expect(App.BuildPromotion).toHaveBeenCalledWith(
        's',
        'staging',
        'p',
        'prod',
        'apps/v1/deployments',
        'web',
        ['spec.template.spec.containers[name=web].image'],
      ),
    )
    // the dry-run runs on the TARGET cluster, and nothing is applied yet
    await waitFor(() =>
      expect(App.PreviewManifest).toHaveBeenCalledWith('p', 'kind: Deployment\n', 'prod', false),
    )
    expect(App.ApplyManifest).not.toHaveBeenCalled()
    const apply = await screen.findByRole('button', { name: 'Apply to Production' })
    await waitFor(() => expect((apply as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(apply)
    await waitFor(() =>
      expect(App.ApplyManifest).toHaveBeenCalledWith('p', 'kind: Deployment\n', 'prod', false),
    )
  })

  it('offers to create an object that is missing in the target', async () => {
    App.CompareObject.mockResolvedValue({
      ...obj,
      name: 'new-svc',
      status: 'onlySource',
      fields: [],
      dstExists: false,
    })
    App.BuildPromotion.mockResolvedValue({
      manifest: 'kind: Deployment\n',
      create: true,
      fields: 0,
    })
    await compare()
    fireEvent.click(screen.getByText('new-svc'))
    fireEvent.click(await screen.findByRole('button', { name: /Create in Production/ }))
    await waitFor(() =>
      expect(App.BuildPromotion).toHaveBeenCalledWith(
        's',
        'staging',
        'p',
        'prod',
        'apps/v1/deployments',
        'new-svc',
        [],
      ),
    )
    expect(await screen.findByText(/whole object will be created/)).toBeTruthy()
  })

  it('does not apply when the dry-run fails, and explains ownership conflicts', async () => {
    App.PreviewManifest.mockResolvedValue([
      {
        index: 0,
        kind: 'Deployment',
        name: 'web',
        namespace: 'prod',
        action: 'error',
        before: '',
        after: '',
        error: 'Apply failed with 1 conflict: conflict with "helm"',
      },
    ])
    await compare()
    fireEvent.click(screen.getByText('web'))
    fireEvent.click(
      await screen.findByLabelText('Promote spec.template.spec.containers[name=web].image'),
    )
    fireEvent.click(screen.getByRole('button', { name: /Promote 1 field/ }))
    const apply = (await screen.findByRole('button', {
      name: 'Apply to Production',
    })) as HTMLButtonElement
    await waitFor(() => expect(screen.getByText(/Another tool/)).toBeTruthy())
    expect(apply.disabled).toBe(true)
  })

  it('shows the error when a cluster cannot be reached', async () => {
    App.CompareClusters.mockRejectedValue(new Error('target cluster: connection refused'))
    render(<CompareDialog onClose={() => {}} />)
    await waitFor(() => expect(screen.getAllByRole('option', { name: 'prod' }).length).toBe(2))
    fireEvent.change(screen.getByLabelText('Source namespace'), { target: { value: 'staging' } })
    fireEvent.change(screen.getByLabelText('Target namespace'), { target: { value: 'prod' } })
    fireEvent.click(screen.getByRole('button', { name: /^Compare$/ }))
    expect(await screen.findByText(/connection refused/)).toBeTruthy()
  })
})
