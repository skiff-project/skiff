import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, Check, ExternalLink, FileText, History, RefreshCw, Save, Search, WrapText, Lock } from 'lucide-react'
import { keymap } from '@codemirror/view'
import { openSearchPanel, gotoLine } from '@codemirror/search'
import { call } from '../../lib/api'
import { errorToast, toast, updateTab, useStore, type EditorTab } from '../../lib/store'
import { openVersions } from './VersionsDialog'
import { displayPath, paneStyle } from '../../lib/actions'
import { editExternal } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import type { FileContent } from '../../lib/types'
import { formatSize } from '../../lib/format'
import { baseExtensions, Compartment, EditorState, EditorView as CMView, languageFor, languageName } from './cm'
import { registerSaver } from './closeGuard'
import { EditorView as View } from '@codemirror/view'

const ENCODINGS = ['utf-8', 'windows-1252', 'iso-8859-1', 'iso-8859-15', 'windows-1250', 'windows-1251', 'koi8-r', 'cp850', 'utf-16le', 'utf-16be']

export function EditorView({ tab, visible }: { tab: EditorTab; visible: boolean }) {
  return (
    <FileEditor
      id={tab.id} conn={tab.conn} path={tab.path} readOnly={tab.readOnly} visible={visible} dirty={tab.dirty}
      onDirty={(dirty) => updateTab<EditorTab>(tab.id, (cur) => (cur.dirty === dirty ? {} : { dirty }))}
    />
  )
}

export interface Jump { line: number; col: number; len: number; seq: number }

/**
 * A text file of a connection, edited with CodeMirror. id names it for the
 * savers of the close guard; label replaces the path shown above it.
 */
export function FileEditor({ id, conn, path, readOnly, visible, dirty, onDirty, jump, label }: {
  id: string; conn: string; path: string; readOnly?: boolean; visible: boolean; dirty: boolean
  onDirty: (dirty: boolean) => void; jump?: Jump; label?: string
}) {
  const tab = { id, conn, path, readOnly, dirty, title: path.split(/[\\/]/).pop() || path }
  const dirtyCb = useRef(onDirty)
  dirtyCb.current = onDirty
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<CMView | null>(null)
  const wrapComp = useRef(new Compartment())
  const langComp = useRef(new Compartment())
  const [meta, setMeta] = useState<FileContent | null>(null)
  const [err, setErr] = useState('')
  const [status, setStatus] = useState<'idle' | 'saving' | 'saved'>('idle')
  const [conflict, setConflict] = useState(false)
  const [pos, setPos] = useState({ line: 1, col: 1 })
  const [wrap, setWrap] = useState<boolean>(() => !!useStore.getState().prefs?.editor.wordWrap)
  const savedDoc = useRef('')
  const saveRef = useRef<(force?: boolean) => void>(() => {})
  const prefs = useStore((s) => s.prefs)
  const web = useStore((s) => s.info?.mode === 'web')
  const name = tab.title

  const load = async (encoding?: string) => {
    setErr('')
    try {
      const fc = await call<FileContent>('fs.readText', { conn: tab.conn, path: tab.path, encoding: encoding || '' })
      setMeta(fc)
      savedDoc.current = fc.content
      if (view.current) {
        view.current.dispatch({ changes: { from: 0, to: view.current.state.doc.length, insert: fc.content } })
      } else {
        mount(fc.content)
      }
      dirtyCb.current(false)
      setConflict(false)
    } catch (e: any) {
      setErr(e.message || String(e))
    }
  }

  const mount = (doc: string) => {
    if (!host.current) return
    const st = EditorState.create({
      doc,
      extensions: [
        ...baseExtensions({ readOnly: tab.readOnly, tabSize: prefs?.editor.tabSize || 4 }),
        wrapComp.current.of(wrap ? View.lineWrapping : []),
        langComp.current.of([]),
        keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => (saveRef.current(), true) }]),
        CMView.updateListener.of((u) => {
          if (u.docChanged) {
            dirtyCb.current(u.state.doc.toString() !== savedDoc.current)
            setStatus((st) => (st === 'saved' ? 'idle' : st))
          }
          if (u.selectionSet || u.docChanged) {
            const head = u.state.selection.main.head
            const line = u.state.doc.lineAt(head)
            setPos({ line: line.number, col: head - line.from + 1 })
          }
        }),
      ],
    })
    view.current = new CMView({ state: st, parent: host.current })
    languageFor(name).then((lang) => {
      if (lang && view.current) view.current.dispatch({ effects: langComp.current.reconfigure(lang) })
    })
    setTimeout(() => view.current?.focus(), 30)
  }

  const save = async (force = false) => {
    const v = view.current
    if (!v || !meta || tab.readOnly) return
    setStatus('saving')
    const content = v.state.doc.toString()
    try {
      const e = await call<{ mtime: string; size: number }>('fs.writeText', {
        conn: tab.conn, path: tab.path, content, encoding: meta.encoding, bom: meta.bom, eol: meta.eol, expectMtime: meta.mtime, expectSize: meta.size, force, backup: true,
      })
      savedDoc.current = content
      setMeta({ ...meta, mtime: e.mtime, size: e.size })
      dirtyCb.current(v.state.doc.toString() !== content)
      setStatus('saved')
      setConflict(false)
      setTimeout(() => setStatus((s) => (s === 'saved' ? 'idle' : s)), 1800)
    } catch (e: any) {
      setStatus('idle')
      if (e.code === 'conflict') setConflict(true)
      else errorToast(t('common.save'), e)
    }
  }

  saveRef.current = save

  useEffect(() => {
    load()
    return () => {
      view.current?.destroy()
      view.current = null
    }
  }, [])

  useEffect(() => registerSaver(tab.id, () => save()), [meta])

  useEffect(() => {
    view.current?.dispatch({ effects: wrapComp.current.reconfigure(wrap ? View.lineWrapping : []) })
  }, [wrap])

  useEffect(() => {
    if (visible) setTimeout(() => view.current?.focus(), 20)
  }, [visible])

  // a search result: select the match
  useEffect(() => {
    const v = view.current
    if (!jump || !v) return
    const go = () => {
      const doc = v.state.doc
      const line = doc.line(Math.min(Math.max(jump.line, 1), doc.lines))
      const from = Math.min(line.from + Math.max(jump.col - 1, 0), line.to)
      const to = Math.min(from + jump.len, line.to)
      v.dispatch({ selection: { anchor: from, head: to }, effects: CMView.scrollIntoView(from, { y: 'center' }) })
      v.focus()
    }
    go()
  }, [jump?.seq, meta === null])

  // the previous versions: one replaces the content (saved by the user)
  const versions = () => {
    const v = view.current
    if (!v || !meta) return
    openVersions({
      conn: tab.conn, path: tab.path, name, encoding: meta.encoding, current: v.state.doc.toString(),
      restore: (text) => v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: text } }),
    })
  }

  const st = paneStyle(tab.conn)
  const fontSize = prefs?.editor.fontSize || 13

  return (
    <div className="editor">
      <div className="editor-bar">
        <FileText className="faint" />
        <span className="mono ellipsis editor-path" data-tip={tab.path}>{label ?? displayPath(tab.conn, tab.path)}</span>
        {tab.readOnly && <span className="badge"><Lock /> {t('editor.readOnly')}</span>}
        <div className="spacer" />
        {status === 'saving' && <span className="muted row" style={{ gap: 6 }}><span className="spinner" /> {t('editor.saving')}</span>}
        {status === 'saved' && <span className="ok-text row" style={{ gap: 4 }}><Check style={{ width: 14 }} /> {t('editor.saved')}</span>}
        {tab.dirty && status === 'idle' && <span className="badge warn">{t('editor.modified')}</span>}
        <button className="icon-btn sm" data-tip={t('editor.find')} data-kbd="Ctrl+F" onClick={() => view.current && openSearchPanel(view.current)}><Search /></button>
        <button className={`icon-btn sm${wrap ? ' active' : ''}`} data-tip={t('editor.wrap')} onClick={() => setWrap(!wrap)}><WrapText /></button>
        <button className="icon-btn sm" data-tip={t('editor.reload')} onClick={() => load(meta?.encoding)}><RefreshCw /></button>
        {!tab.readOnly && <button className="icon-btn sm" data-tip={t('history.title')} disabled={!meta} onClick={versions}><History /></button>}
        {!web && <button className="icon-btn sm" data-tip={t('ctx.editExternal')} onClick={() => editExternal(tab.conn, tab.path, prefs?.editor.externalCmd || '')}><ExternalLink /></button>}
        {!tab.readOnly && <button className="btn sm primary" disabled={!tab.dirty || status === 'saving'} onClick={() => save()}><Save /> {t('common.save')}</button>}
      </div>
      {conflict && (
        <div className="lost-bar warn">
          <AlertTriangle />
          <span>{t('editor.conflict')}</span>
          <div className="spacer" />
          <button className="btn sm" onClick={() => load(meta?.encoding)}>{t('editor.reload')}</button>
          <button className="btn sm danger" onClick={() => save(true)}>{t('editor.overwriteServer')}</button>
        </div>
      )}
      {meta?.binary && <div className="lost-bar warn"><AlertTriangle /><span>{t('editor.binary')}</span></div>}
      {err ? (
        <div className="empty">
          <AlertTriangle className="big" style={{ color: 'var(--danger)' }} />
          <h4>{name}</h4>
          <p className="selectable">{err}</p>
          <button className="btn sm" onClick={() => load()}>{t('common.retry')}</button>
        </div>
      ) : (
        <div className="editor-host" ref={host} style={{ fontSize }} />
      )}
      <div className="editor-status">
        <span>{t('editor.line', { line: pos.line, col: pos.col })}</span>
        <span className="faint">·</span>
        <span>{languageName(name) || t('editor.plainText')}</span>
        <div className="spacer" />
        {meta && (
          <>
            <select className="mini-select" value={meta.encoding} onChange={(e) => { if (tab.dirty) setMeta({ ...meta, encoding: e.target.value }); else load(e.target.value) }} data-tip={t('editor.encoding')}>
              {ENCODINGS.map((e) => <option key={e} value={e}>{e.toUpperCase()}</option>)}
            </select>
            <label className="check" style={{ fontSize: 11 }}><input type="checkbox" checked={meta.bom} onChange={(e) => { setMeta({ ...meta, bom: e.target.checked }); dirtyCb.current(true) }} /> BOM</label>
            <select className="mini-select" value={meta.eol} onChange={(e) => { setMeta({ ...meta, eol: e.target.value }); dirtyCb.current(true) }} data-tip={t('editor.eol')}>
              {['LF', 'CRLF', 'CR'].map((x) => <option key={x} value={x}>{x}</option>)}
            </select>
            <span className="faint">{formatSize(meta.size)}</span>
          </>
        )}
        <button className="mini-btn" onClick={() => view.current && gotoLine(view.current)}>{t('editor.goToLine')}</button>
        <span className="faint">{st === 'windows' ? '' : ''}</span>
      </div>
    </div>
  )
}

void toast
