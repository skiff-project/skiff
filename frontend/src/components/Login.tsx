import { useState } from 'react'
import { LogIn } from 'lucide-react'
import { boot } from '../lib/actions'
import { set, useStore } from '../lib/store'
import { t } from '../lib/i18n'

export function Login() {
  const online = useStore((s) => s.online)
  const [user, setUser] = useState('')
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      const r = await fetch('/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Skiff': '1' }, body: JSON.stringify({ user, password: pw }), credentials: 'same-origin' })
      const b = await r.json().catch(() => ({}))
      if (!r.ok) throw new Error(b?.error?.message || r.statusText)
      set({ authed: null })
      await boot()
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }
  if (!online) {
    return (
      <div className="login">
        <div className="login-card">
          <img src="/favicon.svg" width={52} height={52} alt="" />
          <p className="muted">{t('status.offline')}</p>
          <button className="btn" onClick={() => location.reload()}>{t('common.retry')}</button>
        </div>
      </div>
    )
  }
  return (
    <div className="login">
      <form className="login-card" onSubmit={submit}>
        <img src="/favicon.svg" width={52} height={52} alt="" />
        <h2>{t('web.login')}</h2>
        <div className="field" style={{ width: '100%' }}>
          <label>{t('web.user')}</label>
          <input className="input" value={user} onChange={(e) => setUser(e.target.value)} autoFocus autoComplete="username" />
        </div>
        <div className="field" style={{ width: '100%' }}>
          <label>{t('web.password')}</label>
          <input className="input" type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="current-password" />
        </div>
        {err && <div className="alert danger" style={{ width: '100%' }}>{err}</div>}
        <button className="btn primary block lg" disabled={busy || !user || !pw}><LogIn /> {t('web.signIn')}</button>
      </form>
    </div>
  )
}
