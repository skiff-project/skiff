import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { AlertTriangle, ArrowDown, ArrowUp, CornerLeftUp, FolderOpen, Upload } from 'lucide-react'
import { get, updatePane, useStore, type PaneState, type Side } from '../../lib/store'
import { goHistory, goUp, loadDir, paneStyle, refreshPane, savePrefs, setActiveSide } from '../../lib/actions'
import { commitRename, copyOrMove, deleteSelected, dropEntries, openEntry, selectAll, selectByMask, uploadDropped } from '../../lib/fileops'
import { visibleEntries } from '../../lib/listing'
import { formatDate, formatSize, octal, permString } from '../../lib/format'
import { FileIcon, typeLabel } from '../../lib/fileicons'
import { isRoot, join } from '../../lib/paths'
import { t } from '../../lib/i18n'
import type { Entry } from '../../lib/types'
import { backgroundMenu, fileMenu } from './menus'
import { clipboardCopy, clipboardPaste } from '../../lib/clipboard'
import { downloadUrl } from '../../lib/api'

type ColId = 'name' | 'size' | 'mtime' | 'type' | 'perm' | 'owner'
const COLS: { id: ColId; label: string; width: number; align?: 'right' }[] = [
  { id: 'name', label: 'common.name', width: 0 },
  { id: 'size', label: 'common.size', width: 86, align: 'right' },
  { id: 'mtime', label: 'common.modified', width: 140 },
  { id: 'type', label: 'common.type', width: 120 },
  { id: 'perm', label: 'common.permissions', width: 96 },
  { id: 'owner', label: 'common.owner', width: 110 },
]

const PARENT: Entry = { name: '..', size: 0, mode: 0, mtime: '', isDir: true }

const ROW_H: Record<string, number> = { compact: 24, comfortable: 28, spacious: 32 }

interface Props {
  tabId: string
  side: Side
  pane: PaneState
  active: boolean
  visible: boolean
  layout: 'commander' | 'explorer'
}

export function FileList({ tabId, side, pane, active, visible }: Props) {
  const prefs = useStore((s) => s.prefs)
  const caps = useStore((s) => (pane.conn === 'local' ? s.info?.localCaps : s.conns[pane.conn]?.caps))
  const style = paneStyle(pane.conn)
  const scrollRef = useRef<HTMLDivElement>(null)
  const [dropTarget, setDropTarget] = useState<string | null>(null) // '' = pane, name = folder
  const [dragMode, setDragMode] = useState<'copy' | 'move' | null>(null)
  const typeBuf = useRef({ text: '', at: 0 })
  const [typed, setTyped] = useState('')
  const pressed = useRef<{ name: string; reselect: boolean } | null>(null)
  const colsRef = useRef<HTMLDivElement>(null)
  const wrapRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(800)

  useEffect(() => {
    const el = wrapRef.current
    if (!el) return
    const ro = new ResizeObserver((es) => setWidth(es[0].contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const entries = useMemo(() => visibleEntries(pane, prefs), [pane.entries, pane.filter, pane.sort, prefs?.showHidden, prefs?.dirsFirst, prefs?.naturalSort])
  const showParent = !isRoot(style, pane.path) && !(style === 'windows' && pane.path === '')
  const rows = useMemo(() => (showParent ? [PARENT, ...entries] : entries), [entries, showParent])
  const rowH = ROW_H[prefs?.density || 'comfortable'] || 28

  // columns
  const hiddenCols: ColId[] = (prefs?.ui?.hiddenCols as ColId[]) ?? ['type']
  const colWidths: Record<string, number> = (prefs?.ui?.colWidths as Record<string, number>) ?? {}
  // Hide less important columns when the panel is narrow (no horizontal scroll).
  const autoHidden: ColId[] = []
  if (width < 700) autoHidden.push('owner')
  if (width < 600) autoHidden.push('type')
  if (width < 520) autoHidden.push('perm')
  if (width < 380) autoHidden.push('mtime')
  if (width < 260) autoHidden.push('size')
  const cols = COLS.filter((c) => {
    if (c.id === 'name') return true
    if (hiddenCols.includes(c.id) || autoHidden.includes(c.id)) return false
    if (c.id === 'perm' && !caps?.permissions) return false
    if (c.id === 'owner' && !caps?.owners) return false
    return true
  })
  const grid = cols.map((c) => (c.id === 'name' ? 'minmax(140px, 1fr)' : `${colWidths[c.id] || c.width}px`)).join(' ')

  const virt = useVirtualizer({ count: rows.length, getScrollElement: () => scrollRef.current, estimateSize: () => rowH, overscan: 16 })
  useEffect(() => virt.measure(), [rowH])

  const indexOf = useCallback((name?: string) => (name ? rows.findIndex((e) => e.name === name) : -1), [rows])

  // keep focus visible
  useEffect(() => {
    const i = indexOf(pane.focus)
    if (i >= 0) virt.scrollToIndex(i, { align: 'auto' })
  }, [pane.focus])

  // scroll to top on navigation
  useLayoutEffect(() => {
    if (!pane.focus && scrollRef.current) scrollRef.current.scrollTop = 0
  }, [pane.path])

  // focus the list when the pane becomes active
  useEffect(() => {
    if (active && visible && !pane.renaming) {
      const ae = document.activeElement as HTMLElement | null
      if (!ae || ae === document.body || ae.closest('.filelist') || ae.classList.contains('tab')) scrollRef.current?.focus({ preventScroll: true })
    }
  }, [active, visible, pane.path])

  useEffect(() => {
    const onFocus = (e: Event) => {
      const d = (e as CustomEvent).detail
      if (d.tabId === tabId && d.side === side) {
        scrollRef.current?.focus()
        if (!pane.focus && entries[0]) updatePane(tabId, side, () => ({ focus: entries[0].name }))
      }
    }
    window.addEventListener('skiff:focuslist', onFocus)
    return () => window.removeEventListener('skiff:focuslist', onFocus)
  })

  const setFocus = (name: string | undefined, opts: { select?: 'only' | 'toggle' | 'range' | 'none'; anchor?: boolean } = {}) => {
    updatePane(tabId, side, (p) => {
      const patch: Partial<PaneState> = { focus: name }
      const sel = opts.select ?? 'none'
      if (sel === 'only') patch.selected = name && name !== '..' ? new Set([name]) : new Set()
      else if (sel === 'toggle' && name && name !== '..') {
        const s = new Set(p.selected)
        s.has(name) ? s.delete(name) : s.add(name)
        patch.selected = s
      } else if (sel === 'range' && name) {
        const a = indexOf(p.anchor ?? p.focus)
        const b = indexOf(name)
        if (a >= 0 && b >= 0) {
          const [lo, hi] = a < b ? [a, b] : [b, a]
          patch.selected = new Set(rows.slice(lo, hi + 1).filter((e) => e !== PARENT).map((e) => e.name))
        }
      }
      if (opts.anchor || sel === 'only' || sel === 'toggle') patch.anchor = name
      return patch
    })
  }

  // ---- keyboard ----
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (pane.renaming) return
    const i = indexOf(pane.focus)
    const page = Math.max(1, Math.floor((scrollRef.current?.clientHeight || 400) / rowH) - 1)
    const move = (to: number) => {
      to = Math.max(0, Math.min(rows.length - 1, to))
      const name = rows[to]?.name
      if (!name) return
      if (e.shiftKey) setFocus(name, { select: 'range' })
      else setFocus(name, { anchor: true })
    }
    const mod = e.ctrlKey || e.metaKey
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        if (e.altKey) return
        move(i < 0 ? 0 : i + 1)
        return
      case 'ArrowUp':
        e.preventDefault()
        if (e.altKey) return goUp(tabId, side)
        move(i < 0 ? 0 : i - 1)
        return
      case 'PageDown':
        e.preventDefault()
        move(i + page)
        return
      case 'PageUp':
        e.preventDefault()
        move(i - page)
        return
      case 'Home':
        e.preventDefault()
        move(0)
        return
      case 'End':
        e.preventDefault()
        move(rows.length - 1)
        return
      case 'ArrowLeft':
        if (e.altKey) {
          e.preventDefault()
          goHistory(tabId, side, -1)
        }
        return
      case 'ArrowRight':
        if (e.altKey) {
          e.preventDefault()
          goHistory(tabId, side, 1)
        }
        return
      case 'Enter': {
        e.preventDefault()
        if (e.altKey) return
        const ent = rows[i]
        if (!ent) return
        if (ent === PARENT) return goUp(tabId, side)
        if (mod && ent.isDir) return
        openEntry(tabId, side, ent)
        return
      }
      case 'Backspace':
        e.preventDefault()
        goUp(tabId, side)
        return
      case ' ':
      case 'Insert': {
        e.preventDefault()
        const ent = rows[i]
        if (ent && ent !== PARENT) {
          setFocus(ent.name, { select: 'toggle' })
          if (e.key === 'Insert' || (e.key === ' ' && false)) move(i + 1)
        }
        return
      }
      case 'Escape':
        if (pane.selected.size) {
          e.preventDefault()
          e.stopPropagation()
          updatePane(tabId, side, () => ({ selected: new Set() }))
        } else if (pane.filter) {
          e.preventDefault()
          updatePane(tabId, side, () => ({ filter: '' }))
        }
        return
      case 'ContextMenu': {
        e.preventDefault()
        openMenuForFocus()
        return
      }
    }
    if (e.key === 'F10' && e.shiftKey) {
      e.preventDefault()
      openMenuForFocus()
      return
    }
    if (mod && !e.altKey) {
      const k = e.key.toLowerCase()
      if (k === 'a') {
        e.preventDefault()
        selectAll(tabId, side, e.shiftKey ? 'none' : 'all')
        return
      }
      if (k === 'c' && !e.shiftKey) {
        e.preventDefault()
        clipboardCopy(tabId, side, false)
        return
      }
      if (k === 'x') {
        e.preventDefault()
        clipboardCopy(tabId, side, true)
        return
      }
      if (k === 'v') {
        e.preventDefault()
        clipboardPaste(tabId, side)
        return
      }
      if (k === 'r') {
        e.preventDefault()
        refreshPane(tabId, side)
        return
      }
      return
    }
    if (e.code === 'NumpadAdd') {
      e.preventDefault()
      selectByMask(tabId, side, true)
      return
    }
    if (e.code === 'NumpadSubtract') {
      e.preventDefault()
      selectByMask(tabId, side, false)
      return
    }
    if (e.code === 'NumpadMultiply') {
      e.preventDefault()
      selectAll(tabId, side, 'invert')
      return
    }
    // type-ahead search
    if (e.key.length === 1 && !e.altKey && !mod && e.key !== ' ') {
      const now = Date.now()
      const buf = now - typeBuf.current.at > 900 ? e.key : typeBuf.current.text + e.key
      typeBuf.current = { text: buf, at: now }
      setTyped(buf)
      setTimeout(() => {
        if (Date.now() - typeBuf.current.at >= 900) setTyped('')
      }, 950)
      const lower = buf.toLowerCase()
      const start = buf.length === 1 ? i + 1 : Math.max(0, i)
      const order = [...rows.slice(start), ...rows.slice(0, start)]
      const hit = order.find((r) => r !== PARENT && r.name.toLowerCase().startsWith(lower)) || order.find((r) => r !== PARENT && r.name.toLowerCase().includes(lower))
      if (hit) setFocus(hit.name, { anchor: true })
      e.preventDefault()
    }
  }

  const openMenuForFocus = () => {
    const i = indexOf(pane.focus)
    const el = scrollRef.current?.querySelector<HTMLElement>(`[data-index="${i}"]`)
    const r = el?.getBoundingClientRect() || scrollRef.current!.getBoundingClientRect()
    const ent = rows[i]
    if (ent && ent !== PARENT) fileMenu(tabId, side, r.left + 40, r.bottom)
    else backgroundMenu(tabId, side, r.left + 40, r.top + 20)
  }

  // ---- mouse ----
  const onRowMouseDown = (e: React.MouseEvent, ent: Entry) => {
    setActiveSide(tabId, side)
    if (e.button === 2) {
      if (ent !== PARENT && !pane.selected.has(ent.name)) setFocus(ent.name, { select: 'only' })
      return
    }
    if (e.button !== 0) return
    const mod = e.ctrlKey || e.metaKey
    if (e.shiftKey) setFocus(ent.name, { select: 'range' })
    else if (mod) setFocus(ent.name, { select: 'toggle' })
    else if (pane.selected.has(ent.name) && pane.selected.size > 1) {
      pressed.current = { name: ent.name, reselect: true }
      updatePane(tabId, side, () => ({ focus: ent.name }))
    } else setFocus(ent.name, { select: 'only' })
  }

  const onRowMouseUp = (ent: Entry) => {
    if (pressed.current?.reselect && pressed.current.name === ent.name) setFocus(ent.name, { select: 'only' })
    pressed.current = null
  }

  const onContextMenu = (e: React.MouseEvent, ent?: Entry) => {
    e.preventDefault()
    e.stopPropagation()
    setActiveSide(tabId, side)
    if (ent && ent !== PARENT) fileMenu(tabId, side, e.clientX, e.clientY)
    else {
      if (!ent) updatePane(tabId, side, () => ({ selected: new Set() }))
      backgroundMenu(tabId, side, e.clientX, e.clientY)
    }
  }

  // ---- drag & drop ----
  const onDragStart = (e: React.DragEvent, ent: Entry) => {
    if (ent === PARENT) return e.preventDefault()
    pressed.current = null
    let names = [...pane.selected]
    if (!pane.selected.has(ent.name)) {
      names = [ent.name]
      setFocus(ent.name, { select: 'only' })
    }
    const payload = { tabId, side, names, conn: pane.conn, path: pane.path }
    e.dataTransfer.setData('application/x-skiff', JSON.stringify(payload))
    e.dataTransfer.setData('text/plain', names.map((n) => join(style, pane.path, n)).join('\n'))
    if (names.length === 1 && !ent.isDir) {
      // Chromium/WebView2: drag a remote file out to the desktop
      const url = new URL(downloadUrl(pane.conn, join(style, pane.path, ent.name)), location.href).href
      e.dataTransfer.setData('DownloadURL', `application/octet-stream:${ent.name}:${url}`)
    }
    e.dataTransfer.effectAllowed = 'copyMove'
    const ghost = document.createElement('div')
    ghost.className = 'drag-ghost'
    ghost.textContent = names.length === 1 ? names[0] : t('common.items', { n: names.length })
    document.body.appendChild(ghost)
    e.dataTransfer.setDragImage(ghost, -8, -8)
    setTimeout(() => ghost.remove(), 0)
    ;(window as any).__skiffDrag = payload
  }

  const dragKind = (e: React.DragEvent): 'skiff' | 'files' | null => {
    const types = Array.from(e.dataTransfer.types)
    if (types.includes('application/x-skiff')) return 'skiff'
    if (types.includes('Files')) return 'files'
    return null
  }

  const onDragOver = (e: React.DragEvent, ent?: Entry) => {
    const kind = dragKind(e)
    if (!kind) return
    e.preventDefault()
    e.stopPropagation()
    const drag = (window as any).__skiffDrag
    let target: string | null = ''
    if (ent && ent.isDir && ent !== PARENT && !(drag && drag.conn === pane.conn && drag.path === pane.path && drag.names.includes(ent.name))) target = ent.name
    else if (ent === PARENT) target = '..'
    if (kind === 'skiff' && drag && drag.conn === pane.conn && drag.path === pane.path && target === '') {
      e.dataTransfer.dropEffect = 'none'
      setDropTarget(null)
      return
    }
    let mode: 'copy' | 'move' = 'copy'
    if (kind === 'skiff' && drag) mode = drag.conn === pane.conn ? 'move' : 'copy'
    if (e.ctrlKey || e.altKey) mode = 'copy'
    if (e.shiftKey) mode = 'move'
    if (kind === 'files') mode = 'copy'
    e.dataTransfer.dropEffect = mode
    setDropTarget(target)
    setDragMode(mode)
  }

  const onDrop = async (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const target = dropTarget
    const mode = dragMode
    setDropTarget(null)
    setDragMode(null)
    if (target === null) return
    const dir = target === '' ? null : target === '..' ? null : join(style, pane.path, target)
    const parent = target === '..'
    const kind = dragKind(e)
    if (kind === 'files') {
      uploadDropped(tabId, side, e.dataTransfer, parent ? undefined : dir ?? undefined)
      return
    }
    const raw = e.dataTransfer.getData('application/x-skiff')
    if (!raw) return
    const data = JSON.parse(raw)
    let targetDir = dir
    if (parent) {
      const { dirname } = await import('../../lib/paths')
      targetDir = dirname(style, pane.path)
    }
    dropEntries(data, tabId, side, targetDir, mode === 'move')
  }

  const fmt = prefs?.sizeFormat || 'iec'
  const dateFmt = prefs?.dateFormat || 'short'

  const sortBy = (id: ColId) => {
    updatePane(tabId, side, (p) => ({ sort: { key: id, dir: p.sort.key === id ? ((-p.sort.dir) as 1 | -1) : 1 } }))
  }

  const headerMenu = async (e: React.MouseEvent) => {
    e.preventDefault()
    const { showMenu } = await import('../ui/Menu')
    showMenu(e.clientX, e.clientY, COLS.filter((c) => c.id !== 'name').map((c) => ({
      label: t(c.label),
      checked: !hiddenCols.includes(c.id),
      onClick: () => savePrefs((p) => {
        const hc: ColId[] = (p.ui?.hiddenCols as ColId[]) ?? ['type']
        return { ui: { ...p.ui, hiddenCols: hc.includes(c.id) ? hc.filter((x) => x !== c.id) : [...hc, c.id] } }
      }),
    })))
  }

  const startResize = (e: React.PointerEvent, id: ColId) => {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    const startW = colWidths[id] || COLS.find((c) => c.id === id)!.width
    const el = e.currentTarget as HTMLElement
    el.setPointerCapture(e.pointerId)
    let w = startW
    const onMove = (ev: PointerEvent) => {
      w = Math.max(50, Math.min(480, startW - (ev.clientX - startX)))
      const host = colsRef.current?.closest('.filelist-wrap') as HTMLElement | null
      host?.style.setProperty(`--w-${id}`, `${w}px`)
      const g = cols.map((c) => (c.id === 'name' ? 'minmax(140px, 1fr)' : c.id === id ? `${w}px` : `${colWidths[c.id] || c.width}px`)).join(' ')
      host?.style.setProperty('--grid', g)
    }
    const onUp = () => {
      el.removeEventListener('pointermove', onMove)
      el.removeEventListener('pointerup', onUp)
      savePrefs((p) => ({ ui: { ...p.ui, colWidths: { ...(p.ui?.colWidths || {}), [id]: w } } }))
      ;(colsRef.current?.closest('.filelist-wrap') as HTMLElement | null)?.style.removeProperty('--grid')
    }
    el.addEventListener('pointermove', onMove)
    el.addEventListener('pointerup', onUp)
  }

  const items = virt.getVirtualItems()

  return (
    <div ref={wrapRef} className={`filelist-wrap${dropTarget === '' ? ' drop-pane' : ''}`} style={{ ['--grid' as any]: grid }}>
      <div className="fl-head" ref={colsRef} onContextMenu={headerMenu}>
        {cols.map((c) => (
          <div key={c.id} className={`fl-hcell${c.align === 'right' ? ' right' : ''}${pane.sort.key === c.id ? ' sorted' : ''}`} onClick={() => sortBy(c.id)}>
            <span className="ellipsis">{t(c.label)}</span>
            {pane.sort.key === c.id && (pane.sort.dir === 1 ? <ArrowUp /> : <ArrowDown />)}
            {c.id !== 'name' && <span className="col-resize" onPointerDown={(e) => startResize(e, c.id)} onClick={(e) => e.stopPropagation()} />}
          </div>
        ))}
      </div>
      <div
        className={`filelist${active ? ' active' : ''}`}
        ref={scrollRef}
        tabIndex={0}
        onKeyDown={onKeyDown}
        onMouseDown={(e) => {
          if (e.target === e.currentTarget || (e.target as HTMLElement).classList.contains('fl-canvas')) {
            setActiveSide(tabId, side)
            if (e.button === 0 && !e.ctrlKey && !e.shiftKey) updatePane(tabId, side, () => ({ selected: new Set() }))
          }
        }}
        onContextMenu={(e) => onContextMenu(e)}
        onDragOver={(e) => onDragOver(e)}
        onDragLeave={(e) => {
          if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) setDropTarget(null)
        }}
        onDrop={onDrop}
        data-dropzone
      >
        {pane.error ? (
          <div className="empty">
            <AlertTriangle className="big" style={{ color: 'var(--danger)' }} />
            <h4>{t('pane.loadError')}</h4>
            <p className="selectable">{pane.error}</p>
            <div className="row">
              {showParent && <button className="btn sm" onClick={() => goUp(tabId, side)}><CornerLeftUp /> {t('pane.up')}</button>}
              <button className="btn sm" onClick={() => loadDir(tabId, side, pane.path, { history: 'none' })}>{t('common.retry')}</button>
            </div>
          </div>
        ) : pane.loading && !pane.entries.length ? (
          <div className="fl-skeleton">
            {Array.from({ length: 9 }).map((_, i) => (
              <div key={i} className="fl-row" style={{ height: rowH }}>
                <div className="skeleton" style={{ width: 16, height: 16, borderRadius: 4 }} />
                <div className="skeleton" style={{ width: `${30 + ((i * 37) % 45)}%`, height: 10 }} />
              </div>
            ))}
          </div>
        ) : rows.length === 0 ? (
          <div className="empty">
            {pane.filter ? <FolderOpen className="big" /> : <Upload className="big" />}
            <h4>{pane.filter ? t('palette.noResults') : t('pane.empty')}</h4>
            {!pane.filter && <p>{t('pane.emptyHint')}</p>}
          </div>
        ) : (
          <div className="fl-canvas" style={{ height: virt.getTotalSize() }}>
            {items.map((vi) => {
              const ent = rows[vi.index]
              return (
                <Row
                  key={ent === PARENT ? '..' : ent.name}
                  index={vi.index}
                  start={vi.start}
                  height={rowH}
                  ent={ent}
                  cols={cols}
                  selected={pane.selected.has(ent.name)}
                  focused={pane.focus === ent.name}
                  active={active}
                  renaming={pane.renaming === ent.name}
                  compare={pane.compare?.[ent.name]}
                  dropping={dropTarget === ent.name && ent.isDir}
                  fmt={fmt}
                  dateFmt={dateFmt}
                  onMouseDown={onRowMouseDown}
                  onMouseUp={onRowMouseUp}
                  onDoubleClick={(en) => (en === PARENT ? goUp(tabId, side) : openEntry(tabId, side, en))}
                  onContextMenu={onContextMenu}
                  onDragStart={onDragStart}
                  onDragOver={onDragOver}
                  onRename={(newName) => commitRename(tabId, side, ent.name, newName)}
                  onCancelRename={() => {
                    updatePane(tabId, side, () => ({ renaming: undefined }))
                    setTimeout(() => scrollRef.current?.focus(), 0)
                  }}
                />
              )
            })}
          </div>
        )}
        {typed && <div className="typeahead">{typed}</div>}
      </div>
      {dropTarget !== null && dragMode && (
        <div className="drop-hint">{dragMode === 'move' ? t('pane.dropMove') : t('pane.dropCopy')}{dropTarget && dropTarget !== '..' ? ` → ${dropTarget}` : ''}</div>
      )}
    </div>
  )
}

interface RowProps {
  index: number
  start: number
  height: number
  ent: Entry
  cols: typeof COLS
  selected: boolean
  focused: boolean
  active: boolean
  renaming: boolean
  compare?: string
  dropping: boolean
  fmt: any
  dateFmt: any
  onMouseDown: (e: React.MouseEvent, ent: Entry) => void
  onMouseUp: (ent: Entry) => void
  onDoubleClick: (ent: Entry) => void
  onContextMenu: (e: React.MouseEvent, ent: Entry) => void
  onDragStart: (e: React.DragEvent, ent: Entry) => void
  onDragOver: (e: React.DragEvent, ent: Entry) => void
  onRename: (name: string) => void
  onCancelRename: () => void
}

const Row = memo(function Row(p: RowProps) {
  const { ent } = p
  const parent = ent === PARENT
  const cls = `fl-row${p.selected ? ' sel' : ''}${p.focused ? ' focus' : ''}${p.dropping ? ' drop' : ''}${ent.hidden ? ' dim' : ''}${p.compare ? ` cmp-${p.compare}` : ''}`
  return (
    <div
      className={cls}
      data-index={p.index}
      style={{ transform: `translateY(${p.start}px)`, height: p.height }}
      onMouseDown={(e) => p.onMouseDown(e, ent)}
      onMouseUp={() => p.onMouseUp(ent)}
      onDoubleClick={() => p.onDoubleClick(ent)}
      onContextMenu={(e) => p.onContextMenu(e, ent)}
      draggable={!parent && !p.renaming}
      onDragStart={(e) => p.onDragStart(e, ent)}
      onDragOver={(e) => p.onDragOver(e, ent)}
    >
      {p.cols.map((c) => {
        switch (c.id) {
          case 'name':
            return (
              <div key="name" className="fl-cell name">
                {parent ? <CornerLeftUp className="parent-icon" /> : <FileIcon entry={ent} />}
                {p.renaming ? (
                  <RenameInput value={ent.name} onCommit={p.onRename} onCancel={p.onCancelRename} />
                ) : (
                  <span className="fl-name">{parent ? '..' : ent.name}</span>
                )}
                {ent.isLink && ent.linkTarget && !p.renaming && <span className="fl-link">→ {ent.linkTarget}</span>}
                {p.compare && p.compare !== 'older' && <span className={`cmp-badge ${p.compare}`}>{t(p.compare === 'missing' ? 'pane.missing' : p.compare === 'newer' ? 'pane.newer' : 'pane.differs')}</span>}
              </div>
            )
          case 'size':
            return <div key="size" className="fl-cell right tnum muted">{parent ? '' : ent.isDir ? (ent.size ? formatSize(ent.size, p.fmt) : '') : formatSize(ent.size, p.fmt)}</div>
          case 'mtime':
            return <div key="mtime" className="fl-cell tnum muted">{parent ? '' : formatDate(ent.mtime, p.dateFmt)}</div>
          case 'type':
            return <div key="type" className="fl-cell muted">{parent ? '' : typeLabel(ent)}</div>
          case 'perm':
            return <div key="perm" className="fl-cell mono muted perm" data-tip={parent ? undefined : octal(ent.mode)}>{parent ? '' : permString(ent.mode, ent.isDir, ent.isLink)}</div>
          case 'owner':
            return <div key="owner" className="fl-cell muted">{parent ? '' : [ent.owner, ent.group].filter(Boolean).join(':')}</div>
        }
        return null
      })}
    </div>
  )
})

function RenameInput({ value, onCommit, onCancel }: { value: string; onCommit: (v: string) => void; onCancel: () => void }) {
  const ref = useRef<HTMLInputElement>(null)
  const [v, setV] = useState(value)
  const done = useRef(false)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.focus()
    const dot = value.lastIndexOf('.')
    el.setSelectionRange(0, dot > 0 ? dot : value.length)
  }, [])
  return (
    <input
      ref={ref}
      className="rename-input"
      value={v}
      spellCheck={false}
      onChange={(e) => setV(e.target.value)}
      onMouseDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Enter') {
          done.current = true
          onCommit(v)
        } else if (e.key === 'Escape') {
          done.current = true
          onCancel()
        }
      }}
      onBlur={() => {
        if (!done.current) {
          done.current = true
          onCommit(v)
        }
      }}
    />
  )
}

void copyOrMove
void deleteSelected
void get
