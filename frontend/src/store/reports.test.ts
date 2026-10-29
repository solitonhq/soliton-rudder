import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FzReport, FzSummary } from '../lib/types'
import { startReportEvents, useReports } from './reports'

const sum = (id: string, at: number, score: number): FzSummary =>
  ({
    id,
    clusterId: 'c1',
    startedAt: at,
    durationMs: 1,
    score,
    serverVersion: 'v1',
    counts: { critical: 0, high: 0, medium: 0, low: 0, info: 0 },
    categories: [],
  }) as FzSummary
const rep = (id: string): FzReport =>
  ({ ...sum(id, 2, 90), clusterName: 'c', stats: {} as never, notes: [], rules: [] }) as FzReport

let handlers: Record<string, (d: unknown) => unknown>
let rows: FzSummary[]
beforeEach(() => {
  handlers = {}
  rows = [sum('old', 1, 70)]
  ;(window as unknown as { runtime: unknown }).runtime = {
    EventsOn: (name: string, cb: (d: unknown) => unknown) => {
      handlers[name] = cb
      return () => {}
    },
  }
  ;(window as unknown as { go: unknown }).go = {
    main: {
      App: {
        ListAssessments: vi.fn(async () => rows),
        AssessmentRunning: vi.fn(async () => ''),
        GetAssessment: vi.fn(async (id: string) => rep(id)),
        CompareAssessments: vi.fn(async () => ({
          from: sum('old', 1, 70),
          to: sum('new', 2, 90),
          scoreDelta: 20,
          new: [],
          resolved: [],
          unchanged: 0,
          ruleDeltas: [],
        })),
        StartAssessment: vi.fn(async () => 'new'),
      },
    },
  }
  useReports.setState({
    clusterId: null,
    history: [],
    selectedId: null,
    report: null,
    diff: null,
    running: null,
    baselineId: null,
  })
})
afterEach(() => {
  delete (window as unknown as { go?: unknown }).go
  delete (window as unknown as { runtime?: unknown }).runtime
})

describe('assessment completion', () => {
  it('shows the new run in the history and chart as soon as it finishes, without reopening the view', async () => {
    startReportEvents()
    await useReports.getState().load('c1')
    expect(useReports.getState().history.map((h) => h.id)).toEqual(['old'])
    await useReports.getState().start()
    expect(useReports.getState().running).not.toBeNull()

    rows = [sum('old', 1, 70), sum('new', 2, 90)] // what the backend lists after saving
    await handlers['fzdone']({ run: 'new', report: 'new', error: '' })

    const s = useReports.getState()
    expect(s.running).toBeNull()
    expect(s.history.map((h) => h.id)).toEqual(['old', 'new'])
    expect(s.selectedId).toBe('new')
    expect(s.report?.id).toBe('new')
  })
})
