import { useEffect, useMemo, useRef, useState } from 'react'
import { CaseSensitive, ChevronDown, ChevronRight, Regex, Search, X } from 'lucide-react'
import { call } from '../../lib/api'
import { openProjectFile, paneStyle } from '../../lib/actions'
import { joinRel, rel } from '../../lib/paths'
import { FileIcon } from '../../lib/fileicons'
import { t } from '../../lib/i18n'
import type { ProjectTab } from '../../lib/store'
import type { SearchMatch, SearchResult } from '../../lib/types'

/**
 * A search in the files of a project (or of one of its folders). focusSeq
 * puts the keyboard in the search field; scope limits it to a folder.
 */
export function ProjectSearch({ tab, focusSeq, scope, onScope }: { tab: ProjectTab; focusSeq: number; scope: string; onScope: (dir: string) => void }) {
  const style = paneStyle(tab.conn)
  const input = useRef<HTMLInputElement>(null)
  const [q, setQ] = useState('')
  const [regex, setRegex] = useState(false)
  const [cased, setCased] = useState(false)
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<SearchResult | null>(null)
  const [err, setErr] = useState('')
  const [folded, setFolded] = useState<Set<string>>(new Set())
  const root = scope || tab.root
  const seq = useRef(0)

  useEffect(() => {
    if (focusSeq) requestAnimationFrame(() => input.current?.select())
  }, [focusSeq])

  const run = async () => {
    if (!q) return
    const mine = ++seq.current
    setBusy(true)
    setErr('')
    try {
      const r = await call<SearchResult>('project.search', { conn: tab.conn, root, opts: { query: q, regex, caseSensitive: cased } })
      if (mine === seq.current) {
        setRes(r)
        setFolded(new Set())
      }
    } catch (e: any) {
      if (mine === seq.current) {
        setRes(null)
        setErr(e?.message || String(e))
      }
    } finally {
      if (mine === seq.current) setBusy(false)
    }
  }
  // a new option searches again
  useEffect(() => {
    if (res) run()
  }, [regex, cased, scope])

  const groups = useMemo(() => {
    const by = new Map<string, SearchMatch[]>()
    for (const m of res?.matches || []) by.set(m.path, [...(by.get(m.path) || []), m])
    return [...by.entries()]
  }, [res])

  const open = (m: SearchMatch) => openProjectFile(tab.id, joinRel(style, root, m.path), { line: m.line, col: m.col, len: m.len })
  const scopeLabel = scope ? rel(style, tab.root, scope) : ''

  return (
    <div className="psearch">
      <div className="psearch-form">
        <div className="input-group">
          <Search />
          <input
            ref={input} className="input" value={q} placeholder={t('project.searchPlaceholder')} spellCheck={false}
            onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && run()}
          />
        </div>
        <button className={`icon-btn sm${cased ? ' on' : ''}`} data-tip={t('project.caseSensitive')} onClick={() => setCased(!cased)}><CaseSensitive /></button>
        <button className={`icon-btn sm${regex ? ' on' : ''}`} data-tip={t('project.regex')} onClick={() => setRegex(!regex)}><Regex /></button>
      </div>
      {scope && (
        <div className="psearch-scope">
          <span className="ellipsis">{t('project.inFolder', { dir: scopeLabel || scope })}</span>
          <button className="icon-btn sm" data-tip={t('project.wholeProject')} onClick={() => onScope('')}><X /></button>
        </div>
      )}
      {busy && <div className="psearch-note"><span className="spinner sm" /> {t('project.searching')}</div>}
      {err && <div className="psearch-note danger-text selectable">{err}</div>}
      {res && !busy && (
        <div className="psearch-note faint">
          {res.matches.length ? t('project.found', { n: res.matches.length, files: res.files }) : t('project.notFound', { q })}
          {res.truncated && <> · {t('project.truncated')}</>}
          <span data-tip={res.method === 'grep' ? t('project.viaGrep') : t('project.viaScan')}> · {res.method === 'grep' ? 'grep' : t('project.scan')}</span>
        </div>
      )}
      <div className="psearch-results">
        {groups.map(([path, ms]) => {
          const isFolded = folded.has(path)
          const name = path.split('/').pop() || path
          return (
            <div key={path} className="psearch-file" data-file={path}>
              <div className="psearch-file-head" onClick={() => { const n = new Set(folded); n.has(path) ? n.delete(path) : n.add(path); setFolded(n) }} title={path}>
                {isFolded ? <ChevronRight className="ptree-chev" /> : <ChevronDown className="ptree-chev" />}
                <FileIcon entry={{ name, isDir: false }} size={14} />
                <span className="strong ellipsis">{name}</span>
                <span className="faint ellipsis psearch-dir">{path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''}</span>
                <span className="badge">{ms.length}</span>
              </div>
              {!isFolded && ms.map((m) => (
                <div key={m.line + ':' + m.col} className="psearch-line" onClick={() => open(m)} data-line={m.line}>
                  <span className="psearch-ln">{m.line}</span>
                  <span className="psearch-text">{highlight(m)}</span>
                </div>
              ))}
            </div>
          )
        })}
      </div>
    </div>
  )
}

/** The line with its match marked (the text may start with spaces). */
function highlight(m: SearchMatch) {
  const chars = [...m.text]
  const start = Math.max(0, m.col - 1)
  const lead = chars.slice(0, start).join('').replace(/^\s+/, '')
  return (
    <>
      {lead}
      <mark>{chars.slice(start, start + m.len).join('')}</mark>
      {chars.slice(start + m.len).join('')}
    </>
  )
}
