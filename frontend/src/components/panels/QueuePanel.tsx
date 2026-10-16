import { ArrowDown, ArrowRight, ArrowUp, CheckCircle2, ChevronDown, ChevronRight, CircleAlert, CircleSlash, Loader2, Pause, Play, RotateCcw, Trash2, X } from 'lucide-react'
import { useState } from 'react'
import { useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { formatDuration, formatSize, formatSpeed } from '../../lib/format'
import { t } from '../../lib/i18n'
import type { Job } from '../../lib/types'
import { showMenu } from '../ui/Menu'

export function QueuePanel() {
  const jobs = useStore((s) => s.jobs)
  const fmt = useStore((s) => s.prefs?.sizeFormat || 'iec')
  const finished = jobs.filter((j) => ['done', 'failed', 'cancelled', 'partial'].includes(j.status)).length
  if (!jobs.length) {
    return (
      <div className="empty">
        <ArrowDown className="big" />
        <h4>{t('queue.empty')}</h4>
        <p>{t('queue.emptyHint')}</p>
      </div>
    )
  }
  return (
    <div className="queue">
      <div className="queue-head">
        <span className="muted">{t('queue.count', { n: jobs.length })}</span>
        <div className="spacer" />
        <button className="btn sm ghost" disabled={!finished} onClick={() => call('transfer.clear')}>
          <Trash2 /> {t('queue.clearDone')}
        </button>
      </div>
      <div className="queue-list">
        {jobs.map((j, i) => (
          <JobRow key={j.id} job={j} first={i === 0} last={i === jobs.length - 1} fmt={fmt} />
        ))}
      </div>
    </div>
  )
}

function StatusIcon({ s }: { s: Job['status'] }) {
  switch (s) {
    case 'done':
      return <CheckCircle2 className="ok-text" />
    case 'failed':
      return <CircleAlert className="danger-text" />
    case 'partial':
      return <CircleAlert className="warn-text" />
    case 'cancelled':
      return <CircleSlash className="faint" />
    case 'paused':
      return <Pause className="warn-text" />
    case 'queued':
      return <Loader2 className="faint" />
    default:
      return <Loader2 className="spin accent-text" />
  }
}

function JobRow({ job: j, first, last, fmt }: { job: Job; first: boolean; last: boolean; fmt: any }) {
  const [open, setOpen] = useState(false)
  const active = ['running', 'scanning', 'queued', 'paused'].includes(j.status)
  const pct = j.totalBytes > 0 ? Math.min(100, (j.doneBytes / j.totalBytes) * 100) : j.status === 'done' ? 100 : 0
  const menu = (e: React.MouseEvent) => {
    e.preventDefault()
    showMenu(e.clientX, e.clientY, [
      { label: j.status === 'paused' ? t('common.resume') : t('common.pause'), icon: j.status === 'paused' ? <Play /> : <Pause />, hidden: !active, onClick: () => call('transfer.pause', { id: j.id, value: j.status !== 'paused' }) },
      { label: t('queue.moveUp'), icon: <ArrowUp />, disabled: first, onClick: () => call('transfer.move', { id: j.id, delta: -1 }) },
      { label: t('queue.moveDown'), icon: <ArrowDown />, disabled: last, onClick: () => call('transfer.move', { id: j.id, delta: 1 }) },
      { label: t('queue.retryFailed'), icon: <RotateCcw />, hidden: !(j.status === 'partial' || j.status === 'failed'), onClick: () => call('transfer.retry', { id: j.id }) },
      { separator: true },
      { label: t('queue.cancel'), icon: <X />, danger: true, hidden: !active, onClick: () => call('transfer.cancel', { id: j.id }) },
      { label: t('queue.remove'), icon: <Trash2 />, hidden: active, onClick: () => call('transfer.remove', { id: j.id }) },
    ])
  }
  const errors = j.errors?.length || 0
  return (
    <div className={`job ${j.status}`} onContextMenu={menu}>
      <div className="job-main">
        <button className="icon-btn sm" data-tip={t('queue.details')} onClick={() => setOpen(!open)} disabled={!errors && !j.active.length && !j.error}>
          {open ? <ChevronDown /> : <ChevronRight />}
        </button>
        <span className="job-status"><StatusIcon s={j.status} /></span>
        <div className="job-info">
          <div className="job-title">
            <span className="ellipsis strong">{j.label}</span>
            <span className="job-route faint">
              {j.srcTitle} <ArrowRight /> {j.dstTitle}
              {j.move && <span className="badge">{t('fkey.move')}</span>}
            </span>
          </div>
          <div className="progress-row">
            <div className={`progress ${j.status === 'done' ? 'ok' : j.status === 'failed' ? 'danger' : ''}${j.status === 'scanning' ? ' indeterminate' : ''}`}>
              <div style={{ width: `${pct}%` }} />
            </div>
          </div>
          <div className="job-meta tnum">
            <span>{t(`queue.status.${j.status}`)}</span>
            {j.totalFiles > 0 && <span>{t('queue.files', { done: j.doneFiles, total: j.totalFiles })}</span>}
            <span>
              {formatSize(j.doneBytes, fmt)} / {formatSize(j.totalBytes, fmt)}
            </span>
            {j.status === 'running' && <span>{formatSpeed(j.speed)}</span>}
            {j.status === 'running' && j.eta > 0 && <span>{t('queue.eta', { eta: formatDuration(j.eta) })}</span>}
            {j.skippedFiles > 0 && <span className="faint">{t('queue.skipped', { n: j.skippedFiles })}</span>}
            {errors > 0 && <span className="danger-text">{t('queue.errors', { n: j.failedFiles })}</span>}
            {j.error && <span className="danger-text ellipsis">{j.error}</span>}
          </div>
        </div>
        <div className="job-actions">
          {active && (
            <button className="icon-btn sm" onClick={() => call('transfer.pause', { id: j.id, value: j.status !== 'paused' })} data-tip={j.status === 'paused' ? t('common.resume') : t('common.pause')}>
              {j.status === 'paused' ? <Play /> : <Pause />}
            </button>
          )}
          {(j.status === 'partial' || j.status === 'failed') && (
            <button className="icon-btn sm" onClick={() => call('transfer.retry', { id: j.id })} data-tip={t('queue.retryFailed')}>
              <RotateCcw />
            </button>
          )}
          <button className="icon-btn sm" onClick={() => call(active ? 'transfer.cancel' : 'transfer.remove', { id: j.id })} data-tip={active ? t('queue.cancel') : t('queue.remove')}>
            <X />
          </button>
        </div>
      </div>
      {open && (
        <div className="job-details">
          {j.active.map((a) => (
            <div key={a.name} className="job-file">
              <span className="ellipsis">{a.name}</span>
              {!!a.parts && <span className="badge accent" data-tip={t('queue.partsTip')}>{t('queue.parts', { n: a.parts })}</span>}
              <div className="progress" style={{ width: 120 }}>
                <div style={{ width: `${a.size ? (a.done / a.size) * 100 : 0}%` }} />
              </div>
              <span className="tnum faint">{formatSize(a.done, fmt)} / {formatSize(a.size, fmt)}</span>
            </div>
          ))}
          {j.errors?.map((e, i) => (
            <div key={i} className="job-file danger-text selectable">
              <CircleAlert /> <span className="mono ellipsis">{e.path}</span> <span className="ellipsis">{e.error}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
