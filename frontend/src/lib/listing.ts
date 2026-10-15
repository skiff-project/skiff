import type { Entry, Prefs } from './types'
import type { PaneState } from './store'
import { naturalCompare } from './format'
import { ext } from './paths'
import { typeLabel } from './fileicons'

// WinSCP-like mask: "*.txt; *.log | *.bak; cache/", with optional size and
// time conditions ("*.jpg>100K", "*>1D", "*<2026-01-01"). Without wildcards
// or conditions, the text is used as a case-insensitive substring filter.
export function compileMask(src: string): (e: Entry) => boolean {
  src = src.trim()
  if (!src) return () => true
  if (!/[*?|;<>=]/.test(src)) {
    const needle = src.toLowerCase()
    return (e) => e.name.toLowerCase().includes(needle)
  }
  const [inc, exc = ''] = src.split('|')
  const parse = (m: string) => {
    m = m.trim()
    const conds: ((e: Entry) => boolean)[] = []
    // trailing conditions: name>1M<1G, name>=2026-01-01
    let cm: RegExpMatchArray | null
    while ((cm = m.match(/(>=|<=|[<>=])([^<>=]+)$/))) {
      const c = maskCond(cm[1], cm[2].trim())
      if (!c) break
      conds.unshift(c)
      m = m.slice(0, cm.index).trim()
    }
    if (!m) m = '*'
    const dir = m.endsWith('/')
    if (dir) m = m.slice(0, -1)
    const re = new RegExp('^' + m.replace(/[.+^${}()[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.') + '$', 'i')
    return { re, dir, test: (e: Entry) => (re.test(e.name) || re.source === '^.*\\..*$') && conds.every((c) => c(e)) }
  }
  const incs = inc.split(/[;,]/).map((s) => s.trim()).filter(Boolean).map(parse)
  const excs = exc.split(/[;,]/).map((s) => s.trim()).filter(Boolean).map(parse)
  const dirIncs = incs.filter((x) => x.dir)
  const fileIncs = incs.filter((x) => !x.dir)
  return (e) => {
    for (const x of excs) if ((!x.dir || e.isDir) && x.test(e)) return false
    if (e.isDir) return !dirIncs.length || dirIncs.some((x) => x.re.test(e.name))
    return !fileIncs.length || fileIncs.some((x) => x.test(e))
  }
}

// maskCond parses one size or time condition, with WinSCP units:
// sizes K, M, G, T; relative times Y, D, H, N (minutes), S (+ optional S
// to round down), "today", "yesterday"; absolute dates YYYY-MM-DD[ HH:MM[:SS]].
function maskCond(op: string, val: string): ((e: Entry) => boolean) | null {
  const cmp = (a: number, b: number) => (op === '>' ? a > b : op === '<' ? a < b : op === '>=' ? a >= b : op === '<=' ? a <= b : a === b)
  const byTime = (t: number) => (e: Entry) => cmp(new Date(e.mtime).getTime(), t)
  const now = new Date()
  const lower = val.toLowerCase()
  if (lower === 'today' || lower === 'yesterday') {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (lower === 'yesterday' ? 1 : 0))
    return byTime(d.getTime())
  }
  const abs = val.match(/^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?$/)
  if (abs) {
    const d = new Date(+abs[1], +abs[2] - 1, +abs[3], +(abs[4] || 0), +(abs[5] || 0), +(abs[6] || 0))
    if (op === '=') return (e) => new Date(e.mtime).toDateString() === d.toDateString()
    return byTime(d.getTime())
  }
  const rel = val.toUpperCase().match(/^(\d+)([YDHNS])(S?)$/)
  if (rel) {
    const n = +rel[1]
    const d = new Date(now)
    if (rel[2] === 'Y') d.setFullYear(d.getFullYear() - n)
    if (rel[2] === 'D') d.setDate(d.getDate() - n)
    if (rel[2] === 'H') d.setHours(d.getHours() - n)
    if (rel[2] === 'N') d.setMinutes(d.getMinutes() - n)
    if (rel[2] === 'S') d.setSeconds(d.getSeconds() - n)
    if (rel[3]) {
      if (rel[2] === 'Y') d.setMonth(0, 1)
      if (rel[2] === 'Y' || rel[2] === 'D') d.setHours(0, 0, 0, 0)
      if (rel[2] === 'H') d.setMinutes(0, 0, 0)
      if (rel[2] === 'N') d.setSeconds(0, 0)
    }
    return byTime(d.getTime())
  }
  const size = val.toUpperCase().match(/^(\d+(?:\.\d+)?)([KMGT]?)B?$/)
  if (size) {
    const mult = { '': 1, K: 1024, M: 1024 ** 2, G: 1024 ** 3, T: 1024 ** 4 }[size[2]]!
    const limit = parseFloat(size[1]) * mult
    return (e) => !e.isDir && cmp(e.size, limit)
  }
  return null
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
