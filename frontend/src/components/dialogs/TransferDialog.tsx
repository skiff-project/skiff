import { useEffect, useState } from 'react'
import { ArrowRightLeft, ChevronDown, ChevronRight, Copy } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { get } from '../../lib/store'
import { savePrefs } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { Entry, TransferOptions } from '../../lib/types'
import { formatSize } from '../../lib/format'

export interface TransferResult {
  dstDir: string
  newName?: string
  options: TransferOptions
}

export function transferDialog(p: { src: string; dst: string; dstDir: string; entries: Entry[]; move: boolean }): Promise<TransferResult | undefined> {
  return openDialog<TransferResult>((close) => <TransferDialog {...p} close={close} />)
}

function TransferDialog({ src, dst, dstDir, entries, move, close }: { src: string; dst: string; dstDir: string; entries: Entry[]; move: boolean; close: (r?: TransferResult) => void }) {
  const [dir, setDir] = useState(dstDir)
  const [name, setName] = useState(entries.length === 1 ? entries[0].name : '')
  const [o, setO] = useState<TransferOptions | null>(null)
  const [adv, setAdv] = useState(false)
  const conns = get().conns
  const dstTitle = dst === 'local' ? t('common.local') : conns[dst]?.title
  const what = entries.length === 1 ? `"${entries[0].name}"` : `${entries.length} items`
  const label = dst === 'local' && src !== 'local' ? t('transfer.downloadTo', { what }) : src === 'local' && dst !== 'local' ? t('transfer.uploadTo', { what }) : move ? t('transfer.moveTo', { what }) : t('transfer.copyTo', { what })
  const bytes = entries.reduce((n, e) => n + (e.isDir ? 0 : e.size), 0)

  useEffect(() => {
    call<TransferOptions>('transfer.defaults', { src, dst }).then((d) => setO({ ...d, move }))
  }, [])

  if (!o) return null
  const up = (patch: Partial<TransferOptions>) => setO({ ...o, ...patch })
  const submit = () => close({ dstDir: dir, newName: entries.length === 1 && name !== entries[0].name ? name : undefined, options: o })
  const saveDefaults = () => {
    savePrefs((p) => ({
      transfer: {
        ...p.transfer, mode: o.mode, textMask: o.textMask || p.transfer.textMask, preserveTime: o.preserveTime, preservePerms: o.preservePerms, setPerms: o.setPerms,
        perms: o.perms || p.transfer.perms, addXToDirs: o.addXToDirs, overwrite: o.overwrite, fileMask: o.fileMask || '', speedLimit: o.speedLimit || 0,
        caseMode: o.caseMode || 'none', verify: !!o.verify, excludeHidden: !!o.excludeHidden, resumeThreshold: o.resumeThreshold,
      },
    }), true)
  }

  return (
    <Dialog
      size="md"
      title={move ? t('transfer.moveTitle') : t('transfer.copyTitle')}
      icon={move ? <ArrowRightLeft /> : <Copy />}
      onClose={() => close()}
      onSubmit={submit}
      footer={
        <>
          <button className="btn ghost sm" onClick={saveDefaults}>{t('transfer.saveDefaults')}</button>
          <div className="spacer" />
          <button className="btn" onClick={() => close()}>{t('common.cancel')}</button>
          <button className="btn primary" onClick={submit}>{t('common.ok')}</button>
        </>
      }
    >
      <div className="form">
        <div className="field">
          <label>{label} <span className="faint">({dstTitle}{bytes ? ` · ${formatSize(bytes)}` : ''})</span></label>
          <input className="input mono" value={dir} onChange={(e) => setDir(e.target.value)} data-autofocus />
        </div>
        {entries.length === 1 && (
          <div className="field">
            <label>{t('common.name')}</label>
            <input className="input mono" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        )}
        <button className="disclosure" onClick={() => setAdv(!adv)}>
          {adv ? <ChevronDown /> : <ChevronRight />} {t('transfer.settings')}
        </button>
        {adv && <TransferOptionsForm o={o} up={up} />}
      </div>
    </Dialog>
  )
}

export function TransferOptionsForm({ o, up }: { o: TransferOptions; up: (p: Partial<TransferOptions>) => void }) {
  return (
    <div className="form">
      <div className="field-row">
        <div className="field">
          <label>{t('transfer.mode')}</label>
          <div className="segmented">
            {[['binary', t('transfer.binary')], ['text', t('transfer.text')], ['auto', t('transfer.auto')]].map(([v, l]) => (
              <button key={v} className={o.mode === v ? 'on' : ''} onClick={() => up({ mode: v })}>{l}</button>
            ))}
          </div>
        </div>
        <div className="field">
          <label>{t('transfer.overwrite')}</label>
          <select className="select" value={o.overwrite} onChange={(e) => up({ overwrite: e.target.value })}>
            {['ask', 'overwrite', 'newer', 'differentSize', 'resume', 'skip', 'rename'].map((v) => <option key={v} value={v}>{t(`transfer.ow.${v}`)}</option>)}
          </select>
        </div>
      </div>
      {o.mode === 'auto' && (
        <div className="field"><label>{t('transfer.textMask')}</label><input className="input mono" value={o.textMask || ''} onChange={(e) => up({ textMask: e.target.value })} /></div>
      )}
      <div className="field">
        <label>{t('transfer.fileMask')}</label>
        <input className="input mono" value={o.fileMask || ''} placeholder="*" onChange={(e) => up({ fileMask: e.target.value })} />
        <span className="hint">{t('transfer.fileMaskHint')}</span>
      </div>
      <div className="check-grid">
        <label className="check"><input type="checkbox" checked={o.preserveTime} onChange={(e) => up({ preserveTime: e.target.checked })} /> {t('transfer.preserveTime')}</label>
        <label className="check"><input type="checkbox" checked={o.preservePerms} onChange={(e) => up({ preservePerms: e.target.checked, setPerms: e.target.checked ? false : o.setPerms })} /> {t('transfer.preservePerms')}</label>
        <label className="check"><input type="checkbox" checked={o.setPerms} onChange={(e) => up({ setPerms: e.target.checked, preservePerms: e.target.checked ? false : o.preservePerms })} /> {t('transfer.setPerms')}
          {o.setPerms && <input className="input sm mono" style={{ width: 70, marginLeft: 8 }} value={(o.perms || 0o644).toString(8)} onChange={(e) => up({ perms: parseInt(e.target.value, 8) || 0 })} />}
        </label>
        <label className="check"><input type="checkbox" checked={o.addXToDirs} onChange={(e) => up({ addXToDirs: e.target.checked })} /> {t('transfer.addX')}</label>
        <label className="check"><input type="checkbox" checked={!!o.newerOnly} onChange={(e) => up({ newerOnly: e.target.checked })} /> {t('transfer.newerOnly')}</label>
        <label className="check"><input type="checkbox" checked={!!o.excludeHidden} onChange={(e) => up({ excludeHidden: e.target.checked })} /> {t('transfer.excludeHidden')}</label>
        <label className="check"><input type="checkbox" checked={!!o.verify} onChange={(e) => up({ verify: e.target.checked })} /> {t('transfer.verify')}</label>
        <label className="check"><input type="checkbox" checked={o.resumeThreshold >= 0} onChange={(e) => up({ resumeThreshold: e.target.checked ? 100 * 1024 : -1 })} /> {t('transfer.resume')}</label>
      </div>
      <div className="field-row r3">
        <div className="field">
          <label>{t('transfer.caseMode')}</label>
          <select className="select" value={o.caseMode || 'none'} onChange={(e) => up({ caseMode: e.target.value })}>
            {['none', 'lower', 'upper', 'firstUpper'].map((v) => <option key={v} value={v}>{t(`transfer.case.${v}`)}</option>)}
          </select>
        </div>
        <div className="field">
          <label>{t('transfer.speed')}</label>
          <input className="input tnum" type="number" min={0} value={o.speedLimit || 0} onChange={(e) => up({ speedLimit: parseInt(e.target.value, 10) || 0 })} />
        </div>
        <div className="field">
          <label>{t('transfer.parallel')}</label>
          <input className="input tnum" type="number" min={1} max={16} value={o.parallel || 2} onChange={(e) => up({ parallel: parseInt(e.target.value, 10) || 1 })} />
        </div>
      </div>
    </div>
  )
}
