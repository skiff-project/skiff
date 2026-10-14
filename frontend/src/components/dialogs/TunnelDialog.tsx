import { useShallow } from 'zustand/react/shallow'
import { useState } from 'react'
import { Network } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, set, toast, useStore } from '../../lib/store'
import { setBottom } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { TunnelInfo } from '../../lib/types'

export function openTunnelDialog(conn: string) {
  openDialog((close) => <TunnelDialog conn={conn} close={close} />)
}

const PRESETS = [
  { label: 'PostgreSQL', port: 5432 },
  { label: 'MySQL / MariaDB', port: 3306 },
  { label: 'Redis', port: 6379 },
  { label: 'MongoDB', port: 27017 },
  { label: 'HTTP', port: 80 },
  { label: 'HTTPS', port: 443 },
  { label: 'RDP', port: 3389 },
  { label: 'VNC', port: 5900 },
]

function TunnelDialog({ conn: initial, close }: { conn: string; close: () => void }) {
  const conns = useStore(useShallow((s) => Object.values(s.conns).filter((c) => c.caps.tunnels && c.state === 'connected')))
  const [conn, setConn] = useState(initial)
  const [type, setType] = useState<'local' | 'remote' | 'dynamic'>('local')
  const [bindHost, setBindHost] = useState('127.0.0.1')
  const [bindPort, setBindPort] = useState(15432)
  const [destHost, setDestHost] = useState('localhost')
  const [destPort, setDestPort] = useState(5432)
  const [label, setLabel] = useState('')

  const start = async () => {
    try {
      const info = await call<TunnelInfo>('tunnel.start', { conn, forward: { id: '', type, bindHost, bindPort, destHost, destPort, label } })
      set((s) => ({ tunnels: { ...s.tunnels, [info.id]: info } }))
      toast('ok', t('tunnel.started', { bind: info.bind }))
      setBottom(true, 'tunnels')
      close()
    } catch (e) {
      errorToast(t('tunnel.title'), e)
    }
  }

  return (
    <Dialog
      size="md"
      title={t('tunnel.title')}
      icon={<Network />}
      onClose={close}
      onSubmit={start}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={close}>{t('common.cancel')}</button>
          <button className="btn primary" onClick={start}>{t('common.start')}</button>
        </>
      }
    >
      <div className="form">
        <div className="field">
          <label>SSH</label>
          <select className="select" value={conn} onChange={(e) => setConn(e.target.value)}>
            {conns.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}
          </select>
        </div>
        <div className="segmented">
          <button className={type === 'local' ? 'on' : ''} onClick={() => setType('local')}>{t('tunnel.local')}</button>
          <button className={type === 'remote' ? 'on' : ''} onClick={() => setType('remote')}>{t('tunnel.remote')}</button>
          <button className={type === 'dynamic' ? 'on' : ''} onClick={() => setType('dynamic')}>{t('tunnel.dynamic')}</button>
        </div>
        <p className="muted" style={{ margin: 0, fontSize: 12 }}>
          {type === 'local' && 'Connections to the local port are forwarded through the server to the destination (ssh -L).'}
          {type === 'remote' && 'Connections to the port on the server are forwarded back to a destination reachable from this computer (ssh -R).'}
          {type === 'dynamic' && 'A local SOCKS4/5 proxy that routes any connection through the server (ssh -D).'}
        </p>
        <div className="field-row r-port">
          <div className="field"><label>{t('tunnel.bind')}</label><input className="input mono" value={bindHost} onChange={(e) => setBindHost(e.target.value)} /></div>
          <div className="field"><label>{t('site.port')}</label><input className="input tnum" type="number" value={bindPort} onChange={(e) => setBindPort(parseInt(e.target.value, 10) || 0)} /></div>
        </div>
        {type !== 'dynamic' && (
          <>
            <div className="field-row r-port">
              <div className="field"><label>{t('tunnel.dest')}</label><input className="input mono" value={destHost} onChange={(e) => setDestHost(e.target.value)} /></div>
              <div className="field"><label>{t('site.port')}</label><input className="input tnum" type="number" value={destPort} onChange={(e) => setDestPort(parseInt(e.target.value, 10) || 0)} /></div>
            </div>
            <div className="chips">
              {PRESETS.map((p) => (
                <button key={p.label} className="chip" onClick={() => { setDestPort(p.port); setBindPort(p.port < 1024 ? 10000 + p.port : 10000 + (p.port % 10000)); setLabel(p.label) }}>{p.label}</button>
              ))}
            </div>
          </>
        )}
        <div className="field"><label>Label</label><input className="input" value={label} onChange={(e) => setLabel(e.target.value)} /></div>
      </div>
    </Dialog>
  )
}
