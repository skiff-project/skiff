import { useEffect, useMemo, useRef, useState } from 'react'
import { Copy, Search } from 'lucide-react'
import { get, set, useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'
import type { LogLine } from '../../lib/types'
import { copyToClipboard } from '../../lib/fileops'

const EMPTY: LogLine[] = []

export function LogPanel() {
  const conns = useStore((s) => s.conns)
  const activeConn = useStore((s) => {
    const tab = s.tabs.find((x) => x.id === s.activeTab)
    return tab && tab.kind === 'session' ? tab.conn : ''
  })
  const [conn, setConn] = useState(activeConn || Object.keys(conns)[0] || '')
  const lines = useStore((s) => s.logs[conn] ?? EMPTY)
  const [q, setQ] = useState('')
  const [follow, setFollow] = useState(true)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (activeConn) setConn(activeConn)
  }, [activeConn])

  useEffect(() => {
    if (!conn) return
    if ((get().logs[conn] || []).length) return
    call<LogLine[]>('conn.log', { conn }).then((ls) => set((s) => ({ logs: { ...s.logs, [conn]: ls } }))).catch(() => {})
  }, [conn])

  const shown = useMemo(() => {
    if (!q) return lines
    const lq = q.toLowerCase()
    return lines.filter((l) => l.m.toLowerCase().includes(lq))
  }, [lines, q])

  useEffect(() => {
    if (follow && ref.current) ref.current.scrollTop = ref.current.scrollHeight
  }, [shown.length, follow])

  const ids = Object.keys(conns)
  if (!ids.length) return <div className="empty"><p>{t('status.disconnected')}</p></div>
  return (
    <div className="logp">
      <div className="logp-head">
        <select className="select" style={{ width: 220, height: 26 }} value={conn} onChange={(e) => setConn(e.target.value)}>
          {ids.map((id) => (
            <option key={id} value={id}>{conns[id].title} · {conns[id].protocol.toUpperCase()}</option>
          ))}
        </select>
        <div className="input-group" style={{ width: 220 }}>
          <Search />
          <input className="input sm" placeholder={t('common.search')} value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <label className="check"><input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> {t('log.autoScroll')}</label>
        <div className="spacer" />
        <button className="btn sm ghost" onClick={() => copyToClipboard(shown.map((l) => `${l.t} ${l.l} ${l.m}`).join('\n'))}><Copy /> {t('common.copy')}</button>
      </div>
      <div className="logp-body selectable" ref={ref} onScroll={(e) => {
        const el = e.currentTarget
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30
        if (atBottom !== follow) setFollow(atBottom)
      }}>
        {shown.map((l, i) => (
          <div key={i} className={`log-line ${l.l}`}>
            <span className="log-t">{new Date(l.t).toLocaleTimeString()}</span>
            <span className="log-m">{l.m}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
