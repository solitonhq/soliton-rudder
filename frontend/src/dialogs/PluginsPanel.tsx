import clsx from 'clsx'
import { FolderOpen, FileArchive, RotateCw, ScrollText, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Pill, type PillTone } from '../components/GitOps'
import { Button, Dialog } from '../components/ui'
import { plugins } from '../lib/backend'
import type { PluginInfo, PluginInspection, PluginPermission } from '../lib/types'
import { usePlugins } from '../store/plugins'
import { useUI } from '../store/ui'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const riskCls: Record<PluginPermission['risk'], string> = {
  low: 'bg-surface-2 text-muted',
  medium: 'bg-warning/15 text-warning',
  high: 'bg-danger/15 text-danger',
}

const stateTone: Record<PluginInfo['state'], PillTone> = {
  running: 'success',
  starting: 'warning',
  stopped: 'neutral',
  crashed: 'danger',
  'ui-only': 'accent',
}
const stateLabel: Record<PluginInfo['state'], string> = {
  running: 'Running',
  starting: 'Starting…',
  stopped: 'Stopped',
  crashed: 'Crashed',
  'ui-only': 'UI only',
}

export function PluginsPanel() {
  const list = usePlugins((s) => s.list)
  const refresh = usePlugins((s) => s.refresh)
  const toast = useUI((s) => s.toast)
  const [inspect, setInspect] = useState<{ path: string; data: PluginInspection } | null>(null)

  const pick = async (kind: 'folder' | 'zip') => {
    try {
      const path = await (kind === 'folder' ? plugins.pickFolder() : plugins.pickZip())
      if (!path) return
      setInspect({ path, data: await plugins.inspect(path) })
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">
          Installed plugins ({list.length})
        </h3>
        <div className="flex gap-2">
          <Button onClick={() => void pick('folder')}>
            <FolderOpen size={13} /> Install from folder…
          </Button>
          <Button onClick={() => void pick('zip')}>
            <FileArchive size={13} /> From .zip…
          </Button>
        </div>
      </div>
      {list.length === 0 && (
        <div className="rounded-lg border border-dashed border-border p-6 text-center text-[13px] text-muted">
          No plugins installed. Plugins can add panels, commands and custom assessment checks.
          <div className="mt-1 text-xs">
            An example lives in <span className="font-mono">examples/plugins/namespace-report</span>
            ; see <span className="font-mono">docs/PLUGINS.md</span> to write your own.
          </div>
        </div>
      )}
      <div className="space-y-3">
        {list.map((p) => (
          <PluginCard key={p.manifest.id} p={p} onChanged={() => void refresh()} />
        ))}
      </div>
      {inspect && (
        <InstallDialog
          path={inspect.path}
          data={inspect.data}
          onClose={() => setInspect(null)}
          onDone={() => {
            setInspect(null)
            void refresh()
          }}
        />
      )}
    </section>
  )
}

function PluginCard({ p, onChanged }: { p: PluginInfo; onChanged: () => void }) {
  const toast = useUI((s) => s.toast)
  const [open, setOpen] = useState(false)
  const [logs, setLogs] = useState<string[] | null>(null)
  const m = p.manifest

  const run = async (fn: () => Promise<unknown>, ok?: string) => {
    try {
      await fn()
      if (ok) toast('success', ok)
      onChanged()
    } catch (e) {
      toast('error', errText(e))
    }
  }

  const togglePerm = (id: string) => {
    const next = p.granted.includes(id) ? p.granted.filter((g) => g !== id) : [...p.granted, id]
    void run(() => plugins.setPermissions(m.id, next))
  }

  const contributes = [
    m.ui?.panels?.length ? `${m.ui.panels.length} panel${m.ui.panels.length > 1 ? 's' : ''}` : '',
    m.commands?.length ? `${m.commands.length} command${m.commands.length > 1 ? 's' : ''}` : '',
    m.assessmentRules?.length
      ? `${m.assessmentRules.length} assessment rule${m.assessmentRules.length > 1 ? 's' : ''}`
      : '',
  ].filter(Boolean)

  return (
    <div
      className={clsx(
        'rounded-xl border bg-surface',
        p.state === 'crashed' ? 'border-danger/50' : 'border-border',
      )}
    >
      <div className="flex items-start gap-3 p-3.5">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-semibold">{m.name}</span>
            <span className="font-mono text-xs text-muted">{m.version}</span>
            {p.enabled && <Pill tone={stateTone[p.state]}>{stateLabel[p.state]}</Pill>}
          </div>
          <div className="mt-0.5 text-xs text-muted">
            {m.author && `${m.author} · `}
            {contributes.join(' · ') || 'no contributions'}
          </div>
          {m.description && <p className="mt-1.5 text-[13px]">{m.description}</p>}
          {p.error && p.enabled && (
            <p className="mt-1.5 select-text break-words text-xs text-danger">{p.error}</p>
          )}
        </div>
        <label className="flex shrink-0 cursor-pointer items-center gap-2 text-xs text-muted">
          <input
            type="checkbox"
            checked={p.enabled}
            onChange={(e) => void run(() => plugins.setEnabled(m.id, e.target.checked))}
            className="accent-[var(--accent)]"
          />{' '}
          Enabled
        </label>
      </div>
      <div className="flex items-center justify-between border-t border-border px-3.5 py-1.5">
        <button
          onClick={() => setOpen(!open)}
          className="cursor-pointer text-xs text-accent hover:underline"
        >
          {open ? 'Hide details' : 'Permissions & details'}
        </button>
        <div className="flex gap-1">
          {m.backend && (
            <>
              <Button
                variant="ghost"
                className="h-7 px-2 text-xs"
                onClick={async () => setLogs(logs ? null : await plugins.logs(m.id))}
              >
                <ScrollText size={13} /> Logs
              </Button>
              <Button
                variant="ghost"
                className="h-7 px-2 text-xs"
                onClick={() => void run(() => plugins.restart(m.id), 'Restarting…')}
              >
                <RotateCw size={13} /> Restart
              </Button>
            </>
          )}
          <Button
            variant="ghost"
            className="h-7 px-2 text-xs text-danger hover:text-danger"
            onClick={() => {
              if (confirm(`Uninstall ${m.name}? Its stored data is deleted too.`))
                void run(() => plugins.uninstall(m.id), 'Plugin uninstalled')
            }}
          >
            <Trash2 size={13} /> Uninstall
          </Button>
        </div>
      </div>
      {open && (
        <div className="space-y-1.5 border-t border-border px-3.5 py-3">
          {p.permissions.length === 0 && (
            <p className="text-xs text-muted">This plugin requests no special permissions.</p>
          )}
          {p.permissions.map((perm) => (
            <label key={perm.id} className="flex cursor-pointer items-start gap-2.5 text-[13px]">
              <input
                type="checkbox"
                checked={p.granted.includes(perm.id)}
                onChange={() => togglePerm(perm.id)}
                className="mt-0.5 accent-[var(--accent)]"
              />
              <span className="min-w-0">
                <span className="font-medium">{perm.title}</span>
                <span
                  className={clsx(
                    'ml-2 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase',
                    riskCls[perm.risk],
                  )}
                >
                  {perm.risk}
                </span>
                <span className="block text-xs text-muted">{perm.description}</span>
              </span>
            </label>
          ))}
          {m.backend && (
            <p className="pt-1 text-xs text-muted">
              Backend:{' '}
              <span className="font-mono">
                {m.backend.command} {(m.backend.args ?? []).join(' ')}
              </span>
            </p>
          )}
        </div>
      )}
      {logs && (
        <pre className="max-h-48 select-text overflow-auto whitespace-pre-wrap break-all border-t border-border bg-bg px-3.5 py-2 font-mono text-[11px]">
          {logs.length ? logs.join('\n') : 'No log output yet.'}
        </pre>
      )}
    </div>
  )
}

function InstallDialog({
  path,
  data,
  onClose,
  onDone,
}: {
  path: string
  data: PluginInspection
  onClose: () => void
  onDone: () => void
}) {
  const toast = useUI((s) => s.toast)
  // low/medium-risk permissions start enabled; high-risk ones must be opted into
  const [granted, setGranted] = useState<Set<string>>(
    new Set(data.permissions.filter((p) => p.risk !== 'high').map((p) => p.id)),
  )
  const [busy, setBusy] = useState(false)
  const m = data.manifest

  const install = async () => {
    setBusy(true)
    try {
      await plugins.install(path, [...granted])
      toast('success', `${m.name} ${data.update ? 'updated' : 'installed'}`)
      onDone()
    } catch (e) {
      toast('error', errText(e))
      setBusy(false)
    }
  }

  return (
    <Dialog
      title={data.update ? `Update ${m.name}` : `Install ${m.name}`}
      onClose={onClose}
      width={520}
    >
      <div className="space-y-4 text-[13px]">
        <div>
          <div className="font-semibold">
            {m.name}{' '}
            <span className="font-mono text-xs font-normal text-muted">
              {data.update ? `${data.existingVersion} → ` : ''}
              {m.version}
            </span>
          </div>
          {m.author && <div className="text-xs text-muted">by {m.author}</div>}
          {m.description && <p className="mt-1">{m.description}</p>}
        </div>
        {data.warnings.map((w, i) => (
          <div key={i} className="rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
            {w}
          </div>
        ))}
        <div>
          <h4 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
            Permissions requested
          </h4>
          {data.permissions.length === 0 && (
            <p className="text-xs text-muted">
              None — the plugin can only use its own UI and storage.
            </p>
          )}
          <div className="space-y-1.5">
            {data.permissions.map((perm) => (
              <label key={perm.id} className="flex cursor-pointer items-start gap-2.5">
                <input
                  type="checkbox"
                  checked={granted.has(perm.id)}
                  onChange={() =>
                    setGranted((g) => {
                      const n = new Set(g)
                      if (!n.delete(perm.id)) n.add(perm.id)
                      return n
                    })
                  }
                  className="mt-0.5 accent-[var(--accent)]"
                />
                <span>
                  <span className="font-medium">{perm.title}</span>
                  <span
                    className={clsx(
                      'ml-2 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase',
                      riskCls[perm.risk],
                    )}
                  >
                    {perm.risk}
                  </span>
                  <span className="block text-xs text-muted">{perm.description}</span>
                </span>
              </label>
            ))}
          </div>
          <p className="mt-2 text-xs text-muted">
            You can change these any time in Settings → Plugins.
          </p>
        </div>
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={busy} onClick={() => void install()}>
            {data.update ? 'Update' : 'Install'}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
