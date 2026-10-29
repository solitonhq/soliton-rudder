import { describe, expect, it } from 'vitest'
import { categorize } from './categories'
import type { Resource } from './types'

const r = (group: string, name: string, kind = name): Resource => ({
  key: `${group || 'core'}/v1/${name}`,
  group: group || 'core',
  version: 'v1',
  name,
  kind,
  namespaced: true,
  shortNames: null,
  verbs: [],
})

describe('categorize', () => {
  it('orders built-ins and groups CRDs by API group', () => {
    const cats = categorize([
      r('core', 'services'),
      r('apps', 'deployments'),
      r('core', 'pods'),
      r('argoproj.io', 'applications'),
      r('argoproj.io', 'appprojects'),
      r('coordination.k8s.io', 'leases'),
    ])
    // CRDs and leftover built-ins share ONE header, sub-grouped by API group
    expect(cats.map((c) => c.label)).toEqual(['Workloads', 'Network', 'Custom Resources'])
    expect(cats[0].items.map((i) => i.name)).toEqual(['pods', 'deployments'])
    const custom = cats[2]
    expect(custom.groups!.map((g) => g.label)).toEqual(['argoproj.io', 'Other built-in'])
    expect(custom.groups![0].items).toHaveLength(2)
    expect(custom.items).toHaveLength(3)
  })
  it('does not steal CRD resources named like built-ins', () => {
    const cats = categorize([r('core', 'services'), r('serving.knative.dev', 'services')])
    expect(cats.find((c) => c.id === 'network')!.items).toHaveLength(1)
    const custom = cats.find((c) => c.id === 'custom')!
    expect(custom.groups!.find((g) => g.label === 'serving.knative.dev')!.items).toHaveLength(1)
  })
})

describe('category order', () => {
  it('is Cluster, Workloads, Network, Config, Storage, Access Control, then Custom Resources', () => {
    const cats = categorize([
      r('core', 'persistentvolumeclaims'),
      r('rbac.authorization.k8s.io', 'roles'),
      r('core', 'configmaps'),
      r('core', 'services'),
      r('core', 'pods'),
      r('core', 'nodes'),
      r('argoproj.io', 'applications'),
    ])
    expect(cats.map((c) => c.label)).toEqual([
      'Cluster',
      'Workloads',
      'Network',
      'Config',
      'Storage',
      'Access Control',
      'Custom Resources',
    ])
  })
})
