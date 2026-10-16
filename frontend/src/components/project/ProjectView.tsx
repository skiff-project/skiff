import { useEffect, useRef, useState } from 'react'
import { dialogOpen } from '../../lib/dialogs'
import { FileSearch, FolderOpen, FolderTree, MoreHorizontal, Save, Search, SquareTerminal, X } from 'lucide-react'
import { get, updateTab, useStore, type ProjectTab, type TerminalTab } from '../../lib/store'
import { closeProjectFile, openSessionAt, paneStyle, projectSaver, saveAllProject, toggleProjectTerminal } from '../../lib/actions'
import { basename, rel } from '../../lib/paths'
import { FileIcon } from '../../lib/fileicons'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import { FileEditor } from '../editor/EditorView'
import { TerminalView } from '../terminal/TerminalView'
import { Splitter } from '../ui/Splitter'
import { showMenuAt } from '../ui/Menu'
import { ProjectTree } from './ProjectTree'
import { ProjectSearch } from './ProjectSearch'
import { QuickOpen } from './QuickOpen'

/** A folder opened as a project: its tree, its files in tabs, a terminal. */
export function ProjectView({ tab, visible }: { tab: ProjectTab; visible: boolean }) {
  const style = paneStyle(tab.conn)
  const conn = useStore((s) => (tab.conn === 'local' ? null : s.conns[tab.conn]))
  const term = useStore((s) => (tab.terminal ? (s.tabs.find((x) => x.id === tab.terminal) as TerminalTab | undefined) : undefined))
  const lost = conn && conn.state !== 'connected'
  const [sideW, setSideW] = useState(280)
  const [termH, setTermH] = useState(240)
  const [quick, setQuick] = useState(false)
  const [searchFocus, setSearchFocus] = useState(0)
  const [scope, setScope] = useState('')
  const start = useRef(0)
  const dirty = tab.files.filter((f) => f.dirty).length

  const setSide = (side: ProjectTab['side']) => updateTab<ProjectTab>(tab.id, () => ({ side }))
  const searchIn = (dir: string) => {
    setScope(dir === tab.root ? '' : dir)
    setSide('search')
    setSearchFocus(Date.now())
  }

  // the shortcuts of the project, before the global ones, wherever the
  // keyboard is in the tab
  const keys = useRef<(e: KeyboardEvent) => void>(() => {})
  keys.current = (e: KeyboardEvent) => {
    const mod = e.ctrlKey || e.metaKey
    if (!mod || dialogOpen() || get().palette || quick) return
    const k = e.key.toLowerCase()
    const take = () => { e.preventDefault(); e.stopPropagation() }
    if (k === 'p' && !e.shiftKey && !e.altKey) { take(); setQuick(true) }
    else if (k === 'f' && e.shiftKey) { take(); searchIn(scope || tab.root) }
    else if (k === 's' && e.altKey) { take(); saveAllProject(tab.id) }
    else if (e.code === 'Backquote' || e.key === '`') { take(); toggleProjectTerminal(tab.id) }
    else if (k === 'w' && !e.shiftKey && tab.active && !(e.target as HTMLElement)?.closest?.('.project-term')) { take(); closeProjectFile(tab.id, tab.active) }
  }
  useEffect(() => {
    if (!visible) return
    const h = (e: KeyboardEvent) => keys.current(e)
    window.addEventListener('keydown', h, true)
    return () => window.removeEventListener('keydown', h, true)
  }, [visible])

  const more = (el: HTMLElement) => showMenuAt(el, [
    { label: t('project.openInPanel'), icon: <FolderOpen />, hidden: tab.conn === 'local' && !get().tabs.some((x) => x.kind === 'session'), onClick: () => openSessionAt(tab.conn, tab.root) },
    { label: t('project.copyRoot'), onClick: () => copyToClipboard(tab.root) },
    { separator: true },
    { label: t('project.closeAll'), disabled: !tab.files.length, onClick: async () => { for (const f of [...tab.files]) await closeProjectFile(tab.id, f.path) } },
  ], 'right')

  const relOf = (p: string) => rel(style, tab.root, p) || basename(style, p)
  const showTerm = !!term && !!tab.showTerminal

  return (
    <div className="project">
      <aside className="project-side" style={{ width: sideW }}>
        <div className="segmented project-switch">
          <button className={tab.side === 'files' ? 'on' : ''} onClick={() => setSide('files')} data-tip={t('project.files')}><FolderTree /> {t('project.files')}</button>
          <button className={tab.side === 'search' ? 'on' : ''} onClick={() => searchIn(scope || tab.root)} data-tip={t('project.search')} data-kbd="Ctrl+Shift+F"><Search /> {t('project.search')}</button>
        </div>
        <div className="project-side-body" style={{ display: tab.side === 'files' ? 'flex' : 'none' }}>
          <ProjectTree tab={tab} onSearchIn={searchIn} />
        </div>
        <div className="project-side-body" style={{ display: tab.side === 'search' ? 'flex' : 'none' }}>
          <ProjectSearch tab={tab} focusSeq={searchFocus} scope={scope} onScope={setScope} />
        </div>
      </aside>
      <Splitter dir="h" onStart={() => (start.current = sideW)} onDrag={(d) => setSideW(Math.max(180, Math.min(600, start.current + d)))} />
      <div className="project-main">
        <div className="project-tabs">
          <div className="project-tabstrip">
            {tab.files.map((f) => (
              <div
                key={f.path}
                className={`ptab${f.path === tab.active ? ' active' : ''}`}
                data-file={relOf(f.path)}
                title={relOf(f.path)}
                onMouseDown={(e) => {
                  if (e.button === 1) { e.preventDefault(); closeProjectFile(tab.id, f.path) }
                  else if (e.button === 0) updateTab<ProjectTab>(tab.id, () => ({ active: f.path }))
                }}
              >
                <FileIcon entry={{ name: basename(style, f.path), isDir: false }} size={14} />
                <span className="ellipsis">{basename(style, f.path)}</span>
                {f.dirty
                  ? <span className="tab-dirty" onMouseDown={(e) => e.stopPropagation()} onClick={() => closeProjectFile(tab.id, f.path)} />
                  : <button className="ptab-close" onMouseDown={(e) => e.stopPropagation()} onClick={() => closeProjectFile(tab.id, f.path)} data-tip={t('common.close')}><X /></button>}
              </div>
            ))}
          </div>
          <div className="project-actions">
            <button className="icon-btn sm" data-tip={t('project.goToFile')} data-kbd="Ctrl+P" onClick={() => setQuick(true)}><FileSearch /></button>
            <button className="icon-btn sm" data-tip={t('project.saveAll')} data-kbd="Ctrl+Alt+S" disabled={!dirty} onClick={() => saveAllProject(tab.id)}><Save /></button>
            <button className={`icon-btn sm${showTerm ? ' on' : ''}`} data-tip={showTerm ? t('project.hideTerminal') : t('term.title')} data-kbd="Ctrl+`" disabled={!!lost || (tab.conn !== 'local' && !conn?.caps.shell) || (tab.conn === 'local' && !get().info?.localTerminal)} onClick={() => toggleProjectTerminal(tab.id)}><SquareTerminal /></button>
            <button className="icon-btn sm" data-tip={t('common.more')} onClick={(e) => more(e.currentTarget)}><MoreHorizontal /></button>
          </div>
        </div>
        {lost && <div className="lost-bar warn"><span>{t('project.lost')}</span></div>}
        <div className="project-editors">
          {tab.files.map((f) => (
            <div key={f.path} className="project-editor" style={{ display: f.path === tab.active ? 'flex' : 'none' }}>
              <FileEditor
                id={projectSaver(tab.id, f.path)} conn={tab.conn} path={f.path} visible={visible && f.path === tab.active} dirty={f.dirty} jump={f.jump}
                label={relOf(f.path)}
                onDirty={(d) => updateTab<ProjectTab>(tab.id, (cur) => (cur.files.find((x) => x.path === f.path)?.dirty === d ? {} : { files: cur.files.map((x) => (x.path === f.path ? { ...x, dirty: d } : x)) }))}
              />
            </div>
          ))}
          {!tab.files.length && (
            <div className="empty project-empty">
              <FolderTree className="big" />
              <h4>{tab.title}</h4>
              <p className="muted">{t('project.emptyHint')}</p>
              <div className="project-keys">
                <span><kbd>Ctrl</kbd>+<kbd>P</kbd> {t('project.goToFile')}</span>
                <span><kbd>Ctrl</kbd>+<kbd>Maj</kbd>+<kbd>F</kbd> {t('project.search')}</span>
                <span><kbd>Ctrl</kbd>+<kbd>`</kbd> {t('term.title')}</span>
              </div>
            </div>
          )}
        </div>
        {term && (
          <>
            {showTerm && <Splitter dir="v" onStart={() => (start.current = termH)} onDrag={(d) => setTermH(Math.max(100, Math.min(window.innerHeight - 260, start.current - d)))} />}
            <div className="project-term" style={{ height: termH, display: showTerm ? 'flex' : 'none' }}>
              <TerminalView tab={term} visible={visible && showTerm} />
            </div>
          </>
        )}
      </div>
      {quick && <QuickOpen tab={tab} onClose={() => setQuick(false)} />}
    </div>
  )
}

