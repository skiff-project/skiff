import { t } from './i18n'
import { formatSize } from './format'
import type { DevSyncView, TaskRun } from './types'

/** The summary of a task run, in the language of the interface. */
export function runSummary(r: TaskRun): string {
  const st = r.stats
  if (r.status !== 'ok' || !st) return r.summary || ''
  if (st.commands) return t('tasks.sum.script', { n: st.commands })
  if (st.upToDate) return t('tasks.sum.upToDate', { n: st.compared })
  return t('tasks.sum.sync', { n: st.files, size: formatSize(st.bytes), deleted: st.deleted })
}

/** The outcome of the last sync between devices. */
export function devSyncSummary(state: DevSyncView['state']): string {
  const r = state.result
  if (!r) return state.summary || ''
  if (r.created) return t('devsync.sum.created')
  const parts: string[] = []
  if (r.sitesIn || r.sitesOut) parts.push(t('devsync.sum.sites', { in: r.sitesIn, out: r.sitesOut }))
  if (r.keysIn || r.keysOut) parts.push(t('devsync.sum.keys', { in: r.keysIn, out: r.keysOut }))
  if (r.prefsIn) parts.push(t('devsync.sum.prefs'))
  if (r.hostKeysIn) parts.push(t('devsync.sum.hostKeys', { n: r.hostKeysIn }))
  if (!parts.length) return r.uploaded ? t('devsync.sum.sent') : t('devsync.sum.upToDate')
  return t('devsync.sum.received', { what: parts.join(', ') })
}
