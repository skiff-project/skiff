import { useEffect, useState } from 'react'
import { useStore, type SessionTab, type Side } from '../../lib/store'
import { setActiveSide, paneStyle } from '../../lib/actions'
import { PathBar } from './PathBar'
import { DirTree } from './DirTree'
import { FileList } from './FileList'
import { formatSize } from '../../lib/format'
import { t } from '../../lib/i18n'
import { visibleEntries } from '../../lib/listing'
import { call } from '../../lib/api'

export function FilePane({ tabId, side, visible }: { tabId: string; side: Side; visible: boolean }) {
  const tab = useStore((s) => s.tabs.find((x) => x.id === tabId)) as SessionTab
  const pane = tab.panes[side]
  const active = tab.active === side
  const showTree = useStore((s) => !!s.prefs?.showTree)
  return (
    <div className={`pane${active ? ' active' : ''}`} onMouseDown={() => setActiveSide(tabId, side)}>
      <PathBar tabId={tabId} side={side} pane={pane} />
      <div className="pane-body">
        {showTree && <DirTree tabId={tabId} side={side} pane={pane} />}
        <FileList tabId={tabId} side={side} pane={pane} active={active} visible={visible} layout={tab.layout} />
      </div>
      <PaneStatus pane={pane} />
    </div>
  )
}

function PaneStatus({ pane }: { pane: SessionTab['panes']['left'] }) {
  const prefs = useStore((s) => s.prefs)
  const caps = useStore((s) => (pane.conn === 'local' ? s.info?.localCaps : s.conns[pane.conn]?.caps))
  const [space, setSpace] = useState<{ total: number; free: number } | null>(null)
  const vis = visibleEntries(pane, prefs)
  const sel = vis.filter((e) => pane.selected.has(e.name))
  const selBytes = sel.reduce((n, e) => n + (e.isDir ? 0 : e.size), 0)
  const totalBytes = vis.reduce((n, e) => n + (e.isDir ? 0 : e.size), 0)
  const hidden = pane.entries.length - vis.length
  const fmt = prefs?.sizeFormat || 'iec'

  useEffect(() => {
    if (!caps?.space || !pane.path || !pane.loadedAt) return
    let alive = true
    call<{ total: number; free: number }>('fs.space', { conn: pane.conn, path: pane.path })
      .then((r) => alive && setSpace(r))
      .catch(() => alive && setSpace(null))
    return () => {
      alive = false
    }
  }, [pane.conn, pane.path, pane.loadedAt > 0, caps?.space])

  return (
    <div className="pane-status">
      <span className="tnum">
        {sel.length > 0 ? `${t('common.selected', { n: sel.length })} · ${formatSize(selBytes, fmt)}` : `${vis.length === 1 ? t('common.item') : t('common.items', { n: vis.length })} · ${formatSize(totalBytes, fmt)}`}
      </span>
      {hidden > 0 && <span className="faint">{t(pane.filter ? 'pane.filteredCount' : 'pane.hiddenCount', { n: hidden })}</span>}
      <span className="spacer" />
      {space && space.total > 0 && (
        <span className="space-meter" data-tip={t('common.free', { free: formatSize(space.free, fmt), total: formatSize(space.total, fmt) })}>
          <span className="space-bar"><span style={{ width: `${Math.min(100, (1 - space.free / space.total) * 100)}%` }} /></span>
          <span className="tnum">{formatSize(space.free, fmt)}</span>
        </span>
      )}
      <span className="faint mono" style={{ fontSize: 10.5 }}>{paneStyle(pane.conn) === 'windows' ? '' : ''}</span>
    </div>
  )
}
