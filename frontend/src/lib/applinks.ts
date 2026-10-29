import type { Application } from './types'

/** Fills a link template such as `https://ci.example.com/job/{release}` for an application. */
export function expandLink(template: string, app: Application): string {
  const vars: Record<string, string> = {
    name: app.name,
    namespace: app.namespaces[0] ?? '',
    release: app.helm?.release ?? app.name,
    version: app.version,
  }
  return template.replace(/\{(name|namespace|release|version)\}/g, (_, k: string) =>
    encodeURIComponent(vars[k] ?? ''),
  )
}

/** The links to show for an application: the ones its own objects declare, then the user's templates. */
export function linksFor(
  app: Application,
  templates: { title: string; url: string }[],
): { title: string; url: string; custom: boolean }[] {
  const out: { title: string; url: string; custom: boolean }[] = app.links.map((l) => ({
    ...l,
    custom: false,
  }))
  for (const t of templates) out.push({ title: t.title, url: expandLink(t.url, app), custom: true })
  return out
}
