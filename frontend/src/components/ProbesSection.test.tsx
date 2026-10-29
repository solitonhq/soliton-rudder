import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ForwardRuntime, PodProbes, ProbeInfo } from '../lib/types'
import { ProbesSection, describeProbe, probeCommand } from './ProbesSection'

const probe = (o: Partial<ProbeInfo>): ProbeInfo => ({
  container: 'app',
  kind: 'liveness',
  type: 'http',
  scheme: 'HTTP',
  path: '/healthz',
  port: 8080,
  portName: '',
  host: '',
  headers: [],
  command: [],
  grpcService: '',
  initialDelay: 0,
  period: 10,
  timeout: 1,
  successThreshold: 1,
  failureThreshold: 3,
  ...o,
})
const fwd = (o: Partial<ForwardRuntime> = {}): ForwardRuntime => ({
  id: 'f1',
  clusterId: 'c1',
  namespace: 'shop',
  kind: 'pod',
  name: 'web-1',
  remotePort: 8080,
  localPort: 0,
  auto: false,
  createdAt: 0,
  status: 'active',
  activePort: 54321,
  pod: 'web-1',
  message: '',
  reconnects: 0,
  lastHealedReason: '',
  lastHealedAt: 0,
  ...o,
})
const data: PodProbes = {
  podIP: '10.244.1.5',
  phase: 'Running',
  containers: [
    { name: 'app', ready: true, started: true, restarts: 0, state: 'running' },
    { name: 'db', ready: false, started: true, restarts: 4, state: 'waiting: CrashLoopBackOff' },
    { name: 'plain', ready: true, started: true, restarts: 0, state: 'running' },
  ],
  probes: [
    probe({ kind: 'startup', path: '/started', failureThreshold: 30, period: 2 }),
    probe({ headers: [{ name: 'X-Token', value: "it's" }], scheme: 'HTTPS', initialDelay: 15 }),
    probe({ container: 'db', type: 'tcp', port: 5432, path: '' }),
    probe({
      container: 'db',
      kind: 'readiness',
      type: 'exec',
      port: 0,
      command: ['pg_isready', '-U', 'app'],
    }),
  ],
}

let App: Record<string, ReturnType<typeof vi.fn>>
let opened: ReturnType<typeof vi.fn>
let written: string[]
beforeEach(() => {
  opened = vi.fn()
  written = []
  ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: (t: string) => (written.push(t), Promise.resolve()) },
  })
  App = {
    GetPodProbes: vi.fn().mockResolvedValue(data),
    CheckProbe: vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      durationMs: 12,
      detail: '200 OK',
      body: '{"status":"UP"}',
    }),
    ForwardPodPort: vi.fn().mockResolvedValue(fwd()),
    ListPortForwards: vi.fn().mockResolvedValue([fwd()]),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
  delete (window as unknown as { runtime?: unknown }).runtime
})

const view = () => render(<ProbesSection clusterId="c1" ns="shop" pod="web-1" />)

describe('describeProbe / probeCommand', () => {
  it('describes each kind of probe', () => {
    expect(describeProbe(probe({}))).toBe('GET /healthz :8080')
    expect(describeProbe(probe({ scheme: 'HTTPS' }))).toBe('GET /healthz :8080 (HTTPS)')
    expect(describeProbe(probe({ type: 'tcp', port: 5432 }))).toBe('TCP :5432')
    expect(describeProbe(probe({ type: 'grpc', port: 50051, grpcService: 'health' }))).toBe(
      'gRPC :50051 (health)',
    )
    expect(describeProbe(probe({ type: 'exec', command: ['pg_isready', '-U', 'app'] }))).toBe(
      'exec pg_isready -U app',
    )
    expect(describeProbe(probe({ port: 0, portName: 'http' }))).toContain(':http (undeclared)')
  })
  it('builds a command that reproduces it from inside the cluster, quoting header values', () => {
    expect(probeCommand(probe({}), '10.0.0.1')).toBe('curl -is http://10.0.0.1:8080/healthz')
    expect(
      probeCommand(
        probe({ scheme: 'HTTPS', headers: [{ name: 'X-Token', value: "it's" }] }),
        '10.0.0.1',
      ),
    ).toBe("curl -isk -H 'X-Token: it'\\''s' https://10.0.0.1:8080/healthz")
    expect(probeCommand(probe({ type: 'tcp', port: 5432 }), '10.0.0.1')).toBe(
      'nc -vz 10.0.0.1 5432',
    )
    expect(probeCommand(probe({ type: 'exec', command: ['a', 'b'] }), '10.0.0.1')).toBe('a b')
  })
})

describe('ProbesSection', () => {
  it('lists every container with its state and probes, and says when there are none', async () => {
    view()
    expect(await screen.findByText('GET /healthz :8080 (HTTPS)')).toBeTruthy()
    expect(screen.getByText('GET /started :8080')).toBeTruthy()
    expect(screen.getByText('TCP :5432')).toBeTruthy()
    expect(screen.getByText('exec pg_isready -U app')).toBeTruthy()
    expect(screen.getByText('waiting: CrashLoopBackOff')).toBeTruthy()
    expect(screen.getByText('4 restarts')).toBeTruthy()
    expect(screen.getByText(/No probes: Kubernetes treats this container as ready/)).toBeTruthy()
    expect(screen.getByText(/fails after 30/)).toBeTruthy()
    expect(screen.getByText(/starts after 15s/)).toBeTruthy()
  })

  it('runs a probe on request and shows the verdict, timing and body', async () => {
    view()
    const checks = await screen.findAllByRole('button', { name: /Check/ })
    fireEvent.click(checks[0])
    await waitFor(() =>
      expect(App.CheckProbe).toHaveBeenCalledWith('c1', 'shop', 'web-1', {
        type: 'http',
        scheme: 'HTTP',
        port: 8080,
        path: '/started',
        headers: [],
      }),
    )
    expect(await screen.findByText('Success')).toBeTruthy()
    expect(screen.getByText('12 ms')).toBeTruthy()
    expect(screen.getByText('{"status":"UP"}')).toBeTruthy()
  })

  it('shows a failed probe and an error from the backend', async () => {
    App.CheckProbe.mockResolvedValueOnce({
      ok: false,
      status: 503,
      durationMs: 3,
      detail: '503 Service Unavailable',
      body: 'not ready',
    })
    view()
    fireEvent.click((await screen.findAllByRole('button', { name: /Check/ }))[0])
    expect(await screen.findByText('Failure')).toBeTruthy()
    App.CheckProbe.mockRejectedValueOnce(new Error('could not reach the pod'))
    fireEvent.click(screen.getAllByRole('button', { name: /Check/ })[1])
    expect(await screen.findByText('could not reach the pod')).toBeTruthy()
  })

  it('forwards the port and opens the endpoint in the browser, with the probe path and scheme', async () => {
    view()
    const opens = await screen.findAllByRole('button', { name: /Open/ })
    fireEvent.click(opens[1]) // the HTTPS liveness probe
    await waitFor(() =>
      expect(App.ForwardPodPort).toHaveBeenCalledWith('c1', 'shop', 'web-1', 8080),
    )
    await waitFor(() => expect(opened).toHaveBeenCalledWith('https://localhost:54321/healthz'))
  })

  it('copies a command that reproduces the probe', async () => {
    view()
    fireEvent.click((await screen.findAllByLabelText(/Copy command for Liveness/))[0])
    await waitFor(() => expect(written[0]).toContain("curl -isk -H 'X-Token:"))
    expect(written[0]).toContain('https://10.244.1.5:8080/healthz')
  })

  it('offers no Check or Open for exec probes, and points to Shell', async () => {
    view()
    await screen.findByText('exec pg_isready -U app')
    expect(screen.getByText(/use Shell to try it/)).toBeTruthy()
    // 3 runnable probes (startup http, liveness http, tcp), exec has none; Open only for the two http ones
    expect(screen.getAllByRole('button', { name: /Check/ }).length).toBe(3)
    expect(screen.getAllByRole('button', { name: /Open/ }).length).toBe(2)
  })

  it('renders nothing while the pod cannot be read', async () => {
    App.GetPodProbes.mockRejectedValue(new Error('gone'))
    const { container } = view()
    await new Promise((r) => setTimeout(r, 30))
    expect(container.textContent).toBe('')
  })
})
