import { useRef, useState } from 'react'
import { ArrowDownUp, Command, FileDiff, Moon, Plus, Settings, SquareTerminal, Sun, X } from 'lucide-react'
import { get, set, useStore, type Tab } from '../lib/store'
import { activate, closeTab, duplicateSession, moveTab, reconnect, savePrefs, setBottom } from '../lib/actions'
import { showMenu } from './ui/Menu'
import { t } from '../lib/i18n'
import { FileIcon } from '../lib/fileicons'
import { openPreferences } from './dialogs/Preferences'

function TabIcon({ tab }: { tab: Tab }) {
  const conns = useStore((s) => s.conns)
  const connecting = useStore((s) => s.connecting[tab.id])
  if (tab.kind === 'session') {
    const c = conns[tab.conn]
    if (connecting && !connecting.error) return <span className="spinner" style={{ width: 12, height: 12, borderWidth: 2 }} />
    const state = connecting?.error ? 'danger' : c?.state === 'connected' ? 'ok' : c?.state === 'lost' ? 'danger' : 'warn'
    return <span className={`tab-dot ${state}`} style={c?.color ? { background: c.color } : undefined} />
  }
  if (tab.kind === 'editor') return <FileIcon entry={{ name: tab.title, isDir: false }} size={14} />
  if (tab.kind === 'terminal') return <SquareTerminal className="tab-svg" />
  if (tab.kind === 'diff') return <FileDiff className="tab-svg" />
  return null
}

export function TabBar() {
  const tabs = useStore((s) => s.tabs)
  const active = useStore((s) => s.activeTab)
  const jobs = useStore((s) => s.jobs)
  const prefs = useStore((s) => s.prefs)
  const terminals = useStore((s) => s.terminals)
  const [dragId, setDragId] = useState<string | null>(null)
  const [dropIdx, setDropIdx] = useState<number | null>(null)
  const stripRef = useRef<HTMLDivElement>(null)

  const running = jobs.filter((j) => ['queued', 'scanning', 'running', 'paused'].includes(j.status))
  const total = running.reduce((n, j) => n + j.totalBytes, 0)
  const done = running.reduce((n, j) => n + j.doneBytes, 0)
  const pct = total ? done / total : 0

  const dark = document.documentElement.dataset.theme === 'dark'

  const tabMenu = (e: React.MouseEvent, tab: Tab) => {
    e.preventDefault()
    if (tab.kind === 'sites') return
    showMenu(e.clientX, e.clientY, [
      { label: t('tabs.closeTab'), onClick: () => closeTab(tab.id), shortcut: 'Ctrl+W' },
      { label: t('tabs.closeOthers'), onClick: () => get().tabs.filter((x) => x.id !== tab.id && x.kind !== 'sites').forEach((x) => closeTab(x.id)) },
      { separator: true, hidden: tab.kind !== 'session' },
      { label: t('tabs.duplicate'), hidden: tab.kind !== 'session', onClick: () => duplicateSession(tab.id) },
      { label: t('tabs.reconnect'), hidden: tab.kind !== 'session', onClick: () => tab.kind === 'session' && reconnect(tab.conn) },
    ])
  }

  return (
    <div className="tabbar" onDoubleClick={(e) => e.target === e.currentTarget && activate('sites')}>
      <div className="tabstrip" ref={stripRef}>
        {tabs.map((tab, i) => {
          const isTerm = tab.kind === 'terminal'
          const exited = isTerm && terminals[tab.termId]?.exited
          return (
            <div
              key={tab.id}
              className={`tab${tab.id === active ? ' active' : ''}${tab.kind === 'sites' ? ' home' : ''}${dropIdx === i && dragId ? ' drop-before' : ''}${exited ? ' exited' : ''}`}
              onMouseDown={(e) => {
                if (e.button === 1 && tab.kind !== 'sites') {
                  e.preventDefault()
                  closeTab(tab.id)
                } else if (e.button === 0) activate(tab.id)
              }}
              onContextMenu={(e) => tabMenu(e, tab)}
              draggable={tab.kind !== 'sites'}
              onDragStart={(e) => {
                setDragId(tab.id)
                e.dataTransfer.effectAllowed = 'move'
                e.dataTransfer.setData('application/x-skiff-tab', tab.id)
              }}
              onDragOver={(e) => {
                if (!dragId) return
                e.preventDefault()
                setDropIdx(i)
              }}
              onDragEnd={() => {
                setDragId(null)
                setDropIdx(null)
              }}
              onDrop={(e) => {
                e.preventDefault()
                if (dragId) moveTab(dragId, i)
                setDragId(null)
                setDropIdx(null)
              }}
              data-dropzone
              data-tip={tab.kind === 'sites' ? t('tabs.sites') : tab.kind === 'editor' ? tab.path : undefined}
            >
              {tab.kind === 'sites' ? (
                <img src="/favicon.svg" alt="" className="tab-logo" />
              ) : (
                <>
                  <TabIcon tab={tab} />
                  <span className="tab-title">{tab.title}</span>
                  {tab.kind === 'editor' && tab.dirty ? (
                    <span className="tab-dirty" onClick={(e) => { e.stopPropagation(); closeTab(tab.id) }} />
                  ) : (
                    <button
                      className="tab-close"
                      onMouseDown={(e) => e.stopPropagation()}
                      onClick={(e) => {
                        e.stopPropagation()
                        closeTab(tab.id)
                      }}
                    >
                      <X />
                    </button>
                  )}
                </>
              )}
            </div>
          )
        })}
        <button className="icon-btn tab-new" data-tip={t('tabs.newSession')} data-kbd="Ctrl+N" onClick={() => activate('sites')}>
          <Plus />
        </button>
      </div>
      <div className="tabbar-actions">
        <button className="palette-btn" onClick={() => set({ palette: true })} data-tip={t('toolbar.commandPalette')}>
          <Command />
          <span>{t('common.search')}</span>
          <kbd>Ctrl K</kbd>
        </button>
        <button className={`icon-btn queue-btn${running.length ? ' busy' : ''}`} data-tip={t('toolbar.queue')} onClick={() => setBottom(!(get().bottom.open && get().bottom.tab === 'queue'), 'queue')}>
          {running.length > 0 && (
            <svg className="ring" viewBox="0 0 36 36">
              <circle cx="18" cy="18" r="15" />
              <circle cx="18" cy="18" r="15" className="val" style={{ strokeDashoffset: 94.2 * (1 - pct) }} />
            </svg>
          )}
          <ArrowDownUp />
          {running.length > 0 && <span className="count">{running.length}</span>}
        </button>
        <button className="icon-btn" data-tip={t('toolbar.theme')} onClick={() => savePrefs({ theme: dark ? 'light' : 'dark' })}>
          {dark ? <Sun /> : <Moon />}
        </button>
        <button className="icon-btn" data-tip={t('toolbar.preferences')} data-kbd="Ctrl+," onClick={() => openPreferences()}>
          <Settings />
        </button>
        {prefs && null}
      </div>
    </div>
  )
}
