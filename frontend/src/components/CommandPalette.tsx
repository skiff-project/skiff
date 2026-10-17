import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowRightLeft, Copy, CornerDownLeft, File, FileSearch, Folder, FolderPlus, GitCompareArrows, KeyRound, Link2, Moon, Network, Plug, Plus,
  RefreshCcwDot, ScrollText, Server, Settings, SquareTerminal, Trash2, Upload, Zap, Eye, Download, Lock, Activity, Box, CalendarClock, FolderCode, Cog, EthernetPort, HardDrive, KeySquare,
} from 'lucide-react'
import { activeSession, get, set, useStore } from '../lib/store'
import { activate, connectSite, connectUrl, loadDir, openMonitor, openProject, openTasks, openUsage, openTerminal, savePrefs, setBottom, toggleSyncBrowsing, attachTerminalTab, openWorkspace, saveWorkspace } from '../lib/actions'
import { prompt } from './dialogs/common'
import { compareDirs, copyOrMove, deleteSelected, mkdir, newFile, terminalHere, openEntry } from '../lib/fileops'
import { t } from '../lib/i18n'
import { call } from '../lib/api'
import { openSyncDialog } from './dialogs/SyncDialog'
import { openFindDialog } from './dialogs/FindDialog'
import { openWatchDialog } from './dialogs/WatchDialog'
import { openPreferences } from './dialogs/Preferences'
import { openContainers } from './dialogs/ContainersDialog'
import { openImportDialog } from './dialogs/ImportDialog'
import { openServices } from './dialogs/ServicesDialog'
import { openPorts } from './dialogs/PortsDialog'
import { openInstallKey, openKeygen } from './dialogs/KeygenDialog'
import { openTunnelDialog } from './dialogs/TunnelDialog'
import { openAuthorizedKeys } from './keys/AuthorizedKeysDialog'
import { FileIcon } from '../lib/fileicons'
import { visibleEntries } from '../lib/listing'

interface Item {
  id: string
  group: string
  label: string
  hint?: string
  icon: any
  keys?: string
  run: () => void
}

function score(text: string, q: string): number {
  if (!q) return 1
  text = text.toLowerCase()
  q = q.toLowerCase()
  const i = text.indexOf(q)
  if (i >= 0) return 100 - i + (i === 0 ? 50 : 0)
  // fuzzy subsequence
  let ti = 0
  let s = 0
  for (const c of q) {
    const f = text.indexOf(c, ti)
    if (f < 0) return 0
    s += f === ti ? 3 : 1
    ti = f + 1
  }
  return s
}

export function CommandPalette() {
  const [q, setQ] = useState('')
  const [hot, setHot] = useState(0)
  const sites = useStore((s) => s.sites)
  const terminals = useStore((s) => s.terminals)
  const conns = useStore((s) => s.conns)
  const listRef = useRef<HTMLDivElement>(null)
  const close = () => set({ palette: false })
  const sess = activeSession()

  const items = useMemo<Item[]>(() => {
    const out: Item[] = []
    const s = sess
    const side = s?.active ?? 'right'
    // URL quick connect
    if (/^(sftp|scp|ftp|ftps|ftpes|davs?|webdavs?|s3|ssh):\/\//i.test(q.trim())) {
      out.push({ id: 'url', group: t('sites.quick'), label: `${t('common.connect')} ${q.trim()}`, icon: Plug, run: () => connectUrl(q.trim()) })
    }
    if (s && (q.startsWith('/') || q.startsWith('~') || /^[A-Za-z]:\\/.test(q))) {
      out.push({ id: 'goto', group: t('dialog.goTo'), label: q, icon: Folder, run: () => loadDir(s.id, side, q) })
    }
    for (const site of sites) {
      out.push({
        id: 'site:' + site.id, group: t('palette.sites'), label: site.name || site.host,
        hint: `${site.protocol.toUpperCase()} · ${site.user ? site.user + '@' : ''}${site.host}${site.folder ? ' · ' + site.folder : ''}`,
        icon: Server, run: () => connectSite(site.id),
      })
    }
    for (const w of get().prefs?.workspaces || []) {
      out.push({ id: 'ws:' + w.name, group: t('ws.title'), label: w.name, hint: `${w.tabs.length} session(s)`, icon: Server, run: () => openWorkspace(w.name) })
    }
    for (const tm of Object.values(terminals)) {
      out.push({ id: 'term:' + tm.id, group: t('term.title'), label: tm.title, icon: SquareTerminal, run: () => attachTerminalTab(tm.id, tm.conn, tm.title) })
    }
    const cmd = (id: string, label: string, icon: any, run: () => void, keys?: string, hidden = false) => {
      if (!hidden) out.push({ id: 'cmd:' + id, group: t('palette.commands'), label, icon, run, keys })
    }
    cmd('newsite', t('sites.newSite'), Plus, () => activate('sites'), 'Ctrl+N')
    cmd('import', t('sites.import'), Upload, () => openImportDialog())
    cmd('wsave', t('ws.save'), Copy, async () => { const n = await prompt({ title: t('ws.title'), label: t('ws.name'), value: '' }); if (n) saveWorkspace(n) })
    cmd('keygen', t('prefs.generateKey'), KeyRound, () => openKeygen())
    cmd('installkey', t('keys.installTitle'), KeyRound, () => openInstallKey({ conn: s?.conn }))
    cmd('localctr', t('ctr.titleLocal'), Box, () => openContainers(), undefined, !get().info?.localTerminal)
    cmd('lock', t('prefs.lockNow'), Lock, () => call('security.lock'), undefined, !(get().info?.hasMaster && !get().info?.locked))
    cmd('prefs', t('prefs.title'), Settings, () => openPreferences(), 'Ctrl+,')
    cmd('theme', t('toolbar.theme'), Moon, () => savePrefs({ theme: document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark' }))
    cmd('queue', t('toolbar.queue'), ArrowRightLeft, () => setBottom(true, 'queue'), 'F12')
    cmd('log', t('toolbar.log'), ScrollText, () => setBottom(true, 'log'))
    cmd('tasks', t('tasks.title'), CalendarClock, () => openTasks())
    cmd('localterm', `${t('term.title')} (${t('common.local')})`, SquareTerminal, () => openTerminal('local'), undefined, !get().info?.localTerminal)
    if (get().info?.localTerminal) {
      for (const sh of get().shells.local || []) {
        out.push({ id: 'shell:' + sh.id, group: t('term.title'), label: t('term.localWith', { name: sh.name }), icon: SquareTerminal, run: () => openTerminal('local', undefined, sh.id) })
      }
    }
    if (s) {
      cmd('copy', t('fkey.copy'), Copy, () => copyOrMove(s.id, side, false), 'F5')
      cmd('move', t('fkey.move'), ArrowRightLeft, () => copyOrMove(s.id, side, true), 'F6')
      cmd('mkdir', t('ctx.newFolder'), FolderPlus, () => mkdir(s.id, side), 'F7')
      cmd('newfile', t('ctx.newFile'), File, () => newFile(s.id, side), 'Shift+F7')
      cmd('delete', t('ctx.delete'), Trash2, () => deleteSelected(s.id, side), 'F8')
      cmd('sync', t('toolbar.sync'), RefreshCcwDot, () => openSyncDialog(s.id), 'Ctrl+S')
      cmd('compare', t('toolbar.compare'), GitCompareArrows, () => compareDirs(s.id), 'Ctrl+=')
      cmd('find', t('toolbar.find'), FileSearch, () => openFindDialog(s.id, side), 'Ctrl+Shift+F')
      cmd('watch', t('toolbar.keepUpToDate'), Zap, () => openWatchDialog(s.id), 'Ctrl+U')
      cmd('syncbrowse', t('toolbar.syncBrowsing'), Link2, () => toggleSyncBrowsing(s.id), 'Ctrl+B')
      cmd('term', t('toolbar.terminal'), SquareTerminal, () => terminalHere(s.id, side), 'Ctrl+T')
      cmd('project', t('project.openHere'), FolderCode, () => openProject(s.panes[side].conn, s.panes[side].path), 'Ctrl+Shift+E')
      cmd('tunnel', t('tunnel.new'), Network, () => openTunnelDialog(s.conn), undefined, !conns[s.conn]?.caps.tunnels)
      cmd('monitor', t('mon.open'), Activity, () => openMonitor(s.conn), undefined, !conns[s.conn]?.caps.shell)
      cmd('services', t('svc.open'), Cog, () => openServices(s.conn), undefined, !conns[s.conn]?.caps.shell)
      cmd('ports', t('ports.openDialog'), EthernetPort, () => openPorts(s.conn), undefined, !conns[s.conn]?.caps.shell || !conns[s.conn]?.caps.tunnels)
      cmd('usage', t('usage.here'), HardDrive, () => openUsage(s.panes[side].conn, s.panes[side].path))
      cmd('containers', t('ctr.ofServer'), Box, () => openContainers({ via: s.conn }), undefined, !conns[s.conn]?.caps.tunnels)
      cmd('authorizedkeys', t('ak.open'), KeySquare, () => openAuthorizedKeys(s.conn), undefined, conns[s.conn]?.protocol !== 'sftp' && conns[s.conn]?.protocol !== 'scp')
      cmd('hidden', t('toolbar.hidden'), Eye, () => savePrefs({ showHidden: !get().prefs?.showHidden }), 'Ctrl+Alt+H')
      // files in the current folder
      const p = s.panes[side]
      for (const e of visibleEntries(p, get().prefs).slice(0, 400)) {
        out.push({ id: 'file:' + e.name, group: t('palette.files'), label: e.name, hint: e.isDir ? '' : undefined, icon: () => <FileIcon entry={e} size={15} />, run: () => openEntry(s.id, side, e) })
      }
    }
    return out
  }, [q, sites, terminals, sess?.id, sess?.active])

  const shown = useMemo(() => {
    const scored = items.map((it) => ({ it, s: Math.max(score(it.label, q), it.hint ? score(it.hint, q) * 0.6 : 0) })).filter((x) => x.s > 0)
    if (q) scored.sort((a, b) => b.s - a.s)
    // when empty: commands first, then sites, then files
    const order = [t('sites.quick'), t('dialog.goTo'), t('palette.commands'), t('ws.title'), t('palette.sites'), t('term.title'), t('palette.files')]
    if (!q) scored.sort((a, b) => order.indexOf(a.it.group) - order.indexOf(b.it.group))
    return scored.slice(0, 80).map((x) => x.it)
  }, [items, q])

  useEffect(() => setHot(0), [q])
  useEffect(() => {
    listRef.current?.querySelector('.pal-item.hot')?.scrollIntoView({ block: 'nearest' })
  }, [hot])

  const run = (it?: Item) => {
    if (!it) return
    close()
    setTimeout(() => it.run(), 0)
  }

  let lastGroup = ''
  return (
    <div className="overlay palette-overlay" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="palette">
        <div className="pal-input">
          <input
            autoFocus
            value={q}
            placeholder={t('palette.placeholder')}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') close()
              else if (e.key === 'ArrowDown') { e.preventDefault(); setHot((h) => Math.min(shown.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHot((h) => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(shown[hot]) }
            }}
            spellCheck={false}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="pal-list" ref={listRef}>
          {shown.length === 0 && <div className="empty" style={{ height: 120 }}><p>{t('palette.noResults')}</p></div>}
          {shown.map((it, i) => {
            const header = it.group !== lastGroup ? it.group : null
            lastGroup = it.group
            const Icon = it.icon
            return (
              <div key={it.id}>
                {header && <div className="pal-group">{header}</div>}
                <div className={`pal-item${i === hot ? ' hot' : ''}`} onMouseMove={() => setHot(i)} onClick={() => run(it)}>
                  <span className="pal-icon"><Icon /></span>
                  <span className="ellipsis">{it.label}</span>
                  {it.hint && <span className="faint ellipsis pal-hint">{it.hint}</span>}
                  <span className="spacer" />
                  {it.keys && <kbd>{it.keys}</kbd>}
                  {i === hot && <CornerDownLeft className="faint" style={{ width: 14 }} />}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

void Download
