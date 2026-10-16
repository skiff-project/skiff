import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, ArrowUp, ChevronRight, Copy, FolderOpen, HardDrive, RefreshCw, Trash2 } from 'lucide-react'
import { call } from '../../lib/api'
import { errorToast, toast, updateTab, useStore, type UsageTab } from '../../lib/store'
import { openSessionAt, paneStyle } from '../../lib/actions'
import { basename, dirname, join } from '../../lib/paths'
import { FileIcon } from '../../lib/fileicons'
import { formatSize } from '../../lib/format'
import { copyToClipboard } from '../../lib/fileops'
import { ask } from '../dialogs/common'
import { t } from '../../lib/i18n'

interface UsageEntry { name: string; size: number; isDir: boolean }
interface Usage {
  dir: string; total: number; entries: UsageEntry[]; method: 'du' | 'scan'; partial?: boolean; denied?: number
  diskSize?: number; diskUsed?: number; diskFree?: number; mount?: string
}

const SHOWN = 200

/** What takes room in a folder: its items by size, one level at a time. */
export function UsageView({ tab }: { tab: UsageTab }) {
  const style = paneStyle(tab.conn)
  const conn = useStore((s) => s.conns[tab.conn])
  const [path, setPath] = useState(tab.path)
  const [usage, setUsage] = useState<Usage | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const cache = useRef(new Map<string, Usage>())
  const seq = useRef(0)

  const load = async (p: string, fresh = false) => {
    setPath(p)
    setError('')
    const cached = !fresh && cache.current.get(p)
    if (cached) {
      setUsage(cached)
      return
    }
    const mine = ++seq.current
    setBusy(true)
    try {
      const u = await call<Usage>('disk.usage', { conn: tab.conn, path: p })
      cache.current.set(p, u)
      if (mine === seq.current) setUsage(u)
    } catch (e: any) {
      if (mine === seq.current) {
        setUsage(null)
        setError(e?.message || String(e))
      }
    } finally {
      if (mine === seq.current) setBusy(false)
    }
  }
  useEffect(() => { load(tab.path) }, [])

  // the folders above lose their sizes when something is deleted
  const forget = (p: string) => {
    for (const k of [...cache.current.keys()]) if (p.startsWith(k) || k.startsWith(p)) cache.current.delete(k)
  }
  const remove = async (e: UsageEntry) => {
    const full = join(style, path, e.name)
    if (!(await ask({ title: t('ctx.delete'), message: t('usage.deleteConfirm', { name: e.name, size: formatSize(e.size) }), danger: true, ok: t('common.delete') }))) return
    try {
      await call('fs.delete', { conn: tab.conn, paths: [full] })
      toast('ok', t('usage.deleted', { size: formatSize(e.size) }), e.name)
      forget(path)
      load(path, true)
    } catch (err) {
      errorToast(t('ctx.delete'), err)
    }
  }

  // the way from the folder of the tab to the folder shown
  const crumbs: { name: string; path: string }[] = []
  for (let p = path; ; p = dirname(style, p)) {
    crumbs.unshift({ name: basename(style, p) || p, path: p })
    if (p === tab.path || dirname(style, p) === p || crumbs.length > 40) break
  }
  const up = path !== tab.path ? dirname(style, path) : dirname(style, path) !== path ? dirname(style, path) : ''
  const go = (p: string) => {
    if (!p.startsWith(tab.path)) updateTab<UsageTab>(tab.id, () => ({ path: p }))
    load(p)
  }

  const max = usage?.entries[0]?.size || 1
  const rest = usage ? usage.entries.slice(SHOWN) : []
  const restSize = rest.reduce((n, e) => n + e.size, 0)
  const diskPct = usage?.diskSize ? Math.round(((usage.diskUsed || 0) / usage.diskSize) * 100) : 0

  return (
    <div className="usage">
      <div className="usage-bar">
        <HardDrive className="faint" />
        <button className="icon-btn sm" data-tip={t('usage.up')} disabled={!up} onClick={() => go(up)}><ArrowUp /></button>
        <div className="usage-crumbs">
          {crumbs.map((c, i) => (
            <span key={c.path} className="usage-crumb">
              {i > 0 && <ChevronRight className="faint" />}
              <button className={`mini-btn${i === crumbs.length - 1 ? ' strong' : ''}`} onClick={() => go(c.path)}>{c.name}</button>
            </span>
          ))}
        </div>
        <span className="faint ellipsis">{conn?.title || (tab.conn === 'local' ? t('common.local') : '')}</span>
        <div className="spacer" />
        <button className="btn sm" onClick={() => openSessionAt(tab.conn, path)}><FolderOpen /> {t('usage.showInPanel')}</button>
        <button className="icon-btn sm" data-tip={t('usage.recount')} disabled={busy} onClick={() => { forget(path); load(path, true) }}><RefreshCw className={busy ? 'spin' : ''} /></button>
      </div>
      {usage?.diskSize ? (
        <div className="usage-disk">
          <div className="usage-disk-text">
            <span className="strong">{t('usage.disk', { mount: usage.mount || '/' })}</span>
            <span>{t('usage.diskUsed', { used: formatSize(usage.diskUsed || 0), size: formatSize(usage.diskSize), pct: diskPct })}</span>
            <span className="faint">· {t('usage.diskFree', { free: formatSize(usage.diskFree || 0) })}</span>
          </div>
          <div className={`usage-disk-bar${diskPct >= 90 ? ' danger' : diskPct >= 75 ? ' warn' : ''}`}><div style={{ width: `${diskPct}%` }} /></div>
        </div>
      ) : null}
      {error && <div className="lost-bar danger selectable"><AlertTriangle /><span>{error}</span></div>}
      {busy && !usage && <div className="empty"><span className="spinner" /><p className="muted">{t('usage.counting')}</p></div>}
      {usage && (
        <>
          <div className="usage-summary">
            <span className="strong">{t('usage.folderTotal', { size: formatSize(usage.total) })}</span>
            <span className="faint">· {t('usage.items', { n: usage.entries.length })}</span>
            <span className="faint" data-tip={usage.method === 'du' ? t('usage.viaDuTip') : t('usage.viaScanTip')}>· {usage.method === 'du' ? t('usage.viaDu') : t('usage.viaScan')}</span>
            {busy && <span className="spinner sm" />}
            {usage.partial && <span className="warn-text">· {t('usage.partial')}</span>}
            {!!usage.denied && <span className="warn-text" data-tip={t('usage.deniedTip')}>· {t('usage.denied', { n: usage.denied })}</span>}
          </div>
          <div className="usage-list">
            {usage.entries.slice(0, SHOWN).map((e) => {
              const pct = usage.total ? (e.size / usage.total) * 100 : 0
              return (
                <div key={e.name} className={`usage-row${e.isDir ? ' dir' : ''}`} data-name={e.name} onClick={() => e.isDir && go(join(style, path, e.name))}>
                  <FileIcon entry={{ name: e.name, isDir: e.isDir }} size={16} />
                  <span className="usage-name ellipsis" title={e.name}>{e.name}</span>
                  <div className="usage-meter"><div style={{ width: `${Math.max(0.5, (e.size / max) * 100)}%` }} /></div>
                  <span className="usage-size">{formatSize(e.size)}</span>
                  <span className="usage-pct faint">{pct >= 0.1 ? `${pct.toFixed(pct >= 10 ? 0 : 1)} %` : '< 0.1 %'}</span>
                  <div className="usage-actions" onClick={(ev) => ev.stopPropagation()}>
                    <button className="icon-btn sm" data-tip={t('ctx.copyPath')} onClick={() => copyToClipboard(join(style, path, e.name))}><Copy /></button>
                    <button className="icon-btn sm" data-tip={t('ctx.delete')} onClick={() => remove(e)}><Trash2 /></button>
                  </div>
                </div>
              )
            })}
            {rest.length > 0 && <div className="usage-row faint">{t('usage.more', { n: rest.length, size: formatSize(restSize) })}</div>}
            {!usage.entries.length && <p className="muted" style={{ margin: 16 }}>{t('usage.empty')}</p>}
          </div>
        </>
      )}
    </div>
  )
}
