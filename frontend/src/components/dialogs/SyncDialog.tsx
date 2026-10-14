import { useMemo, useState } from 'react'
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Clock, FolderPlus, RefreshCcwDot, Trash2 } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { setBottom } from '../../lib/actions'
import { sessionTab } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import type { SyncAction, SyncPlan, SyncRequest } from '../../lib/types'
import { formatDate, formatSize } from '../../lib/format'
import { FileIcon } from '../../lib/fileicons'

export function openSyncDialog(tabId: string) {
  const tab = sessionTab(tabId)
  if (!tab) return
  // pick the local and remote panes
  let localSide: 'left' | 'right' = 'left'
  if (tab.panes.left.conn !== 'local' && tab.panes.right.conn === 'local') localSide = 'right'
  const remoteSide = localSide === 'left' ? 'right' : 'left'
  const l = tab.panes[localSide]
  const r = tab.panes[remoteSide]
  openDialog((close) => <SyncDialog init={{ localConn: l.conn, localDir: l.path, remoteConn: r.conn, remoteDir: r.path }} close={close} />)
}

type Init = Pick<SyncRequest, 'localConn' | 'localDir' | 'remoteConn' | 'remoteDir'>

function SyncDialog({ init, close }: { init: Init; close: () => void }) {
  const [req, setReq] = useState<SyncRequest>({ ...init, direction: 'remote', mode: 'standard', criteria: 'time', delete: false, existingOnly: false, fileMask: '', excludeHidden: false })
  const [plan, setPlan] = useState<SyncPlan | null>(null)
  const [busy, setBusy] = useState(false)
  const [checked, setChecked] = useState<Set<string>>(new Set())
  const conns = useStore((s) => s.conns)
  const title = (c: string) => (c === 'local' ? t('common.local') : conns[c]?.title || c)
  const up = (p: Partial<SyncRequest>) => setReq((r) => ({ ...r, ...p }))

  const compare = async () => {
    setBusy(true)
    try {
      const p = await call<SyncPlan>('sync.compare', req)
      setPlan(p)
      setChecked(new Set(p.actions.map((a) => a.id)))
    } catch (e) {
      errorToast(t('sync.title'), e)
    } finally {
      setBusy(false)
    }
  }

  const run = async () => {
    if (!plan) return
    setBusy(true)
    try {
      const r = await call<{ jobs: string[]; deleted: number; errors?: string[] }>('sync.execute', { planId: plan.id, ids: [...checked] })
      toast('ok', t('sync.started'), `${r.jobs.length} transfer job(s)${r.deleted ? `, ${r.deleted} deleted` : ''}`)
      if (r.errors?.length) toast('error', t('sync.title'), r.errors.join('\n'))
      if (r.jobs.length) setBottom(true, 'queue')
      close()
    } catch (e) {
      errorToast(t('sync.title'), e)
      setBusy(false)
    }
  }

  const selectedBytes = useMemo(() => {
    if (!plan) return 0
    return plan.actions.filter((a) => checked.has(a.id)).reduce((n, a) => n + (a.action === 'upload' ? a.local?.size || 0 : a.action === 'download' ? a.remote?.size || 0 : 0), 0)
  }, [plan, checked])

  if (plan) {
    return (
      <Dialog
        size="xl"
        title={`${t('sync.preview')} — ${plan.actions.length ? t('sync.total', { n: plan.actions.length, size: formatSize(plan.bytes) }) : ''}`}
        icon={<RefreshCcwDot />}
        onClose={close}
        onSubmit={run}
        noPadding
        footer={
          <>
            <button className="btn" onClick={() => setPlan(null)}><ArrowLeft /> {t('sync.title')}</button>
            <div className="spacer" />
            <span className="muted tnum">{checked.size} · {formatSize(selectedBytes)}</span>
            <button className="btn" onClick={close}>{t('common.cancel')}</button>
            <button className="btn primary" disabled={busy || !checked.size} onClick={run}>{t('sync.run', { n: checked.size })}</button>
          </>
        }
      >
        {plan.errors?.length ? <div className="alert warn" style={{ margin: 12 }}>{plan.errors.slice(0, 5).join('\n')}</div> : null}
        {!plan.actions.length ? (
          <div className="empty">
            <RefreshCcwDot className="big ok-text" />
            <h4>{t('sync.nothing')}</h4>
            <p>{t('sync.nothingHint')}</p>
          </div>
        ) : (
          <div className="sync-table">
            <table className="table">
              <thead>
                <tr>
                  <th style={{ width: 30 }}>
                    <label className="check"><input type="checkbox" checked={checked.size === plan.actions.length} ref={(el) => { if (el) el.indeterminate = checked.size > 0 && checked.size < plan.actions.length }} onChange={(e) => setChecked(e.target.checked ? new Set(plan.actions.map((a) => a.id)) : new Set())} /></label>
                  </th>
                  <th>{t('sync.localDir')}</th>
                  <th style={{ textAlign: 'right' }}>{t('common.size')}</th>
                  <th>{t('common.modified')}</th>
                  <th style={{ textAlign: 'center' }}>{t('common.type')}</th>
                  <th>{t('sync.remoteDir')}</th>
                  <th style={{ textAlign: 'right' }}>{t('common.size')}</th>
                  <th>{t('common.modified')}</th>
                </tr>
              </thead>
              <tbody>
                {plan.actions.map((a) => (
                  <ActionRow key={a.id} a={a} checked={checked.has(a.id)} toggle={() => setChecked((s) => { const n = new Set(s); n.has(a.id) ? n.delete(a.id) : n.add(a.id); return n })} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Dialog>
    )
  }

  return (
    <Dialog
      size="lg"
      title={t('sync.title')}
      icon={<RefreshCcwDot />}
      onClose={close}
      onSubmit={compare}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.cancel')}</button>
          <button className="btn primary" disabled={busy} onClick={compare}>{busy ? <span className="spinner" /> : null} {busy ? t('sync.comparing') : t('sync.compare')}</button>
        </>
      }
    >
      <div className="form">
        <div className="field-row">
          <div className="field">
            <label>{t('sync.localDir')} <span className="faint">({title(req.localConn)})</span></label>
            <input className="input mono" value={req.localDir} onChange={(e) => up({ localDir: e.target.value })} />
          </div>
          <div className="field">
            <label>{t('sync.remoteDir')} <span className="faint">({title(req.remoteConn)})</span></label>
            <input className="input mono" value={req.remoteDir} onChange={(e) => up({ remoteDir: e.target.value })} />
          </div>
        </div>
        <div className="field">
          <label>{t('sync.direction')}</label>
          <div className="segmented">
            <button className={req.direction === 'remote' ? 'on' : ''} onClick={() => up({ direction: 'remote' })}><ArrowRight /> {t('sync.toRemote')}</button>
            <button className={req.direction === 'local' ? 'on' : ''} onClick={() => up({ direction: 'local' })}><ArrowLeft /> {t('sync.toLocal')}</button>
            <button className={req.direction === 'both' ? 'on' : ''} onClick={() => up({ direction: 'both', delete: false })}>⇄ {t('sync.both')}</button>
          </div>
        </div>
        <div className="field-row">
          <div className="field">
            <label>{t('sync.mode')}</label>
            <select className="select" value={req.mode} onChange={(e) => up({ mode: e.target.value as any })}>
              <option value="standard">{t('sync.standard')}</option>
              <option value="mirror">{t('sync.mirror')}</option>
              <option value="timestamps">{t('sync.timestamps')}</option>
            </select>
          </div>
          <div className="field">
            <label>{t('sync.criteria')}</label>
            <select className="select" value={req.criteria} onChange={(e) => up({ criteria: e.target.value as any })}>
              {['time', 'size', 'either', 'checksum', 'none'].map((c) => <option key={c} value={c}>{t(`sync.c.${c}`)}</option>)}
            </select>
          </div>
        </div>
        <div className="field">
          <label>{t('transfer.fileMask')}</label>
          <input className="input mono" value={req.fileMask} placeholder="* | .git/; node_modules/; *.tmp" onChange={(e) => up({ fileMask: e.target.value })} />
          <span className="hint">{t('transfer.fileMaskHint')}</span>
        </div>
        <div className="check-grid">
          <label className={`check${req.direction === 'both' ? ' disabled' : ''}`}><input type="checkbox" checked={req.delete} onChange={(e) => up({ delete: e.target.checked })} /> <span className={req.delete ? 'danger-text' : ''}>{t('sync.delete')}</span></label>
          <label className="check"><input type="checkbox" checked={req.existingOnly} onChange={(e) => up({ existingOnly: e.target.checked })} /> {t('sync.existingOnly')}</label>
          <label className="check"><input type="checkbox" checked={!!req.excludeHidden} onChange={(e) => up({ excludeHidden: e.target.checked })} /> {t('transfer.excludeHidden')}</label>
        </div>
      </div>
    </Dialog>
  )
}

function ActionIcon({ a }: { a: SyncAction }) {
  switch (a.action) {
    case 'upload':
      return <ArrowRight className="accent-text" />
    case 'download':
      return <ArrowLeft className="accent-text" />
    case 'deleteLocal':
    case 'deleteRemote':
      return <Trash2 className="danger-text" />
    case 'mkdirLocal':
    case 'mkdirRemote':
      return <FolderPlus className="accent-text" />
    default:
      return <Clock className="warn-text" />
  }
}

function ActionRow({ a, checked, toggle }: { a: SyncAction; checked: boolean; toggle: () => void }) {
  const l = a.local
  const r = a.remote
  return (
    <tr className={checked ? '' : 'faint'} onClick={toggle} style={{ cursor: 'pointer' }}>
      <td><label className="check" onClick={(e) => e.stopPropagation()}><input type="checkbox" checked={checked} onChange={toggle} /></label></td>
      <td>{l ? <span className="row" style={{ gap: 6 }}><FileIcon entry={l} size={14} /><span className="ellipsis mono" style={{ maxWidth: 260 }}>{a.rel}</span></span> : <span className="faint">—</span>}</td>
      <td className="tnum muted" style={{ textAlign: 'right' }}>{l && !l.isDir ? formatSize(l.size) : ''}</td>
      <td className="tnum muted">{l ? formatDate(l.mtime) : ''}</td>
      <td style={{ textAlign: 'center' }} data-tip={`${t(`sync.act.${a.action}`)} · ${t(`sync.reason.${a.reason}`)}`}>
        <span className="sync-act"><ActionIcon a={a} /><span className="faint">{t(`sync.reason.${a.reason}`)}</span></span>
      </td>
      <td>{r ? <span className="row" style={{ gap: 6 }}><FileIcon entry={r} size={14} /><span className="ellipsis mono" style={{ maxWidth: 260 }}>{a.rel}</span></span> : <span className="faint">—</span>}</td>
      <td className="tnum muted" style={{ textAlign: 'right' }}>{r && !r.isDir ? formatSize(r.size) : ''}</td>
      <td className="tnum muted">{r ? formatDate(r.mtime) : ''}</td>
    </tr>
  )
}

void ArrowUp
void ArrowDown
