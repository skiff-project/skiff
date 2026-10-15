import type { PathStyle } from './types'

export function sep(style: PathStyle) {
  return style === 'windows' ? '\\' : '/'
}

export function join(style: PathStyle, dir: string, name: string): string {
  if (style === 'windows') {
    if (!dir || dir === '/') return name
    return dir.endsWith('\\') ? dir + name : dir + '\\' + name
  }
  if (!dir) dir = '/'
  return dir.endsWith('/') ? dir + name : dir + '/' + name
}

export function dirname(style: PathStyle, p: string): string {
  if (style === 'windows') {
    const t = p.replace(/\\+$/, '')
    const i = t.lastIndexOf('\\')
    if (i < 0) return ''
    const d = t.slice(0, i)
    return /^[A-Za-z]:$/.test(d) ? d + '\\' : d
  }
  if (!p || p === '/') return '/'
  const t = p.replace(/\/+$/, '')
  const i = t.lastIndexOf('/')
  if (i <= 0) return '/'
  return t.slice(0, i)
}

export function basename(style: PathStyle, p: string): string {
  if (style === 'windows') {
    const t = p.replace(/\\+$/, '')
    return t.slice(t.lastIndexOf('\\') + 1) || t
  }
  if (p === '/') return '/'
  const t = p.replace(/\/+$/, '')
  return t.slice(t.lastIndexOf('/') + 1)
}

export function isRoot(style: PathStyle, p: string): boolean {
  if (style === 'windows') return p === '' || p === '/'
  return p === '/' || p === ''
}

// segments returns breadcrumb parts: [{name, path}]
export function segments(style: PathStyle, p: string): { name: string; path: string }[] {
  const out: { name: string; path: string }[] = []
  if (style === 'windows') {
    out.push({ name: 'This PC', path: '' })
    if (!p) return out
    const parts = p.split('\\').filter(Boolean)
    let acc = ''
    parts.forEach((part, i) => {
      acc = i === 0 ? part + '\\' : join(style, acc, part)
      out.push({ name: part, path: acc })
    })
    return out
  }
  out.push({ name: '/', path: '/' })
  const parts = p.split('/').filter(Boolean)
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    out.push({ name: part, path: acc })
  }
  return out
}

// relative path (forward slashes) of p under root, or null.
export function rel(style: PathStyle, root: string, p: string): string | null {
  const s = sep(style)
  const r = root.endsWith(s) ? root : root + s
  if (p === root) return ''
  if (!p.startsWith(r)) return null
  const out = p.slice(r.length)
  return style === 'windows' ? out.split('\\').join('/') : out
}

export function joinRel(style: PathStyle, dir: string, relPath: string): string {
  let out = dir
  for (const part of relPath.split('/').filter(Boolean)) out = join(style, out, part)
  return out
}

export function ext(name: string): string {
  const i = name.lastIndexOf('.')
  if (i <= 0) return ''
  return name.slice(i + 1).toLowerCase()
}

export function shellQuote(s: string): string {
  if (/^[A-Za-z0-9_\-./=:@%+,]+$/.test(s)) return s
  return "'" + s.replace(/'/g, `'\\''`) + "'"
}

// tildify shows paths inside the home folder as "~/…" (display only).
export function tildify(style: PathStyle, p: string, home?: string): string {
  if (!home || !p) return p
  const h = home.replace(/[\\/]+$/, '')
  if (p === h) return '~'
  const s = sep(style)
  if (p.startsWith(h + s)) return '~' + p.slice(h.length)
  return p
}

// normalize user-typed paths ("~" is expanded by Skiff itself)
export function normalize(style: PathStyle, p: string, cwd: string): string {
  p = p.trim()
  if (!p) return cwd
  if (p === '~' || p.startsWith('~/') || p.startsWith('~\\')) return p
  if (style === 'windows') {
    p = p.replace(/\//g, '\\')
    if (/^[A-Za-z]:$/.test(p)) return p + '\\'
    if (!/^[A-Za-z]:\\/.test(p) && !p.startsWith('\\\\')) p = join(style, cwd, p)
    const parts: string[] = []
    for (const seg of p.split('\\')) {
      if (seg === '..') parts.pop()
      else if (seg && seg !== '.') parts.push(seg)
    }
    let out = parts.join('\\')
    if (/^[A-Za-z]:$/.test(out)) out += '\\'
    return out
  }
  if (!p.startsWith('/')) p = join(style, cwd, p)
  const parts: string[] = []
  for (const seg of p.split('/')) {
    if (seg === '..') parts.pop()
    else if (seg && seg !== '.') parts.push(seg)
  }
  return '/' + parts.join('/')
}
