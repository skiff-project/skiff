import { useEffect, useState } from 'react'
import { AlertTriangle, BadgeCheck, Copy, FileCode, KeyRound, KeySquare, Plus, RefreshCw, Repeat2, ShieldAlert, Trash2, Wrench } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { displayPath, openEditor } from '../../lib/actions'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import { ask } from '../dialogs/common'
import { openInstallKey } from '../dialogs/KeygenDialog'
import { useKeyChoices } from './KeyPicker'
import { openKeyRollout } from './KeyRolloutDialog'

export interface AuthorizedKey {
  line: number
  type?: string
  fingerprint?: string
  comment?: string
  options?: string[]
  ca?: boolean
  invalid?: boolean
  text: string
}
export interface Authorized {
  path: string
  exists: boolean
  keys: AuthorizedKey[]
  version: string
  looseModes?: string[]
  authKey?: string
  authCert?: boolean
}

/** A short name for a key type: Ed25519, RSA, ECDSA, Ed25519-SK… */
export function keyType(type?: string) {
  if (!type) return '?'
  const sk = type.startsWith('sk-') ? '-SK' : ''
  if (type.includes('ed25519')) return 'Ed25519' + sk
  if (type.includes('ecdsa')) return 'ECDSA' + sk
  if (type.includes('rsa')) return 'RSA'
  if (type.includes('dss')) return 'DSA'
  return type
}

/** The keys a server lets in: ~/.ssh/authorized_keys of the account. */
export function openAuthorizedKeys(conn: string) {
  return openDialog((close) => <AuthorizedKeys conn={conn} close={close} />)
}

function AuthorizedKeys({ conn, close }: { conn: string; close: () => void }) {
  const info = useStore((s) => s.conns[conn])
  const { choices } = useKeyChoices()
  const [au, setAu] = useState<Authorized | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const load = async () => {
    try {
      setAu(await call<Authorized>('keys.authorized', { conn }))
      setError('')
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }
  useEffect(() => { load() }, [])

  const local = (fp?: string) => (fp ? choices?.find((c) => c.fingerprint === fp) : undefined)
  const nameOf = (k: AuthorizedKey) => k.comment || local(k.fingerprint)?.label || k.fingerprint || t('ak.line', { n: k.line })
  const remove = async (k: AuthorizedKey) => {
    if (!au || !k.fingerprint) return
    const own = au.authKey === k.fingerprint
    const ok = await ask({ title: t('ak.remove'), message: own ? t('ak.removeOwn', { name: nameOf(k) }) : t('ak.removeConfirm', { name: nameOf(k) }), danger: true, ok: t('ak.remove') })
    if (!ok) return
    setBusy(k.fingerprint)
    try {
      setAu(await call<Authorized>('keys.removeAuthorized', { conn, fingerprints: [k.fingerprint], version: au.version, force: own }))
      toast('ok', t('ak.removed'), nameOf(k))
    } catch (e) {
      errorToast(t('ak.remove'), e)
      load()
    } finally {
      setBusy('')
    }
  }
  // "/home/alex/.ssh 0775" → remove the write rights of the others
  const fixModes = async () => {
    try {
      for (const l of au?.looseModes || []) {
        const i = l.lastIndexOf(' ')
        const mode = parseInt(l.slice(i + 1), 8) & ~0o022
        await call('fs.setAttrs', { conn, paths: [l.slice(0, i)], attrs: { mode } })
      }
      toast('ok', t('ak.modesFixed'))
      load()
    } catch (e) {
      errorToast(t('ak.fixModes'), e)
    }
  }
  const add = async () => {
    await openInstallKey({ conn })
    load()
  }
  const replace = async (k: AuthorizedKey) => {
    await openKeyRollout({ conn, replace: true, old: k.fingerprint })
    load()
  }
  const keys = au?.keys || []
  const ownListed = !!au?.authKey && keys.some((k) => k.fingerprint === au.authKey)

  return (
    <Dialog
      size="lg"
      title={t('ak.title', { host: info?.title || conn })}
      icon={<KeySquare />}
      onClose={close}
      footer={<>
        {au && <button className="btn" onClick={() => { close(); openEditor(conn, au.path) }}><FileCode /> {t('ak.edit')}</button>}
        <div className="spacer" />
        <button className="btn" onClick={close}>{t('common.close')}</button>
      </>}
    >
      <div className="form authorized-keys">
        <div className="ctr-bar">
          <div className="grow">
            <div className="mono faint ellipsis">{au ? displayPath(conn, au.path) : '~/.ssh/authorized_keys'}</div>
            {au && <div className="muted">{t('ak.count', { n: keys.filter((k) => !k.invalid).length })}</div>}
          </div>
          <button className="btn primary" onClick={add}><Plus /> {t('ak.add')}</button>
          <button className="icon-btn" data-tip={t('common.refresh')} onClick={load}><RefreshCw /></button>
        </div>
        {error && <div className="alert danger"><AlertTriangle /><span className="selectable">{error}</span></div>}
        {!au && !error && <div className="ctr-empty"><span className="spinner" /></div>}
        {au && !!au.looseModes?.length && (
          <div className="alert danger">
            <ShieldAlert />
            <div className="grow">
              <div>{t('ak.looseModes')}</div>
              <div className="mono" style={{ marginTop: 4 }}>{au.looseModes.join(' · ')}</div>
            </div>
            <button className="btn sm" onClick={fixModes}><Wrench /> {t('ak.fixModes')}</button>
          </div>
        )}
        {au && !au.exists && <div className="alert info"><KeyRound /><span>{t('ak.none')}</span></div>}
        {au && au.authKey && !ownListed && <p className="hint" style={{ margin: 0 }}>{au.authCert ? t('ak.authCert') : t('ak.authElsewhere')}</p>}
        {au && (
          <div className="ak-list">
            {keys.map((k) => {
              const own = !!au.authKey && k.fingerprint === au.authKey && !au.authCert
              const mine = local(k.fingerprint)
              return (
                <div key={k.line} className={`ak-row${k.invalid ? ' invalid' : ''}${own ? ' own' : ''}`} data-fingerprint={k.fingerprint || ''}>
                  <div className="ak-type">{k.invalid ? <AlertTriangle /> : <span className="badge">{keyType(k.type)}</span>}</div>
                  <div className="ak-main">
                    {k.invalid ? (
                      <>
                        <div className="strong">{t('ak.invalid', { n: k.line })}</div>
                        <div className="mono faint ellipsis" title={k.text}>{k.text}</div>
                      </>
                    ) : (
                      <>
                        <div className="ak-title">
                          <span className={`ellipsis${k.comment ? ' strong' : ' faint'}`}>{k.comment || t('ak.noComment')}</span>
                          {own && <span className="badge accent" data-tip={t('ak.thisConnectionTip')}><BadgeCheck /> {t('ak.thisConnection')}</span>}
                          {mine && <span className="badge" data-tip={t('ak.mineTip')}>{mine.group === '~/.ssh' ? mine.label.split(' · ')[0] : t('ak.inKeychain', { name: mine.label.split(' · ')[0] })}</span>}
                          {k.ca && <span className="badge warn" data-tip={t('ak.caTip')}>{t('ak.ca')}</span>}
                        </div>
                        <div className="mono faint ellipsis selectable">{k.fingerprint}</div>
                        {!!k.options?.length && (
                          <div className="ak-options">
                            {k.options.map((o, i) => <span key={i} className="ak-option mono" title={o}>{o.length > 48 ? o.slice(0, 47) + '…' : o}</span>)}
                          </div>
                        )}
                      </>
                    )}
                  </div>
                  <div className="ctr-actions">
                    {!k.invalid && <button className="icon-btn sm" data-tip={t('ak.replace')} onClick={() => replace(k)}><Repeat2 /></button>}
                    <button className="icon-btn sm" data-tip={t('ak.copy')} onClick={() => copyToClipboard(k.text)}><Copy /></button>
                    {!k.invalid && (
                      <button className="icon-btn sm danger-hover" data-tip={t('ak.remove')} disabled={busy === k.fingerprint} onClick={() => remove(k)}>
                        {busy === k.fingerprint ? <span className="spinner sm" /> : <Trash2 />}
                      </button>
                    )}
                  </div>
                </div>
              )
            })}
            {au.exists && !keys.length && <p className="muted" style={{ margin: 12 }}>{t('ak.empty')}</p>}
          </div>
        )}
        <p className="hint" style={{ margin: 0 }}>{t('ak.hint')}</p>
      </div>
    </Dialog>
  )
}
