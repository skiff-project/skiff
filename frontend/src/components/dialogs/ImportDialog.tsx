import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, Download, FileUp, Upload } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { displayPath, reloadSites } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { ImportItem, ImportSource } from '../../lib/types'
import { copyToClipboard } from '../../lib/fileops'

export function openImportDialog() {
  openDialog((close) => <ImportDialog close={close} />)
}

function ImportDialog({ close }: { close: () => void }) {
  const [sources, setSources] = useState<ImportSource[]>([])
  const [src, setSrc] = useState('')
  const [data, setData] = useState('')
  const [items, setItems] = useState<ImportItem[] | null>(null)
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [folder, setFolder] = useState(t('import.defaultFolder'))
  const [busy, setBusy] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    call<ImportSource[]>('sites.importSources').then((s) => {
      setSources(s)
      const first = s.find((x) => x.available && !x.needsFile) || s[0]
      if (first) setSrc(first.id)
    })
  }, [])

  const source = sources.find((s) => s.id === src)

  const load = async () => {
    setBusy(true)
    try {
      const r = await call<ImportItem[]>('sites.importPreview', { source: src, data: data || undefined })
      setItems(r)
      setSel(new Set(r.map((_, i) => i)))
    } catch (e) {
      errorToast(t('import.title'), e)
    } finally {
      setBusy(false)
    }
  }

  const commit = async () => {
    if (!items) return
    setBusy(true)
    try {
      const n = await call<number>('sites.importCommit', { items: items.filter((_, i) => sel.has(i)), folder })
      await reloadSites()
      toast('ok', t('import.done', { n }))
      close()
    } catch (e) {
      errorToast(t('import.title'), e)
      setBusy(false)
    }
  }

  const pickFile = (f?: File) => {
    if (!f) return
    f.text().then((txt) => setData(txt))
  }

  return (
    <Dialog
      size="lg"
      title={t('import.title')}
      icon={<Download />}
      onClose={close}
      footer={
        <>
          {items && <button className="btn" onClick={() => setItems(null)}>←</button>}
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.cancel')}</button>
          {!items ? (
            <button className="btn primary" disabled={busy || !src || (source?.needsFile && !data)} onClick={load}>{t('import.load')}</button>
          ) : (
            <button className="btn primary" disabled={busy || !sel.size} onClick={commit}>{t('import.do', { n: sel.size })}</button>
          )}
        </>
      }
    >
      {!items ? (
        <div className="form">
          <div className="source-list">
            {sources.map((s) => (
              <label key={s.id} className={`source-item${src === s.id ? ' on' : ''}${!s.available ? ' disabled' : ''}`}>
                <input type="radio" name="src" checked={src === s.id} disabled={!s.available} onChange={() => { setSrc(s.id); setData('') }} />
                <div className="grow">
                  <div className="strong">{t(`import.src.${s.id}`) === `import.src.${s.id}` ? s.name : t(`import.src.${s.id}`)}</div>
                  <div className="faint mono ellipsis" style={{ fontSize: 11 }}>{s.path ? displayPath('local', s.path) : s.needsFile ? t('import.selectFile') : s.available ? t('import.detected') : t('import.notFound')}</div>
                </div>
                {s.path && !s.needsFile && <span className="badge ok">✓</span>}
              </label>
            ))}
          </div>
          {source?.needsFile && (
            <div className="field">
              <label>{t('import.file')}</label>
              <div
                className="drop-area"
                onClick={() => fileRef.current?.click()}
                onDragOver={(e) => e.preventDefault()}
                onDrop={(e) => { e.preventDefault(); pickFile(e.dataTransfer.files[0]) }}
                data-dropzone
              >
                <FileUp />
                <span>{data ? t('import.loaded', { n: data.length.toLocaleString() }) : 'WinSCP.ini, sitemanager.xml, ssh config, JSON…'}</span>
              </div>
              <input ref={fileRef} type="file" hidden onChange={(e) => pickFile(e.target.files?.[0])} />
              <textarea className="textarea mono" rows={5} value={data} onChange={(e) => setData(e.target.value)} placeholder={t('import.fileHint')} style={{ fontSize: 11 }} />
            </div>
          )}
        </div>
      ) : (
        <div className="form">
          <div className="row">
            <span className="muted">{t('import.found', { n: items.length })}</span>
            <div className="spacer" />
            <label className="field-label">{t('import.into')}</label>
            <input className="input sm" style={{ width: 180 }} value={folder} onChange={(e) => setFolder(e.target.value)} />
          </div>
          <div style={{ maxHeight: 380, overflow: 'auto', border: '1px solid var(--border)', borderRadius: 8 }}>
            <table className="table">
              <thead>
                <tr>
                  <th style={{ width: 30 }}><label className="check"><input type="checkbox" checked={sel.size === items.length} onChange={(e) => setSel(e.target.checked ? new Set(items.map((_, i) => i)) : new Set())} /></label></th>
                  <th>{t('site.name')}</th>
                  <th>{t('site.protocol')}</th>
                  <th>{t('site.host')}</th>
                  <th>{t('site.password')}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it, i) => (
                  <tr key={i} onClick={() => setSel((s) => { const n = new Set(s); n.has(i) ? n.delete(i) : n.add(i); return n })} style={{ cursor: 'pointer' }}>
                    <td><label className="check"><input type="checkbox" checked={sel.has(i)} readOnly /></label></td>
                    <td><span className="faint">{it.site.folder ? it.site.folder + ' / ' : ''}</span>{it.site.name}</td>
                    <td><span className="proto-badge">{it.site.protocol.toUpperCase()}</span></td>
                    <td className="mono">{it.site.user ? it.site.user + '@' : ''}{it.site.host}{it.site.port ? ':' + it.site.port : ''}</td>
                    <td>{it.password ? '••••••' : it.note ? <span className="warn-text" data-tip={it.note}><AlertTriangle style={{ width: 14 }} /></span> : ''}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </Dialog>
  )
}

export function openExportDialog() {
  openDialog((close) => <ExportDialog close={close} />)
}

function ExportDialog({ close }: { close: () => void }) {
  const sites = useStore((s) => s.sites)
  const [format, setFormat] = useState('json')
  const [out, setOut] = useState('')
  useEffect(() => {
    call<string>('sites.export', { ids: [], format }).then(setOut).catch((e) => setOut(String(e.message)))
  }, [format])
  const save = () => {
    const ext = format === 'json' ? 'json' : format === 'winscp-ini' ? 'ini' : format === 'ssh-config' ? 'config' : 'txt'
    const blob = new Blob([out], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `skiff-sites.${ext}`
    a.click()
    URL.revokeObjectURL(a.href)
  }
  return (
    <Dialog size="lg" title={t('export.title')} icon={<Upload />} onClose={close} footer={<><span className="faint">{t('export.footer', { n: sites.length })}</span><div className="spacer" /><button className="btn" onClick={() => copyToClipboard(out)}>{t('common.copy')}</button><button className="btn primary" onClick={save}><Download /> {t('common.save')}</button></>}>
      <div className="form">
        <div className="segmented">
          {[['json', 'Skiff JSON'], ['winscp-ini', 'WinSCP.ini'], ['ssh-config', 'OpenSSH config'], ['urls', 'URLs']].map(([v, l]) => <button key={v} className={format === v ? 'on' : ''} onClick={() => setFormat(v)}>{l}</button>)}
        </div>
        <pre className="code-block" style={{ maxHeight: 420, margin: 0 }}>{out}</pre>
      </div>
    </Dialog>
  )
}
