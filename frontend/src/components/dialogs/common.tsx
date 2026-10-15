import { useState, type ReactNode } from 'react'
import { AlertTriangle, HelpCircle, Pencil } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { t } from '../../lib/i18n'

export function confirm(opts: { title: string; message: ReactNode; ok?: string; cancel?: string; danger?: boolean; icon?: ReactNode; check?: string; checkDefault?: boolean }): Promise<{ ok: boolean; checked: boolean } | undefined> {
  return openDialog<{ ok: boolean; checked: boolean }>((close) => <ConfirmDialog {...opts} close={close} />)
}

export async function ask(opts: { title: string; message: ReactNode; ok?: string; danger?: boolean }): Promise<boolean> {
  const r = await confirm(opts)
  return !!r?.ok
}

function ConfirmDialog({ title, message, ok, cancel, danger, icon, check, checkDefault, close }: any) {
  const [checked, setChecked] = useState(!!checkDefault)
  return (
    <Dialog
      size="sm"
      title={title}
      icon={icon ?? (danger ? <AlertTriangle /> : <HelpCircle />)}
      iconTone={danger ? 'danger' : 'accent'}
      onClose={() => close(undefined)}
      onSubmit={() => close({ ok: true, checked })}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={() => close(undefined)}>{cancel ?? t('common.cancel')}</button>
          <button className={`btn ${danger ? 'danger' : 'primary'}`} data-autofocus onClick={() => close({ ok: true, checked })}>
            {ok ?? t('common.ok')}
          </button>
        </>
      }
    >
      <div style={{ color: 'var(--text-2)', wordBreak: 'break-word', whiteSpace: 'pre-line' }}>{message}</div>
      {check && (
        <label className="check" style={{ marginTop: 14 }}>
          <input type="checkbox" checked={checked} onChange={(e) => setChecked(e.target.checked)} />
          {check}
        </label>
      )}
    </Dialog>
  )
}

export function prompt(opts: { title: string; label: string; value?: string; placeholder?: string; ok?: string; selectStem?: boolean; icon?: ReactNode; hint?: ReactNode; validate?: (v: string) => string | null; mono?: boolean; password?: boolean }): Promise<string | undefined> {
  return openDialog<string>((close) => <PromptDialog {...opts} close={close} />)
}

function PromptDialog({ title, label, value, placeholder, ok, selectStem, icon, hint, validate, mono, password, close }: any) {
  const [v, setV] = useState(value ?? '')
  const err = validate ? validate(v) : null
  const submit = () => {
    if (err || (!v.trim() && !password)) return
    close(password ? v : v.trim())
  }
  return (
    <Dialog
      size="sm"
      title={title}
      icon={icon ?? <Pencil />}
      onClose={() => close(undefined)}
      onSubmit={submit}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={() => close(undefined)}>{t('common.cancel')}</button>
          <button className="btn primary" disabled={!!err || (!v.trim() && !password)} onClick={submit}>{ok ?? t('common.ok')}</button>
        </>
      }
    >
      <div className="field">
        <label>{label}</label>
        <input
          className={`input${mono ? ' mono' : ''}`}
          type={password ? 'password' : 'text'}
          value={v}
          placeholder={placeholder}
          onChange={(e) => setV(e.target.value)}
          data-autofocus
          data-select-stem={selectStem ? '' : undefined}
          spellCheck={false}
          autoComplete="off"
        />
        {err && v && <div className="hint danger-text">{err}</div>}
        {hint && <div className="hint">{hint}</div>}
      </div>
    </Dialog>
  )
}

export function validName(v: string): string | null {
  if (!v.trim()) return null
  if (/[\/\\]/.test(v) && false) return t('err.invalidChar')
  if (v === '.' || v === '..') return t('err.invalidName')
  return null
}
