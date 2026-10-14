import { useEffect, useState } from 'react'
import { Hash } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { get } from '../../lib/store'
import { t } from '../../lib/i18n'
import { copyToClipboard } from '../../lib/fileops'

export function checksumDialog(conn: string, path: string) {
  openDialog((close) => <ChecksumDialog conn={conn} path={path} close={close} />)
}

function ChecksumDialog({ conn, path, close }: { conn: string; path: string; close: () => void }) {
  const s3 = conn !== 'local' && get().conns[conn]?.caps.protocol === 's3'
  const [algo, setAlgo] = useState(s3 ? 'md5' : 'sha256')
  const [sum, setSum] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [verify, setVerify] = useState('')
  const run = async () => {
    setBusy(true)
    setErr('')
    setSum('')
    try {
      setSum(await call<string>('fs.checksum', { conn, path, algo }))
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    run()
  }, [algo])
  const match = verify.trim() && sum ? verify.trim().toLowerCase() === sum.toLowerCase() : null
  return (
    <Dialog size="md" title={t('ctx.checksum')} icon={<Hash />} onClose={close} footer={<><div className="spacer" /><button className="btn primary" onClick={close}>{t('common.close')}</button></>}>
      <div className="form">
        <div className="mono faint ellipsis">{path}</div>
        <div className="segmented">
          {(s3 ? ['md5'] : ['md5', 'sha1', 'sha256', 'sha512']).map((a) => <button key={a} className={algo === a ? 'on' : ''} onClick={() => setAlgo(a)}>{a.toUpperCase()}</button>)}
        </div>
        {busy && <div className="progress indeterminate"><div /></div>}
        {err && <div className="alert danger">{err}</div>}
        {sum && <div className="code-block" style={{ cursor: 'copy' }} onClick={() => copyToClipboard(sum)}>{sum}</div>}
        <div className="field">
          <label>Compare with</label>
          <input className={`input mono${match === true ? ' ok-border' : match === false ? ' bad-border' : ''}`} value={verify} onChange={(e) => setVerify(e.target.value)} placeholder="paste the expected checksum" />
          {match !== null && <span className={`hint ${match ? 'ok-text' : 'danger-text'}`}>{match ? '✓ match' : '✗ different'}</span>}
        </div>
      </div>
    </Dialog>
  )
}
