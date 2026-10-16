import { activeSession, barTabs, get, set } from './store'
import { activate, closeTab, cycleTab, openProject, refreshPane, savePrefs, setActiveSide, setBottom, toggleSyncBrowsing } from './actions'
import {
  copyOrMove, deleteSelected, editEntry, mkdir, newFile, properties, startRename, viewEntry, compareDirs, terminalHere, copyPaths,
  goToDialog, runCommandDialog, other,
} from './fileops'
import { dialogOpen } from './dialogs'
import { openSyncDialog } from '../components/dialogs/SyncDialog'
import { openFindDialog } from '../components/dialogs/FindDialog'
import { openWatchDialog } from '../components/dialogs/WatchDialog'
import { openPreferences } from '../components/dialogs/Preferences'
import { bookmarkKey, toggleBookmark } from '../components/pane/PathBar'

function inTextInput(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement
  if (!el) return false
  return !!el.closest('input, textarea, select, [contenteditable="true"], .cm-editor, .xterm')
}

// handleGlobalKey implements application shortcuts (Norton Commander style
// function keys plus modern ones).
export function handleGlobalKey(e: KeyboardEvent) {
  if (dialogOpen() || get().palette) return
  if (document.querySelector('.menu')) return
  const mod = e.ctrlKey || e.metaKey
  const key = e.key
  const lower = key.toLowerCase()

  // shortcuts that work everywhere (even in editors/terminals)
  if (mod && lower === 'k' && !e.shiftKey) {
    e.preventDefault()
    set({ palette: true })
    return
  }
  if (mod && e.shiftKey && lower === 'p') {
    e.preventDefault()
    set({ previewOpen: !get().previewOpen })
    return
  }
  if (mod && key === ',') {
    e.preventDefault()
    openPreferences()
    return
  }
  if (mod && e.shiftKey && lower === 'e') {
    const s = activeSession()
    if (s) {
      e.preventDefault()
      openProject(s.panes[s.active].conn, s.panes[s.active].path)
    }
    return
  }
  if (mod && (key === 'Tab' || key === 'PageDown' || key === 'PageUp')) {
    e.preventDefault()
    cycleTab(key === 'PageUp' || (key === 'Tab' && e.shiftKey) ? -1 : 1)
    return
  }
  if (mod && lower === 'w' && !e.shiftKey) {
    const tab = get().tabs.find((x) => x.id === get().activeTab)
    if (tab && tab.kind !== 'sites' && tab.kind !== 'terminal') {
      e.preventDefault()
      closeTab(tab.id)
    }
    return
  }
  if (mod && lower === 'n' && !e.shiftKey) {
    e.preventDefault()
    activate('sites')
    return
  }
  if (e.altKey && /^[1-9]$/.test(key)) {
    const tab = barTabs(get().tabs)[parseInt(key, 10) - 1]
    if (tab) {
      e.preventDefault()
      activate(tab.id)
    }
    return
  }
  if (mod && lower === 'q' && e.shiftKey) {
    e.preventDefault()
    setBottom(!get().bottom.open)
    return
  }

  const s = activeSession()
  if (!s) return
  if (inTextInput(e)) return
  const tabId = s.id
  const side = s.active
  const pane = s.panes[side]
  if (pane.renaming) return

  const run = (fn: () => any) => {
    e.preventDefault()
    e.stopPropagation()
    fn()
  }

  switch (key) {
    case 'Tab':
      if (!mod && !e.altKey && s.layout === 'commander') {
        return run(() => {
          setActiveSide(tabId, other(side))
          window.dispatchEvent(new CustomEvent('skiff:focuslist', { detail: { tabId, side: other(side) } }))
        })
      }
      return
    case 'F2':
      return run(() => startRename(tabId, side))
    case 'F3':
      return run(() => viewEntry(tabId, side))
    case 'F4':
      return run(() => editEntry(tabId, side, e.shiftKey ? 'external' : 'auto'))
    case 'F5':
      if (mod) return run(() => refreshPane(tabId, side))
      return run(() => copyOrMove(tabId, side, false, e.shiftKey))
    case 'F6':
      if (e.shiftKey) return run(() => startRename(tabId, side))
      return run(() => copyOrMove(tabId, side, true))
    case 'F7':
      return run(() => (e.shiftKey ? newFile(tabId, side) : mkdir(tabId, side)))
    case 'F8':
    case 'Delete':
      return run(() => deleteSelected(tabId, side, e.shiftKey))
    case 'F9':
      return run(() => properties(tabId, side))
    case 'F10':
      if (e.shiftKey) return
      return run(() => closeTab(tabId))
    case 'F12':
      return run(() => setBottom(!get().bottom.open, 'queue'))
  }

  if (mod && !e.altKey) {
    switch (lower) {
      case 's':
        return run(() => openSyncDialog(tabId))
      case '=':
        return run(() => compareDirs(tabId))
      case 'b':
        return run(() => toggleSyncBrowsing(tabId))
      case 'd':
        return run(() => toggleBookmark(bookmarkKey(pane.conn), pane.path, side))
      case 't':
        return run(() => terminalHere(tabId, side))
      case 'u':
        return run(() => openWatchDialog(tabId))
      case 'o':
        return run(() => goToDialog(tabId, side))
      case 'l':
        return run(() => window.dispatchEvent(new CustomEvent('skiff:editpath', { detail: { tabId, side } })))
      case 'f':
        if (e.shiftKey) return run(() => openFindDialog(tabId, side))
        return run(() => window.dispatchEvent(new CustomEvent('skiff:filter', { detail: { tabId, side } })))
      case 'c':
        if (e.shiftKey) return run(() => copyPaths(tabId, side, 'path'))
        return
      case 'x':
        if (e.shiftKey) return run(() => runCommandDialog(tabId, side))
        return
    }
  }
  if (mod && e.altKey && lower === 'h') return run(() => savePrefs({ showHidden: !get().prefs?.showHidden }))
}
