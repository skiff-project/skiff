import { get, toast, type Side } from './store'
import { paneStyle } from './actions'
import { selected, sessionTab, transfer } from './fileops'
import { call } from './api'
import { join } from './paths'
import { t } from './i18n'

// Internal file clipboard (Ctrl+C / Ctrl+X / Ctrl+V between panels/tabs).
interface Clip {
  conn: string
  dir: string
  names: string[]
  cut: boolean
}

let clip: Clip | null = null

export function hasClip() {
  return !!clip
}

export function clipboardCopy(tabId: string, side: Side, cut: boolean) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const names = selected(tabId, side).map((e) => e.name)
  if (!names.length) return
  clip = { conn: p.conn, dir: p.path, names, cut }
  toast('info', cut ? t('common.cut') : t('common.copy'), names.length === 1 ? names[0] : t('common.items', { n: names.length }), { timeout: 1500 })
  // also put paths on the system clipboard
  import('./textclip').then(({ writeClipboard }) => writeClipboard(names.map((n) => join(paneStyle(p.conn), p.path, n)).join('\n')))
}

export async function clipboardPaste(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab || !clip) return
  const p = tab.panes[side]
  const c = clip
  const st = paneStyle(c.conn)
  const paths = c.names.map((n) => join(st, c.dir, n))
  if (c.conn === p.conn && c.dir === p.path && !c.cut) {
    if (p.conn === 'local' || get().conns[p.conn]?.caps.serverCopy) {
      await call('fs.duplicate', { conn: p.conn, paths, dstDir: p.path }).catch((e) => toast('error', t('common.error'), e.message))
    }
    return
  }
  if (c.conn === p.conn && c.cut) {
    await call('fs.move', { conn: p.conn, paths, dstDir: p.path }).catch((e) => toast('error', t('common.error'), e.message))
    clip = null
    return
  }
  if (c.conn === p.conn && p.conn !== 'local') {
    await call('fs.duplicate', { conn: p.conn, paths, dstDir: p.path }).catch((e) => toast('error', t('common.error'), e.message))
    return
  }
  await transfer(c.conn, paths, p.conn, p.path, { move: c.cut })
  if (c.cut) clip = null
}
