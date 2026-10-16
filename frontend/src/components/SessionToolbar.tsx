import {
  ArrowLeftRight, Columns2, Eye, EyeOff, FileSearch, GitCompareArrows, Info, Link2, Network, PanelRight, Power, RefreshCcwDot,
  ScrollText, SquareTerminal, TerminalSquare, Zap, FolderTree, Activity, Box, ChevronDown, FolderCode, Cog, EthernetPort,
} from 'lucide-react'
import { get, set, useStore, type SessionTab } from '../lib/store'
import { closeTab, editNewSite, openMonitor, openProject, savePrefs, setBottom, toggleSyncBrowsing, updateLayout } from '../lib/actions'
import { openServices } from './dialogs/ServicesDialog'
import { openPorts } from './dialogs/PortsDialog'
import { openContainers } from './dialogs/ContainersDialog'
import { showMenuAt } from './ui/Menu'
import { shellMenu } from '../lib/shellmenu'
import { compareDirs, terminalHere, runCommandDialog } from '../lib/fileops'
import { t } from '../lib/i18n'
import { openSyncDialog } from './dialogs/SyncDialog'
import { openFindDialog } from './dialogs/FindDialog'
import { openWatchDialog } from './dialogs/WatchDialog'
import { openTunnelDialog } from './dialogs/TunnelDialog'
import { openServerInfo } from './dialogs/ServerInfo'

export function SessionToolbar({ tab }: { tab: SessionTab }) {
  const conn = useStore((s) => s.conns[tab.conn])
  const showHidden = useStore((s) => s.prefs?.showHidden)
  const previewOpen = useStore((s) => s.previewOpen)
  const showTree = useStore((s) => s.prefs?.showTree)
  const comparing = !!(tab.panes.left.compare || tab.panes.right.compare)
  const caps = conn?.caps
  const side = tab.active
  return (
    <div className="toolbar">
      <div className="conn-badge" data-tip={conn?.url}>
        <span className={`dot ${conn?.state === 'connected' ? 'ok' : conn?.state === 'lost' ? 'danger' : 'busy'}`} />
        <span className="proto">{conn?.protocol.toUpperCase()}</span>
        <span className="ellipsis">{conn ? `${conn.user ? conn.user + '@' : ''}${conn.host}` : ''}</span>
      </div>
      <div className="vdivider" />
      <button className="tb-btn" onClick={() => openSyncDialog(tab.id)} data-tip={t('toolbar.sync')} data-kbd="Ctrl+S">
        <RefreshCcwDot /> <span>{t('toolbar.sync')}</span>
      </button>
      <button className={`tb-btn${comparing ? ' on' : ''}`} onClick={() => compareDirs(tab.id)} data-tip={t('toolbar.compare')} data-kbd="Ctrl+=">
        <GitCompareArrows /> <span>{t('toolbar.compare')}</span>
      </button>
      <button className="tb-btn" onClick={() => openFindDialog(tab.id, side)} data-tip={t('toolbar.find')} data-kbd="Ctrl+Shift+F">
        <FileSearch /> <span>{t('toolbar.find')}</span>
      </button>
      <button className="tb-btn icon-only" onClick={() => openWatchDialog(tab.id)} data-tip={t('toolbar.keepUpToDate')} data-kbd="Ctrl+U">
        <Zap />
      </button>
      <button className={`tb-btn icon-only${tab.syncBrowsing ? ' on' : ''}`} onClick={() => toggleSyncBrowsing(tab.id)} data-tip={t('toolbar.syncBrowsing')} data-kbd="Ctrl+B">
        <Link2 />
      </button>
      <div className="vdivider" />
      <div className="tb-split">
        <button className="tb-btn" disabled={!caps?.shell && tab.panes[side].conn !== 'local'} onClick={() => terminalHere(tab.id, side)} data-tip={t('toolbar.terminal')} data-kbd="Ctrl+T">
          <SquareTerminal /> <span>{t('term.title')}</span>
        </button>
        <button className="tb-btn icon-only tb-chevron" disabled={!caps?.shell && tab.panes[side].conn !== 'local'} data-tip={t('term.choose')} onClick={async (e) => { const el = e.currentTarget; showMenuAt(el, await shellMenu(tab.panes[side].conn, (s) => terminalHere(tab.id, side, s.id))) }}>
          <ChevronDown />
        </button>
      </div>
      <button className="tb-btn" onClick={() => openProject(tab.panes[side].conn, tab.panes[side].path)} data-tip={t('project.openHereTip')} data-kbd="Ctrl+Shift+E">
        <FolderCode /> <span>{t('project.title')}</span>
      </button>
      <button className="tb-btn icon-only" disabled={!caps?.exec} onClick={() => runCommandDialog(tab.id, side)} data-tip={t('cmd.run')} data-kbd="Ctrl+Shift+X">
        <TerminalSquare />
      </button>
      <button className="tb-btn icon-only" disabled={!caps?.tunnels} onClick={() => openTunnelDialog(tab.conn)} data-tip={t('toolbar.tunnels')}>
        <Network />
      </button>
      <button className="tb-btn icon-only" disabled={!caps?.shell} onClick={() => openMonitor(tab.conn)} data-tip={t('mon.open')}>
        <Activity />
      </button>
      <button className="tb-btn icon-only" disabled={!caps?.shell} onClick={() => openServices(tab.conn)} data-tip={t('svc.open')}>
        <Cog />
      </button>
      <button className="tb-btn icon-only" disabled={!caps?.shell || !caps?.tunnels} onClick={() => openPorts(tab.conn)} data-tip={t('ports.openDialog')}>
        <EthernetPort />
      </button>
      <button className="tb-btn icon-only" disabled={!caps?.tunnels} onClick={() => openContainers({ via: tab.conn, onNewSite: (st) => editNewSite({ ...st, viaConn: undefined, viaSite: get().conns[tab.conn]?.siteId }) })} data-tip={t('ctr.ofServer')}>
        <Box />
      </button>
      <div className="spacer" />
      <button className={`tb-btn icon-only${showHidden ? ' on' : ''}`} onClick={() => savePrefs({ showHidden: !get().prefs?.showHidden })} data-tip={t('toolbar.hidden')} data-kbd="Ctrl+Alt+H">
        {showHidden ? <Eye /> : <EyeOff />}
      </button>
      <button className={`tb-btn icon-only${showTree ? ' on' : ''}`} onClick={() => savePrefs({ showTree: !get().prefs?.showTree })} data-tip={t('toolbar.tree')}>
        <FolderTree />
      </button>
      <button className={`tb-btn icon-only${previewOpen ? ' on' : ''}`} onClick={() => set({ previewOpen: !get().previewOpen })} data-tip={t('toolbar.preview')} data-kbd="Ctrl+Shift+P">
        <PanelRight />
      </button>
      <button className="tb-btn icon-only" onClick={() => updateLayout(tab.id)} data-tip={t('toolbar.layout')}>
        {tab.layout === 'commander' ? <Columns2 /> : <ArrowLeftRight />}
      </button>
      <button className="tb-btn icon-only" onClick={() => setBottom(true, 'log')} data-tip={t('toolbar.log')}>
        <ScrollText />
      </button>
      <button className="tb-btn icon-only" onClick={() => openServerInfo(tab.conn)} data-tip={t('toolbar.serverInfo')}>
        <Info />
      </button>
      <button className="tb-btn icon-only danger-hover" onClick={() => closeTab(tab.id)} data-tip={t('common.disconnect')}>
        <Power />
      </button>
    </div>
  )
}
