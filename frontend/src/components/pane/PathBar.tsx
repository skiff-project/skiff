import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, ArrowUp, ChevronDown, ChevronRight, Filter, HardDrive, History, Home, RotateCw, Server, Star, X } from 'lucide-react'
import { get, updatePane, useStore, type PaneState, type Side } from '../../lib/store'
import { goHistory, goHome, goUp, loadDir, navigate, paneStyle, refreshPane, savePrefs, setPaneConn } from '../../lib/actions'
import { segments } from '../../lib/paths'
import { showMenu, showMenuAt, type MenuItem } from '../ui/Menu'
import { t } from '../../lib/i18n'
import type { Bookmark } from '../../lib/types'

const EMPTY_BOOKMARKS: Bookmark[] = []

function bookmarkKey(conn: string): string {
  if (conn === 'local') return 'local'
  const c = get().conns[conn]
  return c?.siteId || `${c?.protocol}://${c?.user}@${c?.host}:${c?.port}`
}

export function PathBar({ tabId, side, pane }: { tabId: string; side: Side; pane: PaneState }) {
  const conns = useStore((s) => s.conns)
  const info = useStore((s) => s.info)
  const bookmarks = useStore((s) => s.prefs?.bookmarks?.[bookmarkKey(pane.conn)] ?? EMPTY_BOOKMARKS)
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState(pane.path)
  const [filterOpen, setFilterOpen] = useState(false)
  const crumbsRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const filterRef = useRef<HTMLInputElement>(null)
  const style = paneStyle(pane.conn)
  const conn = conns[pane.conn]

  useEffect(() => setValue(pane.path), [pane.path])
  useEffect(() => {
    // keep the last crumb visible
    const el = crumbsRef.current
    if (el) el.scrollLeft = el.scrollWidth
  }, [pane.path, editing])
  useEffect(() => {
    if (pane.filter && !filterOpen) setFilterOpen(true)
  }, [pane.filter])

  useEffect(() => {
    const onFilter = (e: Event) => {
      const d = (e as CustomEvent).detail
      if (d.tabId === tabId && d.side === side) {
        setFilterOpen(true)
        setTimeout(() => filterRef.current?.focus(), 0)
      }
    }
    const onEdit = (e: Event) => {
      const d = (e as CustomEvent).detail
      if (d.tabId === tabId && d.side === side) startEdit()
    }
    window.addEventListener('skiff:filter', onFilter)
    window.addEventListener('skiff:editpath', onEdit)
    return () => {
      window.removeEventListener('skiff:filter', onFilter)
      window.removeEventListener('skiff:editpath', onEdit)
    }
  })

  const startEdit = () => {
    setValue(pane.path)
    setEditing(true)
    setTimeout(() => {
      inputRef.current?.focus()
      inputRef.current?.select()
    }, 0)
  }

  const connMenu = (el: HTMLElement) => {
    const items: MenuItem[] = [
      { header: t('pane.switchConn') },
      { label: `${t('common.local')} — ${info?.hostname || ''}`, icon: <HardDrive />, checked: pane.conn === 'local', onClick: () => setPaneConn(tabId, side, 'local') },
      ...Object.values(conns).filter((c) => c.state !== 'closed').map((c) => ({
        label: `${c.title} (${c.protocol.toUpperCase()})`, icon: <Server />, checked: pane.conn === c.id, onClick: () => setPaneConn(tabId, side, c.id),
      })),
    ]
    showMenuAt(el, items)
  }

  const bookmarkMenu = (el: HTMLElement) => {
    const key = bookmarkKey(pane.conn)
    const exists = bookmarks.some((b) => b.path === pane.path)
    const items: MenuItem[] = [
      {
        label: exists ? 'Remove bookmark' : t('pane.addBookmark'), icon: <Star />, shortcut: 'Ctrl+D',
        onClick: () => toggleBookmark(key, pane.path, side),
      },
      { separator: true },
      ...(bookmarks.length ? bookmarks.map((b) => ({ label: b.name, icon: <Star fill="currentColor" />, shortcut: b.path.length > 30 ? '…' + b.path.slice(-28) : b.path, onClick: () => loadDir(tabId, side, b.path) })) : [{ label: t('pane.noBookmarks'), disabled: true }]),
    ]
    showMenuAt(el, items, 'right')
  }

  const historyMenu = (el: HTMLElement) => {
    const seen = new Set<string>()
    const items: MenuItem[] = [{ header: t('pane.history') }]
    for (let i = pane.history.length - 1; i >= 0 && items.length < 16; i--) {
      const p = pane.history[i]
      if (seen.has(p)) continue
      seen.add(p)
      items.push({ label: p || '/', checked: i === pane.hIndex, onClick: () => loadDir(tabId, side, p) })
    }
    showMenuAt(el, items)
  }

  const crumbs = segments(style, pane.path)
  const isBookmarked = bookmarks.some((b) => b.path === pane.path)

  return (
    <div className="pathbar">
      <button className="conn-chip" onClick={(e) => connMenu(e.currentTarget)} data-tip={pane.conn === 'local' ? t('common.local') : conn?.url}>
        {pane.conn === 'local' ? <HardDrive /> : <Server />}
        <span className="ellipsis">{pane.conn === 'local' ? t('common.local') : conn?.title || '…'}</span>
        <ChevronDown className="chev" />
      </button>
      <div className="nav-btns">
        <button className="icon-btn sm" disabled={pane.hIndex <= 0} onClick={() => goHistory(tabId, side, -1)} onContextMenu={(e) => { e.preventDefault(); historyMenu(e.currentTarget) }} data-tip={t('pane.back')} data-kbd="Alt+←"><ArrowLeft /></button>
        <button className="icon-btn sm" disabled={pane.hIndex >= pane.history.length - 1} onClick={() => goHistory(tabId, side, 1)} data-tip={t('pane.forward')} data-kbd="Alt+→"><ArrowRight /></button>
        <button className="icon-btn sm" onClick={() => goUp(tabId, side)} data-tip={t('pane.up')} data-kbd="Backspace"><ArrowUp /></button>
      </div>
      {editing ? (
        <input
          ref={inputRef}
          autoFocus
          onFocus={(e) => e.currentTarget.select()}
          className="input sm mono path-input"
          value={value}
          spellCheck={false}
          onChange={(e) => setValue(e.target.value)}
          onBlur={() => setEditing(false)}
          onKeyDown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter') {
              navigate(tabId, side, value)
              setEditing(false)
            } else if (e.key === 'Escape') setEditing(false)
          }}
        />
      ) : (
        <div className="crumbs" ref={crumbsRef} onClick={(e) => e.target === e.currentTarget && startEdit()} onDoubleClick={startEdit}>
          {crumbs.map((c, i) => (
            <span key={c.path + i} className="crumb-wrap">
              {i > 0 && <ChevronRight className="crumb-sep" />}
              <button
                className={`crumb${i === crumbs.length - 1 ? ' last' : ''}`}
                onClick={() => loadDir(tabId, side, c.path)}
                onDragOver={(e) => e.preventDefault()}
                data-dropzone
              >
                {i === 0 && style !== 'windows' ? <span className="root-slash">/</span> : c.name}
              </button>
            </span>
          ))}
          <span className="crumb-fill" onClick={startEdit} />
        </div>
      )}
      {filterOpen && (
        <div className="filter-box">
          <Filter />
          <input
            ref={filterRef}
            className="input sm"
            placeholder={t('pane.filterPlaceholder')}
            value={pane.filter}
            spellCheck={false}
            onChange={(e) => updatePane(tabId, side, () => ({ filter: e.target.value }))}
            onKeyDown={(e) => {
              e.stopPropagation()
              if (e.key === 'Escape' || (e.key === 'Enter' && !pane.filter)) {
                updatePane(tabId, side, () => ({ filter: '' }))
                setFilterOpen(false)
              } else if (e.key === 'Enter' || e.key === 'ArrowDown') {
                window.dispatchEvent(new CustomEvent('skiff:focuslist', { detail: { tabId, side } }))
              }
            }}
          />
          <button className="icon-btn sm" onClick={() => { updatePane(tabId, side, () => ({ filter: '' })); setFilterOpen(false) }}><X /></button>
        </div>
      )}
      <div className="nav-btns">
        {!filterOpen && (
          <button className="icon-btn sm" onClick={() => { setFilterOpen(true); setTimeout(() => filterRef.current?.focus(), 0) }} data-tip={t('common.filter')} data-kbd="Ctrl+F"><Filter /></button>
        )}
        <button className="icon-btn sm" onClick={() => goHome(tabId, side)} data-tip={t('pane.home')}><Home /></button>
        <button className={`icon-btn sm${isBookmarked ? ' active' : ''}`} onClick={(e) => bookmarkMenu(e.currentTarget)} data-tip={t('pane.bookmarks')}>
          <Star fill={isBookmarked ? 'currentColor' : 'none'} />
        </button>
        <button className="icon-btn sm" onClick={(e) => historyMenu(e.currentTarget)} data-tip={t('pane.history')}><History /></button>
        <button className={`icon-btn sm${pane.loading ? ' spinning' : ''}`} onClick={() => refreshPane(tabId, side)} data-tip={t('common.refresh')} data-kbd="Ctrl+R"><RotateCw /></button>
      </div>
      {pane.loading && pane.entries.length > 0 && <div className="pathbar-progress" />}
    </div>
  )
}

export function toggleBookmark(key: string, path: string, side: Side) {
  savePrefs((p) => {
    const list: Bookmark[] = [...(p.bookmarks?.[key] || [])]
    const idx = list.findIndex((b) => b.path === path)
    if (idx >= 0) list.splice(idx, 1)
    else list.push({ name: path.split(/[\\/]/).filter(Boolean).pop() || path, path, side: side === 'left' ? 'local' : 'remote' })
    return { bookmarks: { ...p.bookmarks, [key]: list } }
  })
}

export { bookmarkKey }
void showMenu
