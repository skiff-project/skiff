import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'
import {
  ClipboardPaste, Columns2, Copy, FolderOpen, FolderSync, Minus, Plus, Radio, Rows2, RotateCw, Search, Settings2, SquareCode, X,
} from 'lucide-react'
import { termSocketUrl } from '../../lib/api'
import { get, useStore, type TerminalTab, type TermPaneState, updateTab } from '../../lib/store'
import { closeTermPane, followTerminalCwd, openTerminal, closeTab, revealTerminalCwd, splitTerminal, terminalPanes } from '../../lib/actions'
import { snippetName, t } from '../../lib/i18n'
import { showMenuAt, type MenuItem } from '../ui/Menu'
import { openPreferences } from '../dialogs/Preferences'

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

// The shell hook that reports the current folder with OSC 7, for bash and
// zsh (fish and some setups already send it). The leading space keeps it
// out of the history.
export const CWD_HOOK =
  ` __skiff_cwd() { printf '\\033]7;file://%s%s\\033\\\\' "\${HOSTNAME:-localhost}" "$PWD"; }; ` +
  `if [ -n "$ZSH_VERSION" ]; then precmd_functions+=(__skiff_cwd); else PROMPT_COMMAND="__skiff_cwd\${PROMPT_COMMAND:+;$PROMPT_COMMAND}"; fi; __skiff_cwd; clear\r`

/** parseOsc7 reads "file://host/path" (percent-encoded) into the path. */
export function parseOsc7(data: string): string | null {
  if (!data.startsWith('file://')) return null
  const rest = data.slice(7)
  const slash = rest.indexOf('/')
  if (slash < 0) return null
  const p = rest.slice(slash)
  try {
    return decodeURIComponent(p)
  } catch {
    return p
  }
}

interface PaneHandle {
  send: (data: string) => void
  xt: Terminal
  fit: () => void
  sawCwd: boolean
}

/** Panes of one tab: broadcast state and handles, shared without re-rendering. */
class PaneRegistry {
  panes = new Map<string, PaneHandle>()
  broadcast = false
  /** input typed in a pane: to that pane, or to all of them */
  input(from: string, data: string) {
    if (this.broadcast) this.panes.forEach((p) => p.send(data))
    else this.panes.get(from)?.send(data)
  }
}

function TermPane({ pane, tabId, reg, fontSize, focused, many, cwd, onFocus, onCwd, onFind }: {
  pane: TermPaneState; tabId: string; reg: PaneRegistry; fontSize: number; focused: boolean; many: boolean
  cwd?: string; onFocus: () => void; onCwd: (path: string) => void; onFind: () => void
}) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<Terminal | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const theme = useStore((s) => s.prefs?.theme)
  const exited = useStore((s) => s.terminals[pane.termId]?.exited)
  const cb = useRef({ onCwd, onFocus, onFind })
  cb.current = { onCwd, onFocus, onFind }

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
    const search = new SearchAddon()
    xt.loadAddon(f)
    xt.loadAddon(search)
    xt.loadAddon(new WebLinksAddon((_e, uri) => window.open(uri, '_blank', 'noopener')))
    xt.open(host.current!)
    term.current = xt
    fit.current = f
    ;(host.current as any).__search = search
    requestAnimationFrame(() => f.fit())

    const sock = new WebSocket(termSocketUrl(pane.termId))
    sock.binaryType = 'arraybuffer'
    const enc = new TextEncoder()
    const handle: PaneHandle = {
      send: (d) => sock.readyState === WebSocket.OPEN && sock.send(enc.encode(d)),
      xt,
      fit: () => f.fit(),
      sawCwd: false,
    }
    reg.panes.set(pane.termId, handle)
    sock.onopen = () => sock.send(JSON.stringify({ type: 'resize', cols: xt.cols, rows: xt.rows }))
    sock.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) xt.write(new Uint8Array(ev.data))
      else xt.write(ev.data)
    }
    sock.onclose = () => xt.write(`\r\n\x1b[2m— ${t('term.exited')} —\x1b[0m\r\n`)
    xt.onData((d) => reg.input(pane.termId, d))
    xt.onBinary((d) => {
      if (sock.readyState !== WebSocket.OPEN) return
      const buf = new Uint8Array(d.length)
      for (let i = 0; i < d.length; i++) buf[i] = d.charCodeAt(i) & 255
      sock.send(buf)
    })
    xt.onResize(({ cols, rows }) => sock.readyState === WebSocket.OPEN && sock.send(JSON.stringify({ type: 'resize', cols, rows })))
    xt.onTitleChange((title) => {
      if (!title) return
      updateTab<TerminalTab>(tabId, (tb) => ({ panes: terminalPanes(tb).map((p) => (p.termId === pane.termId ? { ...p, title } : p)), ...(tb.termId === pane.termId ? { title } : {}) }))
    })
    // OSC 7: the shell reports its current folder
    xt.parser.registerOscHandler(7, (data) => {
      const p = parseOsc7(data)
      if (p) {
        handle.sawCwd = true
        cb.current.onCwd(p)
      }
      return true
    })
    xt.onSelectionChange(() => {
      if (get().prefs?.terminal.copyOnSelect && xt.hasSelection()) navigator.clipboard?.writeText(xt.getSelection()).catch(() => {})
    })
    xt.textarea?.addEventListener('focus', () => cb.current.onFocus())
    xt.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true
      const mod = e.ctrlKey || e.metaKey
      if (mod && e.shiftKey && e.key.toLowerCase() === 'c') {
        navigator.clipboard?.writeText(xt.getSelection())
        return false
      }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'v') {
        navigator.clipboard?.readText().then((txt) => reg.input(pane.termId, txt))
        return false
      }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'f') {
        cb.current.onFind()
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
      reg.panes.delete(pane.termId)
      sock.close()
      xt.dispose()
    }
  }, [])

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
    <div className={`term-pane${focused && many ? ' focused' : ''}`} data-term={pane.termId} onMouseDown={onFocus}>
      {many && (
        <div className="term-pane-head">
          <span className={`dot ${exited ? 'danger' : 'ok'}`} />
          <span className="ellipsis strong">{pane.title}</span>
          {cwd && <span className="mono faint ellipsis term-cwd">{cwd}</span>}
          <div className="spacer" />
          <button className="icon-btn sm" data-tip={t('term.closePane')} onClick={(e) => { e.stopPropagation(); closeTermPane(tabId, pane.termId) }}><X /></button>
        </div>
      )}
      <div className="term-host" ref={host} onContextMenu={(e) => e.stopPropagation()} />
    </div>
  )
}

export function TerminalView({ tab, visible }: { tab: TerminalTab; visible: boolean }) {
  const reg = useRef(new PaneRegistry()).current
  const prefs = useStore((s) => s.prefs)
  const conns = useStore((s) => s.conns)
  const info = useStore((s) => s.info)
  const panes = terminalPanes(tab)
  const exited = useStore((s) => panes.every((p) => s.terminals[p.termId]?.exited))
  const [focusId, setFocusId] = useState(panes[0].termId)
  const [cwds, setCwds] = useState<Record<string, string>>({})
  const [findOpen, setFindOpen] = useState(false)
  const [q, setQ] = useState('')
  const [fontSize, setFontSize] = useState(prefs?.terminal.fontSize || 13)
  reg.broadcast = !!tab.broadcast
  const focused = panes.find((p) => p.termId === focusId) || panes[0]
  const followRef = useRef(tab.follow)
  followRef.current = tab.follow

  // a new terminal gets the keyboard; a closed one gives it to the last
  const count = useRef(panes.length)
  useEffect(() => {
    if (panes.length > count.current || !panes.some((p) => p.termId === focusId)) setFocusId(panes[panes.length - 1].termId)
    count.current = panes.length
  }, [panes.length])
  const refocus = () => requestAnimationFrame(() => reg.panes.get(focused.termId)?.xt.focus())

  useEffect(() => {
    if (visible) {
      requestAnimationFrame(() => {
        reg.panes.forEach((p) => p.fit())
        reg.panes.get(focused.termId)?.xt.focus()
      })
    }
  }, [visible, panes.length, focused.termId])

  const onCwd = (p: TermPaneState) => (path: string) => {
    setCwds((c) => (c[p.termId] === path ? c : { ...c, [p.termId]: path }))
    if (followRef.current) followTerminalCwd(p.conn, path)
  }

  /** Types a saved command in the focused pane (or all of them when broadcasting). */
  const insert = (text: string) => {
    reg.input(focused.termId, text)
    reg.panes.get(focused.termId)?.xt.focus()
  }

  const canHook = (p: TermPaneState) => p.conn !== 'local' || info?.os !== 'windows'
  const toggleFollow = () => {
    const on = !tab.follow
    updateTab<TerminalTab>(tab.id, () => ({ follow: on }))
    refocus()
    if (!on) return
    // install the hook in the shells that do not report their folder yet
    for (const p of panes) {
      const h = reg.panes.get(p.termId)
      if (h && !h.sawCwd && canHook(p)) h.send(CWD_HOOK)
    }
    const cur = cwds[focused.termId]
    if (cur) followTerminalCwd(focused.conn, cur)
  }

  const splitMenu = (el: HTMLElement, dir: 'columns' | 'rows') => {
    const sshConns = Object.values(conns).filter((c) => c.state === 'connected' && c.caps.shell)
    const items: MenuItem[] = [
      { header: t('term.splitWith') },
      ...sshConns.map((c) => ({ label: c.id === focused.conn ? `${c.title} (${t('term.sameServer')})` : c.title, onClick: () => splitTerminal(tab.id, c.id, dir, c.id === focused.conn ? cwds[focused.termId] || focused.cwd : undefined) })),
      { label: t('common.local'), hidden: !info?.localTerminal, onClick: () => splitTerminal(tab.id, 'local', dir) },
    ]
    showMenuAt(el, items)
  }

  const snippetsMenu = (el: HTMLElement) => {
    const list = prefs?.terminal.snippets || []
    const items: MenuItem[] = list.length
      ? list.map((s) => ({ label: snippetName(s), shortcut: s.run ? '↵' : undefined, onClick: () => insert(s.command + (s.run ? '\r' : '')) }))
      : [{ label: t('term.noSnippets'), disabled: true }]
    items.push({ separator: true }, { label: t('term.manageSnippets'), icon: <Settings2 />, onClick: () => openPreferences('terminal') })
    showMenuAt(el, items, 'right')
  }

  const search = () => (document.querySelector(`.tab-content [data-term="${focused.termId}"] .term-host`) as any)?.__search
  const grid = panes.length > 2 ? 'grid' : tab.split || 'columns'

  return (
    <div className={`terminal-view${tab.broadcast ? ' broadcast' : ''}`}>
      <div className="term-bar">
        <span className={`dot ${exited ? 'danger' : 'ok'}`} />
        <span className="ellipsis">{focused.title}</span>
        {cwds[focused.termId] && <span className="mono faint ellipsis term-cwd" data-tip={t('term.cwd')}>{cwds[focused.termId]}</span>}
        {tab.broadcast && <span className="badge warn"><Radio /> {t('term.broadcasting', { n: panes.length })}</span>}
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
              search()?.findNext(e.target.value, { incremental: true })
            }}
            onKeyDown={(e) => {
              e.stopPropagation()
              if (e.key === 'Enter') (e.shiftKey ? search()?.findPrevious(q) : search()?.findNext(q))
              if (e.key === 'Escape') {
                setFindOpen(false)
                reg.panes.get(focused.termId)?.xt.focus()
              }
            }}
          />
        )}
        <button className="icon-btn sm" data-tip={t('term.splitRight')} onClick={(e) => splitMenu(e.currentTarget, 'columns')}><Columns2 /></button>
        <button className="icon-btn sm" data-tip={t('term.splitDown')} onClick={(e) => splitMenu(e.currentTarget, 'rows')}><Rows2 /></button>
        <button className={`icon-btn sm${tab.broadcast ? ' on' : ''}`} data-tip={t('term.broadcast')} disabled={panes.length < 2} onClick={() => { updateTab<TerminalTab>(tab.id, () => ({ broadcast: !tab.broadcast })); refocus() }}><Radio /></button>
        <button className="icon-btn sm" data-tip={t('term.snippets')} onClick={(e) => snippetsMenu(e.currentTarget)}><SquareCode /></button>
        <div className="vdivider" />
        <button className={`icon-btn sm${tab.follow ? ' on' : ''}`} data-tip={t('term.follow')} disabled={!canHook(focused)} onClick={toggleFollow}><FolderSync /></button>
        <button className="icon-btn sm" data-tip={t('term.reveal')} disabled={!cwds[focused.termId]} onClick={() => revealTerminalCwd(focused.conn, cwds[focused.termId])}><FolderOpen /></button>
        <div className="vdivider" />
        <button className="icon-btn sm" data-tip={t('common.search')} data-kbd="Ctrl+Shift+F" onClick={() => setFindOpen(!findOpen)}><Search /></button>
        <button className="icon-btn sm" data-tip={t('common.copy')} data-kbd="Ctrl+Shift+C" onClick={() => navigator.clipboard?.writeText(reg.panes.get(focused.termId)?.xt.getSelection() || '')}><Copy /></button>
        <button className="icon-btn sm" data-tip={t('common.paste')} data-kbd="Ctrl+Shift+V" onClick={() => navigator.clipboard?.readText().then((txt) => insert(txt))}><ClipboardPaste /></button>
        <button className="icon-btn sm" data-tip={t('term.fontSmaller')} onClick={() => setFontSize((n) => Math.max(9, n - 1))}><Minus /></button>
        <button className="icon-btn sm" data-tip={t('term.fontLarger')} onClick={() => setFontSize((n) => Math.min(28, n + 1))}><Plus /></button>
        {exited && panes.length === 1 && (
          <button className="btn sm primary" onClick={() => { closeTab(tab.id); openTerminal(tab.conn) }}><RotateCw /> {t('term.reopen')}</button>
        )}
      </div>
      <div className={`term-panes ${grid}`} style={grid === 'columns' ? { gridTemplateColumns: `repeat(${panes.length}, minmax(0, 1fr))` } : grid === 'rows' ? { gridTemplateRows: `repeat(${panes.length}, minmax(0, 1fr))` } : undefined}>
        {panes.map((p) => (
          <TermPane
            key={p.termId}
            pane={p}
            tabId={tab.id}
            reg={reg}
            fontSize={fontSize}
            focused={p.termId === focused.termId}
            many={panes.length > 1}
            cwd={cwds[p.termId]}
            onFocus={() => setFocusId(p.termId)}
            onCwd={onCwd(p)}
            onFind={() => setFindOpen(true)}
          />
        ))}
      </div>
    </div>
  )
}
