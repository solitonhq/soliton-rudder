import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Application, Applications as AppsData } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { Applications, matchesApp } from './Applications'

const app = (o: Partial<Application> = {}): Application => ({
  id: 'helm|shop|shop',
  name: 'shop',
  groupedBy: 'helm',
  namespaces: ['shop'],
  status: 'ok',
  counts: { workloads: 2, workloadsHealthy: 2, pods: 6, podsReady: 6, restarts: 0 },
  version: '2.4.1',
  chart: 'shop',
  chartVersion: '1.7.0',
  appVersion: '2.4.1',
  images: ['reg.io/shop/web:2.4.1', 'reg.io/shop/api:2.4.1'],
  helm: {
    release: 'shop',
    namespace: 'shop',
    revision: 12,
    status: 'deployed',
    updated: new Date(Date.now() - 3600_000).toISOString(),
    revisions: 12,
  },
  urls: ['https://shop.example.com'],
  links: [{ title: 'Grafana', url: 'https://grafana.example.com/d/shop' }],
  members: [
    {
      kind: 'Deployment',
      namespace: 'shop',
      name: 'web',
      resourceKey: 'apps/v1/deployments',
      status: 'ok',
      detail: '3/3 ready',
    },
    {
      kind: 'Deployment',
      namespace: 'shop',
      name: 'api',
      resourceKey: 'apps/v1/deployments',
      status: 'warn',
      detail: '1/2 ready',
    },
    {
      kind: 'Pod',
      namespace: 'shop',
      name: 'api-2',
      resourceKey: 'core/v1/pods',
      status: 'error',
      detail: 'CrashLoopBackOff · 7 restarts',
    },
  ],
  lastChange: new Date(Date.now() - 3600_000).toISOString(),
  issues: [
    {
      severity: 'warn',
      message:
        '60 revisions are kept in the cluster. Limit them with `helm upgrade --history-max` to keep it lean.',
    },
  ],
  ...o,
})
const broken = app({
  id: 'helm|kong|kong-cp',
  name: 'kong-cp',
  namespaces: ['kong'],
  status: 'error',
  version: '3.1',
  helm: {
    release: 'kong-cp',
    namespace: 'kong',
    revision: 3,
    status: 'failed',
    updated: '',
    revisions: 3,
  },
  urls: [],
  links: [],
  members: [],
  issues: [{ severity: 'error', message: 'The latest Helm revision (3) failed to deploy.' }],
  images: [],
})
const label = app({
  id: 'part-of|d|billing',
  name: 'billing',
  groupedBy: 'part-of',
  namespaces: ['d'],
  helm: null,
  urls: [],
  links: [],
  version: 'v9',
  members: [],
  issues: [],
  images: [],
})
const data: AppsData = {
  apps: [broken, app(), label],
  unassigned: [
    {
      kind: 'Deployment',
      namespace: 'ops',
      name: 'lonely',
      resourceKey: 'apps/v1/deployments',
      status: 'ok',
      detail: '1/1 ready',
    },
  ],
  unassignedTotal: 1,
  notes: [],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  localStorage.clear()
  App = {
    GetApplications: vi.fn().mockResolvedValue(data),
    GetAppLinks: vi
      .fn()
      .mockResolvedValue([{ title: 'CI', url: 'https://ci.example.com/job/{release}' }]),
    SetAppLinks: vi.fn().mockResolvedValue([]),
    MatchRunbooks: vi.fn().mockResolvedValue([]),
    GetHelmRelease: vi.fn().mockResolvedValue({ history: [], manifest: '', values: '' }),
    HelmCLIAvailable: vi.fn().mockResolvedValue(true),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
  ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: vi.fn() }
  useExplorer.setState({ namespaces: [] })
  useUI.setState({ toasts: [] })
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
  delete (window as unknown as { runtime?: unknown }).runtime
})

describe('matchesApp', () => {
  it('looks at name, namespace, version, chart, images and grouping', () => {
    expect(matchesApp(app(), 'shop 2.4')).toBe(true)
    expect(matchesApp(app(), 'reg.io/shop/api')).toBe(true)
    expect(matchesApp(app(), 'helm release')).toBe(true)
    expect(matchesApp(app(), 'nope')).toBe(false)
    expect(matchesApp(app(), '')).toBe(true)
  })
})

describe('Applications list', () => {
  it('shows every application with its health, version and how it was grouped, problems first', async () => {
    render(<Applications clusterId="c1" />)
    expect(await screen.findByText('kong-cp')).toBeTruthy()
    const names = screen
      .getAllByLabelText(/^Open (kong-cp|shop|billing)$/)
      .map((b) => b.getAttribute('aria-label'))
    expect(names).toEqual(['Open kong-cp', 'Open shop', 'Open billing'])
    expect(screen.getAllByText('2/2 workloads · 6/6 pods').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/1 issue/).length).toBe(2) // kong-cp (error) and shop (warning)
    expect(screen.getByText('part-of label')).toBeTruthy()
    expect(screen.getByText('1 need attention')).toBeTruthy()
  })

  it('filters by text and to problems only', async () => {
    render(<Applications clusterId="c1" />)
    await screen.findByText('kong-cp')
    fireEvent.change(screen.getByLabelText('Search applications'), { target: { value: 'billing' } })
    expect(screen.queryByText('kong-cp')).toBeNull()
    expect(screen.getByText('billing')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Search applications'), { target: { value: '' } })
    fireEvent.click(screen.getByText('Problems only'))
    expect(screen.queryByText('billing')).toBeNull()
    expect(screen.getByText('kong-cp')).toBeTruthy()
  })

  it('remembers the "group the rest by namespace" choice and asks the backend for it', async () => {
    const { unmount } = render(<Applications clusterId="c1" />)
    await screen.findByText('kong-cp')
    expect(App.GetApplications).toHaveBeenLastCalledWith('c1', false)
    fireEvent.click(screen.getByLabelText('Group the rest by namespace'))
    await waitFor(() => expect(App.GetApplications).toHaveBeenLastCalledWith('c1', true))
    expect(localStorage.getItem('soliton-rudder.apps.group-by-namespace.c1')).toBe('1')
    unmount()
    render(<Applications clusterId="c1" />)
    expect((screen.getByLabelText('Group the rest by namespace') as HTMLInputElement).checked).toBe(
      true,
    )
  })

  it('lists workloads that belong to no application and says how to fix that', async () => {
    render(<Applications clusterId="c1" />)
    fireEvent.click(await screen.findByText('Not part of any application'))
    expect(screen.getByText('ops/lonely')).toBeTruthy()
    expect(screen.getByText(/app.kubernetes.io\/part-of/)).toBeTruthy()
  })

  it('explains an empty result and a failed read', async () => {
    App.GetApplications.mockResolvedValue({
      apps: [],
      unassigned: [],
      unassignedTotal: 0,
      notes: [],
    })
    const { unmount } = render(<Applications clusterId="c1" />)
    expect(await screen.findByText(/No applications were recognised/)).toBeTruthy()
    unmount()
    App.GetApplications.mockRejectedValue(new Error('forbidden: pods'))
    render(<Applications clusterId="c1" />)
    expect(await screen.findByText(/forbidden: pods/)).toBeTruthy()
  })

  it('opens the first URL from the row without opening the application', async () => {
    const opened = vi.fn()
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
    render(<Applications clusterId="c1" />)
    fireEvent.click(await screen.findByLabelText('Open https://shop.example.com'))
    expect(opened).toHaveBeenCalledWith('https://shop.example.com')
    expect(screen.queryByText('Needs attention')).toBeNull()
  })
})

describe('Application detail', () => {
  const openShop = async () => {
    render(<Applications clusterId="c1" />)
    fireEvent.click(await screen.findByLabelText('Open shop'))
    await screen.findByText('Workloads')
  }

  it('shows health, what needs attention, links (URLs, annotations and templates) and versions', async () => {
    const opened = vi.fn()
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
    await openShop()
    expect(screen.getByText('2 / 2')).toBeTruthy()
    expect(screen.getByText('6 / 6')).toBeTruthy()
    expect(screen.getByText(/60 revisions are kept/)).toBeTruthy()
    expect(screen.getByText('CrashLoopBackOff · 7 restarts')).toBeTruthy()
    expect(screen.getByText('reg.io/shop/web:2.4.1')).toBeTruthy()
    expect(screen.getByText('Helm release')).toBeTruthy()
    fireEvent.click(await screen.findByText('CI'))
    expect(opened).toHaveBeenLastCalledWith('https://ci.example.com/job/shop') // {release} filled in
    fireEvent.click(screen.getByText('Grafana'))
    expect(opened).toHaveBeenLastCalledWith('https://grafana.example.com/d/shop')
    fireEvent.click(screen.getByText('shop.example.com'))
    expect(opened).toHaveBeenLastCalledWith('https://shop.example.com')
  })

  it('opens the runbook for a failing member', async () => {
    await openShop()
    fireEvent.click(screen.getAllByText('Runbook')[1])
    await waitFor(() =>
      expect(App.MatchRunbooks).toHaveBeenCalledWith({
        kind: 'Pod',
        namespace: 'shop',
        name: 'api-2',
        reason: 'CrashLoopBackOff · 7 restarts',
        status: 'error',
      }),
    )
  })

  it('jumps to the object in its table', async () => {
    const jump = vi.fn().mockResolvedValue(undefined)
    const setView = vi.fn()
    useExplorer.setState({ jump, setView } as never)
    await openShop()
    fireEvent.click(screen.getByTitle('Open api-2'))
    expect(setView).toHaveBeenCalledWith('resources')
    expect(jump).toHaveBeenCalledWith('core/v1/pods', 'api-2')
  })

  it('has a History tab only for Helm applications, and a way back', async () => {
    render(<Applications clusterId="c1" />)
    fireEvent.click(await screen.findByLabelText('Open billing'))
    await screen.findByText('Workloads')
    expect(screen.queryByText('history')).toBeNull()
    expect(screen.getByText('resources')).toBeTruthy()
    fireEvent.click(screen.getByText('Applications'))
    expect(await screen.findByLabelText('Open kong-cp')).toBeTruthy()
  })

  it('the Resources tab lists every object', async () => {
    await openShop()
    fireEvent.click(screen.getByText('resources'))
    expect(screen.getByText('web')).toBeTruthy()
    expect(screen.getByText('3/3 ready')).toBeTruthy()
    expect(screen.getByText(/Pods are listed only when they have a problem/)).toBeTruthy()
  })
})
