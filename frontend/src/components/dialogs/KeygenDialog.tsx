import { useShallow } from 'zustand/react/shallow'
import { useState } from 'react'
import { Copy, KeyRound, Server } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { t } from '../../lib/i18n'
import type { GeneratedKey, KeyFile } from '../../lib/types'
import { copyToClipboard } from '../../lib/fileops'
import { displayPath } from '../../lib/actions'
import { PasswordMeter } from './MasterPassword'

export function openKeygen() {
  return openDialog((close) => <Keygen close={close} />)
}

function Keygen({ close }: { close: () => void }) {
  const info = useStore((s) => s.info)
  const conns = useStore(useShallow((s) => Object.values(s.conns).filter((c) => (c.protocol === 'sftp' || c.protocol === 'scp') && c.state === 'connected')))
  const [type, setType] = useState('ed25519')
  const [bits, setBits] = useState(4096)
  const [comment, setComment] = useState(`${info?.hostname ? 'skiff@' + info.hostname : 'skiff'}`)
  const [pass, setPass] = useState('')
  const [format, setFormat] = useState('openssh')
  const sep = info?.localStyle === 'windows' ? '\\' : '/'
  const [saveTo, setSaveTo] = useState(displayPath('local', `${info?.localHome}${sep}.ssh${sep}id_ed25519_skiff`))
  const [res, setRes] = useState<GeneratedKey | null>(null)
  const [busy, setBusy] = useState(false)
  const [target, setTarget] = useState(conns[0]?.id || '')

  const gen = async () => {
    setBusy(true)
    try {
      setRes(await call<GeneratedKey>('keys.generate', { type, bits: type === 'ed25519' ? 0 : bits, comment, passphrase: pass, format, saveTo }))
    } catch (e) {
      errorToast(t('keygen.title'), e)
    } finally {
      setBusy(false)
    }
  }

  const install = async () => {
    if (!res || !target) return
    try {
      const path = await call<string>('keys.install', { conn: target, publicKey: res.public })
      toast('ok', t('keygen.installed', { path }))
    } catch (e) {
      errorToast(t('keygen.install'), e)
    }
  }

  return (
    <Dialog
      size="lg"
      title={t('keygen.title')}
      icon={<KeyRound />}
      onClose={close}
      onSubmit={res ? close : gen}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.close')}</button>
          {!res && <button className="btn primary" disabled={busy} onClick={gen}>{busy ? <span className="spinner" /> : <KeyRound />} {t('keygen.generate')}</button>}
        </>
      }
    >
      {!res ? (
        <div className="form">
          <div className="field">
            <label>{t('keygen.type')}</label>
            <div className="segmented">
              {[['ed25519', `Ed25519 (${t('keygen.recommended')})`], ['ecdsa', 'ECDSA'], ['rsa', 'RSA']].map(([v, l]) => (
                <button key={v} className={type === v ? 'on' : ''} onClick={() => {
                  setType(v)
                  setBits(v === 'rsa' ? 4096 : v === 'ecdsa' ? 256 : 0)
                  setSaveTo(saveTo.replace(/id_\w+?(_skiff)?(\.ppk)?$/, `id_${v}_skiff${format === 'ppk' ? '.ppk' : ''}`))
                }}>{l}</button>
              ))}
            </div>
          </div>
          {type !== 'ed25519' && (
            <div className="field">
              <label>{t('keygen.bits')}</label>
              <select className="select" style={{ width: 160 }} value={bits} onChange={(e) => setBits(parseInt(e.target.value, 10))}>
                {(type === 'rsa' ? [2048, 3072, 4096, 8192] : [256, 384, 521]).map((b) => <option key={b} value={b}>{b}</option>)}
              </select>
            </div>
          )}
          <div className="field"><label>{t('keygen.comment')}</label><input className="input" value={comment} onChange={(e) => setComment(e.target.value)} /></div>
          <div className="field"><label>{t('keygen.passphrase')}</label><input className="input" type="password" value={pass} onChange={(e) => setPass(e.target.value)} />{pass && <PasswordMeter pw={pass} />}</div>
          <div className="field">
            <label>{t('keygen.format')}</label>
            <div className="segmented">
              <button className={format === 'openssh' ? 'on' : ''} onClick={() => { setFormat('openssh'); setSaveTo(saveTo.replace(/\.ppk$/, '')) }}>OpenSSH</button>
              <button className={format === 'ppk' ? 'on' : ''} onClick={() => { setFormat('ppk'); if (!saveTo.endsWith('.ppk')) setSaveTo(saveTo + '.ppk') }}>PuTTY .ppk (v3)</button>
            </div>
          </div>
          <div className="field"><label>{t('keygen.saveTo')}</label><input className="input mono" value={saveTo} onChange={(e) => setSaveTo(e.target.value)} /></div>
        </div>
      ) : (
        <div className="form">
          <div className="alert info"><KeyRound /><span>{res.type} · <span className="mono">{res.fingerprint}</span>{res.path ? <> · <span className="mono">{displayPath('local', res.path)}</span></> : null}</span></div>
          <div className="field">
            <label>{t('keygen.public')}</label>
            <div className="code-block" style={{ cursor: 'copy' }} onClick={() => copyToClipboard(res.public)}>{res.public}</div>
            <div><button className="btn sm" onClick={() => copyToClipboard(res.public)}><Copy /> {t('common.copy')}</button></div>
          </div>
          {conns.length > 0 && (
            <div className="field">
              <label>{t('keygen.install')}</label>
              <div className="row">
                <select className="select" value={target} onChange={(e) => setTarget(e.target.value)}>
                  {conns.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}
                </select>
                <button className="btn" onClick={install}><Server /> {t('keygen.install')}</button>
              </div>
            </div>
          )}
        </div>
      )}
    </Dialog>
  )
}

// openConvertKey converts an existing private key between the OpenSSH and
// PuTTY (.ppk) formats, optionally changing its passphrase.
export function openConvertKey(key: KeyFile) {
  return openDialog((close) => <ConvertKey k={key} close={close} />)
}

function ConvertKey({ k, close }: { k: KeyFile; close: () => void }) {
  const target = k.format === 'ppk' ? 'openssh' : 'ppk'
  const [pass, setPass] = useState('')
  const [newPass, setNewPass] = useState('')
  const [saveTo, setSaveTo] = useState(displayPath('local', target === 'ppk' ? `${k.path}.ppk` : k.path.replace(/\.ppk$/i, '') + (k.path.toLowerCase().endsWith('.ppk') ? '' : '_openssh')))
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const g = await call<GeneratedKey>('keys.convert', { path: k.path, passphrase: pass, newPassphrase: newPass, format: target, saveTo })
      toast('ok', t('keygen.converted'), displayPath('local', g.path || saveTo))
      close()
    } catch (e) {
      errorToast(t('keygen.convert'), e)
      setBusy(false)
    }
  }
  return (
    <Dialog
      size="md"
      title={t('keygen.convert')}
      icon={<KeyRound />}
      onClose={close}
      onSubmit={run}
      footer={<><div className="spacer" /><button className="btn" onClick={close}>{t('common.cancel')}</button><button className="btn primary" disabled={busy || !saveTo} onClick={run}>{t('keygen.convert')}</button></>}
    >
      <div className="form">
        <div className="alert info"><KeyRound /><span><span className="mono">{displayPath('local', k.path)}</span> · {k.type} · {k.format === 'ppk' ? 'PuTTY .ppk' : 'OpenSSH'} → {target === 'ppk' ? 'PuTTY .ppk (v3)' : 'OpenSSH'}</span></div>
        {k.encrypted && <div className="field"><label>{t('keygen.currentPassphrase')}</label><input className="input" type="password" value={pass} onChange={(e) => setPass(e.target.value)} data-autofocus /></div>}
        <div className="field"><label>{t('keygen.newPassphrase')}</label><input className="input" type="password" value={newPass} onChange={(e) => setNewPass(e.target.value)} />{newPass && <PasswordMeter pw={newPass} />}</div>
        <div className="field"><label>{t('keygen.saveTo')}</label><input className="input mono" value={saveTo} onChange={(e) => setSaveTo(e.target.value)} /></div>
      </div>
    </Dialog>
  )
}
