import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownToLine, Copy, Eraser, Filter, Pause, Play, RotateCw, ScrollText, WrapText } from 'lucide-react'
import { call } from '../../lib/api'
import { useStore, type LogsTab } from '../../lib/store'
import { displayPath } from '../../lib/actions'
import { listenLogs } from '../../lib/logs'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'

const MAX_LINES = 20000

// a line that tells of a failure, or of a warning (HTTP codes of access logs too)
const ERROR = /\b(error|err|fail(ed|ure)?|fatal|crit(ical)?|emerg(ency)?|alert|panic|denied|refused|exception|traceback)\b|"\s5\d\d\s|\s5\d\d\s\d/i
const WARN = /\b(warn(ing)?|deprecated|timeout|timed out|retry(ing)?)\b|"\s4\d\d\s|\s4\d\d\s\d/i

export function levelOf(line: string): '' | 'error' | 'warn' {
  if (ERROR.test(line)) return 'error'
  if (WARN.test(line)) return 'warn'
  return ''
}

/** A log followed live: the end of a file, then what is added to it. */
export function LogsView({ tab, visible }: { tab: LogsTab; visible: boolean }) {
  const conn = useStore((s) => s.conns[tab.conn])
  const [lines, setLines] = useState<string[]>([])
  const [state, setState] = useState<'starting' | 'live' | 'ended'>('starting')
  const [error, setError] = useState('')
  const [paused, setPaused] = useState(false)
  const [stick, setStick] = useState(true)
  const [wrap, setWrap] = useState(false)
  const [q, setQ] = useState('')
  const [onlyIssues, setOnlyIssues] = useState(false)
  const [run, setRun] = useState(0)
  const partial = useRef('')
  const held = useRef<string[]>([]) // lines received while paused
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const body = useRef<HTMLDivElement>(null)

  const add = (incoming: string[]) => {
    if (!incoming.length) return
    if (pausedRef.current) {
      held.current.push(...incoming)
      return
    }
    setLines((cur) => {
      const next = cur.concat(incoming)
      return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
    })
  }

  useEffect(() => {
    let id = ''
    let stop = () => {}
    let alive = true
    setState('starting')
    setError('')
    partial.current = ''
    call<string>('logs.follow', { conn: tab.conn, path: tab.path, unit: tab.unit, lines: 500 })
      .then((fid) => {
        if (!alive) {
          call('logs.stop', { id: fid }).catch(() => {})
          return
        }
        id = fid
        setState('live')
        stop = listenLogs(fid, {
          data: (text) => {
            const parts = (partial.current + text).split('\n')
            partial.current = parts.pop() || ''
            add(parts.map((l) => l.replace(/\r$/, '')))
          },
          note: (kind) => { if (kind === 'truncated') add([`— ${t('logs.truncated')} —`]) },
          end: (err) => {
            if (partial.current) add([partial.current])
            partial.current = ''
            setState('ended')
            setError(err)
          },
        })
      })
      .catch((e) => {
        setState('ended')
        setError(e?.message || String(e))
      })
    return () => {
      alive = false
      stop()
      if (id) call('logs.stop', { id }).catch(() => {})
    }
  }, [run])

  // resume: the lines received meanwhile come in
  useEffect(() => {
    if (!paused && held.current.length) {
      const h = held.current
      held.current = []
      add(h)
    }
  }, [paused])

  const filter = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (l: string) => (!needle || l.toLowerCase().includes(needle)) && (!onlyIssues || levelOf(l) !== '')
  }, [q, onlyIssues])
  const shown = useMemo(() => {
    const out = lines.filter(filter)
    return out.length > 5000 ? out.slice(out.length - 5000) : out
  }, [lines, filter])

  // the end stays in view, unless the user scrolled up
  useEffect(() => {
    if (stick && body.current) body.current.scrollTop = body.current.scrollHeight
  }, [shown, stick, visible])
  const onScroll = () => {
    const el = body.current
    if (!el) return
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 24
    if (atEnd !== stick) setStick(atEnd)
  }

  const what = tab.unit ? t('logs.ofService', { name: tab.unit }) : displayPath(tab.conn, tab.path || '')
  const issues = useMemo(() => lines.reduce((n, l) => n + (levelOf(l) ? 1 : 0), 0), [lines])

  return (
    <div className="logs">
      <div className="logs-bar">
        <ScrollText className="faint" />
        <span className="mono ellipsis logs-what" title={what}>{what}</span>
        <span className="faint ellipsis">{conn?.title || (tab.conn === 'local' ? t('common.local') : '')}</span>
        <span className={`logs-state ${state === 'live' ? (paused ? 'warn' : 'ok') : state === 'ended' ? (error ? 'danger' : '') : ''}`}>
          <span className={`dot ${state === 'live' ? (paused ? 'warn' : 'ok') : error ? 'danger' : ''}`} />
          {state === 'starting' ? t('logs.starting') : state === 'live' ? (paused ? t('logs.paused') : t('logs.live')) : t('logs.ended')}
        </span>
        <div className="spacer" />
        <div className="input-group logs-filter">
          <Filter />
          <input className="input" value={q} placeholder={t('logs.filter')} onChange={(e) => setQ(e.target.value)} spellCheck={false} />
        </div>
        <button className={`btn sm${onlyIssues ? ' on' : ''}`} onClick={() => setOnlyIssues(!onlyIssues)} data-tip={t('logs.onlyIssuesTip')}>
          {t('logs.onlyIssues')}{issues > 0 && <span className="badge danger">{issues}</span>}
        </button>
        {state === 'ended' ? (
          <button className="btn sm primary" onClick={() => setRun(run + 1)}><RotateCw /> {t('logs.restart')}</button>
        ) : (
          <button className="btn sm" onClick={() => setPaused(!paused)} disabled={state !== 'live'}>{paused ? <><Play /> {t('logs.resume')}</> : <><Pause /> {t('logs.pause')}</>}</button>
        )}
        <button className={`icon-btn sm${wrap ? ' on' : ''}`} data-tip={t('editor.wrap')} onClick={() => setWrap(!wrap)}><WrapText /></button>
        <button className="icon-btn sm" data-tip={t('logs.copy')} onClick={() => copyToClipboard(shown.join('\n'))}><Copy /></button>
        <button className="icon-btn sm" data-tip={t('logs.clear')} onClick={() => { setLines([]); held.current = [] }}><Eraser /></button>
      </div>
      {error && <div className="lost-bar danger selectable"><span>{error}</span></div>}
      <div className={`logs-body${wrap ? ' wrap' : ''}`} ref={body} onScroll={onScroll}>
        {shown.map((l, i) => <div key={i} className={`logs-line ${levelOf(l)}`}>{l || ' '}</div>)}
        {state === 'live' && !shown.length && <div className="logs-empty faint">{q || onlyIssues ? t('logs.noMatch') : t('logs.waiting')}</div>}
      </div>
      <div className="logs-status">
        <span>{t('logs.count', { n: lines.length })}</span>
        {(q || onlyIssues) && <span className="faint">· {t('logs.shown', { n: shown.length })}</span>}
        {paused && held.current.length > 0 && <span className="warn-text">· {t('logs.held', { n: held.current.length })}</span>}
        <div className="spacer" />
        {!stick && <button className="mini-btn" onClick={() => setStick(true)}><ArrowDownToLine /> {t('logs.toEnd')}</button>}
      </div>
    </div>
  )
}

