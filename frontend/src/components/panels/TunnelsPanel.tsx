import { useShallow } from 'zustand/react/shallow'
import { ArrowRight, Copy, Network, Plus, Square } from 'lucide-react'
import { activeSession, useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { formatSize, formatDate } from '../../lib/format'
import { t } from '../../lib/i18n'
import { openTunnelDialog } from '../dialogs/TunnelDialog'
import { copyToClipboard } from '../../lib/fileops'

export function TunnelsPanel() {
  const tunnels = useStore(useShallow((s) => Object.values(s.tunnels)))
  const sshConns = useStore(useShallow((s) => Object.values(s.conns).filter((c) => c.caps.tunnels && c.state === 'connected')))
  const add = () => {
    const s = activeSession()
    const conn = s && sshConns.find((c) => c.id === s.conn) ? s.conn : sshConns[0]?.id
    if (conn) openTunnelDialog(conn)
  }
  if (!tunnels.length) {
    return (
      <div className="empty">
        <Network className="big" />
        <h4>{t('tunnel.none')}</h4>
        <p>{t('tunnel.noneHint')}</p>
        <button className="btn primary sm" disabled={!sshConns.length} onClick={add}><Plus /> {t('tunnel.new')}</button>
      </div>
    )
  }
  return (
    <div className="tunnelsp">
      <div className="queue-head">
        <span className="muted">{tunnels.length} tunnel(s)</span>
        <div className="spacer" />
        <button className="btn sm" disabled={!sshConns.length} onClick={add}><Plus /> {t('tunnel.new')}</button>
      </div>
      <table className="table">
        <thead>
          <tr>
            <th>{t('common.type')}</th>
            <th>{t('tunnel.bind')}</th>
            <th />
            <th>{t('tunnel.dest')}</th>
            <th>{t('common.connect')}</th>
            <th>Traffic</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {tunnels.map((tu) => (
            <tr key={tu.id}>
              <td><span className="badge accent">{tu.type === 'local' ? 'L' : tu.type === 'remote' ? 'R' : 'SOCKS'}</span> {tu.label}</td>
              <td className="mono">{tu.bind} <button className="icon-btn sm" onClick={() => copyToClipboard(tu.bind)}><Copy /></button></td>
              <td><ArrowRight className="faint" style={{ width: 14 }} /></td>
              <td className="mono">{tu.dest || (tu.type === 'dynamic' ? 'SOCKS4/5 proxy' : '')}</td>
              <td className="muted">{tu.connTitle} · {t('tunnel.connections', { n: tu.active })} <span className="faint">({tu.total})</span></td>
              <td className="tnum muted">↓ {formatSize(tu.bytesIn)} · ↑ {formatSize(tu.bytesOut)}</td>
              <td className="faint" title={formatDate(tu.started, 'iso')}>
                <button className="btn sm" onClick={() => call('tunnel.stop', { id: tu.id })}><Square /> {t('common.stop')}</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
