import clsx from 'clsx'
import { useEffect, useState } from 'react'
import { Button, Dialog } from '../components/ui'
import { system, type AppInfo } from '../lib/backend'
import { useSettings } from '../store/settings'
import { useUI } from '../store/ui'
import { THEMES, THEME_META } from '../themes/themes'
import { PluginsPanel } from './PluginsPanel'
import { doctor } from '../lib/backend'
import type { DoctorConfig } from '../lib/types'
import { Input } from '../components/ui'

type Tab = 'appearance' | 'general' | 'doctor' | 'shortcuts' | 'plugins' | 'about'

const TABS: [Tab, string][] = [
  ['appearance', 'Appearance'],
  ['general', 'General'],
  ['doctor', 'Cluster Doctor'],
  ['shortcuts', 'Shortcuts'],
  ['plugins', 'Plugins'],
  ['about', 'About'],
]

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export const SHORTCUTS: [string, string][] = [
  ['⌘ K', 'Open the command palette (resources, namespaces, clusters, actions, plugin commands)'],
  ['/', 'Focus the search box of the current table'],
  ['↑ ↓  PgUp PgDn  Home End', 'Move the selection in a resource table (click the table first)'],
  ['Esc', 'Close the detail panel, dialog or palette'],
  ['Tab / ⇧ Tab', 'Move between controls (focus stays inside open dialogs)'],
  ['⌃ `', 'Show or hide the bottom dock (terminals, logs, apply)'],
  ['⌘ Enter', 'Run the query in the Prometheus explorer'],
  ['Right-click', 'Context menu on clusters in the hotbar and on resource rows'],
]

export function SettingsDialog({
  onClose,
  initialTab,
}: {
  onClose: () => void
  initialTab?: string
}) {
  const [tab, setTab] = useState<Tab>(
    TABS.some(([id]) => id === initialTab) ? (initialTab as Tab) : 'appearance',
  )
  return (
    <Dialog title="Settings" onClose={onClose} width={780}>
      <div className="flex min-h-[440px] gap-5">
        <nav className="flex w-36 shrink-0 flex-col gap-0.5" aria-label="Settings sections">
          {TABS.map(([id, label]) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              aria-current={tab === id}
              className={clsx(
                'cursor-pointer rounded-md px-3 py-1.5 text-left text-[13px]',
                tab === id
                  ? 'bg-accent/15 font-medium text-accent'
                  : 'text-fg/90 hover:bg-surface-hover',
              )}
            >
              {label}
            </button>
          ))}
        </nav>
        <div className="min-w-0 flex-1">
          {tab === 'appearance' && <Appearance />}
          {tab === 'general' && <General />}
          {tab === 'doctor' && <Doctor />}
          {tab === 'shortcuts' && <Shortcuts />}
          {tab === 'plugins' && <PluginsPanel />}
          {tab === 'about' && <About />}
        </div>
      </div>
    </Dialog>
  )
}

const H = ({ children }: { children: React.ReactNode }) => (
  <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{children}</h3>
)

function Appearance() {
  const { theme, setTheme } = useSettings()
  return (
    <section>
      <H>Theme</H>
      <div className="grid grid-cols-3 gap-3">
        {THEMES.map((t) => {
          const [bg, accent] = THEME_META[t].swatch
          return (
            <button
              key={t}
              onClick={() => void setTheme(t)}
              aria-pressed={t === theme}
              className={clsx(
                'cursor-pointer rounded-lg border p-2 text-left',
                t === theme
                  ? 'border-accent ring-1 ring-accent'
                  : 'border-border hover:border-muted',
              )}
            >
              <div
                className="mb-2 h-14 rounded-md border border-border/60 p-2"
                style={{ background: bg }}
              >
                <div className="h-1.5 w-8 rounded" style={{ background: accent }} />
                <div className="mt-1.5 h-1 w-14 rounded bg-white/20" />
                <div className="mt-1 h-1 w-10 rounded bg-white/10" />
              </div>
              <div className="text-xs font-medium">{THEME_META[t].label}</div>
            </button>
          )
        })}
      </div>
      <p className="mt-3 text-xs text-muted">
        The three themes are also one click away in the hotbar.
      </p>
    </section>
  )
}

function General() {
  const toast = useUI((s) => s.toast)
  const { checkUpdates, setCheckUpdates, update, setUpdate } = useSettings()
  const [info, setInfo] = useState<AppInfo | null>(null)
  const [checking, setChecking] = useState(false)
  const [status, setStatus] = useState('')
  useEffect(() => {
    void system.info().then(setInfo)
  }, [])

  const check = async () => {
    setChecking(true)
    setStatus('')
    try {
      const r = await system.checkUpdates()
      setUpdate(r.available ? r : null)
      setStatus(r.available ? `Version ${r.latest} is available.` : "You're up to date.")
    } catch (e) {
      setStatus(errText(e))
    } finally {
      setChecking(false)
    }
  }
  const run = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn()
      toast('success', ok)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <div className="space-y-7">
      <section>
        <H>Updates</H>
        <label className="flex cursor-pointer items-start gap-2.5 text-[13px]">
          <input
            type="checkbox"
            checked={checkUpdates}
            onChange={(e) => void setCheckUpdates(e.target.checked)}
            className="mt-0.5 accent-[var(--accent)]"
          />
          <span>
            Check for updates once a day
            <span className="block text-xs text-muted">
              Makes one request to GitHub's public release API (your IP address is visible to
              GitHub; nothing else is sent). Updates are never installed automatically.
            </span>
          </span>
        </label>
        <div className="mt-3 flex items-center gap-3">
          <Button onClick={() => void check()} disabled={checking || !info?.repoConfigured}>
            {checking ? 'Checking…' : 'Check now'}
          </Button>
          <span className="text-xs text-muted">
            {!info?.repoConfigured && info
              ? 'Disabled: this build has no GitHub repository configured.'
              : status}
          </span>
        </div>
        {update && (
          <div className="mt-3 rounded-md bg-accent/10 px-3 py-2 text-[13px]">
            <b>{update.latest}</b> is available (you have {update.current}).{' '}
            <button
              className="cursor-pointer text-accent hover:underline"
              onClick={() => void system.open(update.url).catch((e) => toast('error', errText(e)))}
            >
              Release notes &amp; download
            </button>
          </div>
        )}
      </section>
      <section>
        <H>Data</H>
        <p className="text-xs text-muted">
          Clusters, assessment history, port forwards and plugins are stored locally in:
        </p>
        <p className="mt-1 select-text break-all rounded-md bg-bg px-2.5 py-1.5 font-mono text-xs">
          {info?.dataDir || '—'}
        </p>
        <p className="mt-1 text-xs text-muted">
          Cluster credentials for pasted kubeconfigs are kept in the macOS Keychain, not in this
          folder.
        </p>
        <div className="mt-3 flex flex-wrap gap-2">
          <Button onClick={() => void run(system.revealData, 'Opened in Finder')}>
            Show in Finder
          </Button>
          <Button
            onClick={() => {
              if (confirm('Delete every saved assessment report for all clusters?'))
                void run(system.clearHistory, 'Assessment history cleared')
            }}
          >
            Clear assessment history…
          </Button>
          <Button
            onClick={() => {
              if (
                !confirm(
                  'Reset theme, update preference and remembered view/namespace choices to their defaults? Clusters are not touched.',
                )
              )
                return
              void run(async () => {
                await system.resetSettings()
                try {
                  Object.keys(localStorage)
                    .filter((k) => k.startsWith('soliton-rudder.'))
                    .forEach((k) => localStorage.removeItem(k))
                } catch {
                  /* storage unavailable */
                }
                await useSettings.getState().load()
              }, 'Preferences reset')
            }}
          >
            Reset preferences…
          </Button>
        </div>
      </section>
    </div>
  )
}

function Shortcuts() {
  return (
    <section>
      <H>Keyboard shortcuts</H>
      <div className="overflow-hidden rounded-lg border border-border">
        {SHORTCUTS.map(([keys, what]) => (
          <div
            key={keys}
            className="grid grid-cols-[200px_1fr] gap-3 border-b border-border px-3 py-2 text-[13px] last:border-0"
          >
            <kbd className="font-mono text-xs text-accent">{keys}</kbd>
            <span>{what}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

function About() {
  const toast = useUI((s) => s.toast)
  const [info, setInfo] = useState<AppInfo | null>(null)
  useEffect(() => {
    void system.info().then(setInfo)
  }, [])
  const link = (label: string, url?: string) =>
    url ? (
      <button
        className="cursor-pointer text-accent hover:underline"
        onClick={() => void system.open(url).catch((e) => toast('error', errText(e)))}
      >
        {label}
      </button>
    ) : null
  return (
    <section>
      <H>About</H>
      <p className="text-[13px]">
        Soliton Rudder <span className="font-mono text-muted">v{info?.version}</span>
      </p>
      <p className="mt-1 text-xs text-muted">Kubernetes management, simplified.</p>
      <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
        {link('Documentation', info?.docsURL)}
        {link('Report an issue', info?.issuesURL)}
        {link('Releases', info?.releasesURL)}
        <button
          className="cursor-pointer text-accent hover:underline"
          onClick={() => void system.openNotices().catch((e) => toast('error', errText(e)))}
        >
          Third-party licenses
        </button>
      </div>
      <p className="mt-5 text-xs text-muted">
        Soliton Rudder is open-source software licensed under the Apache License 2.0. “Kubernetes”,
        “Helm”, “Argo”, “Flux” and “Prometheus” are trademarks of their respective owners; this
        project is not affiliated with or endorsed by them.
      </p>
    </section>
  )
}

const DEFAULT_DOCTOR: DoctorConfig = {
  prodNamespaces: [],
  cpuHourUsd: 0.031611,
  memGbHourUsd: 0.004237,
  headroomPct: 20,
}

function Doctor() {
  const toast = useUI((s) => s.toast)
  const [cfg, setCfg] = useState<DoctorConfig | null>(null)
  const [prod, setProd] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    void doctor.getConfig().then((c) => {
      setCfg(c)
      setProd((c.prodNamespaces ?? []).join('\n'))
    })
  }, [])
  if (!cfg) return <p className="text-[13px] text-muted">Loading…</p>

  const num = (v: string) => (v === '' ? 0 : Number(v))
  const save = async () => {
    setBusy(true)
    try {
      const saved = await doctor.setConfig({
        ...cfg,
        prodNamespaces: prod
          .split(/[\n,]+/)
          .map((x) => x.trim())
          .filter(Boolean),
      })
      setCfg(saved)
      setProd(saved.prodNamespaces.join('\n'))
      toast('success', 'Cluster Doctor settings saved. They apply to the next assessment.')
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <section>
        <H>Cost estimation</H>
        <p className="mb-3 text-xs text-muted">
          Assessments estimate what your pods' requested CPU and memory would cost per month, and
          how much of that is unused. Enter your real rates for better numbers; the defaults are the
          OpenCost reference on-demand prices. It is an estimate, not your cloud bill.
        </p>
        <div className="grid grid-cols-3 gap-3">
          <label className="block text-xs font-medium text-muted">
            $ per vCPU-hour
            <Input
              value={String(cfg.cpuHourUsd)}
              onChange={(e) => setCfg({ ...cfg, cpuHourUsd: num(e.target.value) })}
              inputMode="decimal"
              className="mt-1"
            />
          </label>
          <label className="block text-xs font-medium text-muted">
            $ per GB-hour (memory)
            <Input
              value={String(cfg.memGbHourUsd)}
              onChange={(e) => setCfg({ ...cfg, memGbHourUsd: num(e.target.value) })}
              inputMode="decimal"
              className="mt-1"
            />
          </label>
          <label className="block text-xs font-medium text-muted">
            Headroom above usage (%)
            <Input
              value={String(cfg.headroomPct)}
              onChange={(e) => setCfg({ ...cfg, headroomPct: num(e.target.value) })}
              inputMode="numeric"
              className="mt-1"
            />
          </label>
        </div>
        <p className="mt-2 text-xs text-muted">
          ≈ {(cfg.cpuHourUsd * 730).toFixed(2)} $/vCPU-month · {(cfg.memGbHourUsd * 730).toFixed(2)}{' '}
          $/GB-month
        </p>
      </section>
      <section>
        <H>Production namespaces</H>
        <p className="mb-2 text-xs text-muted">
          Single replicas and missing PodDisruptionBudgets are rated more severely in production.
          Namespaces named <span className="font-mono">prod</span>,{' '}
          <span className="font-mono">production</span>,<span className="font-mono"> prd</span> or{' '}
          <span className="font-mono">live</span>, or labelled{' '}
          <span className="font-mono">environment=production</span>, are detected automatically. Add
          others here, one per line (<span className="font-mono">*</span> works at the start or
          end).
        </p>
        <textarea
          value={prod}
          onChange={(e) => setProd(e.target.value)}
          rows={4}
          spellCheck={false}
          placeholder={'billing\ncore-*\n*-eu'}
          className="w-full select-text resize-y rounded-md border border-border bg-bg p-2.5 font-mono text-xs outline-none focus:border-accent"
        />
      </section>
      <div className="flex gap-2">
        <Button variant="primary" disabled={busy} onClick={() => void save()}>
          Save
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setCfg(DEFAULT_DOCTOR)
            setProd('')
          }}
        >
          Reset to defaults
        </Button>
      </div>
    </div>
  )
}
