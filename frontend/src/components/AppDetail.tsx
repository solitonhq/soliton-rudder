import clsx from 'clsx'
import { AlertTriangle, ArrowLeft, BookOpen, ExternalLink, Link2, XCircle } from 'lucide-react'
import { useState } from 'react'
import { RunbookDialog } from '../dialogs/RunbookDialog'
import { formatAge } from '../lib/format'
import { linksFor } from '../lib/applinks'
import type { AppMember, AppStatus, Application, AppLinkTemplate, Incident } from '../lib/types'
import { useExplorer } from '../store/explorer'
import { AppHistory } from './AppHistory'
import { openLink } from './IngressDetails'

export const statusDot: Record<AppStatus, string> = {
  ok: 'bg-success',
  warn: 'bg-warning',
  error: 'bg-danger',
  unknown: 'bg-muted/60',
}
const statusText: Record<AppStatus, string> = {
  ok: 'Healthy',
  warn: 'Degraded',
  error: 'Failing',
  unknown: 'Unknown',
}
export const sourceLabel: Record<Application['groupedBy'], string> = {
  helm: 'Helm release',
  'part-of': 'part-of label',
  instance: 'instance label',
  app: 'app label',
  namespace: 'namespace',
}
const sourceHelp: Record<Application['groupedBy'], string> = {
  helm: 'Recognised from the Helm release that deployed its objects.',
  'part-of': 'Grouped by the app.kubernetes.io/part-of label.',
  instance: 'Grouped by the app.kubernetes.io/instance label.',
  app: 'Grouped by the legacy app / k8s-app label.',
  namespace: 'Grouped by namespace because its objects carry no application labels.',
}

function Card({
  label,
  value,
  sub,
  tone,
}: {
  label: string
  value: string
  sub?: string
  tone?: 'warn' | 'error'
}) {
  return (
    <div className="rounded-xl border border-border bg-surface p-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div
        className={clsx(
          'mt-0.5 text-xl font-semibold tabular-nums',
          tone === 'error' && 'text-danger',
          tone === 'warn' && 'text-warning',
        )}
      >
        {value}
      </div>
      {sub && <div className="text-xs text-muted">{sub}</div>}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">
        {title}
      </h3>
      {children}
    </section>
  )
}

function MemberRow({
  m,
  onOpen,
  onRunbook,
}: {
  m: AppMember
  onOpen: () => void
  onRunbook?: () => void
}) {
  return (
    <li className="flex items-center gap-2 px-3 py-1.5">
      <span className={clsx('size-2 shrink-0 rounded-full', statusDot[m.status])} />
      <span className="w-24 shrink-0 truncate text-[10px] font-semibold uppercase text-muted">
        {m.kind === 'HorizontalPodAutoscaler' ? 'HPA' : m.kind}
      </span>
      <button
        className="min-w-0 flex-1 cursor-pointer truncate text-left font-mono text-xs hover:text-accent hover:underline"
        onClick={onOpen}
        title={`Open ${m.name}`}
      >
        {m.namespace && <span className="text-muted">{m.namespace}/</span>}
        {m.name}
      </button>
      <span className="max-w-[40%] truncate text-xs text-muted" title={m.detail}>
        {m.detail}
      </span>
      {onRunbook && (m.status === 'error' || m.status === 'warn') && (
        <button
          className="flex cursor-pointer items-center gap-1 text-xs text-accent hover:underline"
          onClick={onRunbook}
        >
          <BookOpen size={12} /> Runbook
        </button>
      )}
    </li>
  )
}

/** One application: health, what is wrong, versions, links, its objects, and its Helm history. */
export function AppDetail({
  clusterId,
  app,
  templates,
  onBack,
}: {
  clusterId: string
  app: Application
  templates: AppLinkTemplate[]
  onBack: () => void
}) {
  const setView = useExplorer((s) => s.setView)
  const jump = useExplorer((s) => s.jump)
  const [tab, setTab] = useState<'overview' | 'resources' | 'history'>('overview')
  const [incident, setIncident] = useState<Incident | null>(null)
  const open = (m: AppMember) => {
    if (!m.resourceKey) return
    setView('resources')
    void jump(m.resourceKey, m.name)
  }
  const runbook = (m: AppMember) =>
    setIncident({
      kind: m.kind,
      namespace: m.namespace,
      name: m.name,
      reason: m.detail,
      status: m.status === 'error' ? 'error' : 'warn',
    })
  const c = app.counts
  const problems = app.members.filter((m) => m.status === 'error' || m.status === 'warn')
  const links = linksFor(app, templates)
  const tabs = (app.helm ? ['overview', 'resources', 'history'] : ['overview', 'resources']) as (
    'overview' | 'resources' | 'history'
  )[]

  return (
    <div className="flex h-full flex-col">
      <div className="shrink-0 border-b border-border px-6 py-3">
        <button
          onClick={onBack}
          className="mb-1 flex cursor-pointer items-center gap-1 text-xs text-muted hover:text-accent"
        >
          <ArrowLeft size={12} /> Applications
        </button>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className={clsx('size-2.5 rounded-full', statusDot[app.status])} />
          <h1 className="text-lg font-semibold">{app.name}</h1>
          <span className="text-xs text-muted">{statusText[app.status]}</span>
          {app.version && (
            <span className="rounded bg-surface-2 px-2 py-0.5 font-mono text-xs">
              {app.version}
            </span>
          )}
          <span className="text-xs text-muted">{app.namespaces.join(', ')}</span>
          <span
            className="rounded bg-surface-2 px-2 py-0.5 text-[11px] text-muted"
            title={sourceHelp[app.groupedBy]}
          >
            {sourceLabel[app.groupedBy]}
          </span>
        </div>
        <div className="mt-2 flex gap-1">
          {tabs.map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={clsx(
                'cursor-pointer rounded-md px-3 py-1 text-[13px] font-medium capitalize',
                tab === t ? 'bg-accent/15 text-accent' : 'text-muted hover:text-fg',
              )}
            >
              {t}
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {tab === 'history' && app.helm ? (
          <AppHistory clusterId={clusterId} app={app} />
        ) : tab === 'resources' ? (
          <div className="p-6">
            <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border">
              {app.members.map((m) => (
                <MemberRow
                  key={`${m.kind}/${m.namespace}/${m.name}`}
                  m={m}
                  onOpen={() => open(m)}
                  onRunbook={() => runbook(m)}
                />
              ))}
              {app.members.length === 0 && (
                <li className="px-3 py-3 text-xs text-muted">
                  No objects of this application were found.
                </li>
              )}
            </ul>
            <p className="mt-2 text-xs text-muted">
              {sourceHelp[app.groupedBy]} Pods are listed only when they have a problem.
            </p>
          </div>
        ) : (
          <div className="mx-auto max-w-[1100px] space-y-5 p-6 text-[13px]">
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              <Card
                label="Workloads"
                value={`${c.workloadsHealthy} / ${c.workloads}`}
                sub="healthy"
                tone={c.workloadsHealthy < c.workloads ? 'warn' : undefined}
              />
              <Card
                label="Pods"
                value={`${c.podsReady} / ${c.pods}`}
                sub="ready"
                tone={c.podsReady < c.pods ? 'warn' : undefined}
              />
              <Card
                label="Restarts"
                value={String(c.restarts)}
                sub="across all pods"
                tone={c.restarts > 10 ? 'warn' : undefined}
              />
              <Card
                label="Last change"
                value={app.lastChange ? `${formatAge(app.lastChange)} ago` : '—'}
                sub={app.helm ? `Helm revision ${app.helm.revision}` : undefined}
              />
            </div>

            {(app.issues.length > 0 || problems.length > 0) && (
              <Section title="Needs attention">
                <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border">
                  {app.issues.map((i, k) => (
                    <li
                      key={k}
                      className={clsx(
                        'flex items-start gap-2 px-3 py-2',
                        i.severity === 'error'
                          ? 'text-danger'
                          : i.severity === 'warn'
                            ? 'text-warning'
                            : 'text-muted',
                      )}
                    >
                      {i.severity === 'error' ? (
                        <XCircle size={14} className="mt-0.5 shrink-0" />
                      ) : (
                        <AlertTriangle size={14} className="mt-0.5 shrink-0" />
                      )}
                      <span className="select-text">{i.message}</span>
                    </li>
                  ))}
                  {problems.map((m) => (
                    <MemberRow
                      key={`${m.kind}/${m.namespace}/${m.name}`}
                      m={m}
                      onOpen={() => open(m)}
                      onRunbook={() => runbook(m)}
                    />
                  ))}
                </ul>
              </Section>
            )}

            {(app.urls.length > 0 || links.length > 0) && (
              <Section title="Links">
                <ul className="flex flex-wrap gap-2">
                  {app.urls.map((u) => (
                    <li key={u}>
                      <button
                        className="flex cursor-pointer items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 py-1 font-mono text-xs hover:border-accent hover:text-accent"
                        onClick={() => openLink(u)}
                        title={`Open ${u}`}
                      >
                        <ExternalLink size={12} /> {u.replace(/^https?:\/\//, '')}
                      </button>
                    </li>
                  ))}
                  {links.map((l) => (
                    <li key={l.title + l.url}>
                      <button
                        className="flex cursor-pointer items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 py-1 text-xs hover:border-accent hover:text-accent"
                        onClick={() => openLink(l.url)}
                        title={l.url}
                      >
                        <Link2 size={12} /> {l.title}
                      </button>
                    </li>
                  ))}
                </ul>
              </Section>
            )}

            <Section title="Version">
              <dl className="grid grid-cols-[120px_1fr] gap-y-1.5">
                {app.chart && (
                  <>
                    <dt className="text-muted">Chart</dt>
                    <dd className="select-text font-mono text-xs">
                      {app.chart}
                      {app.chartVersion && ` ${app.chartVersion}`}
                    </dd>
                  </>
                )}
                {app.appVersion && (
                  <>
                    <dt className="text-muted">App version</dt>
                    <dd className="select-text font-mono text-xs">{app.appVersion}</dd>
                  </>
                )}
                <dt className="text-muted">Images</dt>
                <dd className="select-text space-y-0.5 font-mono text-xs">
                  {app.images.length ? (
                    app.images.map((i) => (
                      <div key={i} className="break-all">
                        {i}
                      </div>
                    ))
                  ) : (
                    <span className="text-muted">none found</span>
                  )}
                </dd>
              </dl>
            </Section>
          </div>
        )}
      </div>
      {incident && (
        <RunbookDialog
          clusterId={clusterId}
          incident={incident}
          onClose={() => setIncident(null)}
        />
      )}
    </div>
  )
}
