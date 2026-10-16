import { useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, ChevronsDownUp, Copy, Edit3, FilePlus, FolderPlus, RefreshCw, Search, Trash2 } from 'lucide-react'
import { call } from '../../lib/api'
import { errorToast, useStore, type ProjectTab } from '../../lib/store'
import { closeProjectFile, openProjectFile, paneStyle, renameProjectFiles } from '../../lib/actions'
import { basename, dirname, join, rel } from '../../lib/paths'
import { FileIcon } from '../../lib/fileicons'
import { showMenu, type MenuItem } from '../ui/Menu'
import { ask, prompt } from '../dialogs/common'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import type { Entry } from '../../lib/types'

type Listing = Entry[] | 'loading' | { error: string }

/**
 * The folders and files of a project, loaded as folders are opened.
 * onSearchIn starts a search limited to a folder.
 */
export function ProjectTree({ tab, onSearchIn }: { tab: ProjectTab; onSearchIn: (dir: string) => void }) {
  const style = paneStyle(tab.conn)
  const showHidden = useStore((s) => !!s.prefs?.showHidden)
  const changed = useStore((s) => s.fsChanged)
  const [dirs, setDirs] = useState<Record<string, Listing>>({})
  const [open, setOpen] = useState<Set<string>>(() => new Set([tab.root]))
  const [selected, setSelected] = useState<string>('')

  const load = async (dir: string) => {
    setDirs((d) => (d[dir] && d[dir] !== 'loading' ? d : { ...d, [dir]: 'loading' }))
    try {
      const r = await call<{ entries: Entry[] }>('fs.list', { conn: tab.conn, path: dir })
      const entries = [...r.entries].sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }))
      setDirs((d) => ({ ...d, [dir]: entries }))
    } catch (e: any) {
      setDirs((d) => ({ ...d, [dir]: { error: e?.message || String(e) } }))
    }
  }
  useEffect(() => {
    load(tab.root)
  }, [])
  // a folder changed (by Skiff or another session): reload it if shown
  useEffect(() => {
    if (!changed || changed.conn !== tab.conn) return
    if (changed.dir === '') Object.keys(dirs).forEach(load)
    else if (dirs[changed.dir]) load(changed.dir)
  }, [changed?.seq])

  const toggle = (dir: string) => {
    const next = new Set(open)
    if (next.has(dir)) next.delete(dir)
    else {
      next.add(dir)
      if (!dirs[dir] || typeof dirs[dir] === 'object' && 'error' in (dirs[dir] as object)) load(dir)
    }
    setOpen(next)
  }
  const refresh = () => [...open].forEach(load)

  // actions on the tree
  const newItem = async (dir: string, folder: boolean) => {
    const name = await prompt({ title: folder ? t('ctx.newFolder') : t('ctx.newFile'), label: t('common.name'), mono: true })
    if (!name) return
    const path = join(style, dir, name.trim())
    try {
      if (folder) await call('fs.mkdir', { conn: tab.conn, path })
      else {
        const exists = await call('fs.stat', { conn: tab.conn, path }).then(() => true, () => false)
        if (exists) throw new Error(t('project.exists', { name }))
        await call('fs.writeText', { conn: tab.conn, path, content: '', encoding: 'utf-8', eol: 'LF' })
      }
      if (!open.has(dir)) setOpen(new Set([...open, dir]))
      await load(dir)
      if (!folder) openProjectFile(tab.id, path)
    } catch (e) {
      errorToast(folder ? t('ctx.newFolder') : t('ctx.newFile'), e)
    }
  }
  const renameItem = async (e: Entry, path: string) => {
    const name = await prompt({ title: t('ctx.rename'), label: t('common.name'), value: e.name, selectStem: !e.isDir, mono: true })
    if (!name || name === e.name) return
    const to = join(style, dirname(style, path), name.trim())
    try {
      await call('fs.rename', { conn: tab.conn, from: path, to })
      renameProjectFiles(tab.id, path, to)
      await load(dirname(style, path))
    } catch (err) {
      errorToast(t('ctx.rename'), err)
    }
  }
  const deleteItem = async (e: Entry, path: string) => {
    if (!(await ask({ title: t('ctx.delete'), message: t('project.deleteConfirm', { name: e.name }), danger: true, ok: t('common.delete') }))) return
    try {
      await call('fs.delete', { conn: tab.conn, paths: [path] })
      for (const f of tab.files) if (f.path === path || f.path.startsWith(path + (style === 'windows' ? '\\' : '/'))) closeProjectFile(tab.id, f.path, true)
      await load(dirname(style, path))
    } catch (err) {
      errorToast(t('ctx.delete'), err)
    }
  }
  const menu = (ev: React.MouseEvent, e: Entry | null, path: string) => {
    ev.preventDefault()
    ev.stopPropagation()
    setSelected(path)
    const dir = !e || e.isDir ? path : dirname(style, path)
    const items: MenuItem[] = [
      { label: t('ctx.newFile'), icon: <FilePlus />, onClick: () => newItem(dir, false) },
      { label: t('ctx.newFolder'), icon: <FolderPlus />, onClick: () => newItem(dir, true) },
    ]
    if (e) {
      items.push({ separator: true })
      if (e.isDir) items.push({ label: t('project.searchIn'), icon: <Search />, onClick: () => onSearchIn(path) })
      items.push({ label: t('ctx.rename'), icon: <Edit3 />, onClick: () => renameItem(e, path) })
      items.push({ label: t('ctx.delete'), icon: <Trash2 />, danger: true, onClick: () => deleteItem(e, path) })
    }
    items.push({ separator: true })
    items.push({ label: t('ctx.copyPath'), icon: <Copy />, onClick: () => copyToClipboard(path) })
    if (path !== tab.root) items.push({ label: t('project.copyRelative'), icon: <Copy />, onClick: () => copyToClipboard(rel(style, tab.root, path) || path) })
    showMenu(ev.clientX, ev.clientY, items)
  }

  const rows = (dir: string, depth: number): React.ReactNode => {
    const l = dirs[dir]
    if (!l || l === 'loading') return <div className="ptree-note" style={{ paddingLeft: 12 + depth * 14 }}><span className="spinner sm" /></div>
    if (!Array.isArray(l)) return <div className="ptree-note danger-text" style={{ paddingLeft: 12 + depth * 14 }}>{l.error}</div>
    const shown = showHidden ? l : l.filter((e) => !e.name.startsWith('.'))
    if (!shown.length) return <div className="ptree-note faint" style={{ paddingLeft: 12 + depth * 14 }}>{t('project.emptyFolder')}</div>
    return shown.map((e) => {
      const path = join(style, dir, e.name)
      const isOpen = e.isDir && open.has(path)
      const isActive = tab.active === path
      return (
        <div key={path}>
          <div
            className={`ptree-row${isActive ? ' active' : ''}${selected === path ? ' selected' : ''}`}
            style={{ paddingLeft: 6 + depth * 14 }}
            data-path={rel(style, tab.root, path) || e.name}
            onClick={() => { setSelected(path); e.isDir ? toggle(path) : openProjectFile(tab.id, path) }}
            onContextMenu={(ev) => menu(ev, e, path)}
            title={e.name}
          >
            {e.isDir ? (isOpen ? <ChevronDown className="ptree-chev" /> : <ChevronRight className="ptree-chev" />) : <span className="ptree-chev" />}
            <FileIcon entry={e} size={15} />
            <span className="ellipsis">{e.name}</span>
            {tab.files.some((f) => f.path === path && f.dirty) && <span className="ptree-dirty" />}
          </div>
          {isOpen && rows(path, depth + 1)}
        </div>
      )
    })
  }

  return (
    <div className="ptree" onContextMenu={(ev) => menu(ev, null, tab.root)}>
      <div className="ptree-head">
        <span className="ellipsis strong" title={tab.root}>{basename(style, tab.root) || tab.root}</span>
        <div className="spacer" />
        <button className="icon-btn sm" data-tip={t('ctx.newFile')} onClick={() => newItem(tab.root, false)}><FilePlus /></button>
        <button className="icon-btn sm" data-tip={t('ctx.newFolder')} onClick={() => newItem(tab.root, true)}><FolderPlus /></button>
        <button className="icon-btn sm" data-tip={t('project.collapse')} onClick={() => setOpen(new Set([tab.root]))}><ChevronsDownUp /></button>
        <button className="icon-btn sm" data-tip={t('common.refresh')} onClick={refresh}><RefreshCw /></button>
      </div>
      <div className="ptree-body">{rows(tab.root, 0)}</div>
    </div>
  )
}

