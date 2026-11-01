import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TimelineEvent } from '../lib/types'
import { EventsPanel, EventsSummary } from './Insights'

const ev = (o: Partial<TimelineEvent>): TimelineEvent => ({
  type: 'Normal',
  reason: 'Started',
  message: 'Started container web',
  count: 1,
  last: new Date().toISOString(),
  source: 'kubelet',
  object: 'Pod/web-1',
  ...o,
})
afterEach(cleanup)

describe('EventsPanel', () => {
  const events = [
    ev({
      type: 'Warning',
      reason: 'BackOff',
      message: 'Back-off restarting failed container',
      count: 9,
    }),
    ev({ reason: 'Pulled', message: 'Image already present', object: 'Pod/web-2' }),
  ]

  it('lists every event, with repeat counts and the object when it is not the open one', () => {
    render(<EventsPanel events={events} loaded error="" objectName="web-1" />)
    expect(screen.getByText('2 events')).toBeTruthy()
    expect(screen.getByText(/1 warning/)).toBeTruthy()
    expect(screen.getByText(/×9/)).toBeTruthy()
    expect(screen.getByText('Pod/web-2')).toBeTruthy() // an event of a related object names it
    expect(screen.queryByText('Pod/web-1')).toBeNull() // the open object's own events do not repeat its name
  })

  it('filters to warnings and says so when there are none', () => {
    const { rerender } = render(<EventsPanel events={events} loaded error="" objectName="web-1" />)
    fireEvent.click(screen.getByRole('button', { name: 'Warnings' }))
    expect(screen.getByText('BackOff')).toBeTruthy()
    expect(screen.queryByText('Pulled')).toBeNull()
    rerender(<EventsPanel events={[events[1]!]} loaded error="" objectName="web-1" />)
    expect(screen.getByText(/No warnings/)).toBeTruthy()
  })

  it('handles loading, errors and no events', () => {
    const { rerender } = render(<EventsPanel events={[]} loaded={false} error="" objectName="x" />)
    expect(screen.getByText('Loading…')).toBeTruthy()
    rerender(<EventsPanel events={[]} loaded error="boom" objectName="x" />)
    expect(screen.getByText('boom')).toBeTruthy()
    rerender(<EventsPanel events={[]} loaded error="" objectName="x" />)
    expect(screen.getByText('No recent events')).toBeTruthy()
  })
})

describe('EventsSummary', () => {
  it('teases the latest warning and opens the Events tab', () => {
    const onOpen = vi.fn()
    render(
      <EventsSummary
        loaded
        onOpen={onOpen}
        events={[
          ev({ reason: 'Pulled' }),
          ev({ type: 'Warning', reason: 'Unhealthy', message: 'Readiness probe failed' }),
        ]}
      />,
    )
    expect(screen.getByText(/2/)).toBeTruthy()
    expect(screen.getByText('Unhealthy')).toBeTruthy() // the warning wins over the newer normal event
    fireEvent.click(screen.getByRole('button', { name: 'View all' }))
    expect(onOpen).toHaveBeenCalled()
  })

  it('renders nothing until the events are loaded', () => {
    const { container } = render(<EventsSummary loaded={false} onOpen={vi.fn()} events={[]} />)
    expect(container.firstChild).toBeNull()
  })
})
