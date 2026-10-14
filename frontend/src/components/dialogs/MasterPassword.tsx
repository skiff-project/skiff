import { useState } from 'react'
import { Lock, ShieldCheck } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { get, set, toast } from '../../lib/store'
import { t } from '../../lib/i18n'
import type { AppInfo } from '../../lib/types'

export function unlockDialog(): Promise<boolean | undefined> {
  return openDialog<boolean>((close) => <Unlock close={close} />)
}

function Unlock({ close }: { close: (v?: boolean) => void }) {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      await call('security.unlock', { password: pw })
      set({ info: await call<AppInfo>('app.info') })
      close(true)
    } catch (e: any) {
      setErr(e.message)
      setBusy(false)
    }
  }
  return (
    <Dialog size="sm" title={t('master.unlockTitle')} icon={<Lock />} onClose={() => close(false)} onSubmit={submit} footer={<><div className="spacer" /><button className="btn" onClick={() => close(false)}>{t('master.later')}</button><button className="btn primary" disabled={busy || !pw} onClick={submit}>{t('sites.unlock')}</button></>}>
      <p className="muted" style={{ marginTop: 0 }}>{t('master.unlockMsg')}</p>
      <input className="input" type="password" value={pw} onChange={(e) => { setPw(e.target.value); setErr('') }} data-autofocus />
      {err && <div className="hint danger-text" style={{ marginTop: 6 }}>{err}</div>}
    </Dialog>
  )
}

export function masterPasswordDialog(mode: 'set' | 'change' | 'remove') {
  return openDialog<boolean>((close) => <SetMaster mode={mode} close={close} />)
}

function SetMaster({ mode, close }: { mode: 'set' | 'change' | 'remove'; close: (v?: boolean) => void }) {
  const [old, setOld] = useState('')
  const [p1, setP1] = useState('')
  const [p2, setP2] = useState('')
  const [err, setErr] = useState('')
  const has = get().info?.hasMaster
  const valid = mode === 'remove' ? !!old : p1.length >= 8 && p1 === p2 && (!has || !!old)
  const submit = async () => {
    if (!valid) {
      setErr(p1 !== p2 ? t('master.mismatch') : t('master.short'))
      return
    }
    try {
      await call('security.setMaster', { old, new: mode === 'remove' ? '' : p1 })
      set({ info: await call<AppInfo>('app.info') })
      toast('ok', t('master.done'))
      close(true)
    } catch (e: any) {
      setErr(e.message)
    }
  }
  return (
    <Dialog size="sm" title={t('master.setTitle')} icon={<ShieldCheck />} onClose={() => close(false)} onSubmit={submit} footer={<><div className="spacer" /><button className="btn" onClick={() => close(false)}>{t('common.cancel')}</button><button className={`btn ${mode === 'remove' ? 'danger' : 'primary'}`} onClick={submit}>{mode === 'remove' ? t('prefs.removeMaster') : t('common.ok')}</button></>}>
      <div className="form">
        <p className="muted" style={{ margin: 0 }}>{t('prefs.masterPasswordDesc')}</p>
        {has && <div className="field"><label>{t('master.current')}</label><input className="input" type="password" value={old} onChange={(e) => setOld(e.target.value)} data-autofocus /></div>}
        {mode !== 'remove' && (
          <>
            <div className="field"><label>{t('master.new')}</label><input className="input" type="password" value={p1} onChange={(e) => setP1(e.target.value)} data-autofocus={!has ? '' : undefined} /></div>
            <div className="field"><label>{t('master.repeat')}</label><input className="input" type="password" value={p2} onChange={(e) => setP2(e.target.value)} /></div>
            <PasswordMeter pw={p1} />
          </>
        )}
        {err && <div className="hint danger-text">{err}</div>}
      </div>
    </Dialog>
  )
}

export function PasswordMeter({ pw }: { pw: string }) {
  let score = 0
  if (pw.length >= 8) score++
  if (pw.length >= 12) score++
  if (/[A-Z]/.test(pw) && /[a-z]/.test(pw)) score++
  if (/\d/.test(pw)) score++
  if (/[^A-Za-z0-9]/.test(pw)) score++
  const pct = (score / 5) * 100
  const color = score <= 2 ? 'var(--danger)' : score === 3 ? 'var(--warn)' : 'var(--ok)'
  return (
    <div className="progress"><div style={{ width: `${pct}%`, background: color }} /></div>
  )
}
