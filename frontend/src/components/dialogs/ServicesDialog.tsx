import { useEffect, useMemo, useRef, useState } from 'react'
import { Cog, Play, RefreshCw, RotateCw, ScrollText, Search, Square, Zap } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { openLogs } from '../../lib/actions'
import { t } from '../../lib/i18n'
import { formatDateFull, formatSize } from '../../lib/format'

interface Service { name: string; description?: string; load: string; active: string; sub: string; enabled?: string }
interface Details { props: Record<string, string>; logs: string; logsDenied?: boolean }

/** The systemd services of the server behind an SSH connection. */
export function openServices(conn: string) {
  return openDialog((close) => <Services conn={conn} close={close} />)
}

type Filter = 'all' | 'running' | 'failed' | 'stopped'

const short = (name: string) => name.replace(/\.service$/, '')

/** "Fri 2026-10-16 08:58:41 CEST" of systemd, as the interface writes dates. */
function when(ts: string) {
  const m = ts.match(/(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})/)
  return m ? formatDateFull(`${m[1]}T${m[2]}`) : ts
}

function stateOf(s: Service): { label: string; cls: string } {
  if (s.active === 'failed') return { label: t('svc.state.failed'), cls: 'danger' }
  if (s.active === 'active') return s.sub === 'running' ? { label: t('svc.state.running'), cls: 'ok' } : { label: t('svc.state.active'), cls: 'ok' }
  if (s.active === 'activating' || s.active === 'reloading') return { label: t('svc.state.starting'), cls: 'warn' }
  if (s.active === 'deactivating') return { label: t('svc.state.stopping'), cls: 'warn' }
  return { label: t('svc.state.stopped'), cls: '' }
}

/** The errors of the server, said simply. */
export function serviceError(e: any): string {
  const m = String(e?.message || e)
  if (m.includes('does not use systemd')) return t('svc.noSystemd')
  if (m.includes('only root')) return t('svc.needRoot')
  return m
}

function Services({ conn, close }: { conn: string; close: () => void }) {
  const info = useStore((s) => s.conns[conn])
  const [list, setList] = useState<Service[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [filter, setFilter] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const [sel, setSel] = useState('')
  const [details, setDetails] = useState<Details | null>(null)
  const [acting, setActing] = useState('')
  const logsRef = useRef<HTMLPreElement>(null)
  const selRef = useRef(sel)
  selRef.current = sel

  const load = async () => {
    setBusy(true)
    try {
      const l = await call<Service[]>('services.list', { conn })
      setList(l)
      setError('')
      if (!sel) setSel((l.find((s) => s.active === 'failed') || l.find((s) => s.active === 'active') || l[0])?.name || '')
    } catch (e) {
      setList(null)
      setError(serviceError(e))
    } finally {
      setBusy(false)
    }
  }
  const loadDetails = async (name = sel) => {
    if (!name) return
    try {
      const d = await call<Details>('services.details', { conn, name, lines: 80 })
      if (name === selRef.current) setDetails(d)
    } catch (e) {
      if (name === selRef.current) setDetails({ props: {}, logs: serviceError(e) })
    }
  }
  useEffect(() => { load() }, [])
  useEffect(() => {
    setDetails(null)
    loadDetails(sel)
  }, [sel])
  useEffect(() => {
    if (logsRef.current) logsRef.current.scrollTop = logsRef.current.scrollHeight
  }, [details])

  const counts = useMemo(() => {
    const l = list || []
    return { all: l.length, running: l.filter((s) => s.active === 'active').length, failed: l.filter((s) => s.active === 'failed').length, stopped: l.filter((s) => s.active === 'inactive').length }
  }, [list])
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (list || [])
      .filter((s) => filter === 'all' || (filter === 'running' ? s.active === 'active' : filter === 'failed' ? s.active === 'failed' : s.active === 'inactive'))
      .filter((s) => !needle || s.name.toLowerCase().includes(needle) || s.description?.toLowerCase().includes(needle))
      .sort((a, b) => Number(b.active === 'failed') - Number(a.active === 'failed') || a.name.localeCompare(b.name))
  }, [list, filter, q])
  const cur = list?.find((s) => s.name === sel)

  const act = async (action: string) => {
    if (!cur) return
    setActing(action)
    try {
      await call('services.action', { conn, name: cur.name, action })
      toast('ok', t(`svc.done.${action}`, { name: short(cur.name) }))
      await load()
      await loadDetails(cur.name)
    } catch (e) {
      errorToast(t(`svc.${action}`), serviceError(e))
      loadDetails(cur.name)
    } finally {
      setActing('')
    }
  }
  const btn = (action: string, icon: React.ReactNode, primary = false) => (
    <button className={`btn sm${primary ? ' primary' : ''}`} disabled={!!acting} onClick={() => act(action)} data-action={action}>
      {acting === action ? <span className="spinner sm" /> : icon} {t(`svc.${action}`)}
    </button>
  )
  const p = details?.props || {}
  const enabled = (cur?.enabled || p.UnitFileState || '') === 'enabled'
  const canToggle = ['enabled', 'disabled'].includes(cur?.enabled || p.UnitFileState || '')
  const since = cur?.active === 'active' ? p.ActiveEnterTimestamp : p.InactiveEnterTimestamp
  const mem = Number(p.MemoryCurrent)

  return (
    <Dialog
      size="xl"
      title={t('svc.title', { host: info?.title || conn })}
      icon={<Cog />}
      onClose={close}
      footer={<><span className="faint">{list ? t('svc.count', { n: counts.all, running: counts.running, failed: counts.failed }) : t('svc.hint')}</span><div className="spacer" /><button className="btn" onClick={close}>{t('common.close')}</button></>}
    >
      <div className="form svc-dialog">
        <div className="svc-bar">
          <div className="segmented">
            {(['all', 'running', 'failed', 'stopped'] as Filter[]).map((f) => (
              <button key={f} className={filter === f ? 'on' : ''} onClick={() => setFilter(f)}>
                {t(`svc.filter.${f}`)}{list && <span className={`svc-n${f === 'failed' && counts.failed ? ' danger' : ''}`}>{counts[f]}</span>}
              </button>
            ))}
          </div>
          <div className="input-group grow">
            <Search />
            <input className="input" value={q} placeholder={t('svc.search')} onChange={(e) => setQ(e.target.value)} data-autofocus />
          </div>
          <button className="icon-btn" data-tip={t('common.refresh')} disabled={busy} onClick={() => { load(); loadDetails() }}><RefreshCw className={busy ? 'spin' : ''} /></button>
        </div>
        {error ? (
          <div className="ctr-empty"><Cog /><div className="strong">{error}</div>{error === t('svc.noSystemd') && <div className="faint">{t('svc.noSystemdHint')}</div>}</div>
        ) : !list ? (
          <div className="ctr-empty"><span className="spinner" /></div>
        ) : (
          <div className="svc-main">
            <div className="svc-list">
              {shown.map((s) => {
                const st = stateOf(s)
                return (
                  <div key={s.name} className={`svc-row${s.name === sel ? ' on' : ''}`} data-service={short(s.name)} onClick={() => setSel(s.name)}>
                    <span className={`ctr-state ${st.cls}`}><span className={`dot ${st.cls}`} />{st.label}</span>
                    <div className="svc-names">
                      <div className="svc-name">{short(s.name)}</div>
                      {s.description && <div className="svc-desc">{s.description}</div>}
                    </div>
                    {s.enabled === 'enabled' && <span className="badge" data-tip={t('svc.atBootTip')}><Zap /></span>}
                  </div>
                )
              })}
              {!shown.length && <p className="muted" style={{ margin: 12 }}>{t('svc.none')}</p>}
            </div>
            <div className="svc-details">
              {cur ? (
                <>
                  <div className="svc-head">
                    <div className="grow" style={{ minWidth: 0 }}>
                      <h3 className="ellipsis">{short(cur.name)}</h3>
                      <div className="faint ellipsis">{cur.description || p.Description}</div>
                    </div>
                    <span className={`ctr-state ${stateOf(cur).cls}`}><span className={`dot ${stateOf(cur).cls}`} />{stateOf(cur).label}</span>
                  </div>
                  <div className="svc-actions">
                    {cur.active === 'active' ? (
                      <>
                        {btn('restart', <RotateCw />, true)}
                        {btn('reload', <RefreshCw />)}
                        {btn('stop', <Square />)}
                      </>
                    ) : btn('start', <Play />, true)}
                    <div className="spacer" />
                    <button className="btn sm" onClick={() => { close(); openLogs(conn, { unit: cur.name }) }}><ScrollText /> {t('svc.followLogs')}</button>
                  </div>
                  {canToggle && (
                    <label className="switch svc-boot">
                      <input type="checkbox" checked={enabled} disabled={!!acting} onChange={(e) => act(e.target.checked ? 'enable' : 'disable')} /> {t('svc.atBoot')}
                    </label>
                  )}
                  <dl className="svc-props">
                    {since && <><dt>{cur.active === 'active' ? t('svc.since') : t('svc.stoppedSince')}</dt><dd>{when(since)}</dd></>}
                    {p.MainPID && p.MainPID !== '0' && <><dt>PID</dt><dd className="mono">{p.MainPID}</dd></>}
                    {mem > 0 && mem < 1e15 && <><dt>{t('svc.memory')}</dt><dd>{formatSize(mem)}</dd></>}
                    {p.NRestarts && p.NRestarts !== '0' && <><dt>{t('svc.restarts')}</dt><dd>{p.NRestarts}</dd></>}
                    {p.Result && p.Result !== 'success' && <><dt>{t('svc.result')}</dt><dd className="danger-text">{p.Result}{p.ExecMainStatus && p.ExecMainStatus !== '0' ? ` (${t('svc.exitCode', { code: p.ExecMainStatus })})` : ''}</dd></>}
                    {p.FragmentPath && <><dt>{t('svc.file')}</dt><dd className="mono ellipsis" title={p.FragmentPath}>{p.FragmentPath}</dd></>}
                  </dl>
                  <div className="svc-logs-head">{t('svc.lastLogs')}</div>
                  {details?.logsDenied && <div className="alert warn"><span>{t('svc.logsDenied')}</span></div>}
                  <pre className="svc-logs" ref={logsRef}>{details ? details.logs || t('svc.noLogs') : ''}</pre>
                </>
              ) : <p className="muted">{t('svc.pick')}</p>}
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}
