import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, ArrowLeftRight, Edit3, Save } from 'lucide-react'
import { MergeView } from '@codemirror/merge'
import { call } from '../../lib/api'
import { errorToast, toast, useStore, type DiffTab } from '../../lib/store'
import { openEditor } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { FileContent } from '../../lib/types'
import { baseExtensions, EditorView, languageFor } from './cm'

export function DiffView({ tab }: { tab: DiffTab }) {
  const host = useRef<HTMLDivElement>(null)
  const mv = useRef<MergeView | null>(null)
  const [err, setErr] = useState('')
  const [files, setFiles] = useState<[FileContent, FileContent] | null>(null)
  const [stats, setStats] = useState({ chunks: 0 })
  const [dirty, setDirty] = useState(false)
  const conns = useStore((s) => s.conns)
  const prefs = useStore((s) => s.prefs)
  const title = (c: string) => (c === 'local' ? t('common.local') : conns[c]?.title || c)

  useEffect(() => {
    let alive = true
    Promise.all([
      call<FileContent>('fs.readText', { conn: tab.left.conn, path: tab.left.path }),
      call<FileContent>('fs.readText', { conn: tab.right.conn, path: tab.right.path }),
    ])
      .then(async ([a, b]) => {
        if (!alive || !host.current) return
        setFiles([a, b])
        const lang = await languageFor(tab.left.path)
        const ext = (ro: boolean) => [...baseExtensions({ readOnly: ro, tabSize: prefs?.editor.tabSize || 4 }), ...(lang ? [lang] : [])]
        mv.current = new MergeView({
          a: { doc: a.content, extensions: [...ext(true), EditorView.editable.of(false)] },
          b: {
            doc: b.content,
            extensions: [
              ...ext(false),
              EditorView.updateListener.of((u) => {
                if (u.docChanged) setDirty(true)
              }),
            ],
          },
          parent: host.current,
          revertControls: 'a-to-b',
          highlightChanges: true,
          gutter: true,
          collapseUnchanged: { margin: 3, minSize: 6 },
        })
        setStats({ chunks: mv.current.chunks.length })
      })
      .catch((e) => setErr(e.message))
    return () => {
      alive = false
      mv.current?.destroy()
    }
  }, [])

  const saveRight = async () => {
    if (!mv.current || !files) return
    try {
      await call('fs.writeText', { conn: tab.right.conn, path: tab.right.path, content: mv.current.b.state.doc.toString(), encoding: files[1].encoding, bom: files[1].bom, eol: files[1].eol, expectMtime: files[1].mtime })
      setDirty(false)
      toast('ok', t('editor.saved'), tab.right.path)
    } catch (e) {
      errorToast(t('common.save'), e)
    }
  }

  return (
    <div className="editor diff">
      <div className="diff-head">
        <div className="diff-side">
          <span className="badge">{title(tab.left.conn)}</span>
          <span className="mono ellipsis">{tab.left.path}</span>
          <button className="icon-btn sm" onClick={() => openEditor(tab.left.conn, tab.left.path)}><Edit3 /></button>
        </div>
        <ArrowLeftRight className="faint" style={{ width: 16, flex: 'none' }} />
        <div className="diff-side">
          <span className="badge">{title(tab.right.conn)}</span>
          <span className="mono ellipsis">{tab.right.path}</span>
          <span className="faint">{stats.chunks} change(s)</span>
          <button className="btn sm primary" disabled={!dirty} onClick={saveRight}><Save /> {t('common.save')}</button>
        </div>
      </div>
      {err ? (
        <div className="empty"><AlertTriangle className="big" style={{ color: 'var(--danger)' }} /><p>{err}</p></div>
      ) : (
        <div className="diff-host" ref={host} />
      )}
    </div>
  )
}
