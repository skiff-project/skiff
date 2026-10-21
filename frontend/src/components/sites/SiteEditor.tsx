import { useMemo, useState } from 'react'
import {
  Cable, Folder, Globe, KeyRound, Network, NotebookPen, Plug, Plus, Save, Server, Settings2, Shield, Trash2, Waypoints, X, Cloud,
  Clock, Copy, FileCog, Box,
} from 'lucide-react'
import { toast, errorToast, useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { connectAdhoc, connectSite, displayPath, reloadSites } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { Forward, Jump, Protocol, SecretInput, Site, SiteView } from '../../lib/types'
import { ask } from '../dialogs/common'
import { copyToClipboard } from '../../lib/fileops'
import { openContainers, RUNTIME_LABEL } from '../dialogs/ContainersDialog'
import { SshConfigOptions, SshEffective, SshWarnings, useSshReport } from './SshConfig'
import { KeyFileInput } from './KeyFileInput'

export function newSiteDraft(): Site {
  return {
    id: '', name: '', protocol: 'sftp', host: '', useAgent: true, kbdInteractive: true, resolveSymlinks: true, keepalive: 30, timeout: 15, ftps: 'none',
  }
}

const COLORS = ['', '#ef4444', '#f97316', '#eab308', '#22c55e', '#14b8a6', '#3b82f6', '#8b5cf6', '#ec4899', '#64748b']

type Section = 'general' | 'dirs' | 'env' | 'conn' | 'proxy' | 'jumps' | 'forwards' | 'ssh' | 'ftp' | 'tls' | 's3' | 'notes'

function defaultPort(s: Site) {
  switch (s.protocol) {
    case 'sftp':
    case 'scp':
      return 22
    case 'ftp':
      return s.ftps === 'implicit' ? 990 : 21
    default:
      return s.tls ? 443 : 80
  }
}

export function SiteEditor({ site, isNew, onSaved, onDeleted, onCancel }: { site: Site | SiteView; isNew: boolean; onSaved: (s: SiteView) => void; onDeleted: () => void; onCancel: () => void }) {
  const [s, setS] = useState<Site>(() => ({ ...newSiteDraft(), ...site }))
  const [secrets, setSecrets] = useState<SecretInput>({})
  const [section, setSection] = useState<Section>('general')
  const [dirty, setDirty] = useState(isNew)
  const [busy, setBusy] = useState(false)
  const folders = useStore((st) => st.folders)
  const sites = useStore((st) => st.sites)
  const view = site as SiteView
  const ssh = s.protocol === 'sftp' || s.protocol === 'scp'
  const isCtr = s.protocol === 'container'
  // Choose…: the containers of this computer, or of the SSH site, connected
  // for the time of the choice if no session is open on it
  const [picking, setPicking] = useState(false)
  const pickContainer = async () => {
    let via = ''
    let temp = ''
    if (s.viaSite) {
      const open = Object.values(useStore.getState().conns).find((c) => c.siteId === s.viaSite && c.state === 'connected')
      if (open) via = open.id
      else {
        setPicking(true)
        try {
          temp = via = (await call<{ id: string }>('conn.open', { siteId: s.viaSite })).id
        } catch (e) {
          errorToast(t('ctr.pick'), e)
          return
        } finally {
          setPicking(false)
        }
      }
    }
    await openContainers({
      via: via || undefined,
      runtime: s.containerRuntime || 'docker',
      onPick: (c, r, k8s) => up({ host: c.name, containerRuntime: r, name: s.name || c.name, k8sNamespace: c.namespace || k8s.namespace || undefined, k8sContext: k8s.context || undefined, k8sContainer: c.containers?.[0] || undefined }),
    })
    if (temp) call('conn.close', { conn: temp }).catch(() => {})
  }
  const sshSites = sites.filter((x) => x.id !== s.id && (x.protocol === 'sftp' || x.protocol === 'scp'))
  const sshConfigOn = useStore((st) => st.sshConfig?.enabled ?? false)
  const report = useSshReport(ssh ? s : null, sshConfigOn)
  const detected = report?.enabled && report.options.length ? report : null

  const up = (patch: Partial<Site>) => {
    setS((cur) => ({ ...cur, ...patch }))
    setDirty(true)
  }
  const upSecret = (patch: Partial<SecretInput>) => {
    setSecrets((cur) => ({ ...cur, ...patch }))
    setDirty(true)
  }

  const sections: { id: Section; label: string; icon: any; show: boolean }[] = useMemo(() => [
    { id: 'general', label: t('common.general'), icon: Server, show: true },
    { id: 'dirs', label: t('site.directories'), icon: Folder, show: true },
    { id: 'env', label: t('site.environment'), icon: Clock, show: true },
    { id: 'conn', label: t('site.connection'), icon: Cable, show: !isCtr },
    { id: 'proxy', label: t('site.proxy'), icon: Globe, show: !isCtr },
    { id: 'jumps', label: t('site.tunnel'), icon: Waypoints, show: !isCtr },
    { id: 'forwards', label: t('site.forwards'), icon: Network, show: ssh },
    { id: 'ssh', label: t('site.ssh'), icon: KeyRound, show: ssh },
    { id: 'ftp', label: t('site.ftp'), icon: Settings2, show: s.protocol === 'ftp' },
    { id: 'tls', label: 'TLS', icon: Shield, show: (s.protocol === 'ftp' && s.ftps !== 'none') || ((s.protocol === 'webdav' || s.protocol === 's3') && !!s.tls) },
    { id: 's3', label: t('site.s3'), icon: Cloud, show: s.protocol === 's3' },
    { id: 'notes', label: t('site.notes'), icon: NotebookPen, show: true },
  ], [s.protocol, s.ftps, s.tls, ssh, isCtr])

  const save = async (): Promise<SiteView | null> => {
    if (!s.host.trim() && s.protocol !== 's3') {
      toast('warn', t('site.host'), 'Host name is required')
      setSection('general')
      return null
    }
    setBusy(true)
    try {
      const saved = await call<SiteView>('sites.save', { site: { ...s, host: s.host.trim(), port: s.port || 0 }, secrets })
      await reloadSites()
      setDirty(false)
      setSecrets({})
      onSaved(saved)
      return saved
    } catch (e) {
      errorToast(t('common.save'), e)
      return null
    } finally {
      setBusy(false)
    }
  }

  const connect = async () => {
    if (isNew) {
      if (!s.host.trim() && s.protocol !== 's3') return
      connectAdhoc({ ...s, port: s.port || 0, proxy: { ...s.proxy, password: secrets.proxyPassword }, passphrase: secrets.passphrase }, secrets.password)
      return
    }
    if (dirty) {
      const saved = await save()
      if (saved) connectSite(saved.id)
    } else connectSite(s.id)
  }

  const del = async () => {
    if (!(await ask({ title: t('common.delete'), message: t('sites.deleteConfirm', { n: 1 }), danger: true, ok: t('common.delete') }))) return
    await call('sites.delete', { ids: [s.id] })
    await reloadSites()
    onDeleted()
  }

  const pwField = (label: string, key: keyof SecretInput, has: boolean | undefined) => (
    <div className="field">
      <label>{label}</label>
      <div className="input-group">
        <input
          className="input"
          type="password"
          autoComplete="new-password"
          value={(secrets[key] as string) ?? ''}
          placeholder={has && secrets[key] === undefined ? t('site.passwordSaved') : ''}
          onChange={(e) => upSecret({ [key]: e.target.value })}
        />
        {has && secrets[key] === undefined && (
          <div className="input-suffix">
            <button className="icon-btn sm" data-tip={t('site.clearPassword')} onClick={() => upSecret({ [key]: '' })}><X /></button>
          </div>
        )}
      </div>
    </div>
  )

  const isS3 = s.protocol === 's3'

  return (
    <div className="site-editor">
      <div className="se-head">
        <div className="se-title">
          <span className="site-color big" style={{ background: s.color || 'var(--accent)' }} />
          <input className="se-name" value={s.name} placeholder={s.host ? `${s.user ? s.user + '@' : ''}${s.host}` : t('sites.newSite')} onChange={(e) => up({ name: e.target.value })} />
          {dirty && !isNew && <span className="badge warn">{t('editor.modified')}</span>}
          {isNew && <span className="badge">{t('sites.unsaved')}</span>}
        </div>
        <div className="row">
          {!isNew && <button className="icon-btn" data-tip={t('sites.copyUrl')} onClick={async () => copyToClipboard(await call<string>('sites.url', { id: s.id }))}><Copy /></button>}
          {!isNew && <button className="icon-btn" data-tip={t('common.delete')} onClick={del}><Trash2 /></button>}
          {isNew && <button className="btn" onClick={onCancel}>{t('common.cancel')}</button>}
          <button className="btn" disabled={busy || (!dirty && !isNew)} onClick={save}><Save /> {t('common.save')}</button>
          <button className="btn primary" disabled={busy} onClick={connect}><Plug /> {isNew ? t('common.connect') : dirty ? t('sites.saveAndConnect') : t('common.connect')}</button>
        </div>
      </div>
      <div className="se-body">
        <nav className="vtabs">
          {sections.filter((x) => x.show).map((x) => (
            <button key={x.id} className={section === x.id ? 'on' : ''} onClick={() => setSection(x.id)}>
              <x.icon /> {x.label}
            </button>
          ))}
        </nav>
        <div
          className="se-form"
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.target as HTMLElement).tagName === 'INPUT') {
              e.preventDefault()
              connect()
            }
            if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
              e.preventDefault()
              save()
            }
          }}
        >
          {section === 'general' && (
            <div className="form">
              <div className="field">
                <label>{t('site.protocol')}</label>
                <div className="segmented">
                  {(['sftp', 'scp', 'ftp', 'webdav', 's3', 'container'] as Protocol[]).map((p) => (
                    <button key={p} className={s.protocol === p ? 'on' : ''} onClick={() => up({ protocol: p, tls: p === 's3' ? true : p === 'webdav' ? s.tls : s.tls, port: 0, ...(p === 'container' && !s.containerRuntime ? { containerRuntime: 'docker' } : {}) })}>
                      {p === 'webdav' ? 'WebDAV' : p === 'container' ? t('site.container') : p.toUpperCase()}
                    </button>
                  ))}
                </div>
              </div>
              {s.protocol === 'ftp' && (
                <div className="field">
                  <label>{t('site.encryption')}</label>
                  <select className="select" value={s.ftps || 'none'} onChange={(e) => up({ ftps: e.target.value as any, port: 0 })}>
                    <option value="none">{t('site.ftpsNone')}</option>
                    <option value="explicit">{t('site.ftpsExplicit')}</option>
                    <option value="implicit">{t('site.ftpsImplicit')}</option>
                  </select>
                </div>
              )}
              {(s.protocol === 'webdav' || s.protocol === 's3') && (
                <label className="switch"><input type="checkbox" checked={!!s.tls} onChange={(e) => up({ tls: e.target.checked, port: 0 })} /> {t('site.tls')}</label>
              )}
              {isCtr && (
                <>
                  <div className="field">
                    <label>{t('ctr.runtime')}</label>
                    <div className="segmented">
                      {(['docker', 'podman', 'kubectl'] as const).map((r) => <button key={r} className={(s.containerRuntime || 'docker') === r ? 'on' : ''} onClick={() => up({ containerRuntime: r })}>{RUNTIME_LABEL[r]}</button>)}
                    </div>
                  </div>
                  <div className="field">
                    <label>{t('ctr.where')}</label>
                    <select className="select" value={s.viaSite || ''} onChange={(e) => up({ viaSite: e.target.value || undefined })}>
                      <option value="">{t('ctr.thisComputer')}</option>
                      {sshSites.map((x) => <option key={x.id} value={x.id}>{t('ctr.onSite', { name: x.name || x.host })}</option>)}
                    </select>
                    <span className="hint">{t('ctr.whereHint')}</span>
                  </div>
                  <div className="field">
                    <label>{s.containerRuntime === 'kubectl' ? t('ctr.pod') : t('ctr.name')}</label>
                    <div className="row">
                      <input className="input grow" value={s.host} placeholder={s.containerRuntime === 'kubectl' ? 'api-7f9c4d' : 'web'} onChange={(e) => up({ host: e.target.value })} spellCheck={false} autoFocus={isNew} />
                      <button className="btn" disabled={picking} onClick={pickContainer}>{picking ? <span className="spinner" /> : <Box />} {t('ctr.pick')}</button>
                    </div>
                  </div>
                  {s.containerRuntime === 'kubectl' ? (
                    <div className="field-row r3">
                      <div className="field"><label>{t('ctr.context')}</label><input className="input" value={s.k8sContext || ''} placeholder={t('ctr.currentContext')} onChange={(e) => up({ k8sContext: e.target.value })} /></div>
                      <div className="field"><label>{t('ctr.namespace')}</label><input className="input" value={s.k8sNamespace || ''} placeholder="default" onChange={(e) => up({ k8sNamespace: e.target.value })} /></div>
                      <div className="field"><label>{t('ctr.podContainer')}</label><input className="input" value={s.k8sContainer || ''} placeholder={t('ctr.firstContainer')} onChange={(e) => up({ k8sContainer: e.target.value })} /></div>
                    </div>
                  ) : (
                    <div className="field"><label>{t('ctr.user')}</label><input className="input" value={s.user || ''} placeholder={t('ctr.userPlaceholder')} onChange={(e) => up({ user: e.target.value })} spellCheck={false} /></div>
                  )}
                  <div className="field"><label>{t('ctr.command')}</label><input className="input mono" value={s.containerCommand || ''} placeholder={s.containerRuntime || 'docker'} onChange={(e) => up({ containerCommand: e.target.value })} spellCheck={false} /><span className="hint">{t('ctr.commandHint')}</span></div>
                </>
              )}
              {!isCtr && (
              <div className="field-row r-port">
                <div className="field">
                  <label>{isS3 ? 'Endpoint' : t('site.host')}</label>
                  <input className="input" value={s.host} placeholder={isS3 ? 's3.amazonaws.com' : 'example.com'} onChange={(e) => up({ host: e.target.value })} spellCheck={false} autoFocus={isNew} />
                </div>
                <div className="field">
                  <label>{t('site.port')}</label>
                  <input className="input tnum" type="number" min={0} max={65535} value={s.port || ''} placeholder={String(detected?.port || defaultPort(s))} onChange={(e) => up({ port: parseInt(e.target.value, 10) || 0 })} />
                </div>
              </div>
              )}
              {!isCtr && (
              <div className="field-row">
                <div className="field">
                  <label>{isS3 ? t('site.accessKey') : t('site.user')}</label>
                  <input className="input" value={s.user || ''} placeholder={detected?.user || ''} onChange={(e) => up({ user: e.target.value })} spellCheck={false} autoComplete="off" />
                </div>
                {pwField(isS3 ? t('site.secretKey') : t('site.password'), 'password', view.hasPassword)}
              </div>
              )}
              {s.protocol === 'ftp' && (
                <label className="check"><input type="checkbox" checked={s.user === 'anonymous'} onChange={(e) => up({ user: e.target.checked ? 'anonymous' : '' })} /> {t('site.anonymous')}</label>
              )}
              {ssh && (
                <div className="field">
                  <label>{t('site.keyFile')}</label>
                  <KeyFileInput icon value={s.keyFile || ''} placeholder={detected?.keyFiles?.[0] ? displayPath('local', detected.keyFiles[0]) : '~/.ssh/id_ed25519'} onChange={(v) => up({ keyFile: v })} />
                  <span className="hint">{t('site.keyFileHint')}</span>
                </div>
              )}
              {detected && (
                <div className="alert info sshcfg-detected">
                  <FileCog />
                  <div className="grow" style={{ minWidth: 0 }}>
                    <div>{t('sshcfg.detectedFor', { host: s.host.trim(), n: detected.options.filter((o) => o.status === 'applied').length })}</div>
                    <div className="mono faint ellipsis">{detected.options.filter((o) => o.status === 'applied').slice(0, 5).map((o) => `${o.key} ${o.value}`).join(' · ')}</div>
                  </div>
                  <button className="btn sm" onClick={() => setSection('ssh')}>{t('sshcfg.details')}</button>
                </div>
              )}
              {s.protocol === 'webdav' && <div className="alert info"><Globe /><span>{t('site.webdavPathHint')}</span></div>}
              <div className="divider" />
              <div className="field-row">
                <div className="field">
                  <label>{t('site.folder')}</label>
                  <input className="input" list="site-folders" value={s.folder || ''} onChange={(e) => up({ folder: e.target.value })} />
                  <datalist id="site-folders">{folders.map((f) => <option key={f} value={f} />)}</datalist>
                </div>
                <div className="field">
                  <label>{t('site.color')}</label>
                  <div className="color-row">
                    {COLORS.map((c) => (
                      <button key={c || 'none'} className={`swatch${(s.color || '') === c ? ' on' : ''}`} style={{ background: c || 'transparent' }} onClick={() => up({ color: c })} aria-label={c ? t('a11y.color', { color: c }) : t('a11y.noColor')} aria-pressed={(s.color || '') === c}>
                        {!c && <X />}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              <div className="field-row">
                <div className="field">
                  <label>{t('site.remoteDir')}</label>
                  <input className="input mono" value={s.remoteDir || ''} placeholder={s.protocol === 'webdav' ? '/remote.php/dav/files/me/' : '/var/www'} onChange={(e) => up({ remoteDir: e.target.value })} spellCheck={false} />
                </div>
                <div className="field">
                  <label>{t('site.localDir')}</label>
                  <input className="input mono" value={s.localDir || ''} placeholder={t('site.localDirExample')} onChange={(e) => up({ localDir: e.target.value })} spellCheck={false} />
                </div>
              </div>
            </div>
          )}

          {section === 'dirs' && (
            <div className="form">
              <div className="field"><label>{t('site.remoteDir')}</label><input className="input mono" value={s.remoteDir || ''} onChange={(e) => up({ remoteDir: e.target.value })} /></div>
              <div className="field"><label>{t('site.localDir')}</label><input className="input mono" value={s.localDir || ''} onChange={(e) => up({ localDir: e.target.value })} /></div>
              <label className="switch"><input type="checkbox" checked={!!s.syncBrowsing} onChange={(e) => up({ syncBrowsing: e.target.checked })} /> {t('site.syncBrowsing')}</label>
              <label className="switch"><input type="checkbox" checked={s.resolveSymlinks} onChange={(e) => up({ resolveSymlinks: e.target.checked })} /> {t('site.resolveSymlinks')}</label>
              <div className="field">
                <label>{t('site.recycleBin')}</label>
                <input className="input mono" value={s.recycleBin || ''} placeholder="~/.trash" onChange={(e) => up({ recycleBin: e.target.value })} />
                <span className="hint">{t('site.recycleBinHint')}</span>
              </div>
            </div>
          )}

          {section === 'env' && (
            <div className="form">
              <div className="field">
                <label>{t('site.eol')}</label>
                <div className="segmented">
                  {['LF', 'CRLF'].map((v) => <button key={v} className={(s.eol || 'LF') === v ? 'on' : ''} onClick={() => up({ eol: v })}>{v}</button>)}
                </div>
              </div>
              <div className="field">
                <label>{t('site.timeOffset')}</label>
                <input className="input tnum" type="number" style={{ width: 140 }} value={s.timeOffset || 0} onChange={(e) => up({ timeOffset: parseInt(e.target.value, 10) || 0 })} />
              </div>
              {s.protocol === 'ftp' && (
                <div className="field">
                  <label>{t('site.encodingLabel')}</label>
                  <select className="select" value={s.encoding || ''} onChange={(e) => up({ encoding: e.target.value })}>
                    <option value="">UTF-8 (auto)</option>
                    <option value="windows-1252">Windows-1252 (Western)</option>
                    <option value="iso-8859-1">ISO-8859-1 (Latin-1)</option>
                    <option value="iso-8859-15">ISO-8859-15 (Latin-9)</option>
                    <option value="windows-1250">Windows-1250 (Central European)</option>
                    <option value="windows-1251">Windows-1251 (Cyrillic)</option>
                    <option value="koi8-r">KOI8-R</option>
                    <option value="cp850">CP850 (DOS)</option>
                    <option value="cp437">CP437 (DOS US)</option>
                  </select>
                </div>
              )}
            </div>
          )}

          {section === 'conn' && (
            <div className="form">
              <div className="field-row">
                <div className="field"><label>{t('site.timeout')}</label><input className="input tnum" type="number" min={1} value={s.timeout || 15} onChange={(e) => up({ timeout: parseInt(e.target.value, 10) || 15 })} /></div>
                <div className="field"><label>{t('site.keepalive')}</label><input className="input tnum" type="number" min={0} value={s.keepalive ?? 30} onChange={(e) => up({ keepalive: parseInt(e.target.value, 10) || 0 })} /></div>
              </div>
              <div className="field-row">
                <div className="field">
                  <label>{t('site.ipVersion')}</label>
                  <select className="select" value={s.ipVersion || ''} onChange={(e) => up({ ipVersion: e.target.value })}>
                    <option value="">Auto</option>
                    <option value="4">IPv4</option>
                    <option value="6">IPv6</option>
                  </select>
                </div>
                <div className="field">
                  <label>{t('site.parallel')}</label>
                  <input className="input tnum" type="number" min={1} max={16} value={s.parallel || 2} onChange={(e) => up({ parallel: parseInt(e.target.value, 10) || 2 })} />
                </div>
              </div>
            </div>
          )}

          {section === 'proxy' && (
            <div className="form">
              <div className="field">
                <label>{t('site.proxyType')}</label>
                <select className="select" value={s.proxy?.type || ''} onChange={(e) => up({ proxy: { ...s.proxy, type: e.target.value } })}>
                  <option value="">{t('common.none')}</option>
                  <option value="socks4">SOCKS4</option>
                  <option value="socks5">SOCKS5</option>
                  <option value="http">HTTP</option>
                  <option value="telnet">Telnet</option>
                  <option value="command">{t('site.proxyLocalCmd')}</option>
                </select>
              </div>
              {s.proxy?.type && s.proxy.type !== 'command' && (
                <>
                  <div className="field-row r-port">
                    <div className="field"><label>{t('site.proxyHost')}</label><input className="input" value={s.proxy?.host || ''} onChange={(e) => up({ proxy: { ...s.proxy, host: e.target.value } })} /></div>
                    <div className="field"><label>{t('site.proxyPort')}</label><input className="input tnum" type="number" value={s.proxy?.port || ''} onChange={(e) => up({ proxy: { ...s.proxy, port: parseInt(e.target.value, 10) || 0 } })} /></div>
                  </div>
                  <div className="field-row">
                    <div className="field"><label>{t('site.proxyUser')}</label><input className="input" value={s.proxy?.user || ''} onChange={(e) => up({ proxy: { ...s.proxy, user: e.target.value } })} /></div>
                    {pwField(t('site.proxyPassword'), 'proxyPassword', view.hasProxyPassword)}
                  </div>
                  <label className="check"><input type="checkbox" checked={!!s.proxy?.remoteDns} onChange={(e) => up({ proxy: { ...s.proxy, remoteDns: e.target.checked } })} /> {t('site.remoteDns')}</label>
                </>
              )}
              {(s.proxy?.type === 'command' || s.proxy?.type === 'telnet') && (
                <div className="field">
                  <label>{t('site.proxyCommand')}</label>
                  <input className="input mono" value={s.proxy?.command || ''} placeholder={s.proxy?.type === 'command' ? 'ssh -W %host:%port bastion' : 'connect %host %port\\n'} onChange={(e) => up({ proxy: { ...s.proxy, command: e.target.value } })} />
                  <span className="hint">{t('site.proxyCommandHint')}</span>
                </div>
              )}
            </div>
          )}

          {section === 'jumps' && (
            <div className="form">
              <p className="muted" style={{ margin: 0 }}>{t('site.tunnelHint')}</p>
              {(s.jumps || []).map((j, i) => (
                <JumpRow
                  key={i}
                  j={j}
                  index={i}
                  sites={sites.filter((x) => x.id !== s.id && (x.protocol === 'sftp' || x.protocol === 'scp'))}
                  hasPassword={view.jumpHasPassword?.[i]}
                  onChange={(nj) => up({ jumps: (s.jumps || []).map((x, k) => (k === i ? nj : x)) })}
                  onPassword={(pw) => upSecret({ jumpPasswords: { ...(secrets.jumpPasswords || {}), [i]: pw } })}
                  onRemove={() => up({ jumps: (s.jumps || []).filter((_, k) => k !== i) })}
                />
              ))}
              <div><button className="btn sm" onClick={() => up({ jumps: [...(s.jumps || []), { host: '', user: '' }] })}><Plus /> {t('site.addJump')}</button></div>
            </div>
          )}

          {section === 'forwards' && (
            <div className="form">
              <p className="muted" style={{ margin: 0 }}>{t('site.forwardsHint')}</p>
              {(s.forwards || []).map((f, i) => (
                <ForwardRow key={f.id} f={f} onChange={(nf) => up({ forwards: (s.forwards || []).map((x, k) => (k === i ? nf : x)) })} onRemove={() => up({ forwards: (s.forwards || []).filter((_, k) => k !== i) })} />
              ))}
              <div><button className="btn sm" onClick={() => up({ forwards: [...(s.forwards || []), { id: Math.random().toString(36).slice(2), type: 'local', bindPort: 8080, destHost: 'localhost', destPort: 80, autoStart: true }] })}><Plus /> {t('site.addForward')}</button></div>
            </div>
          )}

          {section === 'ssh' && (
            <div className="form">
              <div className="form-section">
                <h4>{t('sshcfg.title')}</h4>
                <label className="switch">
                  <input type="checkbox" checked={!s.ignoreSshConfig} disabled={!sshConfigOn} onChange={(e) => up({ ignoreSshConfig: !e.target.checked })} /> {t('sshcfg.apply')}
                </label>
                <span className="hint">{sshConfigOn ? t('sshcfg.applyHint') : t('sshcfg.disabledHint')}</span>
                {report?.enabled && (
                  <>
                    <SshEffective report={report} />
                    <SshWarnings report={report} />
                    <SshConfigOptions report={report} />
                  </>
                )}
              </div>
              <div className="form-section">
                <h4>{t('site.auth')}</h4>
                <div className="field"><label>{t('site.keyFile')}</label><KeyFileInput value={s.keyFile || ''} placeholder={t('site.keyFilePlaceholder')} onChange={(v) => up({ keyFile: v })} /></div>
                {pwField(t('site.passphrase'), 'passphrase', view.hasPassphrase)}
                <label className="switch"><input type="checkbox" checked={s.useAgent} onChange={(e) => up({ useAgent: e.target.checked })} /> {t('site.useAgent')}</label>
                <label className="switch"><input type="checkbox" checked={!!s.forwardAgent} onChange={(e) => up({ forwardAgent: e.target.checked })} /> {t('site.forwardAgent')}</label>
                <span className="hint">{t('site.forwardAgentHint')}</span>
                <label className="switch"><input type="checkbox" checked={s.kbdInteractive} onChange={(e) => up({ kbdInteractive: e.target.checked })} /> {t('site.kbdint')}</label>
              </div>
              <div className="form-section">
                <h4>{t('prompt.fingerprint')}</h4>
                <div className="field"><label>{t('site.hostKey')}</label><input className="input mono" value={s.hostKey || ''} placeholder="SHA256:…" onChange={(e) => up({ hostKey: e.target.value })} /><span className="hint">{t('site.hostKeyHint')}</span></div>
              </div>
              <div className="form-section">
                <h4>{t('common.advanced')}</h4>
                {s.protocol === 'sftp' && <div className="field"><label>{t('site.sftpServer')}</label><input className="input mono" value={s.sftpServer || ''} placeholder="sudo /usr/lib/openssh/sftp-server" onChange={(e) => up({ sftpServer: e.target.value })} /><span className="hint">{t('site.sftpServerHint')}</span></div>}
                {s.protocol === 'scp' && (
                  <div className="field-row">
                    <div className="field"><label>{t('site.shell')}</label><input className="input mono" value={s.shell || ''} placeholder="/bin/bash" onChange={(e) => up({ shell: e.target.value })} /></div>
                    <div className="field"><label>{t('site.listCommand')}</label><input className="input mono" value={s.listCommand || ''} placeholder="ls -la" onChange={(e) => up({ listCommand: e.target.value })} /></div>
                  </div>
                )}
                <label className="switch"><input type="checkbox" checked={!!s.legacyAlgos} onChange={(e) => up({ legacyAlgos: e.target.checked })} /> {t('site.legacy')}</label>
              </div>
            </div>
          )}

          {section === 'ftp' && (
            <div className="form">
              <label className="switch"><input type="checkbox" checked={!s.ftpActive} onChange={(e) => up({ ftpActive: !e.target.checked })} /> {t('site.passive')}</label>
              {s.ftpActive && <div className="field"><label>{t('site.activeIp')}</label><input className="input mono" value={s.ftpActiveIp || ''} placeholder="auto" onChange={(e) => up({ ftpActiveIp: e.target.value })} /></div>}
              <div className="field">
                <label>{t('site.forcePasv')}</label>
                <div className="segmented">
                  {[['auto', 'Auto'], ['on', t('common.on')], ['off', t('common.off')]].map(([v, l]) => <button key={v} className={(s.ftpForcePasvIp || 'auto') === v ? 'on' : ''} onClick={() => up({ ftpForcePasvIp: v })}>{l}</button>)}
                </div>
              </div>
              <div className="field">
                <label>{t('site.mlsd')}</label>
                <div className="segmented">
                  {[['auto', 'Auto'], ['on', t('common.on')], ['off', t('common.off')]].map(([v, l]) => <button key={v} className={(s.ftpMlsd || 'auto') === v ? 'on' : ''} onClick={() => up({ ftpMlsd: v })}>{l}</button>)}
                </div>
              </div>
              <label className="switch"><input type="checkbox" checked={!!s.ftpListHidden} onChange={(e) => up({ ftpListHidden: e.target.checked })} /> {t('site.listHidden')}</label>
              <div className="field"><label>{t('site.account')}</label><input className="input" value={s.ftpAccount || ''} onChange={(e) => up({ ftpAccount: e.target.value })} /></div>
            </div>
          )}

          {section === 'tls' && (
            <div className="form">
              <div className="field"><label>{t('site.tlsFingerprint')}</label><input className="input mono" value={s.tlsFingerprint || ''} placeholder="AB:CD:…" onChange={(e) => up({ tlsFingerprint: e.target.value })} /></div>
              <label className="switch"><input type="checkbox" checked={!!s.tlsInsecure} onChange={(e) => up({ tlsInsecure: e.target.checked })} /> <span className={s.tlsInsecure ? 'danger-text' : ''}>{t('site.tlsInsecure')}</span></label>
            </div>
          )}

          {section === 's3' && (
            <div className="form">
              <div className="field"><label>{t('site.region')}</label><input className="input" value={s.s3Region || ''} placeholder="us-east-1" onChange={(e) => up({ s3Region: e.target.value })} /></div>
              <label className="switch"><input type="checkbox" checked={!!s.s3PathStyle} onChange={(e) => up({ s3PathStyle: e.target.checked })} /> {t('site.pathStyle')}</label>
              {pwField(t('site.sessionToken'), 's3SessionToken', view.hasS3SessionToken)}
            </div>
          )}

          {section === 'notes' && (
            <div className="form">
              <textarea className="textarea" rows={14} value={s.notes || ''} onChange={(e) => up({ notes: e.target.value })} />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function JumpRow({ j, index, sites, hasPassword, onChange, onPassword, onRemove }: { j: Jump; index: number; sites: SiteView[]; hasPassword?: boolean; onChange: (j: Jump) => void; onPassword: (pw: string) => void; onRemove: () => void }) {
  const [pw, setPw] = useState<string | undefined>(undefined)
  return (
    <div className="card" style={{ padding: 12 }}>
      <div className="row" style={{ marginBottom: 10 }}>
        <span className="badge accent">#{index + 1}</span>
        <select className="select" style={{ width: 240 }} value={j.siteId || ''} onChange={(e) => onChange({ ...j, siteId: e.target.value || undefined })}>
          <option value="">{t('site.customHost')}</option>
          {sites.map((s) => <option key={s.id} value={s.id}>{t('site.useSite')}: {s.name || s.host}</option>)}
        </select>
        <div className="spacer" />
        <button className="icon-btn sm" data-tip={t('common.delete')} onClick={onRemove}><Trash2 /></button>
      </div>
      {!j.siteId && (
        <div className="form" style={{ gap: 10 }}>
          <div className="field-row r-port">
            <input className="input" placeholder={t('site.host')} value={j.host || ''} onChange={(e) => onChange({ ...j, host: e.target.value })} />
            <input className="input tnum" type="number" placeholder="22" value={j.port || ''} onChange={(e) => onChange({ ...j, port: parseInt(e.target.value, 10) || 0 })} />
          </div>
          <div className="field-row">
            <input className="input" placeholder={t('site.user')} value={j.user || ''} onChange={(e) => onChange({ ...j, user: e.target.value })} />
            <input className="input" type="password" placeholder={hasPassword && pw === undefined ? t('site.passwordSaved') : t('site.password')} value={pw ?? ''} onChange={(e) => { setPw(e.target.value); onPassword(e.target.value) }} />
          </div>
          <KeyFileInput compact placeholder={t('site.keyFile')} value={j.keyFile || ''} onChange={(v) => onChange({ ...j, keyFile: v })} />
        </div>
      )}
    </div>
  )
}

function ForwardRow({ f, onChange, onRemove }: { f: Forward; onChange: (f: Forward) => void; onRemove: () => void }) {
  return (
    <div className="card fwd-row">
      <select className="select" style={{ width: 150 }} value={f.type} onChange={(e) => onChange({ ...f, type: e.target.value as any })}>
        <option value="local">{t('tunnel.local')}</option>
        <option value="remote">{t('tunnel.remote')}</option>
        <option value="dynamic">{t('tunnel.dynamic')}</option>
      </select>
      <input className="input mono" style={{ width: 110 }} placeholder="127.0.0.1" value={f.bindHost || ''} onChange={(e) => onChange({ ...f, bindHost: e.target.value })} />
      <input className="input tnum" style={{ width: 80 }} type="number" value={f.bindPort || ''} onChange={(e) => onChange({ ...f, bindPort: parseInt(e.target.value, 10) || 0 })} />
      {f.type !== 'dynamic' && (
        <>
          <span className="faint">→</span>
          <input className="input mono" placeholder="localhost" value={f.destHost || ''} onChange={(e) => onChange({ ...f, destHost: e.target.value })} />
          <input className="input tnum" style={{ width: 80 }} type="number" value={f.destPort || ''} onChange={(e) => onChange({ ...f, destPort: parseInt(e.target.value, 10) || 0 })} />
        </>
      )}
      <label className="check" data-tip={t('site.fwdAutoStart')}><input type="checkbox" checked={!!f.autoStart} onChange={(e) => onChange({ ...f, autoStart: e.target.checked })} /> Auto</label>
      <button className="icon-btn sm" data-tip={t('common.delete')} onClick={onRemove}><Trash2 /></button>
    </div>
  )
}
