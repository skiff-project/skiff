import { useState } from 'react'
import { FileArchive, PackageOpen } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast } from '../../lib/store'
import { paneStyle } from '../../lib/actions'
import { basename, join } from '../../lib/paths'
import { t } from '../../lib/i18n'

type Format = 'zip' | 'tar.gz' | 'tar'

const KINDS = ['.tar.gz', '.tgz', '.tar.bz2', '.tbz2', '.tar.xz', '.txz', '.tar', '.zip']

/** The kind of an archive by its name ('' when it is not one). */
export function archiveKind(name: string): string {
  const n = name.toLowerCase()
  return KINDS.find((k) => n.endsWith(k)) || ''
}

/** The errors of the server, said simply. */
function archiveError(e: any): string {
  const m = String(e?.message || e)
  if (m.includes('neither zip nor python3')) return t('arc.noZip')
  if (m.includes('neither unzip nor python3')) return t('arc.noUnzip')
  if (m.includes('already exists')) return t('arc.exists')
  if (m.includes('unsafe path')) return t('arc.unsafe')
  return m
}

/** Compresses entries of a folder into an archive in that folder. */
export function openCompress(conn: string, dir: string, names: string[]) {
  return openDialog((close) => <Compress conn={conn} dir={dir} names={names} close={close} />)
}

function Compress({ conn, dir, names, close }: { conn: string; dir: string; names: string[]; close: () => void }) {
  const style = paneStyle(conn)
  const stem = names.length === 1 ? names[0] : basename(style, dir) || 'archive'
  const [format, setFormat] = useState<Format>('zip')
  const [name, setName] = useState(`${stem}.zip`)
  const [busy, setBusy] = useState(false)
  const pick = (f: Format) => {
    setFormat(f)
    const base = archiveKind(name) ? name.slice(0, name.length - archiveKind(name).length) : name
    setName(`${base}.${f}`)
  }
  const run = async () => {
    if (!name.trim()) return
    setBusy(true)
    try {
      await call('archive.compress', { conn, dir, names, archive: name.trim(), format })
      toast('ok', t('arc.compressed'), name.trim())
      close()
    } catch (e) {
      errorToast(t('arc.compress'), archiveError(e))
      setBusy(false)
    }
  }
  return (
    <Dialog
      size="md"
      title={t('arc.compress')}
      icon={<FileArchive />}
      onClose={close}
      onSubmit={run}
      footer={<><div className="spacer" /><button className="btn" onClick={close}>{t('common.cancel')}</button><button className="btn primary" disabled={busy || !name.trim()} onClick={run}>{busy ? <span className="spinner sm" /> : <FileArchive />} {busy ? t('arc.compressing') : t('arc.compress')}</button></>}
    >
      <div className="form">
        <p className="muted" style={{ margin: 0 }}>{names.length === 1 ? t('arc.what1', { name: names[0] }) : t('arc.whatN', { n: names.length })}</p>
        <div className="field">
          <label>{t('arc.format')}</label>
          <div className="segmented">
            {(['zip', 'tar.gz', 'tar'] as Format[]).map((f) => <button key={f} className={format === f ? 'on' : ''} onClick={() => pick(f)}>{f.toUpperCase()}</button>)}
          </div>
          <span className="hint">{t(`arc.hint.${format === 'tar.gz' ? 'targz' : format}`)}</span>
        </div>
        <div className="field">
          <label>{t('arc.name')}</label>
          <input className="input mono" value={name} onChange={(e) => setName(e.target.value)} spellCheck={false} data-autofocus />
        </div>
        <span className="hint">{conn === 'local' ? t('arc.byLocal') : t('arc.byServer')}</span>
      </div>
    </Dialog>
  )
}

/** Opens an archive into a new folder named after it, or into its folder. */
export function openExtract(conn: string, dir: string, name: string) {
  return openDialog((close) => <Extract conn={conn} dir={dir} name={name} close={close} />)
}

function Extract({ conn, dir, name, close }: { conn: string; dir: string; name: string; close: () => void }) {
  const style = paneStyle(conn)
  const kind = archiveKind(name)
  const stem = name.slice(0, name.length - kind.length) || name
  const [where, setWhere] = useState<'folder' | 'here'>('folder')
  const [folder, setFolder] = useState(stem)
  const [busy, setBusy] = useState(false)
  const dest = where === 'here' ? dir : join(style, dir, folder.trim())
  const run = async () => {
    if (where === 'folder' && !folder.trim()) return
    setBusy(true)
    try {
      await call('archive.extract', { conn, archive: join(style, dir, name), dest })
      toast('ok', t('arc.extracted'), where === 'here' ? name : folder.trim())
      close()
    } catch (e) {
      errorToast(t('arc.extract'), archiveError(e))
      setBusy(false)
    }
  }
  return (
    <Dialog
      size="md"
      title={t('arc.extractOf', { name })}
      icon={<PackageOpen />}
      onClose={close}
      onSubmit={run}
      footer={<><div className="spacer" /><button className="btn" onClick={close}>{t('common.cancel')}</button><button className="btn primary" disabled={busy || (where === 'folder' && !folder.trim())} onClick={run}>{busy ? <span className="spinner sm" /> : <PackageOpen />} {busy ? t('arc.extracting') : t('arc.extract')}</button></>}
    >
      <div className="form">
        <div className="segmented">
          <button className={where === 'folder' ? 'on' : ''} onClick={() => setWhere('folder')}>{t('arc.inFolder')}</button>
          <button className={where === 'here' ? 'on' : ''} onClick={() => setWhere('here')}>{t('arc.here')}</button>
        </div>
        {where === 'folder' && <div className="field"><label>{t('arc.folderName')}</label><input className="input mono" value={folder} onChange={(e) => setFolder(e.target.value)} spellCheck={false} data-autofocus /></div>}
        <span className="hint">{where === 'here' ? t('arc.hereHint') : t('arc.folderHint')}</span>
        <span className="hint">{conn === 'local' ? t('arc.byLocal') : t('arc.byServer')}</span>
      </div>
    </Dialog>
  )
}
