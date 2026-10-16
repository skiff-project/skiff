import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'
import {
  ClipboardPaste, Columns2, Copy, FolderOpen, FolderSync, Laptop, Minus, Plus, Radio, Rows2, RotateCw, Search, Server, Settings2, SquareCode, X,
} from 'lucide-react'
import { termSocketUrl } from '../../lib/api'
import { get, toast, useStore, type TerminalTab, type TermPaneState, updateTab } from '../../lib/store'
import { closeTermPane, followTerminalCwd, loadShells, openTerminal, closeTab, revealTerminalCwd, splitTerminal, terminalPanes, toggleProjectTerminal } from '../../lib/actions'
import { shellItems } from '../../lib/shellmenu'
import { snippetName, t } from '../../lib/i18n'
import { showMenu, showMenuAt, type MenuItem } from '../ui/Menu'
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

/** Copies the selection of a terminal (the browser's own copy as fallback). */
function copySelection(xt: Terminal): boolean {
  const text = xt.getSelection()
  if (!text) return false
  if (navigator.clipboard?.writeText && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(() => {})
  } else {
    document.execCommand('copy')
  }
  xt.clearSelection()
  return true
}

/** Pastes the clipboard into a terminal; false when the page may not read it. */
function pasteInto(xt: Terminal): boolean {
  if (!navigator.clipboard?.readText || !window.isSecureContext) return false
  navigator.clipboard.readText().then((txt) => txt && xt.paste(txt)).catch(() => toast('warn', t('term.pasteDenied')))
  return true
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

function TermPane({ pane, tabId, reg, fontSize, focused, many, cwd, onFocus, onCwd, onFind, onMenu }: {
  pane: TermPaneState; tabId: string; reg: PaneRegistry; fontSize: number; focused: boolean; many: boolean
  cwd?: string; onFocus: () => void; onCwd: (path: string) => void; onFind: () => void
  onMenu: (e: React.MouseEvent, xt: Terminal) => void
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
      rightClickSelectsWord: tp?.rightClick === 'menu',
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
      const key = e.key.toLowerCase()
      const ctrlKeys = get().prefs?.terminal.copyKeys !== 'ctrlShift'
      // Ctrl+Shift+C / Ctrl+Shift+V always; Ctrl+C (with a selection) and
      // Ctrl+V like Windows Terminal unless turned off. Without a selection
      // Ctrl+C still interrupts the program.
      if (mod && !e.altKey && key === 'c' && (e.shiftKey || (ctrlKeys && xt.hasSelection()))) {
        if (copySelection(xt)) e.preventDefault()
        return false
      }
      if (mod && !e.altKey && key === 'v' && (e.shiftKey || ctrlKeys)) {
        // the browser pastes by itself when the page may not read the clipboard
        if (pasteInto(xt)) e.preventDefault()
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
      <div className="term-host" ref={host} onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); if (term.current) onMenu(e, term.current) }} />
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
  const ctrlKeys = prefs?.terminal.copyKeys !== 'ctrlShift'
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

  const splitMenu = async (el: HTMLElement, dir: 'columns' | 'rows') => {
    const targets = Object.values(conns).filter((c) => c.state === 'connected' && c.caps.shell).map((c) => ({ id: c.id, title: c.title }))
    if (info?.localTerminal) targets.push({ id: 'local', title: t('common.local') })
    const lists = await Promise.all(targets.map((x) => loadShells(x.id)))
    const items: MenuItem[] = [{ header: t('term.splitWith') }]
    targets.forEach((x, i) => {
      const same = x.id === focused.conn
      const cwd = same ? cwds[focused.termId] || focused.cwd : undefined
      const label = same ? `${x.title} (${t('term.sameServer')})` : x.title
      items.push({
        label,
        icon: x.id === 'local' ? <Laptop /> : <Server />,
        // a click opens the shell of the current pane, the submenu another one
        onClick: () => splitTerminal(tab.id, x.id, dir, cwd, same ? focused.shell : undefined),
        submenu: [
          { label: t('term.defaultShell'), onClick: () => splitTerminal(tab.id, x.id, dir, cwd) },
          { separator: true },
          ...shellItems(lists[i], (s) => splitTerminal(tab.id, x.id, dir, cwd, s.id)),
        ],
      })
    })
    showMenuAt(el, items)
  }

  // right-click: copy the selection or paste (like the Windows console),
  // or a menu
  const paneMenu = (e: React.MouseEvent, xt: Terminal, p: TermPaneState) => {
    setFocusId(p.termId)
    if (get().prefs?.terminal.rightClick !== 'menu') {
      if (!copySelection(xt) && !pasteInto(xt)) toast('warn', t('term.pasteDenied'))
      xt.focus()
      return
    }
    // the keyboard stays in the terminal after a choice
    const then = (f: () => void) => () => { f(); xt.focus() }
    showMenu(e.clientX, e.clientY, [
      { label: t('common.copy'), icon: <Copy />, shortcut: ctrlKeys ? 'Ctrl+C' : 'Ctrl+Shift+C', disabled: !xt.hasSelection(), onClick: then(() => copySelection(xt)) },
      { label: t('common.paste'), icon: <ClipboardPaste />, shortcut: ctrlKeys ? 'Ctrl+V' : 'Ctrl+Shift+V', onClick: then(() => { if (!pasteInto(xt)) toast('warn', t('term.pasteDenied')) }) },
      { label: t('term.selectAll'), onClick: then(() => xt.selectAll()) },
      { label: t('term.clear'), onClick: then(() => xt.clear()) },
      { separator: true },
      { label: t('common.search'), icon: <Search />, shortcut: 'Ctrl+Shift+F', onClick: () => setFindOpen(true) },
      { label: t('term.splitRight'), icon: <Columns2 />, onClick: () => splitTerminal(tab.id, p.conn, 'columns', cwds[p.termId] || p.cwd, p.shell) },
      { label: t('term.splitDown'), icon: <Rows2 />, onClick: () => splitTerminal(tab.id, p.conn, 'rows', cwds[p.termId] || p.cwd, p.shell) },
    ])
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
        <button className="icon-btn sm" data-tip={t('common.copy')} data-kbd={ctrlKeys ? 'Ctrl+C' : 'Ctrl+Shift+C'} onClick={() => { const xt = reg.panes.get(focused.termId)?.xt; if (xt) copySelection(xt); refocus() }}><Copy /></button>
        <button className="icon-btn sm" data-tip={t('common.paste')} data-kbd={ctrlKeys ? 'Ctrl+V' : 'Ctrl+Shift+V'} onClick={() => { const xt = reg.panes.get(focused.termId)?.xt; if (xt && !pasteInto(xt)) toast('warn', t('term.pasteDenied')); refocus() }}><ClipboardPaste /></button>
        <button className="icon-btn sm" data-tip={t('term.fontSmaller')} onClick={() => setFontSize((n) => Math.max(9, n - 1))}><Minus /></button>
        <button className="icon-btn sm" data-tip={t('term.fontLarger')} onClick={() => setFontSize((n) => Math.min(28, n + 1))}><Plus /></button>
        {exited && panes.length === 1 && (
          <button className="btn sm primary" onClick={async () => { const project = tab.embeddedIn; await closeTab(tab.id); if (project) toggleProjectTerminal(project, true); else openTerminal(tab.conn, panes[0].cwd, panes[0].shell) }}><RotateCw /> {t('term.reopen')}</button>
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
            onMenu={(e, xt) => paneMenu(e, xt, p)}
          />
        ))}
      </div>
    </div>
  )
}
