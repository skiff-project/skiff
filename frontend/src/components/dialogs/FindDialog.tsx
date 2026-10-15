import { useState } from 'react'
import { FileSearch } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, get, set, type Side } from '../../lib/store'
import { displayPath, setBottom } from '../../lib/actions'
import { sessionTab } from '../../lib/fileops'
import { t } from '../../lib/i18n'

export function openFindDialog(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  openDialog((close) => <FindDialog conn={p.conn} dir={displayPath(p.conn, p.path)} close={close} />)
}

function FindDialog({ conn, dir, close }: { conn: string; dir: string; close: () => void }) {
  const [d, setD] = useState(dir)
  const [mask, setMask] = useState('*')
  const [contains, setContains] = useState('')
  const [minK, setMinK] = useState('')
  const [maxK, setMaxK] = useState('')
  const [newer, setNewer] = useState('')
  const [dirs, setDirs] = useState(false)
  const caps = conn === 'local' ? get().info?.localCaps : get().conns[conn]?.caps

  const start = async () => {
    try {
      const prev = get().find
      if (prev && !prev.done) call('fs.findCancel', { id: prev.id })
      const id = await call<string>('fs.find', {
        conn, dir: d,
        opts: {
          mask, contains: contains || undefined, includeDirs: dirs,
          minSize: minK ? parseFloat(minK) * 1024 : 0, maxSize: maxK ? parseFloat(maxK) * 1024 : 0,
          newerThan: newer ? new Date(newer).toISOString() : undefined,
        },
      })
      set({ find: { id, conn, dir: d, hits: [], done: false, scanned: 0 } })
      setBottom(true, 'find')
      close()
    } catch (e) {
      errorToast(t('find.title'), e)
    }
  }

  return (
    <Dialog
      size="md"
      title={t('find.title')}
      icon={<FileSearch />}
      onClose={close}
      onSubmit={start}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.cancel')}</button>
          <button className="btn primary" onClick={start}>{t('find.start')}</button>
        </>
      }
    >
      <div className="form">
        <div className="field"><label>{t('find.mask')}</label><input className="input mono" value={mask} onChange={(e) => setMask(e.target.value)} data-autofocus /></div>
        <div className="field"><label>{t('find.in')}</label><input className="input mono" value={d} onChange={(e) => setD(e.target.value)} /></div>
        <div className="field">
          <label>{t('find.contains')}</label>
          <input className="input" value={contains} onChange={(e) => setContains(e.target.value)} placeholder={caps?.exec ? t('find.grepHint') : ''} />
        </div>
        <div className="field-row r3">
          <div className="field"><label>{t('find.minSize')}</label><input className="input tnum" type="number" value={minK} onChange={(e) => setMinK(e.target.value)} /></div>
          <div className="field"><label>{t('find.maxSize')}</label><input className="input tnum" type="number" value={maxK} onChange={(e) => setMaxK(e.target.value)} /></div>
          <div className="field"><label>{t('find.newer')}</label><input className="input" type="date" value={newer} onChange={(e) => setNewer(e.target.value)} /></div>
        </div>
        <label className="check"><input type="checkbox" checked={dirs} onChange={(e) => setDirs(e.target.checked)} /> {t('find.dirs')}</label>
      </div>
    </Dialog>
  )
}
