import type { Entry, Prefs } from './types'
import type { PaneState } from './store'
import { naturalCompare } from './format'
import { ext } from './paths'
import { typeLabel } from './fileicons'

// Simple WinSCP-like mask: "*.txt; *.log | *.bak; cache/". Without
// wildcards, the text is used as a case-insensitive substring filter.
export function compileMask(src: string): (e: Entry) => boolean {
  src = src.trim()
  if (!src) return () => true
  if (!/[*?|;]/.test(src)) {
    const needle = src.toLowerCase()
    return (e) => e.name.toLowerCase().includes(needle)
  }
  const [inc, exc = ''] = src.split('|')
  const toRe = (m: string) => {
    m = m.trim()
    const dir = m.endsWith('/')
    if (dir) m = m.slice(0, -1)
    const re = new RegExp('^' + m.replace(/[.+^${}()[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.') + '$', 'i')
    return { re, dir }
  }
  const incs = inc.split(/[;,]/).map((s) => s.trim()).filter(Boolean).map(toRe)
  const excs = exc.split(/[;,]/).map((s) => s.trim()).filter(Boolean).map(toRe)
  const dirIncs = incs.filter((x) => x.dir)
  const fileIncs = incs.filter((x) => !x.dir)
  return (e) => {
    for (const x of excs) if ((!x.dir || e.isDir) && x.re.test(e.name)) return false
    if (e.isDir) return !dirIncs.length || dirIncs.some((x) => x.re.test(e.name))
    return !fileIncs.length || fileIncs.some((x) => x.re.test(e.name) || x.re.source === '^.*\\..*$')
  }
}

export function sortEntries(entries: Entry[], sort: PaneState['sort'], prefs: Pick<Prefs, 'dirsFirst' | 'naturalSort'>): Entry[] {
  const cmpName = prefs.naturalSort ? naturalCompare : (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0)
  const out = [...entries]
  const dir = sort.dir
  out.sort((a, b) => {
    if (prefs.dirsFirst && a.isDir !== b.isDir) return a.isDir ? -1 : 1
    let r = 0
    switch (sort.key) {
      case 'size':
        r = a.size - b.size
        break
      case 'mtime':
        r = new Date(a.mtime).getTime() - new Date(b.mtime).getTime()
        break
      case 'type':
        r = a.isDir === b.isDir ? cmpName(a.isDir ? '' : ext(a.name) || typeLabel(a), b.isDir ? '' : ext(b.name) || typeLabel(b)) : 0
        break
      case 'perm':
        r = (a.mode & 0o7777) - (b.mode & 0o7777)
        break
      case 'owner':
        r = cmpName(a.owner || '', b.owner || '')
        break
    }
    if (r === 0) r = cmpName(a.name, b.name)
    return r * dir
  })
  return out
}

export function visibleEntries(p: PaneState, prefs: Prefs | null): Entry[] {
  const pr = prefs ?? ({ showHidden: false, dirsFirst: true, naturalSort: true } as Prefs)
  let list = p.entries
  if (!pr.showHidden) list = list.filter((e) => !e.hidden)
  if (p.filter) {
    const m = compileMask(p.filter)
    list = list.filter(m)
  }
  return sortEntries(list, p.sort, pr)
}

export function selectionOf(p: PaneState, visible: Entry[]): Entry[] {
  if (p.selected.size > 0) return visible.filter((e) => p.selected.has(e.name))
  const f = visible.find((e) => e.name === p.focus)
  return f ? [f] : []
}
