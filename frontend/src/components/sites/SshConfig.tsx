import { useEffect, useState } from 'react'
import { AlertTriangle, Copy, FileCog, Plug, Plus, SquareTerminal } from 'lucide-react'
import { call } from '../../lib/api'
import { connectSshHost, displayPath, openTerminal } from '../../lib/actions'
import { t } from '../../lib/i18n'
import { copyToClipboard } from '../../lib/fileops'
import type { Site, SshConfigOption, SshConfigReport, SshHost } from '../../lib/types'
import { showMenu, type MenuItem } from '../ui/Menu'

/** Asks the server how the OpenSSH configuration applies to a site (debounced). */
export function useSshReport(site: Partial<Site> | null, enabled = true): SshConfigReport | null {
  const [report, setReport] = useState<SshConfigReport | null>(null)
  const key = enabled && site?.host ? JSON.stringify(site) : ''
  useEffect(() => {
    if (!key) {
      setReport(null)
      return
    }
    let live = true
    const timer = window.setTimeout(() => {
      call<SshConfigReport>('sshconfig.resolve', { site: JSON.parse(key) })
        .then((r) => live && setReport(r))
        .catch(() => live && setReport(null))
    }, 250)
    return () => {
      live = false
      clearTimeout(timer)
    }
  }, [key])
  return report
}

/** A site for a host of the OpenSSH configuration: everything else comes from the file. */
export function sshHostSite(alias: string): Site {
  return { id: '', name: alias, protocol: 'sftp', host: alias, useAgent: true, kbdInteractive: true, resolveSymlinks: true, keepalive: 30, timeout: 15 }
}

const STATUS_CLASS: Record<SshConfigOption['status'], string> = { applied: 'ok', site: 'accent', ignored: '' }

/** Table of the obtained ssh_config values and what Skiff does with them. */
export function SshConfigOptions({ report }: { report: SshConfigReport }) {
  if (!report.options.length) return <p className="muted" style={{ margin: 0 }}>{t('sshcfg.noOptions', { host: report.alias })}</p>
  return (
    <div className="sshcfg-options">
      {report.options.map((o, i) => (
        <div key={i} className="sshcfg-row" data-status={o.status}>
          <span className="sshcfg-key mono">{o.key}</span>
          <span className="sshcfg-value mono ellipsis" title={o.value}>{o.value}</span>
          <span className={`badge ${STATUS_CLASS[o.status]}`} data-tip={t(`sshcfg.statusTip.${o.status}`)}>{t(`sshcfg.status.${o.status}`)}</span>
          <span className="sshcfg-source faint mono ellipsis" title={o.source}>{o.source}</span>
        </div>
      ))}
    </div>
  )
}

/** What the connection will actually use. */
export function SshEffective({ report }: { report: SshConfigReport }) {
  return (
    <dl className="kv sshcfg-effective">
      <dt>{t('site.host')}</dt>
      <dd className="mono">{report.host}:{report.port}</dd>
      <dt>{t('site.user')}</dt>
      <dd className="mono">{report.user}</dd>
      <dt>{t('sshcfg.keys')}</dt>
      <dd className="mono">{report.keyFiles?.length ? report.keyFiles.map((k) => displayPath('local', k)).join(', ') : t('common.none')}{report.agent ? ` · ${t('sshcfg.agent')}` : ''}</dd>
      {!!report.jumps?.length && (
        <>
          <dt>{t('site.tunnel')}</dt>
          <dd className="mono">{report.jumps.join(' → ')}</dd>
        </>
      )}
      {report.proxyCommand && (
        <>
          <dt>{t('site.proxyLocalCmd')}</dt>
          <dd className="mono">{report.proxyCommand}</dd>
        </>
      )}
      {!!report.forwards?.length && (
        <>
          <dt>{t('site.forwards')}</dt>
          <dd className="mono">{report.forwards.join(' · ')}</dd>
        </>
      )}
    </dl>
  )
}

export function SshWarnings({ report }: { report: SshConfigReport }) {
  if (!report.warnings?.length) return null
  return (
    <div className="alert warn">
      <AlertTriangle />
      <div>{report.warnings.map((w, i) => <div key={i}>{w}</div>)}</div>
    </div>
  )
}

export function SshHostRow({ h, selected, onSelect, onNewSite }: { h: SshHost; selected: boolean; onSelect: () => void; onNewSite: () => void }) {
  const menu = (e: React.MouseEvent) => {
    e.preventDefault()
    onSelect()
    const items: MenuItem[] = [
      { label: t('common.connect'), icon: <Plug />, shortcut: 'Enter', onClick: () => connectSshHost(h.alias) },
      { label: t('sites.openTerminal'), icon: <SquareTerminal />, onClick: () => connectSshHost(h.alias, { afterConnect: (c) => openTerminal(c.id) }) },
      { separator: true },
      { label: t('sshcfg.newSite'), icon: <Plus />, onClick: onNewSite },
      { label: t('sshcfg.copyAlias'), icon: <Copy />, onClick: () => copyToClipboard(h.alias) },
    ]
    showMenu(e.clientX, e.clientY, items)
  }
  return (
    <div className={`tree-site${selected ? ' sel' : ''}`} style={{ paddingLeft: 8 }} onClick={onSelect} onDoubleClick={() => connectSshHost(h.alias)} onContextMenu={menu} data-ssh-host={h.alias}>
      <FileCog className="faint" style={{ width: 15, flex: 'none', marginLeft: 1 }} />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="ellipsis site-name">{h.alias}</div>
        <div className="ellipsis site-sub">{h.user ? `${h.user}@` : ''}{h.hostName}{h.port !== 22 ? `:${h.port}` : ''}</div>
      </div>
      <span className="proto-badge">SSH</span>
    </div>
  )
}

/** Main panel of a selected host of the OpenSSH configuration. */
export function SshHostPanel({ h, onNewSite }: { h: SshHost; onNewSite: () => void }) {
  const report = useSshReport(sshHostSite(h.alias))
  return (
    <div className="site-editor sshcfg-panel">
      <div className="se-head">
        <div className="se-title">
          <FileCog style={{ width: 20, color: 'var(--accent-text)' }} />
          <span className="se-name static">{h.alias}</span>
          <span className="badge accent">OpenSSH</span>
        </div>
        <div className="row">
          <button className="btn" onClick={() => connectSshHost(h.alias, { afterConnect: (c) => openTerminal(c.id) })}><SquareTerminal /> {t('sites.openTerminal')}</button>
          <button className="btn" onClick={onNewSite}><Plus /> {t('sshcfg.newSite')}</button>
          <button className="btn primary" onClick={() => connectSshHost(h.alias)}><Plug /> {t('common.connect')}</button>
        </div>
      </div>
      <div className="sshcfg-body">
        <p className="muted" style={{ margin: 0 }}>{t('sshcfg.hostHint', { source: h.source })}</p>
        {report && (
          <>
            <section className="card sshcfg-card">
              <h4>{t('sshcfg.effective')}</h4>
              <SshEffective report={report} />
            </section>
            <SshWarnings report={report} />
            <section className="card sshcfg-card">
              <h4>{t('sshcfg.detected')}</h4>
              <SshConfigOptions report={report} />
            </section>
          </>
        )}
      </div>
    </div>
  )
}
