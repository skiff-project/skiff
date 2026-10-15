import { useState } from 'react'
import { Zap } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, set, toast } from '../../lib/store'
import { displayPath, setBottom } from '../../lib/actions'
import { sessionTab } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import type { WatchInfo } from '../../lib/types'

export function openWatchDialog(tabId: string) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const local = tab.panes.left.conn === 'local' ? tab.panes.left : tab.panes.right.conn === 'local' ? tab.panes.right : null
  const remote = tab.panes.right.conn !== 'local' ? tab.panes.right : tab.panes.left
  if (!local || remote.conn === 'local') {
    toast('warn', t('watch.title'), 'Show a local folder in one panel and a remote folder in the other.')
    return
  }
  openDialog((close) => <WatchDialog localDir={displayPath('local', local.path)} conn={remote.conn} remoteDir={remote.path} close={close} />)
}

function WatchDialog({ localDir, conn, remoteDir, close }: { localDir: string; conn: string; remoteDir: string; close: () => void }) {
  const [ld, setLd] = useState(localDir)
  const [rd, setRd] = useState(remoteDir)
  const [del, setDel] = useState(false)
  const [initial, setInitial] = useState(false)
  const [rec, setRec] = useState(true)
  const [mask, setMask] = useState('')
  const start = async () => {
    try {
      const w = await call<WatchInfo>('watch.start', { localDir: ld, remoteConn: conn, remoteDir: rd, delete: del, initialSync: initial, recursive: rec, fileMask: mask })
      set((s) => ({ watches: { ...s.watches, [w.id]: w } }))
      toast('ok', t('watch.started'), ld)
      setBottom(true, 'watch')
      close()
    } catch (e) {
      errorToast(t('watch.title'), e)
    }
  }
  return (
    <Dialog
      size="md"
      title={t('watch.title')}
      icon={<Zap />}
      onClose={close}
      onSubmit={start}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.cancel')}</button>
          <button className="btn primary" onClick={start}><Zap /> {t('common.start')}</button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0 }}>{t('watch.desc')}</p>
      <div className="form">
        <div className="field"><label>{t('sync.localDir')}</label><input className="input mono" value={ld} onChange={(e) => setLd(e.target.value)} /></div>
        <div className="field"><label>{t('sync.remoteDir')}</label><input className="input mono" value={rd} onChange={(e) => setRd(e.target.value)} /></div>
        <div className="field"><label>{t('transfer.fileMask')}</label><input className="input mono" value={mask} placeholder="* | .git/; *.swp" onChange={(e) => setMask(e.target.value)} /></div>
        <label className="check"><input type="checkbox" checked={rec} onChange={(e) => setRec(e.target.checked)} /> {t('watch.recursive')}</label>
        <label className="check"><input type="checkbox" checked={initial} onChange={(e) => setInitial(e.target.checked)} /> {t('watch.initial')}</label>
        <label className="check"><input type="checkbox" checked={del} onChange={(e) => setDel(e.target.checked)} /> <span className={del ? 'danger-text' : ''}>{t('watch.delete')}</span></label>
      </div>
    </Dialog>
  )
}
