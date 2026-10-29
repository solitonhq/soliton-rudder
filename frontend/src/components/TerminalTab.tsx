import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { RotateCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { tools } from '../lib/backend'
import { termRouter } from '../lib/termrouter'
import { useSettings } from '../store/settings'
import type { DockTab } from '../store/dock'
import { Button } from './ui'

type TermTab = Extract<DockTab, { kind: 'terminal' }>

function css(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

const LIGHT_ANSI = {
  black: '#16181d',
  red: '#c0352f',
  green: '#1f8a56',
  yellow: '#a06a00',
  blue: '#2f6fe0',
  magenta: '#8a4fd0',
  cyan: '#1a8a8a',
  white: '#8b8f99',
  brightBlack: '#626875',
  brightRed: '#d1403a',
  brightGreen: '#1f9d63',
  brightYellow: '#b97d0e',
  brightBlue: '#2a63cc',
  brightMagenta: '#9b5de5',
  brightCyan: '#16a0a0',
  brightWhite: '#16181d',
}

function termTheme() {
  const light = document.documentElement.getAttribute('data-theme') === 'light'
  return {
    background: css('--bg'),
    foreground: css('--fg'),
    cursor: css('--accent'),
    selectionBackground: 'rgba(120,150,230,0.35)',
    ...(light ? LIGHT_ANSI : {}),
  }
}

export function TerminalTab({ tab, active }: { tab: TermTab; active: boolean }) {
  const hostRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const idRef = useRef<string | null>(null)
  const startRef = useRef<(() => Promise<void>) | null>(null)
  const theme = useSettings((s) => s.theme)
  const [exited, setExited] = useState(false)

  useEffect(() => {
    const host = hostRef.current!
    const term = new Terminal({
      fontFamily: 'SF Mono, Menlo, ui-monospace, monospace',
      fontSize: 12.5,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
      theme: termTheme(),
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    try {
      const gl = new WebglAddon()
      gl.onContextLoss(() => gl.dispose())
      term.loadAddon(gl)
    } catch {
      /* canvas/DOM renderer fallback */
    }
    termRef.current = term
    fitRef.current = fit
    const doFit = () => {
      if (host.clientWidth > 0 && host.clientHeight > 0) fit.fit()
    }
    doFit()

    let disposed = false
    const start = async () => {
      setExited(false)
      try {
        const id = tab.pod
          ? await tools.openPodTerminal(
              tab.clusterId,
              tab.pod.ns,
              tab.pod.name,
              tab.pod.container,
              term.cols,
              term.rows,
            )
          : await tools.openLocalTerminal(tab.clusterId, term.cols, term.rows)
        if (disposed) return void tools.termClose(id)
        idRef.current = id
        termRouter.register(id, {
          output: (b) => term.write(b),
          exit: (err) => {
            term.writeln(`\r\n\x1b[90m[session ended${err ? `: ${err}` : ''}]\x1b[0m`)
            setExited(true)
          },
        })
      } catch (e) {
        term.writeln(`\r\n\x1b[31m${e instanceof Error ? e.message : String(e)}\x1b[0m`)
        setExited(true)
      }
    }
    startRef.current = async () => {
      if (idRef.current) termRouter.unregister(idRef.current)
      idRef.current = null
      term.clear()
      await start()
    }
    void start()

    term.onData((d) => idRef.current && void tools.termWrite(idRef.current, d))
    term.onResize(
      ({ cols, rows }) => idRef.current && void tools.termResize(idRef.current, cols, rows),
    )
    const ro = new ResizeObserver(doFit)
    ro.observe(host)

    return () => {
      disposed = true
      ro.disconnect()
      if (idRef.current) {
        termRouter.unregister(idRef.current)
        void tools.termClose(idRef.current)
      }
      term.dispose()
    }
  }, [tab.clusterId, tab.pod])

  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = termTheme()
  }, [theme])

  useEffect(() => {
    if (!active) return
    const t = setTimeout(() => {
      fitRef.current?.fit()
      termRef.current?.focus()
    }, 0)
    return () => clearTimeout(t)
  }, [active])

  return (
    <div className="relative h-full bg-bg p-2">
      <div ref={hostRef} className="h-full w-full" />
      {exited && (
        <div className="absolute right-4 top-3">
          <Button onClick={() => void startRef.current?.()}>
            <RotateCw size={13} /> Reconnect
          </Button>
        </div>
      )}
    </div>
  )
}
