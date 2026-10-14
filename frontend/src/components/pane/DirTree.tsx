import { useEffect, useRef, useState } from 'react'
import { ChevronDown, ChevronRight, Folder, FolderOpen, HardDrive } from 'lucide-react'
import { call } from '../../lib/api'
import { loadDir, paneStyle } from '../../lib/actions'
import { useStore, type PaneState, type Side } from '../../lib/store'
import { basename, join } from '../../lib/paths'
import { naturalCompare } from '../../lib/format'
import type { Entry } from '../../lib/types'

interface Node {
  path: string
  name: string
  children?: string[] // child paths, undefined = not loaded
  loading?: boolean
  error?: boolean
}

// DirTree shows the folder hierarchy of a pane (WinSCP "directory tree").
export function DirTree({ tabId, side, pane }: { tabId: string; side: Side; pane: PaneState }) {
  const showHidden = useStore((s) => !!s.prefs?.showHidden)
  const style = paneStyle(pane.conn)
  const rootPath = style === 'windows' ? '' : '/'
  const [nodes, setNodes] = useState<Record<string, Node>>({ [rootPath]: { path: rootPath, name: style === 'windows' ? 'This PC' : '/' } })
  const [open, setOpen] = useState<Record<string, boolean>>({ [rootPath]: true })
  const ref = useRef<HTMLDivElement>(null)

  const nameOf = (p: string) => (p === rootPath ? (style === 'windows' ? 'This PC' : '/') : basename(style, p))
  const load = async (p: string) => {
    setNodes((n) => ({ ...n, [p]: { ...(n[p] || { path: p, name: nameOf(p) }), loading: true } }))
    try {
      const r = await call<{ path: string; entries: Entry[] }>('fs.list', { conn: pane.conn, path: p })
      const dirs = r.entries.filter((e) => e.isDir && (showHidden || !e.hidden)).sort((a, b) => naturalCompare(a.name, b.name))
      setNodes((n) => {
        const next = { ...n }
        const kids: string[] = []
        for (const d of dirs) {
          const cp = style === 'windows' && p === '' ? d.name : join(style, p, d.name)
          kids.push(cp)
          if (!next[cp]) next[cp] = { path: cp, name: d.name }
        }
        next[p] = { ...(next[p] || { path: p, name: nameOf(p) }), children: kids, loading: false, error: false }
        return next
      })
    } catch {
      setNodes((n) => ({ ...n, [p]: { ...(n[p] || { path: p, name: nameOf(p) }), children: [], loading: false, error: true } }))
    }
  }

  // reset when the connection changes
  useEffect(() => {
    setNodes({ [rootPath]: { path: rootPath, name: style === 'windows' ? 'This PC' : '/' } })
    setOpen({ [rootPath]: true })
  }, [pane.conn, showHidden])

  // expand the ancestors of the current folder
  useEffect(() => {
    const chain: string[] = [rootPath]
    if (style === 'windows') {
      const parts = pane.path.split('\\').filter(Boolean)
      let acc = ''
      parts.forEach((part, i) => {
        acc = i === 0 ? part + '\\' : join(style, acc, part)
        chain.push(acc)
      })
    } else {
      let acc = ''
      for (const part of pane.path.split('/').filter(Boolean)) {
        acc += '/' + part
        chain.push(acc)
      }
    }
    setOpen((o) => {
      const n = { ...o }
      for (const c of chain) n[c] = true
      return n
    })
    for (const c of chain) if (!nodes[c]?.children && !nodes[c]?.loading) load(c)
    setTimeout(() => ref.current?.querySelector('.tree-node.current')?.scrollIntoView({ block: 'nearest' }), 50)
  }, [pane.path, pane.conn, nodes[rootPath]?.children === undefined])

  const render = (p: string, depth: number): React.ReactNode => {
    const n = nodes[p]
    if (!n) return null
    const isOpen = open[p]
    const current = p === pane.path
    return (
      <div key={p}>
        <div
          className={`tree-node${current ? ' current' : ''}`}
          style={{ paddingLeft: 4 + depth * 12 }}
          onClick={() => loadDir(tabId, side, p)}
          title={p}
        >
          <span
            className="tree-twist"
            onClick={(e) => {
              e.stopPropagation()
              const next = !isOpen
              setOpen((o) => ({ ...o, [p]: next }))
              if (next && !n.children) load(p)
            }}
          >
            {n.children && n.children.length === 0 ? null : isOpen ? <ChevronDown /> : <ChevronRight />}
          </span>
          {style === 'windows' && depth === 1 ? <HardDrive className="tree-ic drive" /> : isOpen ? <FolderOpen className="tree-ic" /> : <Folder className="tree-ic" />}
          <span className="ellipsis">{n.name}</span>
          {n.loading && <span className="spinner" style={{ width: 10, height: 10, borderWidth: 1.5 }} />}
        </div>
        {isOpen && n.children?.map((c) => render(c, depth + 1))}
      </div>
    )
  }

  return (
    <div className="dir-tree" ref={ref}>
      {render(rootPath, 0)}
    </div>
  )
}
