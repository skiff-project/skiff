import { ExternalLink, KeyRound, Link2, ShieldAlert } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { toast, useStore } from '../../lib/store'
import { connectSite, connectUrl } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { SiteView } from '../../lib/types'

/** The links Skiff opens (the schemes it registers on the system). */
export const LINK_SCHEMES = ['sftp', 'scp', 'ftp', 'ftps', 'ftpes', 'dav', 'davs']

const DEFAULT_PORT: Record<string, number> = { sftp: 22, scp: 22, ftp: 21, ftpes: 21, ftps: 990, dav: 80, davs: 443 }
const PROTOCOL: Record<string, string> = { sftp: 'sftp', scp: 'scp', ftp: 'ftp', ftps: 'ftp', ftpes: 'ftp', dav: 'webdav', davs: 'webdav' }
const FTPS: Record<string, string> = { ftp: 'none', ftps: 'implicit', ftpes: 'explicit' }

interface Parsed {
  scheme: string
  url: string // without a pinned host key
  host: string
  port: number
  user: string
  path: string
  password: boolean
  fingerprint: boolean
}

/**
 * Reads a link. A host key pinned in it (";fingerprint=", WinSCP style) is
 * dropped: it would skip the check of the host key, and anyone can write a
 * link.
 */
export function parseLink(raw: string): Parsed | null {
  const scheme = raw.slice(0, Math.max(0, raw.indexOf('://'))).toLowerCase()
  if (!LINK_SCHEMES.includes(scheme)) return null
  let rest = raw.slice(scheme.length + 3)
  const at = rest.lastIndexOf('@', rest.indexOf('/') >= 0 ? rest.indexOf('/') : rest.length)
  let fingerprint = false
  if (at >= 0) {
    const userinfo = rest.slice(0, at)
    const kept = userinfo.split(';').filter((p, i) => i === 0 || !/^fingerprint=/i.test(p))
    fingerprint = kept.length !== userinfo.split(';').length
    rest = kept.join(';') + rest.slice(at)
  }
  const url = `${scheme}://${rest}`
  try {
    const u = new URL(url)
    if (!u.hostname) return null
    let path = '/'
    try {
      path = decodeURIComponent(u.pathname || '/')
    } catch {
      path = u.pathname || '/'
    }
    return {
      scheme, url, host: u.hostname.replace(/^\[|\]$/g, ''), port: Number(u.port) || DEFAULT_PORT[scheme],
      user: decodeURIComponent(u.username.split(';')[0] || ''), path, password: !!u.password, fingerprint,
    }
  } catch {
    return null
  }
}

/** The saved site a link names: same protocol, server, port and user. */
function savedSite(sites: SiteView[], p: Parsed) {
  return sites.find((s) => {
    if (s.protocol !== PROTOCOL[p.scheme] || s.host.toLowerCase() !== p.host.toLowerCase()) return false
    if (p.scheme in FTPS && (s.ftps || 'none') !== FTPS[p.scheme]) return false
    if (s.protocol === 'webdav' && !!s.tls !== (p.scheme === 'davs')) return false
    const port = s.port || (s.protocol === 'ftp' && s.ftps === 'implicit' ? 990 : DEFAULT_PORT[p.scheme])
    return port === p.port && (!p.user || (s.user || '') === p.user)
  })
}

/**
 * A link clicked elsewhere (a browser, a mail) asks to open a connection:
 * Skiff shows where to and asks first.
 */
export function openLinkDialog(raw: string) {
  const p = parseLink(raw)
  if (!p) {
    toast('error', t('link.refused'), raw.length > 120 ? raw.slice(0, 120) + '…' : raw)
    return
  }
  return openDialog((close) => <OpenLink link={p} close={close} />)
}

function OpenLink({ link, close }: { link: Parsed; close: () => void }) {
  const sites = useStore((s) => s.sites)
  const site = savedSite(sites, link)
  const go = () => {
    close()
    if (site) connectSite(site.id, link.path !== '/' ? { remote: link.path } : {})
    else connectUrl(link.url)
  }
  const label = t(`link.scheme.${link.scheme}`)
  return (
    <Dialog
      title={t('link.title')}
      icon={<Link2 />}
      onClose={close}
      onSubmit={go}
      footer={<><div className="spacer" /><button className="btn" onClick={close}>{t('common.cancel')}</button><button className="btn primary" onClick={go} data-autofocus><ExternalLink /> {site ? t('link.openSite', { name: site.name || site.host }) : t('link.connect')}</button></>}
    >
      <div className="form open-link">
        <p className="muted" style={{ margin: 0 }}>{t('link.desc')}</p>
        <dl className="kv">
          <dt>{t('link.protocol')}</dt><dd>{label}</dd>
          <dt>{t('link.server')}</dt><dd className="mono selectable">{link.host}:{link.port}</dd>
          {link.user && <><dt>{t('link.user')}</dt><dd className="mono selectable">{link.user}</dd></>}
          {link.path !== '/' && <><dt>{t('link.folder')}</dt><dd className="mono selectable">{link.path}</dd></>}
        </dl>
        {site && <div className="alert info"><KeyRound /><span>{t('link.savedSite', { name: site.name || site.host })}</span></div>}
        {link.password && <div className="alert warn"><KeyRound /><span>{t('link.hasPassword')}</span></div>}
        {link.fingerprint && <div className="alert warn"><ShieldAlert /><span>{t('link.fingerprintIgnored')}</span></div>}
        <p className="hint" style={{ margin: 0 }}>{t('link.warn')}</p>
      </div>
    </Dialog>
  )
}
