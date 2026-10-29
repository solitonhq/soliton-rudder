import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { RowStore } from '../lib/rowstore'
import type { AccessInfo } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { useUI } from '../store/ui'
import { NamespacePicker } from './NamespacePicker'

const limited: AccessInfo = {
  clusterWide: false,
  canListNamespaces: false,
  namespaces: ['team-a', 'team-b'],
  source: 'kubeconfig',
  note: '',
  configured: [],
}

beforeEach(() => {
  useUI.setState({ dialog: null })
  useExplorer.setState({ clusterId: 'c1', namespaces: [], nsStore: new RowStore(), access: null })
})
afterEach(cleanup)

describe('NamespacePicker', () => {
  it('lists the cluster namespaces with full access', () => {
    const store = new RowStore()
    store.apply({
      sub: 'x',
      reset: true,
      upsert: [
        { uid: '1', ns: '', name: 'kube-system', created: '', cells: [] },
        { uid: '2', ns: '', name: 'shop', created: '', cells: [] },
      ],
      delete: [],
    })
    useExplorer.setState({
      nsStore: store,
      access: { ...limited, clusterWide: true, source: 'cluster', namespaces: [] },
    })
    render(<NamespacePicker />)
    fireEvent.click(screen.getByText('All namespaces'))
    expect(screen.getByText('shop')).toBeTruthy()
    expect(screen.queryByText('Choose which namespaces to use…')).toBeNull()
  })

  it('falls back to the namespaces the user can read when the list cannot be read', () => {
    useExplorer.setState({ access: limited })
    render(<NamespacePicker />)
    fireEvent.click(screen.getByText('My namespaces'))
    expect(screen.getByText('team-a')).toBeTruthy()
    expect(screen.getByText('team-b')).toBeTruthy()
    expect(screen.getByText('All my namespaces')).toBeTruthy()
  })

  it('offers the access settings only for limited users', () => {
    useExplorer.setState({ access: limited })
    render(<NamespacePicker />)
    fireEvent.click(screen.getByText('My namespaces'))
    fireEvent.click(screen.getByText('Choose which namespaces to use…'))
    expect(useUI.getState().dialog).toEqual({ kind: 'access', clusterId: 'c1' })
  })
})
