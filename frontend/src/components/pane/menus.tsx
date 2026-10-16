import {
  ArrowRightLeft, Calculator, ClipboardCopy, ClipboardPaste, Copy, Download, Edit3, ExternalLink, Eye, FilePlus2, FileSearch, FolderInput,
  FolderPlus, GitCompare, Hash, Info, Link, MousePointerSquareDashed, PenLine, RefreshCcwDot, RotateCw, Scissors, SquareTerminal, Star,
  Terminal, Trash2, Upload, Wand2, ListChecks, FolderCode,
} from 'lucide-react'
import { showMenu, type MenuItem } from '../ui/Menu'
import { get, set, type SessionTab, type Side } from '../../lib/store'
import { openProject, paneStyle, refreshPane } from '../../lib/actions'
import {
  calcSize, compareDirs, copyOrMove, copyPaths, deleteSelected, diffWithOther, editEntry, mkdir, moveToFolder, newFile, newLink, openEntry,
  openWithDefault, properties, runCustomCommand, saveToComputer, selectAll, selectByMask, selected, sessionTab, startRename, terminalHere, viewEntry, other,
} from '../../lib/fileops'
import { clipboardCopy, clipboardPaste, hasClip } from '../../lib/clipboard'
import { join } from '../../lib/paths'
import { commandName, t } from '../../lib/i18n'
import { compileMask } from '../../lib/listing'
import { openFindDialog } from '../dialogs/FindDialog'
import { openSyncDialog } from '../dialogs/SyncDialog'
import { checksumDialog } from '../dialogs/ChecksumDialog'
import { toggleBookmark, bookmarkKey } from './PathBar'

function transferLabel(tab: SessionTab, side: Side, move: boolean): string {
  const src = tab.panes[side].conn
  const dst = tab.layout === 'explorer' ? tab.panes.left.conn : tab.panes[other(side)].conn
  if (src === dst) return move ? t('ctx.moveTo') : t('ctx.copyTo')
  if (dst === 'local') return move ? t('ctx.moveTo') : t('ctx.download')
  if (src === 'local') return move ? t('ctx.moveTo') : t('ctx.upload')
  return move ? t('ctx.moveTo') : t('ctx.copyTo')
}

export function fileMenu(tabId: string, side: Side, x: number, y: number) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const entries = selected(tabId, side)
  if (!entries.length) return backgroundMenu(tabId, side, x, y)
  const one = entries.length === 1 ? entries[0] : null
  const isFile = !!one && !one.isDir
  const conn = p.conn === 'local' ? null : get().conns[p.conn]
  const caps = p.conn === 'local' ? get().info?.localCaps : conn?.caps
  const web = get().info?.mode === 'web'
  const st = paneStyle(p.conn)
  const cmds = (get().prefs?.customCommands || []).filter((c) => (c.applyDirs || entries.every((e) => !e.isDir)) && (c.local || caps?.exec))
  const items: MenuItem[] = [
    { label: one?.isDir ? t('ctx.open') : t('ctx.open'), icon: <ExternalLink />, shortcut: 'Enter', hidden: !one, onClick: () => one && openEntry(tabId, side, one) },
    { label: t('project.openAs'), icon: <FolderCode />, hidden: !one?.isDir, onClick: () => one && openProject(p.conn, join(st, p.path, one.name)) },
    { label: t('ctx.edit'), icon: <Edit3 />, shortcut: 'F4', hidden: !isFile, onClick: () => editEntry(tabId, side, 'internal') },
    { label: t('ctx.editExternal'), icon: <PenLine />, shortcut: 'Shift+F4', hidden: !isFile || web, onClick: () => editEntry(tabId, side, 'external') },
    { label: t('ctx.view'), icon: <Eye />, shortcut: 'F3', hidden: !isFile, onClick: () => viewEntry(tabId, side) },
    { label: t('ctx.openWith'), icon: <ExternalLink />, hidden: !isFile || web, onClick: () => one && openWithDefault(p.conn, join(st, p.path, one.name)) },
    { separator: true },
    { label: transferLabel(tab, side, false), icon: <Download />, shortcut: 'F5', onClick: () => copyOrMove(tabId, side, false) },
    { label: t('ctx.moveTo'), icon: <ArrowRightLeft />, shortcut: 'F6', onClick: () => copyOrMove(tabId, side, true) },
    { label: t('ctx.copyAs'), icon: <Copy />, shortcut: 'Shift+F5', onClick: () => copyOrMove(tabId, side, false, true) },
    { label: t('ctx.saveAs'), icon: <Download />, hidden: !isFile || !web, onClick: () => one && saveToComputer(p.conn, join(st, p.path, one.name)) },
    { separator: true },
    { label: t('common.cut'), icon: <Scissors />, shortcut: 'Ctrl+X', onClick: () => clipboardCopy(tabId, side, true) },
    { label: t('common.copy'), icon: <ClipboardCopy />, shortcut: 'Ctrl+C', onClick: () => clipboardCopy(tabId, side, false) },
    { label: t('common.paste'), icon: <ClipboardPaste />, shortcut: 'Ctrl+V', hidden: !hasClip(), onClick: () => clipboardPaste(tabId, side) },
    { label: t('ctx.duplicate'), icon: <Copy />, hidden: p.conn === 'local' || !caps?.serverCopy, onClick: async () => {
      const { call } = await import('../../lib/api')
      call('fs.duplicate', { conn: p.conn, paths: entries.map((e) => join(st, p.path, e.name)), dstDir: p.path })
    } },
    { label: t('ctx.moveRemote'), icon: <FolderInput />, onClick: () => moveToFolder(tabId, side) },
    { label: t('ctx.rename'), icon: <PenLine />, shortcut: 'F2', hidden: !one, onClick: () => startRename(tabId, side) },
    { label: t('ctx.delete'), icon: <Trash2 />, shortcut: 'Del', danger: true, onClick: () => deleteSelected(tabId, side) },
    { separator: true },
    {
      label: t('ctx.customCommands'), icon: <Wand2 />, hidden: !cmds.length,
      submenu: cmds.map((c) => ({ label: commandName(c), shortcut: c.shortcut, onClick: () => runCustomCommand(tabId, side, c) })),
    },
    { label: t('ctx.diffOther'), icon: <GitCompare />, hidden: !isFile, onClick: () => diffWithOther(tabId, side) },
    { label: t('ctx.checksum'), icon: <Hash />, hidden: !isFile || !caps?.checksum, onClick: () => one && checksumDialog(p.conn, join(st, p.path, one.name)) },
    { label: t('ctx.calcSize'), icon: <Calculator />, onClick: () => calcSize(tabId, side) },
    {
      label: t('ctx.copyPath'), icon: <ClipboardCopy />, submenu: [
        { label: t('ctx.copyPath'), shortcut: 'Ctrl+Shift+C', onClick: () => copyPaths(tabId, side, 'path') },
        { label: t('ctx.copyName'), onClick: () => copyPaths(tabId, side, 'name') },
        { label: t('ctx.copyUrl'), hidden: p.conn === 'local', onClick: () => copyPaths(tabId, side, 'url') },
      ],
    },
    { label: t('ctx.reveal'), icon: <FolderInput />, hidden: p.conn !== 'local' || web || !one, onClick: async () => {
      const { call } = await import('../../lib/api')
      call('fs.reveal', { conn: 'local', path: join(st, p.path, one!.name) })
    } },
    { separator: true },
    { label: t('ctx.properties'), icon: <Info />, shortcut: 'F9', onClick: () => properties(tabId, side) },
  ]
  showMenu(x, y, items)
}

export function backgroundMenu(tabId: string, side: Side, x: number, y: number) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const caps = p.conn === 'local' ? get().info?.localCaps : get().conns[p.conn]?.caps
  const key = bookmarkKey(p.conn)
  const items: MenuItem[] = [
    { label: t('ctx.newFolder'), icon: <FolderPlus />, shortcut: 'F7', onClick: () => mkdir(tabId, side) },
    { label: t('ctx.newFile'), icon: <FilePlus2 />, shortcut: 'Shift+F7', onClick: () => newFile(tabId, side) },
    { label: t('ctx.newLink'), icon: <Link />, hidden: !caps?.symlink, onClick: () => newLink(tabId, side) },
    { label: t('common.paste'), icon: <ClipboardPaste />, shortcut: 'Ctrl+V', hidden: !hasClip(), onClick: () => clipboardPaste(tabId, side) },
    { separator: true },
    { label: t('common.refresh'), icon: <RotateCw />, shortcut: 'Ctrl+R', onClick: () => refreshPane(tabId, side) },
    { label: t('pane.selectAll'), icon: <ListChecks />, shortcut: 'Ctrl+A', onClick: () => selectAll(tabId, side, 'all') },
    {
      label: t('pane.selectMask'), icon: <MousePointerSquareDashed />, submenu: [
        { label: t('pane.selectMask'), shortcut: 'Num +', onClick: () => selectByMask(tabId, side, true) },
        { label: t('pane.unselectMask'), shortcut: 'Num −', onClick: () => selectByMask(tabId, side, false) },
        { label: t('pane.invertSelection'), shortcut: 'Num *', onClick: () => selectAll(tabId, side, 'invert') },
        { label: t('pane.selectSameExt'), onClick: () => selectAll(tabId, side, 'sameExt') },
      ],
    },
    { separator: true },
    { label: t('project.openHere'), icon: <FolderCode />, onClick: () => openProject(p.conn, p.path) },
    { label: t('ctx.terminalHere'), icon: <SquareTerminal />, hidden: !caps?.shell && p.conn !== 'local', onClick: () => terminalHere(tabId, side) },
    { label: t('ctx.findHere'), icon: <FileSearch />, onClick: () => openFindDialog(tabId, side) },
    { label: t('ctx.syncHere'), icon: <RefreshCcwDot />, onClick: () => openSyncDialog(tabId) },
    { label: t('toolbar.compare'), icon: <GitCompare />, onClick: () => compareDirs(tabId) },
    { label: t('pane.addBookmark'), icon: <Star />, onClick: () => toggleBookmark(key, p.path, side) },
    { label: t('ctx.copyPath'), icon: <ClipboardCopy />, onClick: () => copyPaths(tabId, side, 'path') },
    { separator: true },
    { label: t('ctx.properties'), icon: <Info />, onClick: () => properties(tabId, side) },
  ]
  showMenu(x, y, items)
}

void compileMask
void set
void Upload
void Terminal
