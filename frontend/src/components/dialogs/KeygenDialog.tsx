import { useShallow } from 'zustand/react/shallow'
import { useEffect, useState } from 'react'
import { Copy, KeyRound, Server } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { t } from '../../lib/i18n'
import type { GeneratedKey, KeychainKey, KeyFile } from '../../lib/types'
import { copyToClipboard } from '../../lib/fileops'
import { displayPath } from '../../lib/actions'
import { PasswordMeter } from './MasterPassword'

/** Resolves with the path of the saved private key, if one was generated. */
export function openKeygen() {
  return openDialog<string>((close) => <Keygen close={close} />)
}

function Keygen({ close: closeDialog }: { close: (path?: string) => void }) {
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
  const [keychain, setKeychain] = useState(false)
  const [busy, setBusy] = useState(false)
  const [target, setTarget] = useState(conns[0]?.id || '')
  const close = () => closeDialog(res?.path)

  const gen = async () => {
    setBusy(true)
    try {
      setRes(await call<GeneratedKey>('keys.generate', { type, bits: type === 'ed25519' ? 0 : bits, comment, passphrase: pass, format, saveTo, keychain, name: comment }))
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
          <label className="switch"><input type="checkbox" checked={keychain} onChange={(e) => setKeychain(e.target.checked)} /> {t('keygen.toKeychain')}</label>
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

/** A public key the install dialog can offer. */
interface PublicChoice { id: string; label: string; group: string; publicKey: string }

// openInstallKey installs an existing public key (Skiff keychain, ~/.ssh or
// pasted) in ~/.ssh/authorized_keys of a connected SFTP or SCP server, like
// ssh-copy-id. publicKey preselects a key, conn a server.
export function openInstallKey(opts: { publicKey?: string; conn?: string } = {}) {
  return openDialog((close) => <InstallKey {...opts} close={close} />)
}

const sameKey = (a: string, b: string) => a.split(/\s+/).slice(0, 2).join(' ') === b.split(/\s+/).slice(0, 2).join(' ')

function InstallKey({ publicKey, conn, close }: { publicKey?: string; conn?: string; close: () => void }) {
  const conns = useStore(useShallow((s) => Object.values(s.conns).filter((c) => (c.protocol === 'sftp' || c.protocol === 'scp') && c.state === 'connected')))
  const [choices, setChoices] = useState<PublicChoice[] | null>(null)
  const [hidden, setHidden] = useState(0) // key files whose public key is unknown
  const [choice, setChoice] = useState('')
  const [pasted, setPasted] = useState('')
  const [target, setTarget] = useState(conn && conns.some((c) => c.id === conn) ? conn : conns[0]?.id || '')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    Promise.all([
      call<{ keys: KeychainKey[] }>('keychain.list').then((r) => r.keys).catch(() => [] as KeychainKey[]),
      call<KeyFile[]>('keys.list').catch(() => [] as KeyFile[]),
    ]).then(([chain, files]) => {
      const list: PublicChoice[] = chain.map((k) => ({ id: 'chain:' + k.id, label: `${k.name} · ${k.type}`, group: t('keychain.title'), publicKey: k.publicKey }))
      for (const f of files) {
        // a key of ~/.ssh that is also in the keychain is offered once
        if (!f.publicKey || list.some((c) => sameKey(c.publicKey, f.publicKey!))) continue
        list.push({ id: 'file:' + f.path, label: `${displayPath('local', f.path)} · ${f.type}`, group: '~/.ssh', publicKey: f.publicKey })
      }
      setHidden(files.filter((f) => !f.publicKey).length)
      setChoices(list)
      const pre = publicKey ? list.find((c) => sameKey(c.publicKey, publicKey)) : undefined
      setChoice(pre?.id || list[0]?.id || 'paste')
    })
  }, [])
  const key = choice === 'paste' ? pasted.trim() : choices?.find((c) => c.id === choice)?.publicKey || ''
  const install = async () => {
    if (!key || !target) return
    setBusy(true)
    try {
      const path = await call<string>('keys.install', { conn: target, publicKey: key })
      toast('ok', t('keygen.installed', { path }), conns.find((c) => c.id === target)?.title)
      close()
    } catch (e) {
      errorToast(t('keys.installTitle'), e)
      setBusy(false)
    }
  }
  const groups = [...new Set((choices || []).map((c) => c.group))]
  return (
    <Dialog
      size="lg"
      title={t('keys.installTitle')}
      icon={<KeyRound />}
      onClose={close}
      onSubmit={install}
      footer={<><div className="spacer" /><button className="btn" onClick={close}>{t('common.cancel')}</button><button className="btn primary" disabled={busy || !key || !target} onClick={install}>{busy ? <span className="spinner" /> : <Server />} {t('keys.install')}</button></>}
    >
      <div className="form">
        <p className="muted" style={{ margin: 0 }}>{t('keys.installDesc')}</p>
        <div className="field">
          <label>{t('keys.key')}</label>
          {choices === null ? <span className="spinner" /> : (
            <select className="select" value={choice} onChange={(e) => setChoice(e.target.value)} data-autofocus>
              {groups.map((g) => (
                <optgroup key={g} label={g}>
                  {choices.filter((c) => c.group === g).map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
                </optgroup>
              ))}
              <option value="paste">{t('keys.paste')}</option>
            </select>
          )}
          {hidden > 0 && <span className="hint">{t('keys.noPublic', { n: hidden })}</span>}
        </div>
        {choice === 'paste' ? (
          <div className="field">
            <label>{t('keygen.public')}</label>
            <textarea className="input mono" rows={4} value={pasted} placeholder="ssh-ed25519 AAAA… moi@pc" onChange={(e) => setPasted(e.target.value)} />
          </div>
        ) : key && (
          <div className="field">
            <label>{t('keygen.public')}</label>
            <div className="code-block" style={{ cursor: 'copy' }} onClick={() => copyToClipboard(key)}>{key}</div>
            <div><button className="btn sm" onClick={() => copyToClipboard(key)}><Copy /> {t('common.copy')}</button></div>
          </div>
        )}
        <div className="field">
          <label>{t('keys.server')}</label>
          {conns.length ? (
            <select className="select" value={target} onChange={(e) => setTarget(e.target.value)}>
              {conns.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}
            </select>
          ) : <div className="alert warn"><Server /><span>{t('keys.noServer')}</span></div>}
          <span className="hint">{t('keys.serverHint')}</span>
        </div>
      </div>
    </Dialog>
  )
}
