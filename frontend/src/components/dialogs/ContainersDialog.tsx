import { useEffect, useState } from 'react'
import { Box, FolderOpen, Plus, RefreshCw, SquareTerminal } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { useStore } from '../../lib/store'
import { connectAdhoc, openTerminal } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { ContainerInfo, Site } from '../../lib/types'

export type Runtime = 'docker' | 'podman' | 'kubectl'

export const RUNTIME_LABEL: Record<string, string> = { docker: 'Docker', podman: 'Podman', kubectl: 'Kubernetes' }

/** A site for a container (Host is its name); via is an open SSH connection. */
export function containerSite(c: ContainerInfo, runtime: Runtime, via?: string, extra: Partial<Site> = {}): Site {
  return {
    id: '', name: c.name, protocol: 'container', host: c.name, containerRuntime: runtime, viaConn: via || undefined,
    k8sNamespace: c.namespace || undefined, k8sContainer: c.containers?.[0] || undefined,
    useAgent: false, kbdInteractive: false, resolveSymlinks: true, keepalive: 0, timeout: 15, ...extra,
  }
}

/**
 * Lists the containers of this computer, or of the server behind an open
 * SSH connection (via). With onPick, choosing a container returns it.
 */
export function openContainers(opts: { via?: string; runtime?: Runtime; onPick?: (c: ContainerInfo, runtime: Runtime, k8s: { context: string; namespace: string }) => void; onNewSite?: (s: Site) => void } = {}) {
  return openDialog((close) => <Containers {...opts} close={close} />)
}

function Containers({ via, runtime: initial, onPick, onNewSite, close }: { via?: string; runtime?: Runtime; onPick?: (c: ContainerInfo, r: Runtime, k8s: { context: string; namespace: string }) => void; onNewSite?: (s: Site) => void; close: () => void }) {
  const conn = useStore((s) => (via ? s.conns[via] : undefined))
  const [runtime, setRuntime] = useState<Runtime>(initial || 'docker')
  const [ns, setNs] = useState('')
  const [ctx, setCtx] = useState('')
  const [list, setList] = useState<ContainerInfo[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const load = async () => {
    setBusy(true)
    setError('')
    try {
      setList(await call<ContainerInfo[]>('containers.list', { runtime, via: via || '', namespace: ns, context: ctx }))
    } catch (e: any) {
      setList(null)
      setError(e?.message || String(e))
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    load()
  }, [runtime])

  const site = (c: ContainerInfo) => containerSite(c, runtime, via, runtime === 'kubectl' ? { k8sContext: ctx || undefined } : {})
  const open = (c: ContainerInfo, terminal = false) => {
    close()
    connectAdhoc(site(c), undefined, terminal ? { afterConnect: (ci: { id: string }) => openTerminal(ci.id) } : {})
  }
  return (
    <Dialog
      size="lg"
      title={via ? t('ctr.titleOn', { host: conn?.title || via }) : t('ctr.titleLocal')}
      icon={<Box />}
      onClose={close}
      footer={<><span className="faint">{t('ctr.hint')}</span><div className="spacer" /><button className="btn" onClick={close}>{t('common.close')}</button></>}
    >
      <div className="form">
        <div className="row">
          <div className="segmented">
            {(['docker', 'podman', 'kubectl'] as Runtime[]).map((r) => <button key={r} className={runtime === r ? 'on' : ''} onClick={() => setRuntime(r)}>{RUNTIME_LABEL[r]}</button>)}
          </div>
          {runtime === 'kubectl' && (
            <>
              <input className="input" style={{ width: 150 }} value={ctx} placeholder={t('ctr.context')} onChange={(e) => setCtx(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} />
              <input className="input" style={{ width: 150 }} value={ns} placeholder={t('ctr.allNamespaces')} onChange={(e) => setNs(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} />
            </>
          )}
          <div className="spacer" />
          <button className="btn" disabled={busy} onClick={load}><RefreshCw /> {t('common.refresh')}</button>
        </div>
        {error && <div className="alert danger"><Box /><span className="selectable">{error}</span></div>}
        {list && list.length === 0 && <p className="muted">{t('ctr.none')}</p>}
        {list && list.length > 0 && (
          <table className="table containers">
            <tbody>
              {list.map((c) => (
                <tr key={(c.namespace || '') + '/' + c.name} data-container={c.name}>
                  <td><span className={`dot ${c.running ? 'ok' : ''}`} /> <span className="strong">{c.name}</span>{c.namespace && <span className="faint"> · {c.namespace}</span>}</td>
                  <td className="mono faint ellipsis" style={{ maxWidth: 220 }}>{c.image}</td>
                  <td className="faint">{c.status}</td>
                  <td style={{ whiteSpace: 'nowrap', textAlign: 'right' }}>
                    {onPick ? (
                      <button className="btn sm primary" onClick={() => { onPick(c, runtime, { context: ctx, namespace: c.namespace || ns }); close() }}>{t('ctr.choose')}</button>
                    ) : (
                      <>
                        <button className="btn sm" disabled={!c.running} onClick={() => open(c)}><FolderOpen /> {t('ctr.files')}</button>
                        <button className="icon-btn sm" disabled={!c.running} data-tip={t('sites.openTerminal')} onClick={() => open(c, true)}><SquareTerminal /></button>
                        {onNewSite && <button className="icon-btn sm" data-tip={t('sshcfg.newSite')} onClick={() => { close(); onNewSite(site(c)) }}><Plus /></button>}
                      </>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Dialog>
  )
}
