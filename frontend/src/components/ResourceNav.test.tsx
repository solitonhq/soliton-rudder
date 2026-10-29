import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Resource } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { ResourceNav } from './ResourceNav'

const r = (group: string, name: string, kind: string): Resource => ({
  key: `${group || 'core'}/v1/${name}`,
  group: group || 'core',
  version: 'v1',
  name,
  kind,
  namespaced: true,
  shortNames: null,
  verbs: [],
})

const catalog = [
  r('core', 'pods', 'Pod'),
  r('apps', 'deployments', 'Deployment'),
  r('core', 'services', 'Service'),
  r('argoproj.io', 'applications', 'Application'),
  r('cert-manager.io', 'certificates', 'Certificate'),
  r('coordination.k8s.io', 'leases', 'Lease'),
  r('core', 'nodes', 'Node'),
  r('core', 'namespaces', 'Namespace'),
  r('core', 'configmaps', 'ConfigMap'),
  r('core', 'persistentvolumeclaims', 'PersistentVolumeClaim'),
  r('rbac.authorization.k8s.io', 'roles', 'Role'),
]

beforeEach(() => {
  localStorage.clear()
  useExplorer.setState({
    catalog,
    selectedKey: 'core/v1/pods',
    view: 'resources',
    integrations: null,
  })
})
afterEach(cleanup)

const top = (name: string) => screen.getByRole('button', { name })
/** A category header: its name is followed by the number of kinds in it. */
const category = (name: string) =>
  screen.getByRole('button', { name: new RegExp(`^${name}\\s*\\d+$`) })
const sectionHeader = (label: string) =>
  screen
    .getAllByRole('button')
    .find((b) => b.getAttribute('aria-controls') === `nav-section-${label}`)!
/** Vertical order of elements in the document. */
const order = (...els: HTMLElement[]) =>
  els
    .map((e, i) => [e, i] as const)
    .sort(([a], [b]) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1))
    .map(([, i]) => i)

describe('ResourceNav layout', () => {
  it('has Tools, Integrations and Resources as collapsible sections, in that order', () => {
    render(<ResourceNav />)
    const tools = sectionHeader('tools')
    const integrations = sectionHeader('integrations')
    const resources = sectionHeader('resources')
    for (const h of [tools, integrations, resources])
      expect(h.getAttribute('aria-expanded')).toBe('true')
    expect(order(tools, integrations, resources)).toEqual([0, 1, 2])
    expect(tools.textContent).toContain('Tools')
  })

  it('puts Overview and Applications on top, above all sections, then the categories in the agreed order', () => {
    render(<ResourceNav />)
    const items = [
      top('Overview'),
      top('Applications'),
      sectionHeader('tools'),
      sectionHeader('integrations'),
      sectionHeader('resources'),
      category('Cluster'),
      category('Workloads'),
      category('Network'),
      category('Config'),
      category('Storage'),
      category('Access Control'),
    ]
    expect(order(...items)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
    const custom = screen.getByRole('button', { name: /Custom Resources/ })
    expect(order(items[10], custom)).toEqual([0, 1])
  })

  it('collapses a section and remembers it', () => {
    const { unmount } = render(<ResourceNav />)
    expect(screen.getByRole('button', { name: 'Topology' })).toBeTruthy()
    fireEvent.click(sectionHeader('tools'))
    expect(sectionHeader('tools').getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByRole('button', { name: 'Topology' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Prometheus' })).toBeTruthy() // the others are untouched
    unmount()
    render(<ResourceNav />)
    expect(sectionHeader('tools').getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(sectionHeader('tools'))
    expect(screen.getByRole('button', { name: 'Topology' })).toBeTruthy()
  })

  it('never folds Overview and Applications away, whichever section is collapsed', () => {
    render(<ResourceNav />)
    for (const id of ['tools', 'integrations', 'resources']) fireEvent.click(sectionHeader(id))
    expect(screen.queryByRole('button', { name: /Workloads/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Topology' })).toBeNull()
    expect(top('Overview')).toBeTruthy()
    expect(top('Applications')).toBeTruthy()
    expect(
      screen.queryByRole('button', { name: /^Overview$/ })?.getAttribute('aria-expanded'),
    ).toBeNull() // not a section
  })

  it('marks a collapsed section that contains the page you are on', () => {
    useExplorer.setState({ view: 'topology' })
    render(<ResourceNav />)
    fireEvent.click(sectionHeader('tools'))
    expect(within(sectionHeader('tools')).getByLabelText('contains the current page')).toBeTruthy()
    expect(
      within(sectionHeader('integrations')).queryByLabelText('contains the current page'),
    ).toBeNull()
    expect(top('Overview').getAttribute('aria-current')).toBe('false')
  })

  it('shows the open kind inside a collapsed Resources section as the marker', () => {
    render(<ResourceNav />) // Pods are open
    fireEvent.click(sectionHeader('resources'))
    expect(
      within(sectionHeader('resources')).getByLabelText('contains the current page'),
    ).toBeTruthy()
  })

  it('a filter shows matches even inside a folded Resources section', () => {
    render(<ResourceNav />)
    fireEvent.click(sectionHeader('resources'))
    fireEvent.change(screen.getByPlaceholderText('Filter kinds…'), { target: { value: 'deploy' } })
    expect(screen.getByRole('button', { name: 'Deployments' })).toBeTruthy()
  })

  it('Overview and Applications switch the view', () => {
    const setView = vi.fn()
    useExplorer.setState({ setView } as never)
    render(<ResourceNav />)
    fireEvent.click(top('Overview'))
    fireEvent.click(top('Applications'))
    expect(setView.mock.calls).toEqual([['overview'], ['applications']])
  })

  it('survives unreadable saved state', () => {
    localStorage.setItem('soliton-rudder.nav.collapsed-sections', '{not json')
    render(<ResourceNav />)
    expect(sectionHeader('tools').getAttribute('aria-expanded')).toBe('true')
  })
})

describe('ResourceNav', () => {
  it('shows category headers that are distinct from their items', () => {
    render(<ResourceNav />)
    const header = screen.getByRole('button', { name: /Workloads/ })
    expect(header.getAttribute('aria-expanded')).toBe('true')
    // items are indented under a guide line, headers are not
    const item = screen.getByRole('button', { name: 'Deployments' })
    expect(item.closest('.border-l')).not.toBeNull()
    expect(header.closest('.border-l')).toBeNull()
    expect(screen.getByRole('button', { name: 'Pods' }).getAttribute('aria-current')).toBe('true')
  })

  it('puts every CRD under one collapsed "Custom Resources" header, grouped by API group', () => {
    render(<ResourceNav />)
    const headers = screen.getAllByRole('button').filter((b) => b.hasAttribute('aria-expanded'))
    expect(headers.filter((h) => /Custom Resources/.test(h.textContent ?? ''))).toHaveLength(1)
    const custom = screen.getByRole('button', { name: /Custom Resources/ })
    expect(custom.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText('argoproj.io')).toBeNull() // collapsed by default
    fireEvent.click(custom)
    expect(screen.getByText('argoproj.io')).toBeTruthy()
    expect(screen.getByText('cert-manager.io')).toBeTruthy()
    expect(screen.getByText('Other built-in')).toBeTruthy()
    expect(within(custom.parentElement!).getByRole('button', { name: 'Applications' })).toBeTruthy()
  })

  it('filtering searches inside collapsed groups and expands them', () => {
    render(<ResourceNav />)
    fireEvent.change(screen.getByPlaceholderText('Filter kinds…'), { target: { value: 'certif' } })
    const custom = screen.getByRole('button', { name: /Custom Resources/ })
    expect(within(custom.parentElement!).getByRole('button', { name: 'Certificates' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Deployments' })).toBeNull()
  })
})
