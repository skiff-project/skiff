import { useEffect, useMemo, useState } from 'react'
import { marked } from 'marked'
import { Download, Edit3, ExternalLink, X } from 'lucide-react'
import { set, useStore, type SessionTab } from '../../lib/store'
import { call, downloadUrl } from '../../lib/api'
import { paneStyle, openEditor } from '../../lib/actions'
import { visibleEntries } from '../../lib/listing'
import { FileIcon, previewKind, typeLabel } from '../../lib/fileicons'
import { formatDateFull, formatSize, permString } from '../../lib/format'
import { join } from '../../lib/paths'
import { t } from '../../lib/i18n'
import { saveToComputer, openWithDefault } from '../../lib/fileops'

export function PreviewPane({ tab }: { tab: SessionTab }) {
  const prefs = useStore((s) => s.prefs)
  const web = useStore((s) => s.info?.mode === 'web')
  const pane = tab.panes[tab.active]
  const entry = useMemo(() => {
    const vis = visibleEntries(pane, prefs)
    return vis.find((e) => e.name === pane.focus) || (pane.selected.size === 1 ? vis.find((e) => pane.selected.has(e.name)) : undefined)
  }, [pane.focus, pane.selected, pane.entries])
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const st = paneStyle(pane.conn)
  const path = entry ? join(st, pane.path, entry.name) : ''
  const kind = entry && !entry.isDir ? previewKind(entry.name) : null

  useEffect(() => {
    setText(null)
    setErr('')
    if (!entry || entry.isDir || (kind !== 'text' && kind !== 'markdown')) return
    if (entry.size > 512 * 1024) {
      setErr(t('preview.tooLarge'))
      return
    }
    const id = setTimeout(() => {
      call<{ content: string }>('fs.readText', { conn: pane.conn, path })
        .then((r) => setText(r.content))
        .catch((e) => setErr(e.message))
    }, 120)
    return () => clearTimeout(id)
  }, [path, entry?.mtime])

  const src = path ? downloadUrl(pane.conn, path, true) : ''

  return (
    <aside className="preview">
      <div className="preview-head">
        <span className="strong">{t('toolbar.preview')}</span>
        <div className="spacer" />
        <button className="icon-btn sm" data-tip={t('common.close')} onClick={() => set({ previewOpen: false })}><X /></button>
      </div>
      {!entry ? (
        <div className="empty"><p>{t('common.file')}…</p></div>
      ) : (
        <div className="preview-body">
          <div className="preview-media">
            {kind === 'image' && <img src={src} alt={entry.name} loading="lazy" />}
            {kind === 'video' && <video src={src} controls preload="metadata" />}
            {kind === 'audio' && <audio src={src} controls preload="metadata" />}
            {kind === 'pdf' && <iframe src={src} title={entry.name} sandbox="" />}
            {kind === 'markdown' && text !== null && <div className="markdown selectable" dangerouslySetInnerHTML={{ __html: sanitize(marked.parse(text, { async: false }) as string) }} />}
            {kind === 'text' && text !== null && <pre className="preview-text selectable">{text.slice(0, 20000)}</pre>}
            {(!kind || entry.isDir) && <FileIcon entry={entry} size={72} />}
            {err && <div className="faint" style={{ padding: 12 }}>{err}</div>}
          </div>
          <div className="preview-meta">
            <div className="strong ellipsis selectable">{entry.name}</div>
            <div className="faint">{typeLabel(entry)}</div>
            <dl className="kv">
              {!entry.isDir && <><dt>{t('common.size')}</dt><dd className="tnum">{formatSize(entry.size)}</dd></>}
              <dt>{t('common.modified')}</dt><dd>{formatDateFull(entry.mtime)}</dd>
              {entry.mode ? <><dt>{t('common.permissions')}</dt><dd className="mono">{permString(entry.mode, entry.isDir, entry.isLink)}</dd></> : null}
              {entry.owner && <><dt>{t('common.owner')}</dt><dd>{entry.owner}:{entry.group}</dd></>}
              {entry.linkTarget && <><dt>{t('props.linkTarget')}</dt><dd className="mono">{entry.linkTarget}</dd></>}
            </dl>
            {!entry.isDir && (
              <div className="row" style={{ flexWrap: 'wrap' }}>
                {(kind === 'text' || kind === 'markdown') && <button className="btn sm" onClick={() => openEditor(pane.conn, path)}><Edit3 /> {t('ctx.edit')}</button>}
                {!web && <button className="btn sm" onClick={() => openWithDefault(pane.conn, path)}><ExternalLink /> {t('ctx.open')}</button>}
                <button className="btn sm" onClick={() => saveToComputer(pane.conn, path)}><Download /> {t('ctx.saveAs')}</button>
              </div>
            )}
          </div>
        </div>
      )}
    </aside>
  )
}

// very small sanitizer for rendered markdown (no scripts, no event handlers)
function sanitize(html: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  doc.querySelectorAll('script,style,iframe,object,embed,link,meta,form').forEach((n) => n.remove())
  doc.querySelectorAll('*').forEach((el) => {
    for (const a of Array.from(el.attributes)) {
      if (a.name.startsWith('on') || (['href', 'src'].includes(a.name) && /^\s*javascript:/i.test(a.value))) el.removeAttribute(a.name)
    }
    if (el.tagName === 'A') {
      el.setAttribute('target', '_blank')
      el.setAttribute('rel', 'noopener noreferrer')
    }
    if (el.tagName === 'IMG' && /^https?:/i.test(el.getAttribute('src') || '')) el.removeAttribute('src')
  })
  return doc.body.innerHTML
}
