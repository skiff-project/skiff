import { useRef, useState } from 'react'
import { AlertTriangle, PlugZap, RefreshCw } from 'lucide-react'
import { get, useStore, type SessionTab } from '../lib/store'
import { abortConnection, reconnect, retryConnection, savePrefs } from '../lib/actions'
import { FilePane } from './pane/FilePane'
import { SessionToolbar } from './SessionToolbar'
import { Splitter } from './ui/Splitter'
import { PreviewPane } from './pane/PreviewPane'
import { t } from '../lib/i18n'

export function SessionView({ tab, visible }: { tab: SessionTab; visible: boolean }) {
  const connecting = useStore((s) => s.connecting[tab.id])
  const conn = useStore((s) => s.conns[tab.conn])
  const previewOpen = useStore((s) => s.previewOpen)
  const ratio = useStore((s) => (s.prefs?.ui?.splitRatio as number) || 0.5)
  const [r, setR] = useState<number | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  const startRatio = useRef(0.5)

  if (!tab.conn) {
    if (connecting?.error) {
      return (
        <div className="session-state">
          <div className="state-card">
            <div className="state-icon danger"><AlertTriangle /></div>
            <h3>{t('conn.failed')}</h3>
            <p className="selectable">{connecting.error}</p>
            <div className="row" style={{ justifyContent: 'center' }}>
              <button className="btn" onClick={() => abortConnection(tab.id)}>{t('common.close')}</button>
              <button className="btn primary" onClick={() => retryConnection(tab.id)}><RefreshCw /> {t('common.retry')}</button>
            </div>
          </div>
        </div>
      )
    }
    return (
      <div className="session-state">
        <div className="state-card">
          <div className="connect-anim"><span /><span /><span /></div>
          <h3>{t('conn.connecting', { host: connecting?.title || tab.title })}</h3>
          <button className="btn" onClick={() => abortConnection(tab.id)}>{t('conn.abort')}</button>
        </div>
      </div>
    )
  }

  const lost = conn && conn.state === 'lost'
  const cur = r ?? ratio
  const explorer = tab.layout === 'explorer'

  return (
    <div className="session">
      <SessionToolbar tab={tab} />
      {lost && (
        <div className="lost-bar">
          <PlugZap />
          <span><b>{t('conn.lost')}</b> — {!conn.error || /^connection lost$/i.test(conn.error) ? t('conn.lostHint') : conn.error}</span>
          <div className="spacer" />
          <button className="btn sm primary" onClick={() => reconnect(tab.conn)}><RefreshCw /> {t('tabs.reconnect')}</button>
        </div>
      )}
      <div className="commander" ref={ref}>
        {!explorer && (
          <>
            <div className="pane-wrap" style={{ flexBasis: `${cur * 100}%` }}>
              <FilePane tabId={tab.id} side="left" visible={visible} />
            </div>
            <Splitter
              dir="h"
              onStart={() => (startRatio.current = cur)}
              onDrag={(d) => {
                const w = ref.current?.offsetWidth || 1
                setR(Math.max(0.18, Math.min(0.82, startRatio.current + d / w)))
              }}
              onEnd={() => {
                if (r != null) savePrefs({ ui: { ...get().prefs!.ui, splitRatio: r } })
              }}
              onDoubleClick={() => {
                setR(0.5)
                savePrefs({ ui: { ...get().prefs!.ui, splitRatio: 0.5 } })
              }}
            />
          </>
        )}
        <div className="pane-wrap" style={{ flexBasis: explorer ? '100%' : `${(1 - cur) * 100}%` }}>
          <FilePane tabId={tab.id} side="right" visible={visible} />
        </div>
        {previewOpen && <PreviewPane tab={tab} />}
      </div>
    </div>
  )
}
