import { useEffect, useRef } from 'react'
import { barTabs, get, set, useStore, type SessionTab } from '../lib/store'
import { TabBar } from './TabBar'
import { SessionView } from './SessionView'
import { SitesView } from './sites/SitesView'
import { BottomPanel } from './panels/BottomPanel'
import { StatusBar } from './StatusBar'
import { EditorView } from './editor/EditorView'
import { DiffView } from './editor/DiffView'
import { TerminalView } from './terminal/TerminalView'
import { ProjectView } from './project/ProjectView'
import { LogsView } from './logs/LogsView'
import { UsageView } from './usage/UsageView'
import { MonitorView } from './monitor/MonitorView'
import { TasksView } from './tasks/TasksView'
import { CommandPalette } from './CommandPalette'
import { Splitter } from './ui/Splitter'
import { handleGlobalKey } from '../lib/keys'
import { t } from '../lib/i18n'
import { WifiOff } from 'lucide-react'

export function Shell() {
  const tabs = useStore((s) => s.tabs)
  const active = useStore((s) => s.activeTab)
  const bottom = useStore((s) => s.bottom)
  const online = useStore((s) => s.online)
  const palette = useStore((s) => s.palette)
  const startH = useRef(bottom.height)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => handleGlobalKey(e)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <div className="shell">
      <TabBar />
      {!online && (
        <div className="offline-bar">
          <WifiOff /> {t('status.offline')}
        </div>
      )}
      <div className="main">
        {barTabs(tabs).map((tab) => {
          const visible = tab.id === active
          return (
            <div key={tab.id} className="tab-content" style={{ display: visible ? 'flex' : 'none' }}>
              {tab.kind === 'sites' && <SitesView />}
              {tab.kind === 'session' && <SessionView tab={tab as SessionTab} visible={visible} />}
              {tab.kind === 'editor' && <EditorView tab={tab} visible={visible} />}
              {tab.kind === 'diff' && <DiffView tab={tab} />}
              {tab.kind === 'terminal' && <TerminalView tab={tab} visible={visible} />}
              {tab.kind === 'monitor' && <MonitorView tab={tab} visible={visible} />}
              {tab.kind === 'project' && <ProjectView tab={tab} visible={visible} />}
              {tab.kind === 'logs' && <LogsView tab={tab} visible={visible} />}
              {tab.kind === 'usage' && <UsageView tab={tab} />}
              {tab.kind === 'tasks' && <TasksView />}
            </div>
          )
        })}
      </div>
      {bottom.open && (
        <>
          <Splitter
            dir="v"
            onStart={() => (startH.current = get().bottom.height)}
            onDrag={(d) => set((s) => ({ bottom: { ...s.bottom, height: Math.max(120, Math.min(window.innerHeight - 220, startH.current - d)) } }))}
          />
          <div className="bottom" style={{ height: bottom.height }}>
            <BottomPanel />
          </div>
        </>
      )}
      <StatusBar />
      {palette && <CommandPalette />}
    </div>
  )
}
