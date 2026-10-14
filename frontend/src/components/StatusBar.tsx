import { ArrowDownUp, Lock, Network, ScrollText, ShieldCheck, Zap } from 'lucide-react'
import { activeSession, get, useStore } from '../lib/store'
import { closeTab, setBottom } from '../lib/actions'
import { copyOrMove, deleteSelected, editEntry, mkdir, properties, startRename, viewEntry } from '../lib/fileops'
import { formatDuration, formatSpeed } from '../lib/format'
import { t } from '../lib/i18n'

export function StatusBar() {
  const jobs = useStore((s) => s.jobs)
  const prefs = useStore((s) => s.prefs)
  const activeTab = useStore((s) => s.tabs.find((x) => x.id === s.activeTab))
  const conn = useStore((s) => (activeTab?.kind === 'session' ? s.conns[activeTab.conn] : undefined))
  const tunnels = useStore((s) => Object.keys(s.tunnels).length)
  const watches = useStore((s) => Object.keys(s.watches).length)
  const info = useStore((s) => s.info)
  const running = jobs.filter((j) => ['scanning', 'running'].includes(j.status))
  const speed = running.reduce((n, j) => n + j.speed, 0)
  const remaining = running.reduce((n, j) => n + Math.max(0, j.totalBytes - j.doneBytes), 0)
  const eta = speed > 0 ? remaining / speed : 0
  const session = activeTab?.kind === 'session'

  const fkeys: [string, string, () => void][] = [
    ['F2', t('fkey.rename'), () => withSession((id, side) => startRename(id, side))],
    ['F3', t('fkey.view'), () => withSession((id, side) => viewEntry(id, side))],
    ['F4', t('fkey.edit'), () => withSession((id, side) => editEntry(id, side))],
    ['F5', t('fkey.copy'), () => withSession((id, side) => copyOrMove(id, side, false))],
    ['F6', t('fkey.move'), () => withSession((id, side) => copyOrMove(id, side, true))],
    ['F7', t('fkey.mkdir'), () => withSession((id, side) => mkdir(id, side))],
    ['F8', t('fkey.delete'), () => withSession((id, side) => deleteSelected(id, side))],
    ['F9', t('fkey.props'), () => withSession((id, side) => properties(id, side))],
    ['F10', t('fkey.quit'), () => withSession((id) => closeTab(id))],
  ]

  return (
    <div className="statusbar">
      {session && prefs?.showFKeyBar ? (
        <div className="fkeys">
          {fkeys.map(([k, label, fn]) => (
            <button key={k} className="fkey" onClick={fn}>
              <kbd>{k}</kbd>
              <span>{label}</span>
            </button>
          ))}
        </div>
      ) : (
        <div className="status-left">
          {info?.mode === 'web' ? <span className="faint">Skiff web · {info.hostname}</span> : <span className="faint">Skiff {info?.version}</span>}
        </div>
      )}
      <div className="spacer" />
      {conn && (
        <span className="status-item" data-tip={conn.url}>
          {conn.protocol === 'ftp' && !conn.url.startsWith('ftps') && !conn.url.startsWith('ftpes') ? <Lock style={{ opacity: 0.35 }} /> : <ShieldCheck className="ok-text" />}
          {conn.protocol.toUpperCase()}
        </span>
      )}
      {watches > 0 && (
        <button className="status-item btnlike" onClick={() => setBottom(true, 'watch')}>
          <Zap /> {watches}
        </button>
      )}
      {tunnels > 0 && (
        <button className="status-item btnlike" onClick={() => setBottom(true, 'tunnels')}>
          <Network /> {tunnels}
        </button>
      )}
      <button className="status-item btnlike" onClick={() => setBottom(true, 'log')} data-tip={t('toolbar.log')}>
        <ScrollText />
      </button>
      <button className={`status-item btnlike${running.length ? ' active' : ''}`} onClick={() => setBottom(!(get().bottom.open && get().bottom.tab === 'queue'), 'queue')}>
        <ArrowDownUp />
        {running.length > 0 ? (
          <span className="tnum">
            {formatSpeed(speed)} · {formatDuration(eta)}
          </span>
        ) : (
          <span>{t('panel.queue')}</span>
        )}
      </button>
    </div>
  )
}

function withSession(fn: (tabId: string, side: 'left' | 'right') => void) {
  const s = activeSession()
  if (s) fn(s.id, s.active)
}
