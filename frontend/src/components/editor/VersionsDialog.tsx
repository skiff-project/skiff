import { useEffect, useRef, useState } from 'react'
import { History, RotateCcw } from 'lucide-react'
import { MergeView } from '@codemirror/merge'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'
import { formatDate, formatDateFull, formatSize } from '../../lib/format'
import type { FileContent, HistoryVersion } from '../../lib/types'
import { baseExtensions, EditorView, languageFor } from './cm'

interface Opts {
  conn: string
  path: string
  name: string
  encoding: string
  current: string // the content in the editor
  restore: (text: string) => void
}

/** The copies Skiff kept of a file before each save, compared with it. */
export function openVersions(opts: Opts) {
  return openDialog((close) => <Versions {...opts} close={close} />)
}

function Versions({ conn, path, name, encoding, current, restore, close }: Opts & { close: () => void }) {
  const [list, setList] = useState<HistoryVersion[] | null>(null)
  const [sel, setSel] = useState('')
  const [content, setContent] = useState<FileContent | null>(null)
  const [err, setErr] = useState('')
  const host = useRef<HTMLDivElement>(null)
  const mv = useRef<MergeView | null>(null)

  useEffect(() => {
    call<HistoryVersion[]>('edit.history', { conn, path }).then((vs) => {
      setList(vs)
      if (vs.length) setSel(vs[0].id)
    }).catch((e) => setErr(e.message))
  }, [])

  useEffect(() => {
    if (!sel) return
    let alive = true
    call<FileContent>('edit.historyRead', { conn, path, id: sel, encoding }).then(async (fc) => {
      if (!alive || !host.current) return
      setContent(fc)
      mv.current?.destroy()
      const lang = await languageFor(name)
      const ext = [...baseExtensions({ readOnly: true, tabSize: 4 }), EditorView.editable.of(false), ...(lang ? [lang] : [])]
      mv.current = new MergeView({
        a: { doc: fc.content, extensions: ext },
        b: { doc: current, extensions: ext },
        parent: host.current,
        highlightChanges: true,
        gutter: true,
        collapseUnchanged: { margin: 3, minSize: 6 },
      })
    }).catch((e) => setErr(e.message))
    return () => {
      alive = false
    }
  }, [sel])
  useEffect(() => () => mv.current?.destroy(), [])

  return (
    <Dialog
      size="xl"
      title={t('history.titleOf', { name })}
      icon={<History />}
      onClose={close}
      footer={
        <>
          <span className="faint">{t('history.hint')}</span>
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.close')}</button>
          <button className="btn primary" disabled={!content} onClick={() => { if (content) { restore(content.content); close() } }}><RotateCcw /> {t('history.restore')}</button>
        </>
      }
    >
      <div className="versions">
        <div className="versions-list">
          {list === null && !err && <span className="spinner" />}
          {list?.length === 0 && <p className="muted">{t('history.none')}</p>}
          {list?.map((v) => (
            <button key={v.id} className={`versions-item${v.id === sel ? ' on' : ''}`} data-version={v.id} onClick={() => setSel(v.id)} data-tip={formatDateFull(v.time)}>
              <span>{formatDate(v.time, 'relative')}</span>
              <span className="faint">{formatDate(v.time, 'short')} · {formatSize(v.size)}</span>
            </button>
          ))}
        </div>
        <div className="versions-diff">
          {err && <div className="alert danger"><span className="selectable">{err}</span></div>}
          {list && list.length > 0 && (
            <div className="versions-heads"><span>{t('history.before')}</span><span>{t('history.now')}</span></div>
          )}
          <div className="versions-merge" ref={host} />
        </div>
      </div>
    </Dialog>
  )
}
