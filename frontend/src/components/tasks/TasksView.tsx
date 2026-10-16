import { useEffect, useState } from 'react'
import {
  CalendarClock, ChevronDown, ChevronRight, Copy, FileCode2, Play, Plus, RefreshCcwDot, Save, Square, Trash2,
} from 'lucide-react'
import { call } from '../../lib/api'
import { errorToast, toast, useStore } from '../../lib/store'
import { reloadTasks } from '../../lib/actions'
import { t } from '../../lib/i18n'
import { formatDate, formatDuration } from '../../lib/format'
import type { SyncRequest, Task, TaskRun, TaskView } from '../../lib/types'
import { SyncOptionsFields } from '../dialogs/SyncDialog'
import { ask } from '../dialogs/common'
import { showMenuAt } from '../ui/Menu'
import { copyToClipboard } from '../../lib/fileops'
import { runSummary } from '../../lib/summaries'

const DAYS = [1, 2, 3, 4, 5, 6, 0] // Monday first

function newTask(kind: Task['kind'], patch: Partial<Task> = {}): Task {
  const sync: SyncRequest = { localConn: 'local', localDir: '', remoteConn: '', remoteDir: '', direction: 'remote', mode: 'standard', criteria: 'time', delete: false, existingOnly: false, fileMask: '', excludeHidden: false }
  return {
    id: '', name: '', enabled: true, kind, catchUp: false, notify: 'failure',
    schedule: { kind: 'daily', at: '02:00' },
    ...(kind === 'sync' ? { sync } : { script: 'open mon-site\n# commandes WinSCP : put, get, synchronize, rm…\nexit\n' }),
    ...patch,
  }
}

/** A short description of a schedule. */
export function scheduleText(s: Task['schedule']): string {
  switch (s.kind) {
    case 'interval':
      return t('tasks.every', { n: s.every || 0 })
    case 'daily':
      return t('tasks.dailyAt', { at: s.at || '' })
    case 'weekly':
      return t('tasks.weeklyAt', { days: (s.days || []).slice().sort((a, b) => DAYS.indexOf(a) - DAYS.indexOf(b)).map((d) => t(`tasks.day.${d}`)).join(', '), at: s.at || '' })
    case 'cron':
      return `cron ${s.cron || ''}`
  }
  return t('tasks.manual')
}

function StatusDot({ status, running }: { status?: string; running?: boolean }) {
  if (running) return <span className="spinner" style={{ width: 10, height: 10, borderWidth: 2 }} />
  const cls = status === 'ok' ? 'ok' : status === 'failed' ? 'danger' : status === 'cancelled' ? 'warn' : ''
  return <span className={`dot ${cls}`} />
}

export function TasksView() {
  const tasks = useStore((s) => s.tasks)
  const sites = useStore((s) => s.sites)
  const draftIn = useStore((s) => s.taskDraft)
  const [sel, setSel] = useState<string | null>(null)
  const [draft, setDraft] = useState<Task | null>(null)

  useEffect(() => {
    if (!tasks) reloadTasks()
  }, [])
  useEffect(() => {
    if (!draftIn) return
    setSel(null)
    setDraft(newTask(draftIn.kind || 'sync', draftIn as Partial<Task>))
    useStore.setState({ taskDraft: null })
  }, [draftIn])

  const selected = tasks?.find((x) => x.id === sel)
  const editing: Task | null = draft ?? selected ?? null
  const view = selected

  const newMenu = (el: HTMLElement) =>
    showMenuAt(el, [
      { label: t('tasks.newSync'), icon: <RefreshCcwDot />, onClick: () => { setSel(null); setDraft(newTask('sync')) } },
      { label: t('tasks.newScript'), icon: <FileCode2 />, onClick: () => { setSel(null); setDraft(newTask('script')) } },
    ])

  return (
    <div className="sites tasks">
      <aside className="sites-side">
        <div className="sites-side-head">
          <h3 className="grow tasks-title"><CalendarClock /> {t('tasks.title')}</h3>
          <button className="icon-btn" data-tip={t('tasks.new')} onClick={(e) => newMenu(e.currentTarget)}><Plus /></button>
        </div>
        <div className="sites-tree">
          {tasks && tasks.length === 0 && !draft && (
            <div className="empty" style={{ height: 'auto', paddingTop: 40 }}>
              <CalendarClock className="big" />
              <h4>{t('tasks.empty')}</h4>
              <p>{t('tasks.emptyHint')}</p>
              <button className="btn sm primary" onClick={(e) => newMenu(e.currentTarget)}><Plus /> {t('tasks.new')}</button>
            </div>
          )}
          {(tasks || []).map((tk) => (
            <div key={tk.id} className={`tree-site${sel === tk.id && !draft ? ' sel' : ''}${tk.enabled ? '' : ' disabled'}`} style={{ paddingLeft: 10 }} onClick={() => { setDraft(null); setSel(tk.id) }} data-task={tk.name}>
              <StatusDot status={tk.last?.status} running={tk.running} />
              <div className="grow" style={{ minWidth: 0 }}>
                <div className="ellipsis site-name">{tk.name}</div>
                <div className="ellipsis site-sub">
                  {tk.enabled ? (tk.next ? t('tasks.nextAt', { when: formatDate(tk.next, 'short') }) : scheduleText(tk.schedule)) : t('tasks.disabled')}
                </div>
              </div>
              <span className="proto-badge">{tk.kind === 'sync' ? 'SYNC' : 'SCRIPT'}</span>
            </div>
          ))}
        </div>
        <div className="tasks-foot faint">{t('tasks.whileOpen')}</div>
      </aside>
      <main className="sites-main">
        {editing ? (
          <TaskEditor
            key={draft ? 'draft' : editing.id}
            task={editing}
            view={draft ? undefined : view}
            isNew={!!draft}
            sites={sites}
            onSaved={(v) => { setDraft(null); setSel(v.id) }}
            onDeleted={() => { setDraft(null); setSel(null) }}
            onCancel={() => setDraft(null)}
          />
        ) : (
          <div className="empty"><CalendarClock className="big" /><p>{t('tasks.pick')}</p></div>
        )}
      </main>
    </div>
  )
}

function TaskEditor({ task, view, isNew, sites, onSaved, onDeleted, onCancel }: {
  task: Task; view?: TaskView; isNew: boolean; sites: import('../../lib/types').SiteView[]
  onSaved: (v: TaskView) => void; onDeleted: () => void; onCancel: () => void
}) {
  const [s, setS] = useState<Task>(task)
  const [dirty, setDirty] = useState(isNew)
  const [busy, setBusy] = useState(false)
  const [runs, setRuns] = useState<TaskRun[]>([])
  const [open, setOpen] = useState<string | null>(null)
  const up = (p: Partial<Task>) => {
    setS((cur) => ({ ...cur, ...p }))
    setDirty(true)
  }
  const sch = s.schedule
  const upSch = (p: Partial<Task['schedule']>) => up({ schedule: { ...sch, ...p } })
  const upSync = (p: Partial<SyncRequest>) => up({ sync: { ...(s.sync as SyncRequest), ...p } })
  const remoteSites = sites

  const loadRuns = () => {
    if (!isNew && task.id) call<TaskRun[]>('tasks.runs', { id: task.id }).then(setRuns).catch(() => {})
  }
  useEffect(loadRuns, [task.id, view?.last?.id, view?.running])

  const save = async (): Promise<TaskView | null> => {
    setBusy(true)
    try {
      const v = await call<TaskView>('tasks.save', s)
      setDirty(false)
      await reloadTasks()
      onSaved(v)
      return v
    } catch (e) {
      errorToast(t('common.save'), e)
      return null
    } finally {
      setBusy(false)
    }
  }
  const run = async () => {
    let id = s.id
    if (dirty || !id) {
      const v = await save()
      if (!v) return
      id = v.id
    }
    try {
      await call('tasks.run', { id })
      toast('info', t('tasks.started'), s.name)
      reloadTasks()
    } catch (e) {
      errorToast(t('tasks.runNow'), e)
    }
  }
  const del = async () => {
    if (!(await ask({ title: t('common.delete'), message: t('tasks.deleteConfirm', { name: s.name }), danger: true, ok: t('common.delete') }))) return
    await call('tasks.delete', { id: s.id })
    await reloadTasks()
    onDeleted()
  }
  const cli = `skiff task run "${s.name}"`

  return (
    <div className="site-editor task-editor">
      <div className="se-head">
        <div className="se-title">
          {s.kind === 'sync' ? <RefreshCcwDot style={{ width: 20, color: 'var(--accent-text)' }} /> : <FileCode2 style={{ width: 20, color: 'var(--accent-text)' }} />}
          <input className="se-name" value={s.name} placeholder={t('tasks.namePlaceholder')} onChange={(e) => up({ name: e.target.value })} autoFocus={isNew} />
          {dirty && !isNew && <span className="badge warn">{t('editor.modified')}</span>}
          {isNew && <span className="badge">{t('sites.unsaved')}</span>}
        </div>
        <div className="row">
          {!isNew && <button className="icon-btn" data-tip={t('common.delete')} onClick={del}><Trash2 /></button>}
          {isNew && <button className="btn" onClick={onCancel}>{t('common.cancel')}</button>}
          <button className="btn" disabled={busy || (!dirty && !isNew)} onClick={save}><Save /> {t('common.save')}</button>
          {view?.running
            ? <button className="btn danger" onClick={() => call('tasks.cancel', { id: s.id })}><Square /> {t('tasks.stop')}</button>
            : <button className="btn primary" disabled={busy} onClick={run}><Play /> {t('tasks.runNow')}</button>}
        </div>
      </div>
      <div className="sshcfg-body task-body">
        <label className="switch"><input type="checkbox" checked={s.enabled} onChange={(e) => up({ enabled: e.target.checked })} /> {t('tasks.enabled')}</label>

        <section className="card sshcfg-card">
          <h4>{s.kind === 'sync' ? t('tasks.what.sync') : t('tasks.what.script')}</h4>
          {s.kind === 'sync' ? (
            <div className="form">
              <div className="field">
                <label>{t('tasks.site')}</label>
                <select className="select" value={s.siteId || ''} onChange={(e) => up({ siteId: e.target.value })}>
                  <option value="">—</option>
                  {remoteSites.map((x) => <option key={x.id} value={x.id}>{x.name || x.host}</option>)}
                </select>
              </div>
              <div className="field-row">
                <div className="field"><label>{t('sync.localDir')}</label><input className="input mono" value={s.sync?.localDir || ''} placeholder="~/Projets/site" onChange={(e) => upSync({ localDir: e.target.value })} spellCheck={false} /></div>
                <div className="field"><label>{t('sync.remoteDir')}</label><input className="input mono" value={s.sync?.remoteDir || ''} placeholder={t('tasks.remoteDirDefault')} onChange={(e) => upSync({ remoteDir: e.target.value })} spellCheck={false} /></div>
              </div>
              <SyncOptionsFields req={s.sync as SyncRequest} up={upSync} />
            </div>
          ) : (
            <div className="field">
              <textarea className="textarea mono" rows={9} value={s.script || ''} onChange={(e) => up({ script: e.target.value })} spellCheck={false} />
              <span className="hint">{t('tasks.scriptHint')}</span>
            </div>
          )}
        </section>

        <section className="card sshcfg-card">
          <h4>{t('tasks.when')}</h4>
          <div className="form">
            <div className="segmented">
              {(['manual', 'interval', 'daily', 'weekly', 'cron'] as const).map((k) => (
                <button key={k} className={sch.kind === k ? 'on' : ''} onClick={() => upSch({ kind: k, every: sch.every || 60, at: sch.at || '02:00', days: sch.days?.length ? sch.days : [1, 2, 3, 4, 5], cron: sch.cron || '0 2 * * *' })}>{t(`tasks.kind.${k}`)}</button>
              ))}
            </div>
            {sch.kind === 'interval' && (
              <div className="field"><label>{t('tasks.everyMinutes')}</label><input className="input tnum" type="number" min={1} style={{ width: 140 }} value={sch.every || 60} onChange={(e) => upSch({ every: Math.max(1, parseInt(e.target.value, 10) || 1) })} /></div>
            )}
            {(sch.kind === 'daily' || sch.kind === 'weekly') && (
              <div className="field"><label>{t('tasks.at')}</label><input className="input tnum" type="time" style={{ width: 140 }} value={sch.at || '02:00'} onChange={(e) => upSch({ at: e.target.value })} /></div>
            )}
            {sch.kind === 'weekly' && (
              <div className="row day-picker">
                {DAYS.map((d) => (
                  <label key={d} className={`chip${sch.days?.includes(d) ? ' on' : ''}`}>
                    <input type="checkbox" checked={!!sch.days?.includes(d)} onChange={(e) => upSch({ days: e.target.checked ? [...(sch.days || []), d] : (sch.days || []).filter((x) => x !== d) })} />
                    {t(`tasks.day.${d}`)}
                  </label>
                ))}
              </div>
            )}
            {sch.kind === 'cron' && (
              <div className="field"><label>{t('tasks.cron')}</label><input className="input mono" style={{ width: 260 }} value={sch.cron || ''} placeholder="*/30 8-18 * * 1-5" onChange={(e) => upSch({ cron: e.target.value })} spellCheck={false} /><span className="hint">{t('tasks.cronHint')}</span></div>
            )}
            {sch.kind !== 'manual' && (
              <label className="switch"><input type="checkbox" checked={s.catchUp} onChange={(e) => up({ catchUp: e.target.checked })} /> {t('tasks.catchUp')}</label>
            )}
            <div className="field">
              <label>{t('tasks.notify')}</label>
              <select className="select" style={{ width: 260 }} value={s.notify || 'failure'} onChange={(e) => up({ notify: e.target.value as Task['notify'] })}>
                <option value="failure">{t('tasks.notify.failure')}</option>
                <option value="always">{t('tasks.notify.always')}</option>
                <option value="never">{t('tasks.notify.never')}</option>
              </select>
            </div>
            {!isNew && view && (
              <div className="faint">{view.enabled && view.next ? t('tasks.nextAt', { when: formatDate(view.next, 'short') }) : view.enabled ? scheduleText(view.schedule) : t('tasks.disabled')}</div>
            )}
            <div className="alert info task-cli">
              <CalendarClock />
              <div className="grow" style={{ minWidth: 0 }}>
                <div>{t('tasks.cliHint')}</div>
                <div className="mono ellipsis">{cli}</div>
              </div>
              <button className="icon-btn sm" data-tip={t('common.copy')} onClick={() => copyToClipboard(cli)}><Copy /></button>
            </div>
          </div>
        </section>

        {!isNew && (
          <section className="card sshcfg-card">
            <h4>{t('tasks.history')}</h4>
            {runs.length === 0 && <p className="faint" style={{ margin: 0 }}>{t('tasks.noRun')}</p>}
            <div className="task-runs">
              {runs.map((r) => (
                <div key={r.id} className="task-run" data-status={r.status}>
                  <div className="task-run-head" onClick={() => setOpen(open === r.id ? null : r.id)}>
                    {open === r.id ? <ChevronDown /> : <ChevronRight />}
                    <StatusDot status={r.status} running={r.status === 'running'} />
                    <span className="tnum">{formatDate(r.start, 'short')}</span>
                    <span className="faint">{t(`tasks.trigger.${r.trigger.replace(' ', '-')}`)}</span>
                    <span className="ellipsis grow">{runSummary(r)}</span>
                    {r.end && <span className="faint tnum">{formatDuration((new Date(r.end).getTime() - new Date(r.start).getTime()) / 1000)}</span>}
                  </div>
                  {open === r.id && <pre className="task-log selectable">{(r.log || []).join('\n') || '—'}</pre>}
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </div>
  )
}
