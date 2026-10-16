import { useEffect, useMemo, useState } from 'react'
import { Box, Copy, FolderOpen, MoreHorizontal, Play, RefreshCw, RotateCw, Save, Search, Square, SquareTerminal } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { showMenuAt, type MenuItem } from '../ui/Menu'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, useStore } from '../../lib/store'
import { connectAdhoc, editNewSite, openTerminal } from '../../lib/actions'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import type { ContainerInfo, Site } from '../../lib/types'

export type Runtime = 'docker' | 'podman' | 'kubectl'

export const RUNTIME_LABEL: Record<string, string> = { docker: 'Docker', podman: 'Podman', kubectl: 'Kubernetes' }
const RUNTIMES: Runtime[] = ['docker', 'podman', 'kubectl']

/** A site for a container (Host is its name); via is an open SSH connection. */
export function containerSite(c: ContainerInfo, runtime: Runtime, via?: string, extra: Partial<Site> = {}): Site {
  return {
    id: '', name: c.name, protocol: 'container', host: c.name, containerRuntime: runtime, viaConn: via || undefined,
    k8sNamespace: c.namespace || undefined, k8sContainer: c.containers?.[0] || undefined,
    useAgent: false, kbdInteractive: false, resolveSymlinks: true, keepalive: 0, timeout: 15, ...extra,
  }
}

type K8s = { context: string; namespace: string }

/**
 * Lists the containers of this computer, or of the server behind an open
 * SSH connection (via). With onPick, choosing a container returns it.
 */
export function openContainers(opts: { via?: string; runtime?: Runtime; onPick?: (c: ContainerInfo, runtime: Runtime, k8s: K8s) => void; onNewSite?: (s: Site) => void } = {}) {
  return openDialog((close) => <Containers {...opts} close={close} />)
}

/** "5 hours", "About a minute"… of Docker, in the language of the interface. */
function duration(d: string) {
  const m = d.trim().match(/^(\d+|an?|about an?|less than an?)\s+(second|minute|hour|day|week|month|year)s?$/i)
  if (!m) return d
  const q = m[1].toLowerCase()
  const n = /^\d+$/.test(q) ? parseInt(q, 10) : 1
  const unit = t(`ctr.u.${m[2].toLowerCase()}${n > 1 ? 's' : ''}`)
  if (q.startsWith('less')) return `< 1 ${unit}`
  if (q.startsWith('about')) return `≈ 1 ${unit}`
  return `${n} ${unit}`
}

/** The status of Docker or Podman, said in the language of the interface. */
export function containerStatus(c: ContainerInfo) {
  const s = (c.status || '').trim()
  let m = s.match(/^Up\s+(.+?)(?:\s+\((healthy|unhealthy|health: starting|Paused)\))?$/i)
  if (m) {
    const extra = m[2] ? ` · ${t(`ctr.health.${m[2].toLowerCase().replace('health: ', '')}`)}` : ''
    return t('ctr.upFor', { d: duration(m[1]) }) + extra
  }
  m = s.match(/^Exited\s+\((-?\d+)\)\s+(.+?)\s+ago$/i)
  if (m) return t('ctr.exitedAgo', { d: duration(m[2]), code: m[1] })
  if (/^Created$/i.test(s)) return ''
  return c.namespace ? '' : s // a pod: its phase is the badge
}

const STATE_CLASS: Record<string, string> = { running: 'ok', paused: 'warn', restarting: 'warn', pending: 'warn', failed: 'danger', dead: 'danger' }

function stateOf(c: ContainerInfo) {
  const st = c.state || (c.running ? 'running' : 'exited')
  const key = `ctr.state.${st}`
  const label = t(key)
  return { label: label === key ? st : label, cls: STATE_CLASS[st] || '' }
}

function Containers({ via, runtime: initial, onPick, onNewSite, close }: { via?: string; runtime?: Runtime; onPick?: (c: ContainerInfo, r: Runtime, k8s: K8s) => void; onNewSite?: (s: Site) => void; close: () => void }) {
  const conn = useStore((s) => (via ? s.conns[via] : undefined))
  const where = via ? conn?.title || via : t('ctr.here')
  const [installed, setInstalled] = useState<Runtime[] | null>(null)
  const [runtime, setRuntime] = useState<Runtime>(initial || 'docker')
  const [ns, setNs] = useState('')
  const [ctx, setCtx] = useState('')
  const [q, setQ] = useState('')
  const [list, setList] = useState<ContainerInfo[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [acting, setActing] = useState('') // a container being started or stopped

  // the runtimes installed there: the first one is shown
  useEffect(() => {
    call<Runtime[]>('containers.runtimes', { via: via || '' }).then((rs) => {
      setInstalled(rs)
      if (!initial || !rs.includes(initial)) {
        if (rs.length) setRuntime(rs[0])
        else setBusy(false)
      }
    }).catch(() => setInstalled([...RUNTIMES]))
  }, [])

  const load = async (r = runtime) => {
    setBusy(true)
    setError('')
    try {
      setList(await call<ContainerInfo[]>('containers.list', { runtime: r, via: via || '', namespace: ns, context: ctx }))
    } catch (e: any) {
      setList(null)
      setError(e?.message || String(e))
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    if (installed && installed.includes(runtime)) load(runtime)
  }, [runtime, installed])

  const site = (c: ContainerInfo) => containerSite(c, runtime, via, runtime === 'kubectl' ? { k8sContext: ctx || undefined } : {})
  const open = (c: ContainerInfo, terminal = false) => {
    close()
    connectAdhoc(site(c), undefined, terminal ? { afterConnect: (ci: { id: string }) => openTerminal(ci.id) } : {})
  }
  const pick = (c: ContainerInfo) => {
    onPick!(c, runtime, { context: ctx, namespace: c.namespace || ns })
    close()
  }
  // a site keeps the container; behind SSH, only for a saved SSH site
  const canSave = !via || !!conn?.siteId
  const saveSite = (c: ContainerInfo) => {
    const st = { ...site(c), viaConn: undefined, viaSite: via ? conn?.siteId : undefined }
    close()
    if (onNewSite) onNewSite(st)
    else editNewSite(st)
  }
  const act = async (c: ContainerInfo, action: 'start' | 'stop' | 'restart') => {
    setActing(c.name)
    try {
      await call('containers.action', { runtime, via: via || '', name: c.name, action })
      await load()
    } catch (e) {
      errorToast(t(`ctr.${action}`), e)
    } finally {
      setActing('')
    }
  }
  const canAct = runtime !== 'kubectl'

  const more = (el: HTMLElement, c: ContainerInfo) => {
    const items: MenuItem[] = [
      { label: t('ctr.files'), icon: <FolderOpen />, disabled: !c.running, onClick: () => open(c) },
      { label: t('term.title'), icon: <SquareTerminal />, disabled: !c.running, onClick: () => open(c, true) },
    ]
    if (canAct) {
      items.push({ separator: true })
      if (c.running) {
        items.push({ label: t('ctr.restart'), icon: <RotateCw />, onClick: () => act(c, 'restart') })
        items.push({ label: t('ctr.stop'), icon: <Square />, onClick: () => act(c, 'stop') })
      } else items.push({ label: t('ctr.start'), icon: <Play />, onClick: () => act(c, 'start') })
    }
    items.push({ separator: true })
    if (canSave) items.push({ label: t('ctr.saveSite'), icon: <Save />, onClick: () => saveSite(c) })
    items.push({ label: t('ctx.copyName'), icon: <Copy />, onClick: () => copyToClipboard(c.name) })
    showMenuAt(el, items, 'right')
  }

  // filtered, running first, grouped by Compose project
  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const shown = (list || []).filter((c) => !needle || [c.name, c.image, c.project, c.namespace, c.ports].some((x) => x?.toLowerCase().includes(needle)))
    shown.sort((a, b) => Number(b.running) - Number(a.running) || a.name.localeCompare(b.name))
    const by = new Map<string, ContainerInfo[]>()
    for (const c of shown) {
      const g = c.project || c.namespace || ''
      by.set(g, [...(by.get(g) || []), c])
    }
    const named = [...by.keys()].filter((g) => g).sort()
    return [...named.map((g) => ({ name: g, items: by.get(g)! })), ...(by.has('') ? [{ name: '', items: by.get('')! }] : [])]
  }, [list, q])
  const total = list?.length || 0
  const running = (list || []).filter((c) => c.running).length
  const grouped = groups.some((g) => g.name)

  const row = (c: ContainerInfo) => {
    const st = stateOf(c)
    const status = containerStatus(c)
    return (
      <div key={(c.namespace || '') + '/' + c.name} className={`ctr-row${c.running ? '' : ' stopped'}`} data-container={c.name}
        onDoubleClick={() => (onPick ? pick(c) : c.running ? open(c) : undefined)}>
        <span className={`ctr-state ${st.cls}`}>{acting === c.name ? <span className="spinner sm" /> : <span className={`dot ${st.cls}`} />}{st.label}</span>
        <div className="ctr-main">
          <div className="ctr-name">{c.name}</div>
          <div className="ctr-meta mono">{c.image}{c.ports ? ` · ${c.ports}` : ''}</div>
        </div>
        <div className="ctr-status" title={c.status}>{status}</div>
        <div className="ctr-actions">
          {onPick ? (
            <button className="btn sm primary" onClick={() => pick(c)}>{t('ctr.choose')}</button>
          ) : c.running ? (
            <>
              <button className="btn sm primary" onClick={() => open(c)}><FolderOpen /> {t('ctr.files')}</button>
              <button className="btn sm" onClick={() => open(c, true)}><SquareTerminal /> {t('term.title')}</button>
            </>
          ) : canAct ? (
            <button className="btn sm" disabled={acting === c.name} onClick={() => act(c, 'start')}>{acting === c.name ? <span className="spinner" /> : <Play />} {t('ctr.start')}</button>
          ) : null}
          {!onPick && <button className="icon-btn sm" data-tip={t('common.more')} onClick={(e) => more(e.currentTarget, c)}><MoreHorizontal /></button>}
        </div>
      </div>
    )
  }

  return (
    <Dialog
      size="xl"
      title={onPick ? t('ctr.pickTitle') : via ? t('ctr.titleOn', { host: where }) : t('ctr.titleLocal')}
      icon={<Box />}
      onClose={close}
      footer={<><span className="faint">{list ? t('ctr.count', { n: total, running }) : t('ctr.hint')}</span><div className="spacer" /><button className="btn" onClick={close}>{t('common.close')}</button></>}
    >
      <div className="form ctr-dialog">
        <div className="ctr-bar">
          <div className="segmented">
            {RUNTIMES.map((r) => {
              const missing = !!installed && !installed.includes(r)
              return (
                <button key={r} className={runtime === r && !missing ? 'on' : ''} disabled={missing} data-tip={missing ? t('ctr.notInstalled', { name: RUNTIME_LABEL[r], where }) : undefined} onClick={() => setRuntime(r)}>
                  {RUNTIME_LABEL[r]}
                </button>
              )
            })}
          </div>
          <div className="input-group grow">
            <Search />
            <input className="input" value={q} placeholder={t('ctr.search')} onChange={(e) => setQ(e.target.value)} data-autofocus />
          </div>
          <button className="icon-btn" disabled={busy || !installed?.includes(runtime)} data-tip={t('common.refresh')} onClick={() => load()}><RefreshCw className={busy ? 'spin' : ''} /></button>
        </div>
        {runtime === 'kubectl' && installed?.includes('kubectl') && (
          <div className="ctr-k8s">
            <div className="field"><label>{t('ctr.context')}</label><input className="input" value={ctx} placeholder={t('ctr.currentContext')} onChange={(e) => setCtx(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} /></div>
            <div className="field"><label>{t('ctr.namespace')}</label><input className="input" value={ns} placeholder={t('ctr.allNamespaces')} onChange={(e) => setNs(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} /></div>
            <button className="btn" onClick={() => load()}>{t('ctr.show')}</button>
          </div>
        )}
        {installed && installed.length === 0 && (
          <div className="ctr-empty">
            <Box />
            <div className="strong">{t('ctr.noRuntime', { where })}</div>
            <div className="faint">{t('ctr.noRuntimeHint')}</div>
          </div>
        )}
        {error && <div className="alert danger"><Box /><div><div>{friendlyError(error, runtime)}</div><div className="mono faint selectable" style={{ fontSize: 11, marginTop: 4 }}>{error}</div></div></div>}
        {!error && list && total === 0 && (
          <div className="ctr-empty">
            <Box />
            <div className="strong">{t('ctr.none')}</div>
            <div className="faint">{t('ctr.noneHint', { name: RUNTIME_LABEL[runtime] })}</div>
          </div>
        )}
        {!list && busy && !error && <div className="ctr-empty"><span className="spinner" /></div>}
        {list && total > 0 && (
          <div className={`ctr-list${onPick ? ' ctr-picking' : ''}`}>
            {groups.length === 0 && <p className="muted" style={{ margin: 8 }}>{t('ctr.noMatch', { q })}</p>}
            {groups.map((g) => (
              <div key={g.name || '-'} className="ctr-group">
                {grouped && <div className="ctr-group-head">{g.name ? (runtime === 'kubectl' ? g.name : t('ctr.project', { name: g.name })) : t('ctr.standalone')} <span className="faint">· {g.items.length}</span></div>}
                {g.items.map(row)}
              </div>
            ))}
          </div>
        )}
      </div>
    </Dialog>
  )
}

/** The usual failures of the runtime, explained. */
function friendlyError(msg: string, runtime: Runtime) {
  const m = msg.toLowerCase()
  if (m.includes('permission denied') && m.includes('docker')) return t('ctr.err.permission')
  if (m.includes('cannot connect to the docker daemon') || m.includes('is the docker daemon running')) return t('ctr.err.daemon')
  if (m.includes('is not installed')) return t('ctr.err.missing', { name: RUNTIME_LABEL[runtime] })
  if (runtime === 'kubectl' && (m.includes('connection refused') || m.includes('unable to connect') || m.includes('no configuration'))) return t('ctr.err.k8s')
  return t('ctr.err.other', { name: RUNTIME_LABEL[runtime] })
}
