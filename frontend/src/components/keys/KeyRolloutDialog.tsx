import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, Check, KeyRound, Minus, Repeat2, Server, Square } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { get, toast, uid, useStore } from '../../lib/store'
import { t } from '../../lib/i18n'
import type { ConnInfo, SiteView } from '../../lib/types'
import { KeyPicker, pickedKey, useKeyChoices } from './KeyPicker'

interface ReplaceResult { path: string; added: boolean; verified: boolean; removed: number; oldAbsent?: boolean }

type Status = { state: 'waiting' | 'connecting' | 'working' | 'done' | 'error' | 'skipped'; detail?: string }

interface Target { id: string; title: string; sub: string; site?: SiteView; conn?: string }

const isSSH = (p?: string) => p === 'sftp' || p === 'scp'

/**
 * Installs a public key on servers, or replaces an old key with it: on the
 * server of a connection (conn), or on saved sites, each connected in turn
 * (and disconnected after, unless it was already open). A replacement
 * removes the old key only once the server let the new one in.
 */
export function openKeyRollout(opts: { siteIds?: string[]; conn?: string; replace?: boolean; old?: string }) {
  return openDialog((close) => <KeyRollout {...opts} close={close} />)
}

function KeyRollout({ siteIds, conn, replace: replaceAtFirst, old, close }: { siteIds?: string[]; conn?: string; replace?: boolean; old?: string; close: () => void }) {
  const sites = useStore((s) => s.sites)
  const connInfo = useStore((s) => (conn ? s.conns[conn] : undefined))
  const { choices, hidden } = useKeyChoices()
  const [replace, setReplace] = useState(!!replaceAtFirst)
  const [choice, setChoice] = useState('')
  const [pasted, setPasted] = useState('')
  const [oldChoice, setOldChoice] = useState('')
  const [oldText, setOldText] = useState(old || '')
  const [status, setStatus] = useState<Record<string, Status>>({})
  const [running, setRunning] = useState(false)
  const [finished, setFinished] = useState(false)
  const stop = useRef(false)

  const targets: Target[] = conn
    ? [{ id: conn, title: connInfo?.title || conn, sub: connInfo ? `${connInfo.user ? connInfo.user + '@' : ''}${connInfo.host}` : '', conn }]
    : (siteIds || []).map((id) => sites.find((s) => s.id === id)).filter((s): s is SiteView => !!s).map((s) => ({ id: s.id, title: s.name || s.host, sub: `${s.user ? s.user + '@' : ''}${s.host}`, site: s }))
  const ssh = targets.filter((x) => (x.site ? isSSH(x.site.protocol) : true))

  useEffect(() => {
    if (!choices) return
    if (!choice) setChoice(choices[0]?.id || 'paste')
    // the key to remove is never guessed
    if (!oldChoice && old) {
      const known = choices.find((c) => c.fingerprint === old)
      setOldChoice(known ? known.id : 'other')
    }
  }, [choices])

  const key = pickedKey(choices, choice, pasted)
  const oldKey = oldChoice === 'other' || oldChoice === 'paste' ? oldText.trim() : choices?.find((c) => c.id === oldChoice)?.fingerprint || ''
  const newFp = choice !== 'paste' ? choices?.find((c) => c.id === choice)?.fingerprint : undefined
  const same = replace && !!newFp && newFp === oldKey
  const ready = !!key && ssh.length > 0 && (!replace || (!!oldKey && !same))

  const setOne = (id: string, st: Status) => setStatus((s) => ({ ...s, [id]: st }))

  const run = async () => {
    if (!ready || running) return
    stop.current = false
    setRunning(true)
    setFinished(false)
    // the saved passwords are locked behind the master password
    if (get().info?.locked && ssh.some((x) => x.site && (x.site.hasPassword || x.site.hasPassphrase))) {
      const m = await import('../dialogs/MasterPassword')
      if (!(await m.unlockDialog())) {
        setRunning(false)
        return
      }
    }
    const init: Record<string, Status> = {}
    for (const x of targets) init[x.id] = x.site && !isSSH(x.site.protocol) ? { state: 'skipped', detail: t('roll.notSSH') } : { state: 'waiting' }
    setStatus(init)
    let ok = 0
    let failed = 0
    for (const x of ssh) {
      if (stop.current) break
      let id = x.conn || ''
      let opened = false
      try {
        if (!id) {
          // a connection already open on the site, else a new one
          const open = Object.values(get().conns).find((c) => c.siteId === x.id && c.state === 'connected' && isSSH(c.protocol))
          if (open) id = open.id
          else {
            setOne(x.id, { state: 'connecting' })
            const c = await call<ConnInfo>('conn.open', { siteId: x.id, token: uid('c') })
            id = c.id
            opened = true
          }
        }
        setOne(x.id, { state: 'working' })
        const r = await call<ReplaceResult>('keys.replace', { conn: id, publicKey: key, oldFingerprint: replace ? oldKey : '' })
        const detail = replace ? (r.oldAbsent ? t('roll.oldAbsent') : t('roll.replaced')) : r.added ? t('roll.added') : t('roll.already')
        setOne(x.id, { state: 'done', detail })
        ok++
      } catch (e: any) {
        setOne(x.id, { state: 'error', detail: e?.message || String(e) })
        failed++
      } finally {
        if (opened && id) call('conn.close', { conn: id }).catch(() => {})
      }
    }
    setRunning(false)
    setFinished(true)
    toast(failed ? 'error' : 'ok', replace ? t('roll.doneReplace') : t('roll.doneInstall'), t('roll.summary', { ok, failed }))
  }

  const icon = (st?: Status) => {
    switch (st?.state) {
      case 'connecting':
      case 'working':
        return <span className="spinner sm" />
      case 'done':
        return <Check className="ok-text" />
      case 'error':
        return <AlertTriangle className="danger-text" />
      case 'skipped':
        return <Minus className="faint" />
      default:
        return <Server className="faint" />
    }
  }
  const stateText = (st?: Status) => {
    if (!st || st.state === 'waiting') return ''
    if (st.state === 'connecting') return t('roll.connecting')
    if (st.state === 'working') return replace ? t('roll.replacing') : t('roll.installing')
    return st.detail || ''
  }

  return (
    <Dialog
      size="lg"
      title={replace ? t('roll.titleReplace') : t('roll.title')}
      icon={replace ? <Repeat2 /> : <KeyRound />}
      onClose={() => { stop.current = true; close() }}
      onSubmit={finished ? close : run}
      footer={<>
        {running && <button className="btn" onClick={() => { stop.current = true }}><Square /> {t('roll.stop')}</button>}
        <div className="spacer" />
        <button className="btn" onClick={() => { stop.current = true; close() }}>{finished ? t('common.close') : t('common.cancel')}</button>
        {!finished && (
          <button className="btn primary" disabled={!ready || running} onClick={run}>
            {running ? <span className="spinner sm" /> : replace ? <Repeat2 /> : <KeyRound />} {replace ? t('roll.runReplace', { n: ssh.length }) : t('roll.runInstall', { n: ssh.length })}
          </button>
        )}
      </>}
    >
      <div className="form key-rollout">
        <div className="segmented">
          <button className={!replace ? 'on' : ''} disabled={running} onClick={() => setReplace(false)}>{t('roll.modeInstall')}</button>
          <button className={replace ? 'on' : ''} disabled={running} onClick={() => setReplace(true)}>{t('roll.modeReplace')}</button>
        </div>
        <p className="muted" style={{ margin: 0 }}>{replace ? t('roll.descReplace') : t('roll.descInstall')}</p>
        <KeyPicker label={replace ? t('roll.newKey') : t('keys.key')} choices={choices} hidden={hidden} value={choice} onChange={setChoice} pasted={pasted} onPaste={setPasted} autoFocus />
        {replace && (
          <div className="field">
            <label>{t('roll.oldKey')}</label>
            {choices === null ? <span className="spinner" /> : (
              <select className="select" value={oldChoice} onChange={(e) => setOldChoice(e.target.value)} disabled={running}>
                <option value="" disabled>{t('roll.chooseOld')}</option>
                {choices.map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
                <option value="other">{t('roll.oldOther')}</option>
              </select>
            )}
            {oldChoice === 'other' && <input className="input mono" value={oldText} onChange={(e) => setOldText(e.target.value)} placeholder="SHA256:… / ssh-ed25519 AAAA…" disabled={running} />}
            {same && <span className="hint warn-text">{t('roll.same')}</span>}
            <span className="hint">{t('roll.replaceHint')}</span>
          </div>
        )}
        <div className="field">
          <label>{conn ? t('keys.server') : t('roll.sites', { n: targets.length })}</label>
          <div className="roll-list">
            {targets.map((x) => {
              const st = status[x.id] || (x.site && !isSSH(x.site.protocol) ? { state: 'skipped' as const, detail: t('roll.notSSH') } : undefined)
              return (
                <div key={x.id} className={`roll-row ${st?.state || 'waiting'}`} data-site={x.id}>
                  {icon(st)}
                  <div className="roll-main">
                    <span className="strong ellipsis">{x.title}</span>
                    <span className="faint mono ellipsis">{x.sub}</span>
                  </div>
                  <span className={`roll-state ellipsis${st?.state === 'error' ? ' danger-text selectable' : ''}`} title={stateText(st)}>{stateText(st)}</span>
                </div>
              )
            })}
          </div>
        </div>
      </div>
    </Dialog>
  )
}
