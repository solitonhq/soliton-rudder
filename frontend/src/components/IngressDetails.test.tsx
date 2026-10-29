import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IngressInfo, Resource } from '../lib/types'
import { useUI } from '../store/ui'
import { DetailPanel } from './DetailPanel'
import { IngressCell, canOpen, literalPath, openLink, ruleURL } from './IngressDetails'

afterEach(() => {
  cleanup()
  delete (window as unknown as { go?: unknown }).go
  delete (window as unknown as { runtime?: unknown }).runtime
})

describe('literalPath', () => {
  it('keeps plain paths and cuts regex paths at the first pattern character', () => {
    expect(literalPath('/')).toBe('/')
    expect(literalPath('/api/v1')).toBe('/api/v1')
    expect(literalPath('/api(/|$)(.*)')).toBe('/api')
    expect(literalPath('/foo/(.*)')).toBe('/foo/')
    expect(literalPath('/something.*')).toBe('/something')
    expect(literalPath('/.*')).toBe('/')
    expect(literalPath('/a b')).toBe('/a%20b')
    expect(literalPath('(default)')).toBe('')
    expect(literalPath('')).toBe('')
  })
})

describe('ruleURL', () => {
  it('builds addresses the system will accept, and none for hosts that have no single address', () => {
    expect(ruleURL('shop.example.com', true, '/api(/|$)(.*)')).toBe('https://shop.example.com/api')
    expect(ruleURL('*.example.com', true, '/')).toBe('') // wildcard
    expect(ruleURL('bad host', false, '/')).toBe('')
    expect(ruleURL('', false, '/x', '2001:db8::1')).toBe('http://[2001:db8::1]/x')
    expect(ruleURL('', true, '/x', 'lb.example.net')).toBe('https://lb.example.net/x')
  })
})

describe('openLink', () => {
  let opened: ReturnType<typeof vi.fn>
  beforeEach(() => {
    opened = vi.fn()
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
    useUI.setState({ toasts: [] })
  })
  it('accepts web addresses and refuses what Wails would silently drop, saying so', () => {
    expect(canOpen('https://a.com/x?y=1&z=2')).toBe(true)
    expect(canOpen('https://a.com/api(/|$)')).toBe(false)
    expect(canOpen('https://*.a.com')).toBe(false)
    expect(canOpen('javascript:alert(1)')).toBe(false)
    expect(canOpen('not a url')).toBe(false)
    openLink('https://a.com/ok')
    expect(opened).toHaveBeenCalledWith('https://a.com/ok')
    openLink('https://a.com/bad(1)')
    expect(opened).toHaveBeenCalledTimes(1)
    expect(useUI.getState().toasts.at(-1)?.text).toContain('Cannot open')
  })
})

const ing: IngressInfo = {
  name: 'web',
  namespace: 'shop',
  class: 'nginx',
  created: new Date().toISOString(),
  addresses: ['203.0.113.7'],
  issues: [],
  tls: [],
  rules: [
    {
      host: 'shop.example.com',
      secure: true,
      paths: [
        {
          path: '/api(/|$)(.*)',
          pathType: 'ImplementationSpecific',
          service: 'api',
          port: '8080',
          resource: '',
          clusterIP: '10.96.0.12',
          serviceType: 'ClusterIP',
          found: true,
          ready: 1,
          total: 1,
          issue: '',
          pods: [{ name: 'api-1', ready: true, ip: '10.244.1.5' }],
        },
      ],
    },
  ],
}

describe('the Ingress detail panel (Overview tab)', () => {
  let opened: ReturnType<typeof vi.fn>
  beforeEach(() => {
    opened = vi.fn()
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
    ;(window as unknown as { go: unknown }).go = {
      main: {
        App: {
          GetResourceMeta: vi.fn().mockResolvedValue({
            created: new Date().toISOString(),
            labels: {},
            annotations: {},
            owners: [],
          }),
          GetIngressMap: vi.fn().mockResolvedValue({ ingresses: [ing] }),
          GetHealth: vi
            .fn()
            .mockResolvedValue({ status: 'healthy', summary: 'ok', checks: [], hints: [] }),
          GetMetricsSeries: vi.fn().mockResolvedValue({ series: [] }),
          GetUsage: vi.fn().mockResolvedValue([]),
          GetResourceEvents: vi.fn().mockResolvedValue([]),
          GetTimeline: vi.fn().mockResolvedValue([]),
        },
      },
    }
  })

  const resource = {
    key: 'networking.k8s.io/v1/ingresses',
    group: 'networking.k8s.io',
    version: 'v1',
    name: 'ingresses',
    kind: 'Ingress',
    namespaced: true,
  } as Resource
  const panel = () =>
    render(
      <DetailPanel
        clusterId="c1"
        resource={resource}
        target={{ uid: 'u1', ns: 'shop', name: 'web' }}
        tab="overview"
        onTab={() => {}}
        onClose={() => {}}
      />,
    )

  it('shows the routing section with addresses and URLs that open in the browser', async () => {
    panel()
    expect(await screen.findByText('Routing')).toBeTruthy()
    expect(await screen.findByText('203.0.113.7')).toBeTruthy()
    expect(screen.getByText('10.96.0.12')).toBeTruthy()
    expect(screen.getByText('10.244.1.5')).toBeTruthy()
    const link = await screen.findByRole('button', { name: 'https://shop.example.com/api' })
    fireEvent.click(link)
    expect(opened).toHaveBeenLastCalledWith('https://shop.example.com/api') // the regex path was reduced to its literal part
    fireEvent.click(screen.getByLabelText('Open 203.0.113.7'))
    expect(opened).toHaveBeenLastCalledWith('http://203.0.113.7')
  })

  it('has no routing section for other kinds', async () => {
    render(
      <DetailPanel
        clusterId="c1"
        resource={{ ...resource, name: 'services', kind: 'Service', key: 'core/v1/services' }}
        target={{ uid: 'u2', ns: 'shop', name: 'web' }}
        tab="overview"
        onTab={() => {}}
        onClose={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Created')).toBeTruthy())
    expect(screen.queryByText('Routing')).toBeNull()
  })
})

describe('section order in the Routing panel', () => {
  it('puts the addresses last, after URLs, backends and TLS', async () => {
    ;(window as unknown as { go: unknown }).go = {
      main: {
        App: {
          GetResourceMeta: vi.fn().mockResolvedValue({
            created: new Date().toISOString(),
            labels: {},
            annotations: {},
            owners: [],
          }),
          GetIngressMap: vi.fn().mockResolvedValue({
            ingresses: [
              {
                ...ing,
                tls: [
                  {
                    hosts: ['shop.example.com'],
                    secretName: 'shop-tls',
                    issuer: 'R3',
                    notAfter: '2027-01-01T00:00:00Z',
                    daysLeft: 40,
                    names: [],
                    error: '',
                  },
                ],
              },
            ],
          }),
          GetHealth: vi
            .fn()
            .mockResolvedValue({ status: 'healthy', summary: 'ok', checks: [], hints: [] }),
          GetMetricsSeries: vi.fn().mockResolvedValue({ series: [] }),
          GetUsage: vi.fn().mockResolvedValue([]),
          GetResourceEvents: vi.fn().mockResolvedValue([]),
          GetTimeline: vi.fn().mockResolvedValue([]),
        },
      },
    }
    const resource = {
      key: 'networking.k8s.io/v1/ingresses',
      group: 'networking.k8s.io',
      version: 'v1',
      name: 'ingresses',
      kind: 'Ingress',
      namespaced: true,
    } as Resource
    const { container } = render(
      <DetailPanel
        clusterId="c1"
        resource={resource}
        target={{ uid: 'u1', ns: 'shop', name: 'web' }}
        tab="overview"
        onTab={() => {}}
        onClose={() => {}}
      />,
    )
    await screen.findByText('203.0.113.7')
    const titles = [...container.querySelectorAll('h4')].map((h) => h.textContent)
    expect(titles).toEqual(['URLs', 'Backends', 'TLS', 'Addresses'])
  })
})

describe('IngressCell (Hosts / Address columns of the Ingresses table)', () => {
  let opened: ReturnType<typeof vi.fn>
  beforeEach(() => {
    opened = vi.fn()
    ;(window as unknown as { runtime: unknown }).runtime = { BrowserOpenURL: opened }
  })

  it('makes every host a link that opens in the browser without selecting the row', () => {
    const rowClick = vi.fn()
    render(
      <div onClick={rowClick}>
        <IngressCell text="shop.example.com,api.example.com" secure={false} />
      </div>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'api.example.com' }))
    expect(opened).toHaveBeenCalledWith('http://api.example.com/')
    expect(rowClick).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'shop.example.com' }))
    expect(opened).toHaveBeenLastCalledWith('http://shop.example.com/')
  })

  it('uses https when the ingress listens on 443, and handles IP addresses', () => {
    render(<IngressCell text="203.0.113.7, 198.51.100.2" secure />)
    fireEvent.click(screen.getByRole('button', { name: '203.0.113.7' }))
    expect(opened).toHaveBeenLastCalledWith('https://203.0.113.7/')
  })

  it('leaves wildcard hosts and empty values as plain text', () => {
    render(<IngressCell text="*.example.com,<none>,shop.example.com" secure={false} />)
    expect(screen.queryByRole('button', { name: '*.example.com' })).toBeNull()
    expect(screen.queryByRole('button', { name: '<none>' })).toBeNull()
    expect(screen.getByText('*.example.com')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'shop.example.com' })).toBeTruthy()
  })
})
