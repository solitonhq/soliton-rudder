import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BBInfo, BBRecord } from '../lib/types'
import { Blackbox, incidentOf } from './Blackbox'

const NOW = Date.now()
const info = (o: Partial<BBInfo> = {}): BBInfo => ({
  config: { enabled: true, namespaces: [], intervalSec: 15 },
  retentionHours: 24,
  maxRecords: 200000,
  status: { running: true, lastRun: NOW, lastError: '', recorded: 12 },
  span: { min: NOW - 3 * 3600_000, max: NOW, count: 42 },
  ...o,
})
const state = (name: string, status: string, reason: string, prev = ''): BBRecord => ({
  id: Math.random(),
  ts: NOW - 600_000,
  kind: 'state',
  key: `Pod/shop/${name}`,
  objKind: 'Pod',
  namespace: 'shop',
  name,
  severity: status === 'error' ? 'error' : status === 'warn' ? 'warn' : 'info',
  status,
  reason,
  message: '',
  prev,
})
const event: BBRecord = {
  id: 9,
  ts: NOW - 60_000,
  kind: 'event',
  key: 'u#1',
  objKind: 'Pod',
  namespace: 'shop',
  name: 'web-1',
  severity: 'warn',
  status: '',
  reason: 'BackOff',
  message: 'Back-off restarting failed container',
  prev: '',
}

let App: Record<string, ReturnType<typeof vi.fn>>
beforeEach(() => {
  App = {
    GetBlackbox: vi.fn().mockResolvedValue(info()),
    BlackboxBuckets: vi.fn().mockResolvedValue(
      Array.from({ length: 120 }, (_, i) => ({
        ts: i,
        info: 0,
        warn: i % 10 === 0 ? 1 : 0,
        error: i === 60 ? 3 : 0,
      })),
    ),
    BlackboxStateAt: vi
      .fn()
      .mockResolvedValue([
        state('ok-1', 'ok', 'Running'),
        state('web-1', 'error', 'CrashLoopBackOff · 3 restarts', 'ok: Running'),
        state('slow', 'warn', 'Pending'),
      ]),
    BlackboxQuery: vi.fn().mockResolvedValue([event]),
    SetBlackbox: vi.fn().mockResolvedValue(info()),
    ClearBlackbox: vi.fn().mockResolvedValue(undefined),
    MatchRunbooks: vi.fn().mockResolvedValue([]),
    GetRunbooks: vi.fn().mockResolvedValue({ builtin: [], custom: '', error: '' }),
  }
  ;(window as unknown as { go: unknown }).go = { main: { App } }
})
afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

describe('Blackbox', () => {
  it('explains itself and starts recording with safe defaults when nothing is recorded', async () => {
    App.GetBlackbox.mockResolvedValue(
      info({
        config: { enabled: false, namespaces: [], intervalSec: 15 },
        span: { min: 0, max: 0, count: 0 },
        status: { running: false, lastRun: 0, lastError: '', recorded: 0 },
      }),
    )
    render(<Blackbox clusterId="c1" />)
    expect(await screen.findByText('Nothing recorded yet')).toBeTruthy()
    expect(screen.getByText('Not recording')).toBeTruthy()
    expect(screen.getByText(/only while Soliton Rudder is open/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Start recording' }))
    await waitFor(() =>
      expect(App.SetBlackbox).toHaveBeenCalledWith(
        'c1',
        { enabled: true, intervalSec: 15, namespaces: [] },
        24,
        200000,
      ),
    )
  })

  it('shows what was failing now, with healthy objects only counted', async () => {
    render(<Blackbox clusterId="c1" />)
    expect(await screen.findByText(/CrashLoopBackOff · 3 restarts/)).toBeTruthy()
    expect(screen.getByText(/1 healthy/)).toBeTruthy()
    expect(screen.getByText(/1 warning/)).toBeTruthy()
    expect(screen.getByText(/1 failing/)).toBeTruthy()
    expect(screen.queryByText('shop/ok-1')).toBeNull()
    expect(screen.getAllByText('Recording').length).toBe(2) // status pill + settings button
    expect(screen.getByText('Live')).toBeTruthy()
    expect(await screen.findByText('Back-off restarting failed container')).toBeTruthy()
  })

  it('travelling back in time reconstructs the state at that moment and offers a way back', async () => {
    render(<Blackbox clusterId="c1" />)
    await screen.findByText(/CrashLoopBackOff/)
    const slider = screen.getByLabelText('Time') as HTMLInputElement
    const target = Math.round((Number(slider.min) + Number(slider.max)) / 2 / 1000) * 1000
    fireEvent.change(slider, { target: { value: String(target) } })
    await waitFor(() => expect(App.BlackboxStateAt).toHaveBeenLastCalledWith('c1', target))
    expect(screen.queryByText('Live')).toBeNull()
    expect(screen.getByText(/Cluster at/)).toBeTruthy()
    await waitFor(() =>
      expect(App.BlackboxQuery).toHaveBeenLastCalledWith(
        'c1',
        expect.objectContaining({ to: target, from: target - 5 * 60_000, newest: true }),
      ),
    )
    fireEvent.click(screen.getByText('Back to live'))
    expect(await screen.findByText('Live')).toBeTruthy()
  })

  it('filters the event stream by severity', async () => {
    render(<Blackbox clusterId="c1" />)
    await screen.findByText(/CrashLoopBackOff/)
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'error' } })
    await waitFor(() =>
      expect(App.BlackboxQuery).toHaveBeenLastCalledWith(
        'c1',
        expect.objectContaining({ minSeverity: 'error' }),
      ),
    )
  })

  it('opens the runbook for the failing object that was clicked', async () => {
    render(<Blackbox clusterId="c1" />)
    fireEvent.click(await screen.findByText(/CrashLoopBackOff · 3 restarts/))
    await waitFor(() =>
      expect(App.MatchRunbooks).toHaveBeenCalledWith({
        kind: 'Pod',
        namespace: 'shop',
        name: 'web-1',
        reason: 'CrashLoopBackOff · 3 restarts',
        status: 'error',
      }),
    )
  })

  it('playback starts at the beginning of the window and can be paused', async () => {
    render(<Blackbox clusterId="c1" />)
    await screen.findByText(/CrashLoopBackOff/)
    fireEvent.click(screen.getByLabelText('Play'))
    expect(screen.queryByText('Live')).toBeNull()
    expect(screen.getByLabelText('Pause')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Pause'))
    expect(screen.getByLabelText('Play')).toBeTruthy()
  })

  it('shows recorder problems instead of pretending all is well', async () => {
    App.GetBlackbox.mockResolvedValue(
      info({
        status: { running: true, lastRun: NOW, lastError: 'connection refused', recorded: 0 },
      }),
    )
    render(<Blackbox clusterId="c1" />)
    expect(await screen.findByText('Recording, with problems')).toBeTruthy()
    expect(screen.getAllByText('connection refused').length).toBeGreaterThan(0)
  })

  it('asks before deleting recordings', async () => {
    render(<Blackbox clusterId="c1" />)
    await screen.findByText(/CrashLoopBackOff/)
    fireEvent.click(screen.getByRole('button', { name: /Recording/ }))
    fireEvent.click(screen.getByRole('button', { name: /Delete recordings/ }))
    expect(App.ClearBlackbox).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: /Yes, delete 42 records/ }))
    await waitFor(() => expect(App.ClearBlackbox).toHaveBeenCalledWith('c1'))
  })
})

describe('incidentOf', () => {
  it('maps a state record and an event record to an incident', () => {
    expect(incidentOf(state('a', 'error', 'OOMKilled'))).toEqual({
      kind: 'Pod',
      namespace: 'shop',
      name: 'a',
      reason: 'OOMKilled',
      status: 'error',
    })
    expect(incidentOf(event).status).toBe('warn')
    expect(incidentOf({ ...event, severity: 'info' }).status).toBe('ok')
  })
})
