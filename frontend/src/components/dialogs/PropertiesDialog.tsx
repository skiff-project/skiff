import { Fragment, useEffect, useState } from 'react'
import { Calculator, Hash, Info } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, get } from '../../lib/store'
import { displayPath, paneStyle } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { Entry } from '../../lib/types'
import { FileIcon, typeLabel } from '../../lib/fileicons'
import { formatDateFull, formatSize, octal } from '../../lib/format'
import { join } from '../../lib/paths'
import { copyToClipboard } from '../../lib/fileops'

export function propertiesDialog(conn: string, dir: string, entries: Entry[]): Promise<boolean | undefined> {
  return openDialog<boolean>((close) => <PropertiesDialog conn={conn} dir={dir} entries={entries} close={close} />)
}

const BITS = [
  ['owner', 0o400, 0o200, 0o100],
  ['group', 0o040, 0o020, 0o010],
  ['others', 0o004, 0o002, 0o001],
] as const

function toLocalInput(iso: string) {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function PropertiesDialog({ conn, dir, entries, close }: { conn: string; dir: string; entries: Entry[]; close: (v?: boolean) => void }) {
  const caps = conn === 'local' ? get().info?.localCaps : get().conns[conn]?.caps
  const st = paneStyle(conn)
  const one = entries.length === 1 ? entries[0] : null
  const paths = entries.map((e) => join(st, dir, e.name))
  const initMode = one ? one.mode & 0o7777 : entries.every((e) => (e.mode & 0o7777) === (entries[0].mode & 0o7777)) ? entries[0].mode & 0o7777 : 0
  const [mode, setMode] = useState(initMode)
  const [modeText, setModeText] = useState(octal(initMode))
  const [owner, setOwner] = useState(one?.owner || '')
  const [group, setGroup] = useState(one?.group || '')
  const [mtime, setMtime] = useState(one ? toLocalInput(one.mtime) : '')
  const [recursive, setRecursive] = useState(false)
  const [addX, setAddX] = useState(true)
  const [size, setSize] = useState<{ bytes: number; files: number; dirs: number } | null>(null)
  const [sizing, setSizing] = useState(false)
  const [algo, setAlgo] = useState('sha256')
  const [sum, setSum] = useState('')
  const [summing, setSumming] = useState(false)
  const [owners, setOwners] = useState<{ users: string[]; groups: string[] }>({ users: [], groups: [] })
  const [busy, setBusy] = useState(false)
  const anyDir = entries.some((e) => e.isDir)
  const editableMode = !!caps?.chmod
  const editableOwner = !!caps?.chown

  useEffect(() => {
    if (editableOwner) call('fs.owners', { conn }).then(setOwners).catch(() => {})
  }, [])

  const setBit = (bit: number, on: boolean) => {
    const m = on ? mode | bit : mode & ~bit
    setMode(m)
    setModeText(octal(m))
  }

  const calc = async () => {
    setSizing(true)
    try {
      setSize(await call('fs.size', { conn, paths }))
    } catch (e) {
      errorToast(t('ctx.calcSize'), e)
    } finally {
      setSizing(false)
    }
  }

  const checksum = async () => {
    setSumming(true)
    setSum('')
    try {
      setSum(await call<string>('fs.checksum', { conn, path: paths[0], algo }))
    } catch (e) {
      errorToast(t('ctx.checksum'), e)
    } finally {
      setSumming(false)
    }
  }

  const apply = async () => {
    const attrs: any = { recursive: recursive && anyDir, addXToDirs: addX }
    if (editableMode && (mode !== initMode || recursive)) attrs.mode = mode
    if (editableOwner && (owner !== (one?.owner || '') || recursive)) attrs.owner = owner
    if (editableOwner && (group !== (one?.group || '') || recursive)) attrs.group = group
    if (one && caps?.setMtime && mtime && mtime !== toLocalInput(one.mtime)) attrs.mtime = new Date(mtime).toISOString()
    if (attrs.mode === undefined && !attrs.owner && !attrs.group && !attrs.mtime) return close(false)
    setBusy(true)
    try {
      await call('fs.setAttrs', { conn, paths, attrs })
      close(true)
    } catch (e) {
      errorToast(t('props.title'), e)
      setBusy(false)
    }
  }

  return (
    <Dialog
      size="md"
      title={t('props.title')}
      icon={<Info />}
      onClose={() => close()}
      onSubmit={apply}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={() => close()}>{t('common.cancel')}</button>
          <button className="btn primary" disabled={busy} onClick={apply}>{t('common.ok')}</button>
        </>
      }
    >
      <div className="props-head">
        {one ? <FileIcon entry={one} size={36} /> : <FileIcon entry={{ name: '', isDir: true }} size={36} />}
        <div className="grow" style={{ minWidth: 0 }}>
          <div className="strong ellipsis selectable" style={{ fontSize: 15 }}>{one ? one.name : t('props.multiple', { n: entries.length })}</div>
          <div className="muted">{one ? typeLabel(one) : ''}</div>
        </div>
      </div>
      <dl className="kv" style={{ margin: '14px 0' }}>
        <dt>{t('props.location')}</dt>
        <dd className="mono selectable" style={{ cursor: 'copy' }} onClick={() => copyToClipboard(one ? paths[0] : dir)}>{displayPath(conn, one ? paths[0] : dir)}</dd>
        {one?.isLink && one.linkTarget && (
          <>
            <dt>{t('props.linkTarget')}</dt>
            <dd className="mono selectable">{one.linkTarget}</dd>
          </>
        )}
        <dt>{t('common.size')}</dt>
        <dd className="row">
          {size ? (
            <span className="tnum">{formatSize(size.bytes)} <span className="faint">({size.bytes.toLocaleString()} B · {t('size.files', { n: size.files })} · {t('size.dirs', { n: size.dirs })})</span></span>
          ) : one && !one.isDir ? (
            <span className="tnum">{formatSize(one.size)} <span className="faint">({one.size.toLocaleString()} B)</span></span>
          ) : (
            <button className="btn sm" disabled={sizing} onClick={calc}>{sizing ? <span className="spinner" /> : <Calculator />} {t('props.calc')}</button>
          )}
        </dd>
        {one && (
          <>
            <dt>{t('props.mtime')}</dt>
            <dd>
              {caps?.setMtime ? (
                <input className="input sm tnum" type="datetime-local" step={1} style={{ width: 230 }} value={mtime} onChange={(e) => setMtime(e.target.value)} />
              ) : (
                formatDateFull(one.mtime)
              )}
            </dd>
          </>
        )}
      </dl>

      {(editableOwner || caps?.owners) && (
        <div className="field-row" style={{ marginBottom: 14 }}>
          <div className="field">
            <label>{t('common.owner')}</label>
            <input className="input" list="owners-list" value={owner} disabled={!editableOwner} onChange={(e) => setOwner(e.target.value)} />
            <datalist id="owners-list">{owners.users.map((u) => <option key={u} value={u} />)}</datalist>
          </div>
          <div className="field">
            <label>{t('common.group')}</label>
            <input className="input" list="groups-list" value={group} disabled={!editableOwner} onChange={(e) => setGroup(e.target.value)} />
            <datalist id="groups-list">{owners.groups.map((g) => <option key={g} value={g} />)}</datalist>
          </div>
        </div>
      )}

      {caps?.permissions && (
        <div className="perm-box">
          <div className="perm-grid">
            <div />
            <div className="perm-h">{t('props.read')}</div>
            <div className="perm-h">{t('props.write')}</div>
            <div className="perm-h">{t('props.exec')}</div>
            {BITS.map(([who, r, w, x]) => (
              <Fragment key={who}>
                <div className="perm-who">{who === 'owner' ? t('common.owner') : who === 'group' ? t('common.group') : t('props.others')}</div>
                {[r, w, x].map((bit) => (
                  <label key={`${who}${bit}`} className="check perm-cell"><input type="checkbox" disabled={!editableMode} checked={(mode & bit) !== 0} onChange={(e) => setBit(bit, e.target.checked)} /></label>
                ))}
              </Fragment>
            ))}
          </div>
          <div className="perm-side">
            <div className="field">
              <label>{t('props.octal')}</label>
              <input
                className="input mono"
                style={{ width: 90 }}
                value={modeText}
                disabled={!editableMode}
                onChange={(e) => {
                  setModeText(e.target.value)
                  if (/^[0-7]{3,4}$/.test(e.target.value)) setMode(parseInt(e.target.value, 8))
                }}
              />
            </div>
            <label className="check"><input type="checkbox" disabled={!editableMode} checked={(mode & 0o4000) !== 0} onChange={(e) => setBit(0o4000, e.target.checked)} /> {t('props.setuid')}</label>
            <label className="check"><input type="checkbox" disabled={!editableMode} checked={(mode & 0o2000) !== 0} onChange={(e) => setBit(0o2000, e.target.checked)} /> {t('props.setgid')}</label>
            <label className="check"><input type="checkbox" disabled={!editableMode} checked={(mode & 0o1000) !== 0} onChange={(e) => setBit(0o1000, e.target.checked)} /> {t('props.sticky')}</label>
          </div>
        </div>
      )}
      {anyDir && (editableMode || editableOwner) && (
        <div className="col" style={{ marginTop: 12, gap: 6 }}>
          <label className="check"><input type="checkbox" checked={recursive} onChange={(e) => setRecursive(e.target.checked)} /> {t('props.recursive')}</label>
          {editableMode && <label className="check"><input type="checkbox" checked={addX} onChange={(e) => setAddX(e.target.checked)} /> {t('transfer.addX')}</label>}
        </div>
      )}

      {one && !one.isDir && caps?.checksum && (
        <div className="checksum-box">
          <label className="field-label">{t('props.checksum')}</label>
          <div className="row">
            <select className="select" style={{ width: 110 }} value={algo} onChange={(e) => setAlgo(e.target.value)}>
              {(caps.protocol === 's3' ? ['md5'] : ['md5', 'sha1', 'sha256', 'sha512']).map((a) => <option key={a} value={a}>{a.toUpperCase()}</option>)}
            </select>
            <button className="btn" disabled={summing} onClick={checksum}>{summing ? <span className="spinner" /> : <Hash />} {t('props.compute')}</button>
          </div>
          {sum && <div className="code-block" style={{ marginTop: 8, cursor: 'copy' }} onClick={() => copyToClipboard(sum)}>{sum}</div>}
        </div>
      )}
    </Dialog>
  )
}
