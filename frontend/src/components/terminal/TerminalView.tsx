import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'
import { ClipboardPaste, Copy, Minus, Plus, RotateCw, Search } from 'lucide-react'
import { termSocketUrl } from '../../lib/api'
import { get, useStore, type TerminalTab, updateTab } from '../../lib/store'
import { openTerminal, closeTab } from '../../lib/actions'
import { t } from '../../lib/i18n'

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function themeFor(dark: boolean) {
  return dark
    ? {
        background: '#0f1116', foreground: '#dfe3ea', cursor: cssVar('--accent') || '#4f7cff', cursorAccent: '#0f1116',
        selectionBackground: 'rgba(79,124,255,.35)',
        black: '#1c2029', red: '#ff6b6b', green: '#3dd68c', yellow: '#f5c451', blue: '#5c9dff', magenta: '#c792ea', cyan: '#4fd1e8', white: '#d0d5de',
        brightBlack: '#5d6577', brightRed: '#ff8787', brightGreen: '#69e6a6', brightYellow: '#ffd97a', brightBlue: '#82b4ff', brightMagenta: '#d9b3ff', brightCyan: '#7fe3f2', brightWhite: '#ffffff',
      }
    : {
        background: '#fbfbfd', foreground: '#1d222c', cursor: cssVar('--accent') || '#4f7cff', cursorAccent: '#ffffff',
        selectionBackground: 'rgba(79,124,255,.25)',
        black: '#1d222c', red: '#d6363c', green: '#12924a', yellow: '#a86f00', blue: '#2563eb', magenta: '#9333ea', cyan: '#0891b2', white: '#6b7280',
        brightBlack: '#4b5563', brightRed: '#ef4444', brightGreen: '#16a34a', brightYellow: '#ca8a04', brightBlue: '#3b82f6', brightMagenta: '#a855f7', brightCyan: '#06b6d4', brightWhite: '#111827',
      }
}

export function TerminalView({ tab, visible }: { tab: TerminalTab; visible: boolean }) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<Terminal | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const search = useRef<SearchAddon | null>(null)
  const ws = useRef<WebSocket | null>(null)
  const prefs = useStore((s) => s.prefs)
  const exited = useStore((s) => s.terminals[tab.termId]?.exited)
  const theme = useStore((s) => s.prefs?.theme)
  const [findOpen, setFindOpen] = useState(false)
  const [q, setQ] = useState('')
  const [fontSize, setFontSize] = useState(prefs?.terminal.fontSize || 13)

  useEffect(() => {
    const dark = document.documentElement.dataset.theme === 'dark'
    const tp = get().prefs?.terminal
    const xt = new Terminal({
      fontFamily: tp?.fontFamily || cssVar('--font-mono'),
      fontSize,
      cursorBlink: tp?.cursorBlink ?? true,
      scrollback: tp?.scrollback || 10000,
      allowProposedApi: true,
      theme: themeFor(dark),
      macOptionIsMeta: true,
      rightClickSelectsWord: true,
    })
    const f = new FitAddon()
    const s = new SearchAddon()
    xt.loadAddon(f)
    xt.loadAddon(s)
    xt.loadAddon(new WebLinksAddon((_e, uri) => window.open(uri, '_blank', 'noopener')))
    xt.open(host.current!)
    term.current = xt
    fit.current = f
    search.current = s
    requestAnimationFrame(() => f.fit())

    const sock = new WebSocket(termSocketUrl(tab.termId))
    sock.binaryType = 'arraybuffer'
    ws.current = sock
    const enc = new TextEncoder()
    sock.onopen = () => {
      sock.send(JSON.stringify({ type: 'resize', cols: xt.cols, rows: xt.rows }))
    }
    sock.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) xt.write(new Uint8Array(ev.data))
      else xt.write(ev.data)
    }
    sock.onclose = () => {
      xt.write(`\r\n\x1b[2m— ${t('term.exited')} —\x1b[0m\r\n`)
    }
    xt.onData((d) => sock.readyState === WebSocket.OPEN && sock.send(enc.encode(d)))
    xt.onBinary((d) => {
      if (sock.readyState !== WebSocket.OPEN) return
      const buf = new Uint8Array(d.length)
      for (let i = 0; i < d.length; i++) buf[i] = d.charCodeAt(i) & 255
      sock.send(buf)
    })
    xt.onResize(({ cols, rows }) => sock.readyState === WebSocket.OPEN && sock.send(JSON.stringify({ type: 'resize', cols, rows })))
    xt.onTitleChange((title) => title && updateTab<TerminalTab>(tab.id, () => ({ title })))
    xt.onSelectionChange(() => {
      if (get().prefs?.terminal.copyOnSelect && xt.hasSelection()) navigator.clipboard?.writeText(xt.getSelection()).catch(() => {})
    })
    xt.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true
      const mod = e.ctrlKey || e.metaKey
      if (mod && e.shiftKey && e.key.toLowerCase() === 'c') {
        navigator.clipboard?.writeText(xt.getSelection())
        return false
      }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'v') {
        navigator.clipboard?.readText().then((txt) => xt.paste(txt))
        return false
      }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'f') {
        setFindOpen(true)
        return false
      }
      if (mod && (e.key === 'Tab' || e.key === 'PageDown' || e.key === 'PageUp' || e.key.toLowerCase() === 'k')) return false
      return true
    })
    const ro = new ResizeObserver(() => {
      try {
        f.fit()
      } catch {
        /* hidden */
      }
    })
    ro.observe(host.current!)
    return () => {
      ro.disconnect()
      sock.close()
      xt.dispose()
    }
  }, [])

  useEffect(() => {
    if (visible) {
      requestAnimationFrame(() => {
        fit.current?.fit()
        term.current?.focus()
      })
    }
  }, [visible])

  useEffect(() => {
    const dark = document.documentElement.dataset.theme === 'dark'
    if (term.current) term.current.options.theme = themeFor(dark)
  }, [theme])

  useEffect(() => {
    if (term.current) {
      term.current.options.fontSize = fontSize
      fit.current?.fit()
    }
  }, [fontSize])

  return (
    <div className="terminal-view">
      <div className="term-bar">
        <span className={`dot ${exited ? 'danger' : 'ok'}`} />
        <span className="ellipsis">{tab.title}</span>
        <div className="spacer" />
        {findOpen && (
          <input
            className="input sm"
            style={{ width: 200 }}
            autoFocus
            placeholder={t('common.search')}
            value={q}
            onChange={(e) => {
              setQ(e.target.value)
              search.current?.findNext(e.target.value, { incremental: true })
            }}
            onKeyDown={(e) => {
              e.stopPropagation()
              if (e.key === 'Enter') (e.shiftKey ? search.current?.findPrevious(q) : search.current?.findNext(q))
              if (e.key === 'Escape') {
                setFindOpen(false)
                term.current?.focus()
              }
            }}
          />
        )}
        <button className="icon-btn sm" data-tip={t('common.search')} data-kbd="Ctrl+Shift+F" onClick={() => setFindOpen(!findOpen)}><Search /></button>
        <button className="icon-btn sm" data-tip={t('common.copy')} data-kbd="Ctrl+Shift+C" onClick={() => navigator.clipboard?.writeText(term.current?.getSelection() || '')}><Copy /></button>
        <button className="icon-btn sm" data-tip="Paste" data-kbd="Ctrl+Shift+V" onClick={() => navigator.clipboard?.readText().then((txt) => term.current?.paste(txt))}><ClipboardPaste /></button>
        <button className="icon-btn sm" onClick={() => setFontSize((n) => Math.max(9, n - 1))}><Minus /></button>
        <button className="icon-btn sm" onClick={() => setFontSize((n) => Math.min(28, n + 1))}><Plus /></button>
        {exited && (
          <button className="btn sm primary" onClick={() => { closeTab(tab.id); openTerminal(tab.conn) }}><RotateCw /> {t('term.reopen')}</button>
        )}
      </div>
      <div className="term-host" ref={host} onContextMenu={(e) => e.stopPropagation()} />
    </div>
  )
}
