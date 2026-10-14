import { ArrowDownUp, ChevronDown, FileEdit, FileSearch, Network, ScrollText, Zap } from 'lucide-react'
import { set, useStore, type BottomTab } from '../../lib/store'
import { setBottom } from '../../lib/actions'
import { t } from '../../lib/i18n'
import { QueuePanel } from './QueuePanel'
import { LogPanel } from './LogPanel'
import { FindPanel } from './FindPanel'
import { WatchPanel } from './WatchPanel'
import { TunnelsPanel } from './TunnelsPanel'
import { EditsPanel } from './EditsPanel'

export function BottomPanel() {
  const tab = useStore((s) => s.bottom.tab)
  const jobs = useStore((s) => s.jobs.filter((j) => ['queued', 'scanning', 'running', 'paused'].includes(j.status)).length)
  const find = useStore((s) => s.find)
  const watches = useStore((s) => Object.keys(s.watches).length)
  const tunnels = useStore((s) => Object.keys(s.tunnels).length)
  const edits = useStore((s) => Object.keys(s.edits).length)
  const tabs: { id: BottomTab; label: string; icon: any; badge?: number; hidden?: boolean }[] = [
    { id: 'queue', label: t('panel.queue'), icon: ArrowDownUp, badge: jobs },
    { id: 'log', label: t('panel.log'), icon: ScrollText },
    { id: 'find', label: t('panel.find'), icon: FileSearch, badge: find?.hits.length, hidden: !find },
    { id: 'watch', label: t('panel.watch'), icon: Zap, badge: watches },
    { id: 'tunnels', label: t('panel.tunnels'), icon: Network, badge: tunnels },
    { id: 'edits', label: t('panel.edits'), icon: FileEdit, badge: edits, hidden: !edits },
  ]
  return (
    <div className="bottom-panel">
      <div className="bp-tabs">
        {tabs.filter((x) => !x.hidden).map((x) => (
          <button key={x.id} className={`bp-tab${tab === x.id ? ' on' : ''}`} onClick={() => set((s) => ({ bottom: { ...s.bottom, tab: x.id } }))}>
            <x.icon />
            {x.label}
            {!!x.badge && <span className="badge accent">{x.badge}</span>}
          </button>
        ))}
        <div className="spacer" />
        <button className="icon-btn sm" onClick={() => setBottom(false)}>
          <ChevronDown />
        </button>
      </div>
      <div className="bp-body">
        {tab === 'queue' && <QueuePanel />}
        {tab === 'log' && <LogPanel />}
        {tab === 'find' && <FindPanel />}
        {tab === 'watch' && <WatchPanel />}
        {tab === 'tunnels' && <TunnelsPanel />}
        {tab === 'edits' && <EditsPanel />}
      </div>
    </div>
  )
}
