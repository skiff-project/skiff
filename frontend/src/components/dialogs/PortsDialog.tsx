import { useEffect, useMemo, useState } from 'react'
import { Copy, EthernetPort, ExternalLink, Globe, Lock, Network, RefreshCw, Search } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'
import type { TunnelInfo } from '../../lib/types'

export interface PortInfo { port: number; addresses: string[]; scope: 'public' | 'local' | 'interface'; process?: string; pid?: number; service?: string }

/** The TCP ports the server behind an SSH connection listens on. */
export function openPorts(conn: string) {
  return openDialog((close) => <Ports conn={conn} close={close} />)
}

const HTTPS = new Set([443, 8443, 9443])
// known services that are not web pages: a tunnel, but no browser
const NOT_WEB = new Set([21, 22, 25, 53, 110, 143, 465, 587, 993, 995, 1433, 2375, 2376, 3306, 5432, 5672, 6379, 9000, 11211, 27017])
const NOT_WEB_PROGRAM = /ssh|mysql|mariadb|postgres|redis|mongo|memcache|exim|postfix|dovecot|named|dnsmasq|resolve|php-fpm|rpcbind|smbd/i
const isWeb = (p: PortInfo) => !NOT_WEB.has(p.port) && !NOT_WEB_PROGRAM.test(p.process || '')

/** The address to reach a port from the server itself. */
function destOf(p: PortInfo) {
  const a = p.addresses.find((x) => x !== '0.0.0.0' && x !== '::' && x !== '*')
  if (!a) return '127.0.0.1'
  return a
}

/**
 * A local tunnel to a port of the server (an existing one is reused) and
 * its local address.
 */
export async function tunnelTo(conn: string, p: PortInfo): Promise<string> {
  const dest = destOf(p)
  const tunnels = await call<TunnelInfo[]>('tunnel.list')
  const destKey = (h: string) => (h.includes(':') ? `[${h}]:${p.port}` : `${h}:${p.port}`)
  const same = tunnels.find((x) => x.conn === conn && x.type === 'local' && x.running && (x.dest === destKey(dest) || (dest === '127.0.0.1' && x.dest === `localhost:${p.port}`)))
  if (same) return same.bind
  const info = await call<TunnelInfo>('tunnel.start', {
    conn,
    forward: { id: '', type: 'local', bindHost: '127.0.0.1', bindPort: 0, destHost: dest, destPort: p.port, label: `${p.process || p.service || t('ports.port')} ${p.port}` },
  })
  return info.bind
}

/** Opens a web page served on a port of the server, through a tunnel. */
export async function openInBrowser(conn: string, p: PortInfo) {
  try {
    const bind = await tunnelTo(conn, p)
    const port = bind.slice(bind.lastIndexOf(':') + 1)
    const url = `${HTTPS.has(p.port) ? 'https' : 'http'}://127.0.0.1:${port}/`
    await call('app.openURL', { url })
    toast('ok', t('ports.opened', { port: p.port }), url)
  } catch (e) {
    errorToast(t('ports.open'), e)
  }
}

function Ports({ conn, close }: { conn: string; close: () => void }) {
  const info = useStore((s) => s.conns[conn])
  const web = useStore((s) => s.info?.mode === 'web')
  const [list, setList] = useState<PortInfo[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [opening, setOpening] = useState(0)

  const load = async () => {
    setBusy(true)
    try {
      setList(await call<PortInfo[]>('ports.list', { conn }))
      setError('')
    } catch (e: any) {
      setError(e?.message || String(e))
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => { load() }, [])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (list || []).filter((p) => !needle || String(p.port).includes(needle) || p.process?.toLowerCase().includes(needle) || p.service?.toLowerCase().includes(needle) || p.addresses.some((a) => a.includes(needle)))
  }, [list, q])
  const noProcess = !!list?.length && list.every((p) => !p.process)

  const open = async (p: PortInfo) => {
    setOpening(p.port)
    await openInBrowser(conn, p)
    setOpening(0)
  }
  const tunnel = async (p: PortInfo) => {
    try {
      const bind = await tunnelTo(conn, p)
      copyToClipboard(bind)
      toast('ok', t('ports.tunnelled', { port: p.port }), t('ports.tunnelledHint', { bind }))
    } catch (e) {
      errorToast(t('ports.tunnel'), e)
    }
  }

  return (
    <Dialog
      size="lg"
      title={t('ports.title', { host: info?.title || conn })}
      icon={<EthernetPort />}
      onClose={close}
      footer={<><span className="faint">{web ? t('ports.webMode') : t('ports.hint')}</span><div className="spacer" /><button className="btn" onClick={close}>{t('common.close')}</button></>}
    >
      <div className="form ports-dialog">
        <div className="ctr-bar">
          <div className="input-group grow">
            <Search />
            <input className="input" value={q} placeholder={t('ports.search')} onChange={(e) => setQ(e.target.value)} data-autofocus />
          </div>
          <button className="icon-btn" data-tip={t('common.refresh')} disabled={busy} onClick={load}><RefreshCw className={busy ? 'spin' : ''} /></button>
        </div>
        {error && <div className="alert danger"><EthernetPort /><span className="selectable">{error}</span></div>}
        {!list && !error && <div className="ctr-empty"><span className="spinner" /></div>}
        {list && (
          <div className="ports-list">
            {shown.map((p) => (
              <div key={p.port + '/' + (p.process || '')} className="ports-row" data-port={p.port}>
                <div className="ports-port mono">{p.port}</div>
                <div className="ports-main">
                  <div className="strong ellipsis">{p.process || <span className="faint">{t('ports.unknownProcess')}</span>}{p.service && <span className="badge ports-svc">{p.service}</span>}</div>
                  <div className="faint mono ellipsis">{p.addresses.join(', ')}{p.pid ? ` · PID ${p.pid}` : ''}</div>
                </div>
                <span className={`ports-scope ${p.scope}`} data-tip={t(`ports.scopeTip.${p.scope}`)}>
                  {p.scope === 'public' ? <Globe /> : p.scope === 'local' ? <Lock /> : <Network />} {t(`ports.scope.${p.scope}`)}
                </span>
                <div className="ctr-actions">
                  {!web && isWeb(p) && (
                    <button className="btn sm primary" disabled={opening === p.port} onClick={() => open(p)} data-tip={t('ports.openTip')}>
                      {opening === p.port ? <span className="spinner sm" /> : <ExternalLink />} {t('ports.open')}
                    </button>
                  )}
                  <button className="icon-btn sm" data-tip={t('ports.tunnel')} onClick={() => tunnel(p)}><Network /></button>
                  <button className="icon-btn sm" data-tip={t('ports.copy')} onClick={() => copyToClipboard(`${destOf(p)}:${p.port}`)}><Copy /></button>
                </div>
              </div>
            ))}
            {!shown.length && <p className="muted" style={{ margin: 12 }}>{q ? t('ports.noMatch', { q }) : t('ports.none')}</p>}
          </div>
        )}
        {noProcess && <p className="hint" style={{ margin: 0 }}>{t('ports.noProcessHint')}</p>}
      </div>
    </Dialog>
  )
}

