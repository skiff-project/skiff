import { useState } from 'react'
import { KeySquare } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast } from '../../lib/store'
import { t } from '../../lib/i18n'
import type { KeychainKey } from '../../lib/types'

/** Adds a pasted private key (OpenSSH, PEM or PuTTY .ppk) to the keychain. */
export function openKeychainAdd() {
  return openDialog<KeychainKey>((close) => <KeychainAdd close={close} />)
}

function KeychainAdd({ close }: { close: (k?: KeychainKey) => void }) {
  const [data, setData] = useState('')
  const [name, setName] = useState('')
  const [pass, setPass] = useState('')
  const [busy, setBusy] = useState(false)
  const add = async () => {
    if (!data.trim()) return
    setBusy(true)
    try {
      const k = await call<KeychainKey>('keychain.add', { data, name: name.trim(), passphrase: pass })
      toast('ok', t('keychain.added'), k.name)
      close(k)
    } catch (e) {
      errorToast(t('keychain.add'), e)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      size="md"
      title={t('keychain.add')}
      icon={<KeySquare />}
      onClose={() => close()}
      footer={<><div className="spacer" /><button className="btn" onClick={() => close()}>{t('common.cancel')}</button><button className="btn primary" disabled={busy || !data.trim()} onClick={add}>{t('common.add')}</button></>}
    >
      <div className="form">
        <div className="field">
          <label>{t('keychain.privateKey')}</label>
          <textarea className="textarea mono" rows={8} value={data} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" onChange={(e) => setData(e.target.value)} spellCheck={false} data-autofocus />
          <span className="hint">{t('keychain.privateKeyHint')}</span>
        </div>
        <div className="field-row">
          <div className="field"><label>{t('keychain.name')}</label><input className="input" value={name} placeholder={t('keychain.namePlaceholder')} onChange={(e) => setName(e.target.value)} /></div>
          <div className="field"><label>{t('keygen.currentPassphrase')}</label><input className="input" type="password" autoComplete="off" value={pass} onChange={(e) => setPass(e.target.value)} /></div>
        </div>
      </div>
    </Dialog>
  )
}
