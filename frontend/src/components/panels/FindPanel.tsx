import { CornerDownRight, Download, Edit3, X } from 'lucide-react'
import { get, set, useStore, type SessionTab } from '../../lib/store'
import { call } from '../../lib/api'
import { loadDir, openEditor, paneStyle } from '../../lib/actions'
import { formatDate, formatSize } from '../../lib/format'
import { t } from '../../lib/i18n'
import { FileIcon } from '../../lib/fileicons'
import { dirname, basename } from '../../lib/paths'
import { transfer } from '../../lib/fileops'

export function FindPanel() {
  const find = useStore((s) => s.find)
  const fmt = useStore((s) => s.prefs?.sizeFormat || 'iec')
  if (!find) return null
  const st = paneStyle(find.conn)

  const goto = (p: string) => {
    const s = get()
    const tab = s.tabs.find((x) => x.kind === 'session' && (x.panes.left.conn === find.conn || x.panes.right.conn === find.conn)) as SessionTab | undefined
    if (!tab) return
    const side = tab.panes.right.conn === find.conn ? 'right' : 'left'
    set({ activeTab: tab.id })
    loadDir(tab.id, side, dirname(st, p), { focus: basename(st, p) })
  }
  const download = (p: string) => {
    const s = get()
    const home = s.info?.localHome || '/'
    transfer(find.conn, [p], 'local', home)
  }

  return (
    <div className="findp">
      <div className="logp-head">
        {!find.done ? <span className="spinner" /> : null}
        <span className="muted">{find.done ? t('find.results', { n: find.hits.length }) : t('find.searching', { n: find.scanned })}</span>
        <span className="faint mono ellipsis">{find.dir}</span>
        {find.error && <span className="danger-text ellipsis">{find.error}</span>}
        <div className="spacer" />
        {!find.done && <button className="btn sm" onClick={() => call('fs.findCancel', { id: find.id })}><X /> {t('common.stop')}</button>}
      </div>
      <div className="findp-body">
        {find.done && !find.hits.length ? (
          <div className="empty"><p>{t('find.noResults')}</p></div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>{t('common.name')}</th>
                <th>{t('props.location')}</th>
                <th style={{ textAlign: 'right' }}>{t('common.size')}</th>
                <th>{t('common.modified')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {find.hits.map((h) => (
                <tr key={h.path} onDoubleClick={() => goto(h.path)}>
                  <td><span className="row" style={{ gap: 6 }}><FileIcon entry={h.entry} size={15} /> {h.entry.name}</span></td>
                  <td className="mono muted ellipsis" style={{ maxWidth: 360 }}>{dirname(st, h.path)}</td>
                  <td className="tnum muted" style={{ textAlign: 'right' }}>{h.entry.isDir ? '' : formatSize(h.entry.size, fmt)}</td>
                  <td className="tnum muted">{formatDate(h.entry.mtime)}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="icon-btn sm" data-tip={t('find.goto')} onClick={() => goto(h.path)}><CornerDownRight /></button>
                    {!h.entry.isDir && <button className="icon-btn sm" data-tip={t('ctx.edit')} onClick={() => openEditor(find.conn, h.path)}><Edit3 /></button>}
                    {find.conn !== 'local' && <button className="icon-btn sm" data-tip={t('ctx.download')} onClick={() => download(h.path)}><Download /></button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
