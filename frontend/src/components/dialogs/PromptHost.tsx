import { useState } from 'react'
import { AlertTriangle, FileWarning, Fingerprint, KeyRound, Lock, ShieldAlert, XCircle } from 'lucide-react'
import { set, useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { Dialog } from '../ui/Dialog'
import { t } from '../../lib/i18n'
import type { PromptEvent } from '../../lib/types'
import { formatDate, formatSize } from '../../lib/format'
import { displayPath } from '../../lib/actions'

// PromptHost shows server-side questions one at a time.
export function PromptHost() {
  const prompts = useStore((s) => s.prompts)
  if (!prompts.length) return null
  return <PromptDialog key={prompts[0].id} p={prompts[0]} />
}

function answer(p: PromptEvent, response: { choice: string; values?: string[]; remember?: boolean; applyAll?: boolean }) {
  set((s) => ({ prompts: s.prompts.filter((x) => x.id !== p.id) }))
  call('prompt.answer', { id: p.id, response }).catch(() => {})
}

const CHOICE_KEYS: Record<string, string> = {
  accept: 'prompt.c.accept', once: 'prompt.c.once', cancel: 'common.cancel', always: 'prompt.c.always', ok: 'common.ok',
  overwrite: 'transfer.ow.overwrite', newer: 'transfer.ow.newer', skip: 'transfer.ow.skip', resume: 'transfer.ow.resume',
  rename: 'transfer.ow.rename', skipall: 'prompt.c.skipall', abort: 'prompt.c.abort',
}
const FIELD_KEYS: Record<string, string> = { Password: 'site.password', Passphrase: 'site.passphrase', 'Secret access key': 'site.secretKey' }

function choiceLabel(p: PromptEvent, id: string, fallback: string) {
  if (p.kind === 'hostkey' && id === 'accept' && p.danger) return t('prompt.c.update')
  if (p.kind === 'certificate' && id === 'once') return t('prompt.c.trustOnce')
  return CHOICE_KEYS[id] ? t(CHOICE_KEYS[id]) : fallback
}

function PromptDialog({ p }: { p: PromptEvent }) {
  const [values, setValues] = useState<string[]>((p.fields || []).map((f) => f.value || ''))
  const [remember, setRemember] = useState(false)
  const [applyAll, setApplyAll] = useState(false)
  const conns = useStore((s) => s.conns)
  const conn = p.conn ? conns[p.conn] : undefined
  const choices = p.choices || [{ id: 'ok', label: t('common.ok'), primary: true }]
  const primary = choices.find((c) => c.primary) || choices[0]
  const cancelChoice = choices.find((c) => c.id === 'cancel') || choices[choices.length - 1]

  const submit = (choice: string) => answer(p, { choice, values, remember, applyAll })

  let icon = <KeyRound />
  let tone: 'accent' | 'danger' | 'warn' = 'accent'
  let title = p.title
  let size: 'sm' | 'md' | 'lg' = 'sm'
  let body: React.ReactNode = null

  switch (p.kind) {
    case 'hostkey': {
      const changed = p.danger
      icon = changed ? <ShieldAlert /> : <Fingerprint />
      tone = changed ? 'danger' : 'warn'
      title = changed ? t('prompt.hostkey.changed') : t('prompt.hostkey.unknown')
      size = 'md'
      body = (
        <>
          <p style={{ marginTop: 0 }} className={changed ? 'danger-text' : ''}>{t(changed ? 'prompt.hostkey.changedMsg' : 'prompt.hostkey.unknownMsg', { host: (p.details?.host || '').replace(/:\d+$/, '') })}</p>
          <dl className="kv">
            <dt>{t('prompt.host')}</dt>
            <dd className="mono">{p.details?.host}</dd>
            <dt>{t('prompt.keyType')}</dt>
            <dd className="mono">{p.details?.keyType}</dd>
            <dt>SHA-256</dt>
            <dd className="mono selectable">{p.details?.sha256}</dd>
            <dt>MD5</dt>
            <dd className="mono selectable faint">{p.details?.md5}</dd>
            {p.details?.previous && (
              <>
                <dt>{t('prompt.previous')}</dt>
                <dd className="mono selectable danger-text" style={{ whiteSpace: 'pre-line' }}>{p.details.previous}</dd>
              </>
            )}
          </dl>
        </>
      )
      break
    }
    case 'certificate':
      title = t('prompt.cert.title')
      icon = <ShieldAlert />
      tone = 'warn'
      size = 'md'
      body = (
        <>
          <div className="alert warn" style={{ marginBottom: 12 }}><AlertTriangle /><span>{p.details?.error || p.message}</span></div>
          <dl className="kv">
            <dt>{t('prompt.host')}</dt><dd className="mono">{p.details?.host}</dd>
            <dt>{t('prompt.subject')}</dt><dd>{p.details?.subject}</dd>
            <dt>{t('prompt.issuer')}</dt><dd>{p.details?.issuer}</dd>
            {p.details?.names && <><dt>{t('prompt.names')}</dt><dd>{p.details.names}</dd></>}
            <dt>{t('prompt.valid')}</dt><dd>{p.details?.validFrom} → {p.details?.validTo}</dd>
            <dt>SHA-256</dt><dd className="mono selectable" style={{ fontSize: 11 }}>{p.details?.sha256}</dd>
          </dl>
        </>
      )
      break
    case 'password':
    case 'passphrase':
    case 'kbdint':
      icon = p.kind === 'passphrase' ? <KeyRound /> : <Lock />
      if (FIELD_KEYS[p.title]) title = t(FIELD_KEYS[p.title])
      body = (
        <div className="form">
          {p.message && <p style={{ margin: 0, whiteSpace: 'pre-line' }} className="muted">{p.message}{conn ? '' : ''}</p>}
          {(p.fields || []).map((f, i) => (
            <div className="field" key={i}>
              <label>{FIELD_KEYS[f.label] ? t(FIELD_KEYS[f.label]) : f.label}</label>
              <input
                className="input"
                type={f.secret ? 'password' : 'text'}
                autoComplete="off"
                value={values[i] || ''}
                data-autofocus={i === 0 ? '' : undefined}
                onChange={(e) => setValues((v) => v.map((x, k) => (k === i ? e.target.value : x)))}
              />
            </div>
          ))}
          {p.remember && (
            <label className="check"><input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} /> {t('prompt.remember')}</label>
          )}
        </div>
      )
      break
    case 'overwrite': {
      icon = <FileWarning />
      tone = 'warn'
      size = 'lg'
      const d = p.details || {}
      const srcNewer = d.srcNewer === 'true'
      const dstNewer = !srcNewer && Math.floor(new Date(d.dstTime).getTime() / 1000) > Math.floor(new Date(d.srcTime).getTime() / 1000)
      title = t('prompt.overwriteTitle')
      body = (
        <>
          <p style={{ marginTop: 0 }}>{t('prompt.exists', { name: p.message })}</p>
          <div className="ow-compare">
            <div className={`ow-side${srcNewer ? ' newer' : ''}`}>
              <div className="ow-label">{t('prompt.source')}{srcNewer && <span className="badge ok">{t('prompt.newer')}</span>}</div>
              <div className="tnum">{formatSize(+d.srcSize || 0)}</div>
              <div className="tnum faint">{formatDate(d.srcTime, 'iso')}</div>
            </div>
            <div className={`ow-side${dstNewer ? ' newer' : ''}`}>
              <div className="ow-label">{t('prompt.target')}{dstNewer && <span className="badge ok">{t('prompt.newer')}</span>}</div>
              <div className="tnum">{formatSize(+d.dstSize || 0)}</div>
              <div className="tnum faint">{formatDate(d.dstTime, 'iso')}</div>
            </div>
          </div>
          <div className="mono faint ellipsis" style={{ marginTop: 10, fontSize: 11 }}>{d.target}</div>
          {p.applyAll && (
            <label className="check" style={{ marginTop: 12 }}><input type="checkbox" checked={applyAll} onChange={(e) => setApplyAll(e.target.checked)} /> {t('prompt.applyAll')}</label>
          )}
        </>
      )
      break
    }
    case 'error':
      title = t('prompt.errorTitle')
      icon = <XCircle />
      tone = 'danger'
      size = 'md'
      body = (
        <>
          <p className="mono selectable" style={{ marginTop: 0 }}>{p.details?.conn ? displayPath(p.details.conn, p.details.file || '') : p.details?.file}</p>
          <div className="alert danger"><AlertTriangle /><span className="selectable">{p.message}</span></div>
        </>
      )
      break
    default:
      body = <p style={{ whiteSpace: 'pre-line' }}>{p.message}</p>
  }

  return (
    <Dialog
      size={size}
      title={<span>{title}{conn ? <span className="faint" style={{ fontWeight: 400, marginLeft: 8, fontSize: 13 }}>{conn.title}</span> : null}</span>}
      icon={icon}
      iconTone={tone}
      onClose={() => submit(cancelChoice?.id || 'cancel')}
      onSubmit={() => submit(primary.id)}
      footer={
        <>
          <div className="spacer" />
          {[...choices].reverse().map((c) => (
            <button key={c.id} className={`btn ${c.primary ? (p.danger ? 'danger' : 'primary') : c.danger ? 'danger' : ''}`} onClick={() => submit(c.id)}>
              {choiceLabel(p, c.id, c.label)}
            </button>
          ))}
        </>
      }
    >
      {body}
    </Dialog>
  )
}
