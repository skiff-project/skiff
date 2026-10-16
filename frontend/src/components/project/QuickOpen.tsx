import { useEffect, useMemo, useRef, useState } from 'react'
import { call } from '../../lib/api'
import { openProjectFile, paneStyle } from '../../lib/actions'
import { joinRel } from '../../lib/paths'
import { FileIcon } from '../../lib/fileicons'
import { t } from '../../lib/i18n'
import type { ProjectTab } from '../../lib/store'
import type { ProjectFiles } from '../../lib/types'

// the file lists of the projects, kept a little while
const cache = new Map<string, { at: number; list: ProjectFiles }>()

/**
 * Scores a path for a query: its letters in order, better in the file
 * name, together and at the start of words. null when they are not all there.
 */
export function fuzzyScore(path: string, q: string): number | null {
  if (!q) return 0
  const p = path.toLowerCase()
  const query = q.toLowerCase().replace(/\s+/g, '')
  const nameAt = p.lastIndexOf('/') + 1
  let score = 0
  let at = -1
  let run = 0
  for (const ch of query) {
    const i = p.indexOf(ch, at + 1)
    if (i < 0) return null
    run = i === at + 1 ? run + 1 : 0
    score += 1 + run * 2
    if (i >= nameAt) score += 2
    if (i === 0 || '/._- '.includes(p[i - 1])) score += 3
    at = i
  }
  if (p.slice(nameAt).startsWith(query)) score += 10
  return score - p.length * 0.01
}

/** "Go to file" (Ctrl+P): a file of the project by its name. */
export function QuickOpen({ tab, onClose }: { tab: ProjectTab; onClose: () => void }) {
  const style = paneStyle(tab.conn)
  const key = tab.conn + '|' + tab.root
  const cached = cache.get(key)
  const [list, setList] = useState<ProjectFiles | null>(cached && Date.now() - cached.at < 30_000 ? cached.list : null)
  const [err, setErr] = useState('')
  const [q, setQ] = useState('')
  const [hot, setHot] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (list) return
    call<ProjectFiles>('project.files', { conn: tab.conn, root: tab.root })
      .then((r) => {
        cache.set(key, { at: Date.now(), list: r })
        setList(r)
      })
      .catch((e) => setErr(e?.message || String(e)))
  }, [])

  const shown = useMemo(() => {
    const files = list?.files || []
    if (!q) {
      // the open files first
      const open = tab.files.map((f) => f.path)
      return files.filter((f) => open.includes(joinRel(style, tab.root, f))).concat(files.filter((f) => !open.includes(joinRel(style, tab.root, f)))).slice(0, 60)
    }
    const scored: { f: string; s: number }[] = []
    for (const f of files) {
      const s = fuzzyScore(f, q)
      if (s !== null) scored.push({ f, s })
    }
    scored.sort((a, b) => b.s - a.s)
    return scored.slice(0, 60).map((x) => x.f)
  }, [list, q])
  useEffect(() => setHot(0), [q])
  useEffect(() => {
    listRef.current?.querySelector('.pal-item.hot')?.scrollIntoView({ block: 'nearest' })
  }, [hot])

  const pick = (f: string) => {
    openProjectFile(tab.id, joinRel(style, tab.root, f))
    onClose()
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setHot((h) => Math.min(h + 1, shown.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHot((h) => Math.max(h - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); if (shown[hot]) pick(shown[hot]) }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
  }

  return (
    <div className="overlay palette-overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette quick-open">
        <div className="pal-input">
          <input autoFocus value={q} placeholder={t('project.goToFile')} onChange={(e) => setQ(e.target.value)} onKeyDown={onKey} spellCheck={false} />
          <kbd>Esc</kbd>
        </div>
        <div className="pal-list" ref={listRef}>
          {err && <div className="empty" style={{ height: 100 }}><p className="danger-text">{err}</p></div>}
          {!list && !err && <div className="empty" style={{ height: 100 }}><span className="spinner" /></div>}
          {list && shown.length === 0 && <div className="empty" style={{ height: 100 }}><p>{t('project.noFile', { q })}</p></div>}
          {shown.map((f, i) => {
            const name = f.split('/').pop() || f
            const dir = f.includes('/') ? f.slice(0, f.lastIndexOf('/')) : ''
            return (
              <div key={f} className={`pal-item${i === hot ? ' hot' : ''}`} onMouseMove={() => setHot(i)} onClick={() => pick(f)} data-file={f}>
                <span className="pal-icon"><FileIcon entry={{ name, isDir: false }} size={15} /></span>
                <span className="ellipsis">{name}</span>
                {dir && <span className="faint ellipsis pal-hint">{dir}</span>}
              </div>
            )
          })}
        </div>
        {list?.truncated && <div className="quick-open-foot faint">{t('project.manyFiles', { n: list.files.length })}</div>}
      </div>
    </div>
  )
}
