import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { normalizeReport } from '../lib/backend'
import type { FzReport } from '../lib/types'
import { isIP, ruleURL } from '../components/IngressDetails'
import { Ingresses } from './Ingresses'

afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
})

// What the Go side used to send when a Service had an EndpointSlice with no endpoints: pods: null
const withNulls = {
  ingresses: [
    {
      name: 'web',
      namespace: 'shop',
      class: 'nginx',
      created: new Date().toISOString(),
      addresses: null,
      issues: null,
      tls: null,
      rules: [
        {
          host: 'shop.example.com',
          secure: false,
          paths: [
            {
              path: '/',
              pathType: 'Prefix',
              service: 'frontend',
              port: '80',
              resource: '',
              found: true,
              ready: 0,
              total: 0,
              pods: null,
              issue: 'no endpoints',
            },
          ],
        },
      ],
    },
  ],
}

describe('Ingress map', () => {
  beforeEach(() => {
    ;(window as unknown as { go: unknown }).go = {
      main: { App: { GetIngressMap: () => Promise.resolve(withNulls) } },
    }
  })

  it('does not crash when the backend sends null arrays', async () => {
    render(<Ingresses clusterId="c1" />)
    expect(await screen.findByText('frontend')).toBeTruthy()
    expect(screen.getAllByText('no endpoints').length).toBeGreaterThan(0) // the row and the issue list
  })
})

describe('normalizeReport', () => {
  it('turns null arrays (plugin rules without findings, empty notes) into arrays', () => {
    const raw = {
      notes: null,
      categories: null,
      rules: [{ id: 'p', findings: null }, null].slice(0, 1),
      finops: { namespaces: null, top: null, notes: null },
    } as unknown as FzReport
    const r = normalizeReport(raw)
    expect(r.notes).toEqual([])
    expect(r.categories).toEqual([])
    expect(r.rules[0].findings).toEqual([])
    expect(r.finops).toEqual({ namespaces: [], top: [], notes: [] })
    expect(
      normalizeReport({ ...raw, rules: null, finops: undefined } as unknown as FzReport).rules,
    ).toEqual([])
  })
})

const rich = {
  ingresses: [
    {
      name: 'web',
      namespace: 'shop',
      class: 'nginx',
      created: new Date().toISOString(),
      addresses: ['203.0.113.7', 'lb.example.net'],
      issues: [],
      tls: [
        {
          hosts: ['shop.example.com'],
          secretName: 'shop-tls',
          issuer: 'R3',
          notAfter: '2027-01-01T00:00:00Z',
          daysLeft: 40,
          names: ['shop.example.com'],
          error: '',
        },
      ],
      rules: [
        {
          host: 'shop.example.com',
          secure: true,
          paths: [
            {
              path: '/api',
              pathType: 'Prefix',
              service: 'api',
              port: '8080',
              resource: '',
              clusterIP: '10.96.0.12',
              serviceType: 'ClusterIP',
              found: true,
              ready: 1,
              total: 2,
              issue: '',
              pods: [
                { name: 'api-1', ready: true, ip: '10.244.1.5' },
                { name: 'api-2', ready: false, ip: '10.244.2.9' },
              ],
            },
          ],
        },
        {
          host: '',
          secure: false,
          paths: [
            {
              path: '/health',
              pathType: 'Prefix',
              service: 'hc',
              port: '80',
              resource: '',
              clusterIP: 'None',
              serviceType: 'ClusterIP',
              found: true,
              ready: 1,
              total: 1,
              issue: '',
              pods: [{ name: 'hc-1', ready: true, ip: '10.244.3.3' }],
            },
          ],
        },
      ],
    },
  ],
}

describe('Ingress URLs open in the browser', () => {
  let opened: ReturnType<typeof vi.fn>
  beforeEach(() => {
    opened = vi.fn()
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
    ;(window as unknown as { go: unknown }).go = {
      main: { App: { GetIngressMap: () => Promise.resolve(rich) } },
    }
  })
  afterEach(() => {
    delete (window as unknown as { runtime?: unknown }).runtime
  })

  it('opens the host, a path and the icon with the right scheme', async () => {
    render(<Ingresses clusterId="c1" />)
    fireEvent.click(await screen.findByRole('button', { name: 'shop.example.com', hidden: false }))
    expect(opened).toHaveBeenLastCalledWith('https://shop.example.com/')
    fireEvent.click(screen.getByRole('button', { name: '/api' }))
    expect(opened).toHaveBeenLastCalledWith('https://shop.example.com/api')
    fireEvent.click(screen.getByLabelText('Open https://shop.example.com/api'))
    expect(opened).toHaveBeenCalledTimes(3)
  })

  it('uses the load balancer address for a rule without a host', async () => {
    render(<Ingresses clusterId="c1" />)
    fireEvent.click(await screen.findByRole('button', { name: '/health' }))
    expect(opened).toHaveBeenLastCalledWith('http://203.0.113.7/health')
  })
})

describe('Ingress details', () => {
  let opened: ReturnType<typeof vi.fn>
  let written: string[]
  beforeEach(() => {
    opened = vi.fn()
    written = []
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (t: string) => {
          written.push(t)
          return Promise.resolve()
        },
      },
    })
    ;(window as unknown as { go: unknown }).go = {
      main: { App: { GetIngressMap: () => Promise.resolve(rich) } },
    }
  })
  afterEach(() => {
    delete (window as unknown as { runtime?: unknown }).runtime
  })

  it('is closed until the record is clicked, then lists addresses, URLs, service and pod IPs and TLS', async () => {
    render(<Ingresses clusterId="c1" />)
    const header = await screen.findByLabelText('Show details of web')
    expect(screen.queryByText('Addresses')).toBeNull()
    fireEvent.click(header)
    expect(screen.getByText('Addresses')).toBeTruthy()
    expect(screen.getByText('IP')).toBeTruthy()
    expect(screen.getByText('Hostname')).toBeTruthy() // told apart
    expect(screen.getByText('10.96.0.12')).toBeTruthy() // service IP
    expect(screen.getByText('headless')).toBeTruthy()
    expect(screen.getByText('10.244.1.5')).toBeTruthy() // pod IPs
    expect(screen.getByText('10.244.2.9')).toBeTruthy()
    expect(screen.getByText(/40 days left/)).toBeTruthy()
    expect(screen.getByText(/issued by R3/)).toBeTruthy()
    expect(header.getAttribute('aria-expanded')).toBe('true')
    fireEvent.click(header)
    expect(screen.queryByText('Addresses')).toBeNull()
  })

  it('copies an address and opens it in the browser', async () => {
    render(<Ingresses clusterId="c1" />)
    fireEvent.click(await screen.findByLabelText('Show details of web'))
    fireEvent.click(screen.getByLabelText('Copy 203.0.113.7'))
    await waitFor(() => expect(written).toEqual(['203.0.113.7']))
    fireEvent.click(screen.getByLabelText('Open lb.example.net'))
    expect(opened).toHaveBeenLastCalledWith('http://lb.example.net')
  })

  it('works from the keyboard and says so when no address has been assigned', async () => {
    ;(window as unknown as { go: unknown }).go = {
      main: {
        App: {
          GetIngressMap: () =>
            Promise.resolve({ ingresses: [{ ...rich.ingresses[0], addresses: [] }] }),
        },
      },
    }
    render(<Ingresses clusterId="c1" />)
    const header = await screen.findByLabelText('Show details of web')
    fireEvent.keyDown(header, { key: 'Enter' })
    expect(screen.getByText(/No address assigned yet/)).toBeTruthy()
  })
})

describe('helpers', () => {
  it('isIP tells addresses from hostnames', () => {
    expect(isIP('203.0.113.7')).toBe(true)
    expect(isIP('2001:db8::1')).toBe(true)
    expect(isIP('lb.example.net')).toBe(false)
    expect(isIP('a1b2.elb.amazonaws.com')).toBe(false)
  })
  it('ruleURL picks the scheme, falls back to the address, and ignores the (default) path', () => {
    expect(ruleURL('a.com', true, '/x')).toBe('https://a.com/x')
    expect(ruleURL('a.com', false, '/')).toBe('http://a.com/')
    expect(ruleURL('', false, '/x', '10.0.0.1')).toBe('http://10.0.0.1/x')
    expect(ruleURL('', false, '/x')).toBe('')
    expect(ruleURL('a.com', false, '(default)')).toBe('http://a.com')
  })
})
