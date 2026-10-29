import { useEffect } from 'react'
import { CommandPalette } from './components/CommandPalette'
import { Toasts } from './components/Toasts'
import { AppShell } from './components/AppShell'
import { AddClusterDialog } from './dialogs/AddClusterDialog'
import { EditClusterDialog } from './dialogs/EditClusterDialog'
import { SettingsDialog } from './dialogs/SettingsDialog'
import { useClusters, startConnectionEvents, startHealthPolling } from './store/clusters'
import { startNavigation } from './store/nav'
import { startForwardEvents } from './store/forwards'
import { startPluginEvents } from './store/plugins'
import { startReportEvents } from './store/reports'
import { useDock } from './store/dock'
import { startUpdateEvents, useSettings } from './store/settings'
import { useUI } from './store/ui'
import { AccessDialog } from './dialogs/AccessDialog'
import { CompareDialog } from './dialogs/CompareDialog'
import { ClusterView } from './views/ClusterView'
import { Home } from './views/Home'

export default function App() {
  const loadSettings = useSettings((s) => s.load)
  const loadClusters = useClusters((s) => s.load)
  const activeId = useClusters((s) => s.activeId)
  const activeName = useClusters((s) => s.clusters.find((c) => c.id === s.activeId)?.name)
  const { dialog, close, palette, setPalette } = useUI()

  useEffect(() => {
    startForwardEvents()
    startConnectionEvents()
    const stopNav = startNavigation()
    startReportEvents()
    startPluginEvents()
    startUpdateEvents()
    void loadSettings()
    void loadClusters()
    const stopHealth = startHealthPolling()
    return () => {
      stopNav()
      stopHealth()
    }
  }, [loadSettings, loadClusters])

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === '`') {
        e.preventDefault()
        const d = useDock.getState()
        if (d.tabs.length) d.setExpanded(!d.expanded)
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPalette(!useUI.getState().palette)
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [setPalette])

  return (
    <>
      <AppShell title={activeId ? activeName : 'Home'}>
        {activeId ? <ClusterView id={activeId} /> : <Home />}
      </AppShell>
      {dialog?.kind === 'add' && <AddClusterDialog onClose={close} />}
      {dialog?.kind === 'compare' && <CompareDialog onClose={close} />}
      {dialog?.kind === 'access' && <AccessDialog clusterId={dialog.clusterId} onClose={close} />}
      {dialog?.kind === 'edit' && <EditClusterDialog id={dialog.id} onClose={close} />}
      {palette && <CommandPalette onClose={() => setPalette(false)} />}
      <Toasts />
      {dialog?.kind === 'settings' && <SettingsDialog onClose={close} initialTab={dialog.tab} />}
    </>
  )
}
