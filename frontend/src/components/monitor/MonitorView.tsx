import { useEffect, useMemo, useRef, useState } from 'react'
import { Activity, ArrowDown, ArrowUp, Cpu, HardDrive, MemoryStick, Pause, Play, Search, Skull, Square } from 'lucide-react'
import { call } from '../../lib/api'
import { errorToast, toast, useStore, type MonitorTab } from '../../lib/store'
import { t } from '../../lib/i18n'
import { formatDuration, formatSize, formatSpeed } from '../../lib/format'
import type { MonitorProc, MonitorSample } from '../../lib/types'
import { ask } from '../dialogs/common'
import { openUsage } from '../../lib/actions'

const HISTORY = 60

function Sparkline({ values, max, className }: { values: number[]; max?: number; className?: string }) {
  const w = 160
  const h = 36
  const top = Math.max(max ?? 0, ...values, 1e-9)
  // the newest value on the right
  const x = (i: number) => ((HISTORY - values.length + i) / (HISTORY - 1)) * w
  const pts = values.map((v, i) => `${x(i)},${h - (Math.max(0, v) / top) * (h - 2) - 1}`)
  return (
    <svg className={`sparkline ${className || ''}`} viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden>
      {pts.length > 1 && <polygon points={`${x(0)},${h} ${pts.join(' ')} ${w},${h}`} />}
      {pts.length > 1 && <polyline points={pts.join(' ')} fill="none" vectorEffect="non-scaling-stroke" />}
    </svg>
  )
}

function Bar({ used, total }: { used: number; total: number }) {
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0
  return (
    <div className={`progress mon-bar${pct > 90 ? ' danger' : pct > 75 ? ' warn' : ''}`}>
      <div style={{ width: `${pct}%` }} />
    </div>
  )
}

type SortKey = 'cpu' | 'mem' | 'pid' | 'name' | 'user'

export function MonitorView({ tab, visible }: { tab: MonitorTab; visible: boolean }) {
  const conn = useStore((s) => s.conns[tab.conn])
  const fmt = useStore((s) => s.prefs?.sizeFormat ?? 'iec')
  const [sample, setSample] = useState<MonitorSample | null>(null)
  const [error, setError] = useState('')
  const [paused, setPaused] = useState(false)
  const [interval, setIntervalSec] = useState(2)
  const [q, setQ] = useState('')
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'cpu', dir: -1 })
  const hist = useRef({ cpu: [] as number[], mem: [] as number[], rx: [] as number[], tx: [] as number[] })
  const connected = conn?.state === 'connected'

  useEffect(() => {
    if (!visible || paused || !connected) return
    let live = true
    let timer = 0
    const tick = async () => {
      try {
        const s = await call<MonitorSample>('monitor.sample', { conn: tab.conn, key: tab.id })
        if (!live) return
        const h = hist.current
        const push = (a: number[], v: number) => {
          if (v < 0) return
          a.push(v)
          if (a.length > HISTORY) a.shift()
        }
        push(h.cpu, s.cpu)
        push(h.mem, s.memTotal ? (s.memUsed / s.memTotal) * 100 : 0)
        push(h.rx, s.netRx)
        push(h.tx, s.netTx)
        setSample(s)
        setError('')
      } catch (e: any) {
        if (live) setError(e?.message || String(e))
      }
      if (live) timer = window.setTimeout(tick, interval * 1000)
    }
    tick()
    return () => {
      live = false
      clearTimeout(timer)
    }
  }, [visible, paused, connected, interval, tab.conn, tab.id])

  const procs = useMemo(() => {
    let list = sample?.procs || []
    if (q) {
      const lq = q.toLowerCase()
      list = list.filter((p) => p.name.toLowerCase().includes(lq) || p.user.toLowerCase().includes(lq) || String(p.pid).includes(lq))
    }
    const k = sort.key
    return [...list].sort((a, b) => {
      const va = a[k] as any
      const vb = b[k] as any
      return (typeof va === 'string' ? va.localeCompare(vb) : va - vb) * sort.dir
    })
  }, [sample, q, sort])

  const kill = async (p: MonitorProc, signal: 'TERM' | 'KILL') => {
    const ok = await ask({
      title: signal === 'KILL' ? t('mon.forceKill') : t('mon.kill'),
      message: t('mon.killConfirm', { name: p.name, pid: p.pid, signal }),
      danger: true,
      ok: signal === 'KILL' ? t('mon.forceKill') : t('mon.kill'),
    })
    if (!ok) return
    try {
      await call('monitor.kill', { conn: tab.conn, pid: p.pid, signal })
      toast('ok', t('mon.killed', { name: p.name, pid: p.pid }))
    } catch (e) {
      errorToast(t('mon.kill'), e)
    }
  }

  const th = (key: SortKey, label: string, num = false) => (
    <th className={num ? 'num' : ''} onClick={() => setSort((s) => ({ key, dir: s.key === key ? (-s.dir as 1 | -1) : key === 'name' || key === 'user' ? 1 : -1 }))}>
      {label}{sort.key === key ? (sort.dir < 0 ? ' ↓' : ' ↑') : ''}
    </th>
  )

  const h = hist.current
  return (
    <div className="monitor">
      <div className="mon-head">
        <Activity />
        <div className="grow" style={{ minWidth: 0 }}>
          <div className="strong ellipsis">{sample?.hostname || conn?.title || tab.title}</div>
          <div className="faint ellipsis">
            {sample ? `${sample.kernel} · ${t('mon.cpus', { n: sample.cpus })} · ${t('mon.uptime', { d: formatDuration(sample.uptime) })} · ${t('mon.procs', { n: sample.procCount })}` : t('mon.waiting')}
          </div>
        </div>
        <select className="select" style={{ width: 150 }} value={interval} onChange={(e) => setIntervalSec(parseInt(e.target.value, 10))} data-tip={t('mon.interval')}>
          {[1, 2, 5, 10].map((v) => <option key={v} value={v}>{t('mon.every', { n: v })}</option>)}
        </select>
        <button className="btn" onClick={() => setPaused(!paused)}>{paused ? <><Play /> {t('mon.resume')}</> : <><Pause /> {t('mon.pause')}</>}</button>
      </div>
      {!connected && <div className="alert warn"><Activity /><span>{t('mon.disconnected')}</span></div>}
      {error && <div className="alert danger"><Activity /><span>{error}</span></div>}
      {sample && (
        <>
          <div className="mon-cards">
            <div className="card mon-card" data-metric="cpu">
              <div className="mon-card-head"><Cpu /> {t('mon.cpu')}</div>
              <div className="mon-big tnum">{sample.cpu < 0 ? '…' : `${sample.cpu.toFixed(1)} %`}</div>
              <Sparkline values={h.cpu} max={100} className="accent" />
            </div>
            <div className="card mon-card" data-metric="mem">
              <div className="mon-card-head"><MemoryStick /> {t('mon.memory')}</div>
              <div className="mon-big tnum">{formatSize(sample.memUsed, fmt)} <span className="faint">/ {formatSize(sample.memTotal, fmt)}</span></div>
              <Bar used={sample.memUsed} total={sample.memTotal} />
              {sample.swapTotal > 0 && <div className="faint tnum mon-sub">{t('mon.swap')} {formatSize(sample.swapUsed, fmt)} / {formatSize(sample.swapTotal, fmt)}</div>}
              <Sparkline values={h.mem} max={100} className="ok" />
            </div>
            <div className="card mon-card" data-metric="load">
              <div className="mon-card-head"><Activity /> {t('mon.load')}</div>
              <div className="mon-big tnum">{sample.load[0].toFixed(2)}</div>
              <div className="faint tnum mon-sub">{t('mon.loadDetail', { five: sample.load[1].toFixed(2), fifteen: sample.load[2].toFixed(2) })}</div>
            </div>
            <div className="card mon-card" data-metric="net">
              <div className="mon-card-head"><ArrowDown /><ArrowUp /> {t('mon.network')}</div>
              <div className="mon-big tnum">
                <ArrowDown className="mon-ic" />{sample.netRx < 0 ? '…' : formatSpeed(sample.netRx)} <ArrowUp className="mon-ic" />{sample.netTx < 0 ? '…' : formatSpeed(sample.netTx)}
              </div>
              <Sparkline values={h.rx} className="accent" />
            </div>
          </div>
          <div className="card mon-section">
            <h4><HardDrive /> {t('mon.disks')}</h4>
            {sample.disks.map((d) => (
              <div key={d.mount} className="mon-disk">
                <span className="mono ellipsis" title={d.fs}>{d.mount}</span>
                <Bar used={d.used} total={d.total} />
                <span className="tnum faint">{formatSize(d.used, fmt)} / {formatSize(d.total, fmt)} · {Math.round((d.used / d.total) * 100)} %</span>
                <button className="icon-btn sm" data-tip={t('usage.analyseDisk')} onClick={() => openUsage(tab.conn, d.mount)}><Search /></button>
              </div>
            ))}
          </div>
          <div className="card mon-section mon-procs">
            <div className="row">
              <h4 className="grow">{t('mon.processes')}</h4>
              <div className="input-group" style={{ width: 220 }}><Search /><input className="input" value={q} placeholder={t('mon.filter')} onChange={(e) => setQ(e.target.value)} /></div>
            </div>
            <table className="table mon-table">
              <thead>
                <tr>
                  {th('pid', 'PID', true)}
                  {th('user', t('mon.user'))}
                  {th('name', t('mon.command'))}
                  {th('cpu', 'CPU %', true)}
                  {th('mem', t('mon.mem'), true)}
                  <th />
                </tr>
              </thead>
              <tbody>
                {procs.map((p) => (
                  <tr key={p.pid} data-pid={p.pid}>
                    <td className="num tnum faint">{p.pid}</td>
                    <td className="ellipsis">{p.user}</td>
                    <td className="mono ellipsis" title={`${p.name} · ${p.state} · ${t('mon.threads', { n: p.threads })}`}>{p.name}</td>
                    <td className="num tnum">{p.cpu.toFixed(1)}</td>
                    <td className="num tnum">{formatSize(p.mem, fmt)}</td>
                    <td className="mon-actions">
                      <button className="icon-btn sm" data-tip={t('mon.kill')} onClick={() => kill(p, 'TERM')}><Square /></button>
                      <button className="icon-btn sm" data-tip={t('mon.forceKill')} onClick={() => kill(p, 'KILL')}><Skull /></button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <span className="hint">{t('mon.procsHint', { n: sample.procs.length, total: sample.procCount })}</span>
          </div>
        </>
      )}
    </div>
  )
}
