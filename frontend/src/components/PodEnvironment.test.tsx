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
    GetRefValue: vi.fn().mockResolvedValue('s3cr3t!'),
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
  it('lists references, literals and mounts, and shows no secret value up front', async () => {
    mount()
    expect(await screen.findByText('production')).toBeTruthy() // literals are in the pod spec anyway
    expect(screen.getByText('DB_PASSWORD')).toBeTruthy()
    expect(screen.getByText('/ password')).toBeTruthy()
    expect(screen.getByText('status.podIP')).toBeTruthy()
    expect(screen.getByText(/with prefix/)).toBeTruthy()
    expect(screen.getByText('/etc/tls')).toBeTruthy()
    expect(screen.getByText('init')).toBeTruthy() // the init container is marked
    expect(screen.getByText('web-data')).toBeTruthy()
    expect(screen.queryByText('s3cr3t!')).toBeNull()
    expect(App.GetRefValue).not.toHaveBeenCalled() // nothing is fetched until you ask
  })

  it('shows one value on request, can hide and copy it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    mount()
    await screen.findByText('DB_PASSWORD')
    fireEvent.click(screen.getAllByRole('button', { name: /Show/ })[0]!)
    expect(await screen.findByText('s3cr3t!')).toBeTruthy()
    expect(App.GetRefValue).toHaveBeenCalledWith('c1', 'shop', 'secret', 'db', 'password')
    fireEvent.click(screen.getByRole('button', { name: /Copy/ }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('s3cr3t!'))
    fireEvent.click(screen.getByRole('button', { name: /Hide/ }))
    expect(screen.queryByText('s3cr3t!')).toBeNull()
  })

  it('hides a revealed value by itself after 30 seconds', async () => {
    mount()
    await screen.findByText('DB_PASSWORD')
    vi.useFakeTimers()
    fireEvent.click(screen.getAllByRole('button', { name: /Show/ })[0]!)
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
    fireEvent.click(screen.getAllByRole('button', { name: /Show/ })[0]!)
    expect(await screen.findByText(/is forbidden/)).toBeTruthy()
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
