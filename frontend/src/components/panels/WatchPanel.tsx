import { useShallow } from 'zustand/react/shallow'
import { CircleAlert, FolderPlus, Square, Trash2, Upload, Zap } from 'lucide-react'
import { useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'

export function WatchPanel() {
  const watches = useStore(useShallow((s) => Object.values(s.watches)))
  const conns = useStore((s) => s.conns)
  if (!watches.length) {
    return (
      <div className="empty">
        <Zap className="big" />
        <h4>{t('watch.title')}</h4>
        <p>{t('watch.desc')}</p>
      </div>
    )
  }
  return (
    <div className="watchp">
      {watches.map((w) => (
        <div key={w.id} className="watch-card">
          <div className="watch-head">
            <span className="dot ok" />
            <div className="grow">
              <div className="strong ellipsis mono">{w.req.localDir}</div>
              <div className="faint ellipsis mono">→ {conns[w.req.remoteConn]?.title}:{w.req.remoteDir}</div>
            </div>
            <span className="badge ok"><Upload /> {w.uploads}</span>
            {w.req.delete && <span className="badge danger"><Trash2 /> {w.deletes}</span>}
            <button className="btn sm" onClick={() => call('watch.stop', { id: w.id })}><Square /> {t('common.stop')}</button>
          </div>
          <div className="watch-events selectable">
            {[...w.events].reverse().slice(0, 200).map((ev, i) => (
              <div key={i} className={`log-line ${ev.error ? 'error' : ''}`}>
                <span className="log-t">{new Date(ev.time).toLocaleTimeString()}</span>
                {ev.action === 'upload' ? <Upload className="ok-text" /> : ev.action === 'delete' ? <Trash2 className="danger-text" /> : ev.action === 'mkdir' ? <FolderPlus /> : ev.error ? <CircleAlert className="danger-text" /> : <Zap />}
                <span className="log-m">{ev.path}{ev.error ? ` — ${ev.error}` : ''}</span>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
