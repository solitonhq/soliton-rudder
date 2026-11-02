import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PodEnv } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { PodEnvironment } from './PodEnvironment'

const env: PodEnv = {
  containers: [
    {
      name: 'web',
      init: false,
      env: [
        {
          name: 'MODE',
          source: 'literal',
          value: 'production',
          ref: '',
          key: '',
          optional: false,
          detail: '',
        },
        {
          name: 'DB_PASSWORD',
          source: 'secret',
          value: '',
          ref: 'db',
          key: 'password',
          optional: false,
          detail: '',
        },
        {
          name: 'THEME',
          source: 'configmap',
          value: '',
          ref: 'app-config',
          key: 'theme',
          optional: true,
          detail: '',
        },
        {
          name: 'POD_IP',
          source: 'field',
          value: '',
          ref: '',
          key: '',
          optional: false,
          detail: 'status.podIP',
        },
      ],
      envFrom: [{ kind: 'secret', name: 'api-keys', prefix: 'API_', optional: false }],
      mounts: [{ volume: 'certs', path: '/etc/tls', readOnly: true, subPath: '' }],
    },
    {
      name: 'migrate',
      init: true,
      env: [],
      envFrom: [{ kind: 'configmap', name: 'app-config', prefix: '', optional: false }],
      mounts: [],
    },
  ],
  volumes: [
    { name: 'certs', kind: 'secret', ref: 'tls-cert', sources: [] },
    { name: 'data', kind: 'pvc', ref: 'web-data', sources: [] },
    {
      name: 'token',
      kind: 'projected',
      ref: '',
      sources: [
        { kind: 'configmap', name: 'ca' },
        { kind: 'serviceAccountToken', name: '' },
      ],
    },
  ],
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetPodEnvironment: vi.fn().mockResolvedValue(env),
    GetRefValue: vi
      .fn()
      .mockImplementation(async (_c: string, _n: string, kind: string) =>
        kind === 'secret' ? 's3cr3t!' : 'dark',
      ),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  delete (window as unknown as { go?: unknown }).go
})

const mount = () => render(<PodEnvironment clusterId="c1" ns="shop" pod="web-1" />)

describe('PodEnvironment', () => {
  it('lists references, literals and mounts; ConfigMap values load, Secret values stay masked', async () => {
    mount()
    expect(await screen.findByText('production')).toBeTruthy() // literals are in the pod spec anyway
    expect(screen.getByText('DB_PASSWORD')).toBeTruthy()
    expect(screen.getByText('/ password')).toBeTruthy()
    expect(screen.getByText('status.podIP')).toBeTruthy()
    expect(screen.getByText(/with prefix/)).toBeTruthy()
    expect(screen.getByText('/etc/tls')).toBeTruthy()
    expect(screen.getByText('init')).toBeTruthy() // the init container is marked
    expect(await screen.findByText('dark')).toBeTruthy() // a ConfigMap value is fetched by itself
    expect(screen.queryByText('s3cr3t!')).toBeNull()
    expect(screen.getAllByLabelText('hidden secret value').length).toBeGreaterThan(0)
    // the only fetch is the ConfigMap key; no Secret is read until you ask
    expect(App.GetRefValue).toHaveBeenCalledTimes(1)
    expect(App.GetRefValue).toHaveBeenCalledWith('c1', 'shop', 'configmap', 'app-config', 'theme')
    // a mount names where it comes from; the full volume list is one click away
    expect(screen.getByText('tls-cert')).toBeTruthy()
    expect(screen.getByText('read-only')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Volumes/ }))
    expect(screen.getByText('web-data')).toBeTruthy()
  })

  it('shows one value on request, can hide and copy it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    mount()
    await screen.findByText('DB_PASSWORD')
    fireEvent.click(screen.getAllByRole('button', { name: /^Show$/ })[0]!)
    expect(await screen.findByText('s3cr3t!')).toBeTruthy()
    expect(App.GetRefValue).toHaveBeenCalledWith('c1', 'shop', 'secret', 'db', 'password')
    fireEvent.click(screen.getAllByRole('button', { name: /Copy/ })[0]!)
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('s3cr3t!'))
    fireEvent.click(screen.getByRole('button', { name: /Hide/ }))
    expect(screen.queryByText('s3cr3t!')).toBeNull()
  })

  it('shows and hides every Secret value from the section header', async () => {
    App.GetRefValue!.mockImplementation(async (_c, _n, _k, name: string) => `v-${name}`)
    mount()
    await screen.findByText('DB_PASSWORD')
    expect(await screen.findByText('v-app-config')).toBeTruthy() // ConfigMap: already there
    expect(screen.queryByText('v-db')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Show secrets/ }))
    expect(await screen.findByText('v-db')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Hide secrets/ }))
    await waitFor(() => expect(screen.queryByText('v-db')).toBeNull())
    expect(screen.getByText('v-app-config')).toBeTruthy() // only secrets are hidden again
  })

  it('hides a revealed value by itself after 30 seconds', async () => {
    mount()
    await screen.findByText('DB_PASSWORD')
    vi.useFakeTimers()
    fireEvent.click(screen.getAllByRole('button', { name: /^Show$/ })[0]!)
    await act(async () => {
      await Promise.resolve()
    })
    expect(screen.getByText('s3cr3t!')).toBeTruthy()
    await act(async () => {
      vi.advanceTimersByTime(30_001)
    })
    expect(screen.queryByText('s3cr3t!')).toBeNull()
  })

  it('tells you why a value cannot be shown, instead of failing silently', async () => {
    App.GetRefValue!.mockRejectedValue(new Error('secrets "db" is forbidden'))
    mount()
    await screen.findByText('DB_PASSWORD')
    fireEvent.click(screen.getAllByRole('button', { name: /^Show$/ })[0]!)
    expect((await screen.findAllByText(/is forbidden/)).length).toBeGreaterThan(0)
  })

  it('a reference opens the Secret list filtered to that name', async () => {
    const select = vi.fn().mockResolvedValue(undefined)
    const setSearch = vi.fn()
    useExplorer.setState({ select, setSearch } as never)
    mount()
    await screen.findByText('DB_PASSWORD')
    fireEvent.click(screen.getByTitle('Open secret db'))
    await waitFor(() => expect(setSearch).toHaveBeenCalledWith('db'))
    expect(select).toHaveBeenCalledWith('core/v1/secrets')
  })

  it('copes with a pod that has nothing to show, and with a failing request', async () => {
    App.GetPodEnvironment!.mockResolvedValue({ containers: null, volumes: null })
    const { unmount } = mount()
    expect(await screen.findByText(/sets no environment variables/)).toBeTruthy()
    unmount()
    App.GetPodEnvironment!.mockRejectedValue(new Error('pods "web-1" not found'))
    mount()
    expect(await screen.findByText(/not found/)).toBeTruthy()
  })
})
