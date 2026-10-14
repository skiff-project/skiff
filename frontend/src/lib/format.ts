import { getLang, t } from './i18n'

export type SizeFormat = 'iec' | 'si' | 'bytes'

export function formatSize(n: number, fmt: SizeFormat = 'iec'): string {
  if (n == null || isNaN(n)) return ''
  if (fmt === 'bytes') return n.toLocaleString(getLang())
  const base = fmt === 'si' ? 1000 : 1024
  const units = fmt === 'si' ? ['B', 'kB', 'MB', 'GB', 'TB', 'PB'] : ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  if (n < base) return `${n} B`
  let i = 0
  let v = n
  while (v >= base && i < units.length - 1) {
    v /= base
    i++
  }
  return `${v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)} ${units[i]}`
}

export function formatSpeed(bps: number): string {
  if (!bps || bps < 1) return '—'
  return formatSize(Math.round(bps)) + '/s'
}

export function formatDuration(sec: number): string {
  if (!sec || !isFinite(sec) || sec < 0) return '—'
  sec = Math.round(sec)
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60)
  const s = sec % 60
  if (m < 60) return `${m}m ${String(s).padStart(2, '0')}s`
  const h = Math.floor(m / 60)
  return `${h}h ${String(m % 60).padStart(2, '0')}m`
}

const rtfCache: Record<string, Intl.RelativeTimeFormat> = {}

export function formatDate(iso: string | undefined, fmt: 'relative' | 'short' | 'iso' = 'short'): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime()) || d.getFullYear() < 1971) return ''
  const lang = getLang()
  if (fmt === 'iso') {
    const p = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
  }
  if (fmt === 'relative') {
    const diff = (d.getTime() - Date.now()) / 1000
    const abs = Math.abs(diff)
    const rtf = (rtfCache[lang] ||= new Intl.RelativeTimeFormat(lang, { numeric: 'auto' }))
    if (abs < 60) return t('time.justNow')
    if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute')
    if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour')
    if (abs < 86400 * 7) return rtf.format(Math.round(diff / 86400), 'day')
  }
  const now = new Date()
  const sameYear = d.getFullYear() === now.getFullYear()
  return d.toLocaleString(lang, {
    year: sameYear ? undefined : 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatDateFull(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime()) || d.getFullYear() < 1971) return ''
  return d.toLocaleString(getLang(), { dateStyle: 'full', timeStyle: 'medium' })
}

export function permString(mode: number, isDir = false, isLink = false): string {
  const t = isLink ? 'l' : isDir ? 'd' : '-'
  const r = (b: number, c: string) => (mode & b ? c : '-')
  let s = t
  s += r(0o400, 'r') + r(0o200, 'w')
  s += mode & 0o4000 ? (mode & 0o100 ? 's' : 'S') : r(0o100, 'x')
  s += r(0o040, 'r') + r(0o020, 'w')
  s += mode & 0o2000 ? (mode & 0o010 ? 's' : 'S') : r(0o010, 'x')
  s += r(0o004, 'r') + r(0o002, 'w')
  s += mode & 0o1000 ? (mode & 0o001 ? 't' : 'T') : r(0o001, 'x')
  return s
}

export function octal(mode: number): string {
  return (mode & 0o7777).toString(8).padStart(4, '0')
}

// natural, case-insensitive comparison
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
export function naturalCompare(a: string, b: string): number {
  return collator.compare(a, b)
}

export function plural(n: number, one: string, many: string): string {
  return `${n.toLocaleString(getLang())} ${n === 1 ? one : many}`
}
