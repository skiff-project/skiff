import { call, downloadUrl, uploadFile } from './api'
import { get, getTab, set, toast, errorToast, updatePane, type SessionTab, type Side } from './store'
import type { Entry, TransferOptions, CustomCommand } from './types'
import { commandName, t } from './i18n'
import { basename, dirname, join, shellQuote } from './paths'
import { paneStyle, loadDir, openEditor, openDiff, openTerminal, refreshPane, setBottom, navigate } from './actions'
import { selectionOf, visibleEntries, compileMask } from './listing'
import { isTextLike, previewKind } from './fileicons'
import { confirm, prompt, ask } from '../components/dialogs/common'
import { formatSize } from './format'

export const other = (s: Side): Side => (s === 'left' ? 'right' : 'left')

export function sessionTab(tabId: string) {
  const tab = getTab<SessionTab>(tabId)
  return tab && tab.kind === 'session' ? tab : undefined
}

export function selected(tabId: string, side: Side): Entry[] {
  const tab = sessionTab(tabId)
  if (!tab) return []
  const p = tab.panes[side]
  return selectionOf(p, visibleEntries(p, get().prefs))
}

function describe(entries: Entry[]): string {
  if (entries.length === 1) return `"${entries[0].name}"`
  const dirs = entries.filter((e) => e.isDir).length
  const files = entries.length - dirs
  const parts = []
  if (files) parts.push(`${files} file${files > 1 ? 's' : ''}`)
  if (dirs) parts.push(`${dirs} folder${dirs > 1 ? 's' : ''}`)
  return parts.join(', ')
}

// ---------------------------------------------------------------------------
// Transfers

export async function transfer(src: string, paths: string[], dst: string, dstDir: string, opts: { move?: boolean; names?: Record<string, string>; options?: TransferOptions; label?: string } = {}) {
  try {
    let options = opts.options
    if (!options) {
      options = await call<TransferOptions>('transfer.defaults', { src, dst })
      options.move = !!opts.move
    }
    await call<string>('transfer.enqueue', { src, dst, paths, dstDir, names: opts.names, options, label: opts.label })
    const p = get().prefs
    if (p?.queue.autoPopup) setBottom(true, 'queue')
  } catch (e) {
    errorToast(t('common.error'), e)
  }
}

// copyOrMove implements F5/F6 between the two panels.
export async function copyOrMove(tabId: string, side: Side, move: boolean, forceDialog = false) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const entries = selected(tabId, side)
  if (!entries.length) return
  const src = tab.panes[side]
  const dstSide = other(side)
  let dst = tab.panes[dstSide]
  if (tab.layout === 'explorer') {
    // explorer layout: transfers go to the local panel's folder
    dst = tab.panes.left
  }
  const srcStyle = paneStyle(src.conn)
  const paths = entries.map((e) => join(srcStyle, src.path, e.name))
  const showDialog = forceDialog || get().prefs?.confirmTransfer
  if (src.conn === dst.conn && src.conn !== 'local' && !showDialog) {
    // same server: server-side copy or rename
    if (move) await moveWithin(src.conn, paths, dst.path)
    else await call('fs.duplicate', { conn: src.conn, paths, dstDir: dst.path }).catch((e) => errorToast(t('common.error'), e))
    return
  }
  if (showDialog) {
    const { transferDialog } = await import('../components/dialogs/TransferDialog')
    const r = await transferDialog({ src: src.conn, dst: dst.conn, dstDir: dst.path, entries, move })
    if (!r) return
    let names: Record<string, string> | undefined
    let dstDir = r.dstDir
    if (r.newName && entries.length === 1) {
      names = { [paths[0]]: r.newName }
    }
    if (src.conn === dst.conn && src.conn !== 'local') {
      if (move) return moveWithin(src.conn, paths, dstDir, names)
      return call('fs.duplicate', { conn: src.conn, paths, dstDir, names }).catch((e) => errorToast(t('common.error'), e))
    }
    return transfer(src.conn, paths, dst.conn, dstDir, { move, names, options: r.options })
  }
  return transfer(src.conn, paths, dst.conn, dst.path, { move })
}

async function moveWithin(conn: string, paths: string[], dstDir: string, names?: Record<string, string>) {
  try {
    if (names && paths.length === 1) {
      const st = paneStyle(conn)
      await call('fs.rename', { conn, from: paths[0], to: join(st, dstDir, names[paths[0]]) })
    } else await call('fs.move', { conn, paths, dstDir })
  } catch (e) {
    errorToast(t('common.error'), e)
  }
}

// dropEntries handles internal drag & drop between panels.
export async function dropEntries(data: { tabId: string; side: Side; names: string[]; conn: string; path: string }, tabId: string, side: Side, targetDir: string | null, move: boolean) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const dst = tab.panes[side]
  const dstDir = targetDir ?? dst.path
  const st = paneStyle(data.conn)
  const paths = data.names.map((n) => join(st, data.path, n))
  if (data.conn === dst.conn && paths.some((p) => p === dstDir || dirname(st, p) === dstDir)) {
    if (dirname(st, paths[0]) === dstDir) return
  }
  if (get().prefs?.confirmDragDrop) {
    const ok = await ask({ title: move ? t('transfer.moveTitle') : t('transfer.copyTitle'), message: `${describe(data.names.map((n) => ({ name: n } as Entry)))} → ${dstDir}` })
    if (!ok) return
  }
  if (data.conn === dst.conn && dst.conn !== 'local') {
    if (move) return moveWithin(dst.conn, paths, dstDir)
    return call('fs.duplicate', { conn: dst.conn, paths, dstDir }).catch((e) => errorToast(t('common.error'), e))
  }
  if (data.conn === 'local' && dst.conn === 'local') {
    if (move) return moveWithin('local', paths, dstDir)
  }
  return transfer(data.conn, paths, dst.conn, dstDir, { move })
}

// uploadDropped uploads files dropped from the operating system.
export async function uploadDropped(tabId: string, side: Side, dt: DataTransfer, targetDir?: string) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const pane = tab.panes[side]
  const dir = targetDir ?? pane.path
  const st = paneStyle(pane.conn)
  const files: { file: File; rel: string }[] = []
  const walk = async (entry: any, prefix: string): Promise<void> => {
    if (!entry) return
    if (entry.isFile) {
      const f: File = await new Promise((res, rej) => entry.file(res, rej))
      files.push({ file: f, rel: prefix + f.name })
    } else if (entry.isDirectory) {
      const reader = entry.createReader()
      let batch: any[] = []
      do {
        batch = await new Promise((res, rej) => reader.readEntries(res, rej))
        for (const e of batch) await walk(e, prefix + entry.name + '/')
      } while (batch.length > 0)
    }
  }
  const items = Array.from(dt.items || [])
  if (items.length && (items[0] as any).webkitGetAsEntry) {
    const entries = items.map((i) => (i as any).webkitGetAsEntry())
    for (const e of entries) await walk(e, '')
  } else {
    for (const f of Array.from(dt.files)) files.push({ file: f, rel: f.name })
  }
  if (!files.length) return
  const total = files.reduce((n, f) => n + f.file.size, 0)
  const tid = toast('info', t('ctx.upload'), `${t('size.files', { n: files.length })}, ${formatSize(total)}…`, { timeout: 0 })
  let done = 0
  let failed = 0
  let overwriteAll: boolean | null = null
  for (const { file, rel } of files) {
    let target = dir
    for (const part of rel.split('/')) target = join(st, target, part)
    try {
      await uploadFile(pane.conn, target, file, { mkdirs: true, overwrite: overwriteAll ?? false })
    } catch (e: any) {
      if (e?.code === 'exists') {
        if (overwriteAll === null) {
          const r = await confirm({ title: t('transfer.ow.overwrite'), message: `"${rel}" already exists. Overwrite?`, ok: t('transfer.ow.overwrite'), check: t('prompt.applyAll') })
          if (r?.checked) overwriteAll = !!r?.ok
          if (!r?.ok) {
            continue
          }
        } else if (!overwriteAll) continue
        try {
          await uploadFile(pane.conn, target, file, { mkdirs: true, overwrite: true })
        } catch (e2) {
          failed++
          errorToast(rel, e2)
          continue
        }
      } else {
        failed++
        errorToast(rel, e)
        continue
      }
    }
    done++
  }
  set((s) => ({ toasts: s.toasts.filter((x) => x.id !== tid) }))
  toast(failed ? 'warn' : 'ok', t('ctx.upload'), `${t('upload.done', { n: done })}${failed ? ` · ${t('upload.failed', { n: failed })}` : ''}`)
  refreshPane(tabId, side, true)
}

// ---------------------------------------------------------------------------
// Basic operations

export async function deleteSelected(tabId: string, side: Side, permanent = false) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const entries = selected(tabId, side)
  if (!entries.length) return
  const pane = tab.panes[side]
  const st = paneStyle(pane.conn)
  const conn = pane.conn === 'local' ? null : get().conns[pane.conn]
  const recycleAvailable = !permanent && (pane.conn === 'local' ? get().info?.mode === 'desktop' : !!conn?.recycleBin)
  if (get().prefs?.confirmDelete !== false || permanent) {
    const r = await confirm({
      title: t('dialog.deleteTitle'),
      message: (
        `${t('dialog.deleteMsg', { what: describe(entries) })}\n` +
        (recycleAvailable ? (pane.conn === 'local' ? '' : t('dialog.deleteRemoteRecycle', { dir: conn!.recycleBin! })) : t('dialog.deletePermanent'))
      ).trim(),
      ok: t('common.delete'),
      danger: true,
    })
    if (!r?.ok) return
  }
  const paths = entries.map((e) => join(st, pane.path, e.name))
  // move focus to the next item
  const vis = visibleEntries(pane, get().prefs)
  const lastIdx = vis.findIndex((e) => e.name === entries[entries.length - 1].name)
  const next = vis.slice(lastIdx + 1).find((e) => !entries.some((x) => x.name === e.name)) || vis.slice(0, lastIdx).reverse().find((e) => !entries.some((x) => x.name === e.name))
  updatePane(tabId, side, (p) => ({ entries: p.entries.filter((e) => !entries.some((x) => x.name === e.name)), selected: new Set(), focus: next?.name }))
  try {
    await call('fs.delete', { conn: pane.conn, paths, recycle: recycleAvailable })
  } catch (e) {
    errorToast(t('common.delete'), e)
  }
  refreshPane(tabId, side, true)
}

export function startRename(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const target = p.focus || [...p.selected][0]
  if (!target) return
  updatePane(tabId, side, () => ({ renaming: target, selected: new Set(), focus: target }))
}

export async function commitRename(tabId: string, side: Side, oldName: string, newName: string) {
  updatePane(tabId, side, () => ({ renaming: undefined }))
  newName = newName.trim()
  if (!newName || newName === oldName) return
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const st = paneStyle(p.conn)
  updatePane(tabId, side, (pp) => ({ entries: pp.entries.map((e) => (e.name === oldName ? { ...e, name: newName } : e)), focus: newName }))
  try {
    await call('fs.rename', { conn: p.conn, from: join(st, p.path, oldName), to: join(st, p.path, newName) })
  } catch (e) {
    errorToast(t('common.rename'), e)
    updatePane(tabId, side, (pp) => ({ entries: pp.entries.map((e) => (e.name === newName ? { ...e, name: oldName } : e)), focus: oldName }))
  }
  refreshPane(tabId, side, true)
}

export async function mkdir(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const name = await prompt({ title: t('dialog.mkdir'), label: t('dialog.mkdirName'), value: '' })
  if (!name) return
  const st = paneStyle(p.conn)
  try {
    await call('fs.mkdir', { conn: p.conn, path: join(st, p.path, name), mode: 0 })
    await loadDir(tabId, side, p.path, { history: 'none', focus: name.split(/[\\/]/)[0] })
  } catch (e) {
    errorToast(t('dialog.mkdir'), e)
  }
}

export async function newFile(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const name = await prompt({ title: t('dialog.newFile'), label: t('dialog.newFileName'), value: 'untitled.txt', selectStem: true })
  if (!name) return
  const st = paneStyle(p.conn)
  const path = join(st, p.path, name)
  try {
    await call('fs.createFile', { conn: p.conn, path, content: '' })
    await loadDir(tabId, side, p.path, { history: 'none', focus: name })
    openEditor(p.conn, path)
  } catch (e) {
    errorToast(t('dialog.newFile'), e)
  }
}

export async function newLink(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const st = paneStyle(p.conn)
  const sel = selected(tabId, side)[0]
  const { linkDialog } = await import('../components/dialogs/LinkDialog')
  const r = await linkDialog(sel ? join(st, p.path, sel.name) : '', sel ? sel.name + '.link' : '')
  if (!r) return
  try {
    await call('fs.link', { conn: p.conn, target: r.target, link: join(st, p.path, r.name), hard: r.hard })
  } catch (e) {
    errorToast(t('dialog.link'), e)
  }
}

export async function editEntry(tabId: string, side: Side, mode: 'auto' | 'internal' | 'external' = 'auto', entry?: Entry) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const e = entry || selected(tabId, side)[0]
  if (!e || e.isDir) return
  const path = join(paneStyle(p.conn), p.path, e.name)
  const prefs = get().prefs
  let m = mode
  if (m === 'auto') {
    m = prefs?.editor.default === 'external' ? 'external' : 'internal'
    const assoc = prefs?.editor.associations?.find((a) => compileMask(a.mask)(e))
    if (assoc) {
      if (assoc.command === 'internal') m = 'internal'
      else return editExternal(p.conn, path, assoc.command)
    }
  }
  if (m === 'external' && get().info?.mode !== 'web') return editExternal(p.conn, path, prefs?.editor.externalCmd || '')
  if (!isTextLike(e.name, e.size) && e.size > 2 * 1024 * 1024) {
    const ok = await ask({ title: t('ctx.edit'), message: t('editor.binary') })
    if (!ok) return
  }
  openEditor(p.conn, path)
}

export async function editExternal(conn: string, path: string, editor: string) {
  try {
    await call('edit.external', { conn, path, editor })
  } catch (e) {
    errorToast(t('ctx.editExternal'), e)
  }
}

export async function viewEntry(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const e = selected(tabId, side)[0]
  if (!e || e.isDir) return
  const path = join(paneStyle(p.conn), p.path, e.name)
  const kind = previewKind(e.name)
  if (kind && kind !== 'text' && kind !== 'markdown') {
    set({ previewOpen: true })
    return
  }
  openEditor(p.conn, path, true)
}

export async function openWithDefault(conn: string, path: string) {
  if (get().info?.mode === 'web') return saveToComputer(conn, path)
  try {
    await call('fs.open', { conn, path })
  } catch (e) {
    errorToast(t('ctx.openWith'), e)
  }
}

export function saveToComputer(conn: string, path: string) {
  const a = document.createElement('a')
  a.href = downloadUrl(conn, path)
  a.download = basename(paneStyle(conn), path)
  document.body.appendChild(a)
  a.click()
  a.remove()
}

// openEntry handles Enter / double-click.
export function openEntry(tabId: string, side: Side, e: Entry) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const st = paneStyle(p.conn)
  if (e.isDir) {
    const target = p.conn === 'local' && st === 'windows' && p.path === '' ? e.name : join(st, p.path, e.name)
    loadDir(tabId, side, target)
    return
  }
  const dc = get().prefs?.doubleClick || 'open'
  if (dc === 'copy') return copyOrMove(tabId, side, false)
  if (dc === 'edit' || (isTextLike(e.name, e.size) && e.size < 5 * 1024 * 1024)) return editEntry(tabId, side, 'auto', e)
  const kind = previewKind(e.name)
  if (kind && get().info?.mode === 'web') return set({ previewOpen: true })
  openWithDefault(p.conn, join(st, p.path, e.name))
}

export async function properties(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const entries = selected(tabId, side)
  const p = tab.panes[side]
  const { propertiesDialog } = await import('../components/dialogs/PropertiesDialog')
  const target = entries.length ? entries : [{ name: basename(paneStyle(p.conn), p.path) || p.path, isDir: true, size: 0, mode: 0, mtime: '' } as Entry]
  const dir = entries.length ? p.path : dirname(paneStyle(p.conn), p.path)
  const changed = await propertiesDialog(p.conn, dir, target)
  if (changed) refreshPane(tabId, side, true)
}

export async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast('ok', t('common.copied'), text.length > 120 ? text.slice(0, 120) + '…' : text, { timeout: 2000 })
  } catch {
    toast('error', t('common.error'), 'Clipboard not available')
  }
}

export function copyPaths(tabId: string, side: Side, kind: 'path' | 'name' | 'url') {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const st = paneStyle(p.conn)
  const entries = selected(tabId, side)
  const list = entries.length ? entries : [{ name: '' } as Entry]
  const lines = list.map((e) => {
    const full = e.name ? join(st, p.path, e.name) : p.path
    if (kind === 'name') return e.name || basename(st, p.path)
    if (kind === 'url' && p.conn !== 'local') {
      const c = get().conns[p.conn]
      const base = c?.url.replace(/\/[^/]*$/, '').replace(/\/$/, '') || ''
      return base.replace(/(:\/\/[^/]+).*/, '$1') + full.split('/').map(encodeURIComponent).join('/')
    }
    return full
  })
  copyToClipboard(lines.join('\n'))
}

export async function selectByMask(tabId: string, side: Side, select: boolean) {
  const mask = await prompt({ title: select ? t('pane.selectMask') : t('pane.unselectMask'), label: t('dialog.selectMask'), value: '*.*', mono: true })
  if (!mask) return
  const m = compileMask(mask === '*.*' ? '*' : mask, { dirsByName: true })
  const tab = sessionTab(tabId)
  if (!tab) return
  const vis = visibleEntries(tab.panes[side], get().prefs)
  updatePane(tabId, side, (p) => {
    const s = new Set(p.selected)
    for (const e of vis) if (m(e)) (select ? s.add(e.name) : s.delete(e.name))
    return { selected: s }
  })
}

export function selectAll(tabId: string, side: Side, mode: 'all' | 'none' | 'invert' | 'sameExt') {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const vis = visibleEntries(p, get().prefs)
  let s = new Set<string>()
  if (mode === 'all') s = new Set(vis.map((e) => e.name))
  else if (mode === 'invert') s = new Set(vis.filter((e) => !p.selected.has(e.name)).map((e) => e.name))
  else if (mode === 'sameExt') {
    const f = vis.find((e) => e.name === p.focus)
    if (!f) return
    const x = f.name.includes('.') ? f.name.slice(f.name.lastIndexOf('.')).toLowerCase() : ''
    s = new Set(vis.filter((e) => !e.isDir && (e.name.includes('.') ? e.name.slice(e.name.lastIndexOf('.')).toLowerCase() : '') === x).map((e) => e.name))
  }
  updatePane(tabId, side, () => ({ selected: s }))
}

// compareDirs marks differences between both panels (WinSCP "Compare directories").
export function compareDirs(tabId: string) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const { left, right } = tab.panes
  if (left.compare || right.compare) {
    updatePane(tabId, 'left', () => ({ compare: undefined }))
    updatePane(tabId, 'right', () => ({ compare: undefined }))
    return
  }
  const prefs = get().prefs
  const prec = (c: string) => (c === 'local' ? 1 : Math.max(1, (get().conns[c]?.caps.mtimePrecision || 1e9) / 1e9))
  const p = Math.max(prec(left.conn), prec(right.conn)) * 1000
  const lm = new Map(left.entries.filter((e) => prefs?.showHidden || !e.hidden).map((e) => [e.name, e]))
  const rm = new Map(right.entries.filter((e) => prefs?.showHidden || !e.hidden).map((e) => [e.name, e]))
  const lc: Record<string, any> = {}
  const rc: Record<string, any> = {}
  let n = 0
  for (const [name, a] of lm) {
    const b = rm.get(name)
    if (!b) {
      lc[name] = 'missing'
      n++
      continue
    }
    if (a.isDir || b.isDir) continue
    const ta = Math.floor(new Date(a.mtime).getTime() / p)
    const tb = Math.floor(new Date(b.mtime).getTime() / p)
    if (ta > tb) {
      lc[name] = 'newer'
      rc[name] = 'older'
      n++
    } else if (tb > ta) {
      rc[name] = 'newer'
      lc[name] = 'older'
      n++
    } else if (a.size !== b.size) {
      lc[name] = rc[name] = 'differs'
      n++
    }
  }
  for (const [name] of rm) if (!lm.has(name)) {
    rc[name] = 'missing'
    n++
  }
  updatePane(tabId, 'left', () => ({ compare: lc, selected: new Set(Object.keys(lc).filter((k) => lc[k] !== 'older')) }))
  updatePane(tabId, 'right', () => ({ compare: rc, selected: new Set(Object.keys(rc).filter((k) => rc[k] !== 'older')) }))
  toast(n ? 'info' : 'ok', n ? t('compare.result', { n }) : t('compare.none'), undefined, { timeout: 2500 })
}

export async function calcSize(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const entries = selected(tabId, side)
  if (!entries.length) return
  const st = paneStyle(p.conn)
  const id = toast('info', t('ctx.calcSize'), t('common.loading'), { timeout: 0 })
  try {
    const r = await call<{ bytes: number; files: number; dirs: number }>('fs.size', { conn: p.conn, paths: entries.map((e) => join(st, p.path, e.name)) })
    set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) }))
    toast('info', `${formatSize(r.bytes, get().prefs?.sizeFormat)}`, `${t('size.bytes', { n: r.bytes.toLocaleString() })} · ${t('size.files', { n: r.files })} · ${t('size.dirs', { n: r.dirs })}`, { timeout: 8000 })
    // show folder sizes in the list
    if (entries.length === 1 && entries[0].isDir) updatePane(tabId, side, (pp) => ({ entries: pp.entries.map((e) => (e.name === entries[0].name ? { ...e, size: r.bytes } : e)) }))
  } catch (e) {
    set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) }))
    errorToast(t('ctx.calcSize'), e)
  }
}

export async function moveToFolder(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const entries = selected(tabId, side)
  if (!entries.length) return
  const dst = await prompt({ title: t('dialog.moveTo'), label: t('dialog.goToLabel'), value: p.path, mono: true })
  if (!dst) return
  const st = paneStyle(p.conn)
  await moveWithin(p.conn, entries.map((e) => join(st, p.path, e.name)), dst)
}

export async function goToDialog(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const v = await prompt({ title: t('dialog.goTo'), label: t('dialog.goToLabel'), value: p.path, mono: true })
  if (v) navigate(tabId, side, v)
}

export function diffWithOther(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const a = selected(tabId, side)[0]
  if (!a || a.isDir) return
  const o = tab.panes[other(side)]
  const bName = o.selected.size === 1 ? [...o.selected][0] : o.focus && !o.entries.find((e) => e.name === o.focus)?.isDir ? o.focus : a.name
  const p = tab.panes[side]
  const left = { conn: p.conn, path: join(paneStyle(p.conn), p.path, a.name) }
  const right = { conn: o.conn, path: join(paneStyle(o.conn), o.path, bName!) }
  if (side === 'right') openDiff(right, left)
  else openDiff(left, right)
}

export function terminalHere(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const conn = p.conn
  if (conn !== 'local' && !get().conns[conn]?.caps.shell) {
    toast('warn', t('term.title'), t('term.notSupported'))
    return
  }
  openTerminal(conn, p.path)
}

// ---------------------------------------------------------------------------
// Custom commands (WinSCP patterns: ! !& !/ !@ !U !?prompt?default!)

export async function runCustomCommand(tabId: string, side: Side, cmd: CustomCommand) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const conn = cmd.local ? 'local' : p.conn
  const c = get().conns[p.conn]
  const entries = selected(tabId, side)
  let template = cmd.command
  // parameter prompts
  const re = /!\?([^?]*)\?([^!]*)!/g
  const prompts = [...template.matchAll(re)]
  for (const m of prompts) {
    const v = await prompt({ title: commandName(cmd), label: m[1] || t('cmd.promptTitle'), value: m[2] })
    if (v === undefined) return
    template = template.replace(m[0], v)
  }
  const expand = (files: Entry[]) =>
    template
      .replace(/!&/g, files.map((e) => shellQuote(e.name)).join(' '))
      .replace(/!\//g, p.path.endsWith('/') ? p.path : p.path + '/')
      .replace(/!@/g, c?.host || 'localhost')
      .replace(/!U/g, c?.user || '')
      .replace(/!(?![\w&/@?])/g, files[0]?.name ?? '')
  const runs = cmd.perFile ? entries.map((e) => expand([e])) : [expand(entries)]
  const outputs: string[] = []
  let code = 0
  for (const r of runs) {
    try {
      const res = await call<{ stdout: string; stderr: string; code: number }>('fs.exec', { conn, command: r, cwd: p.path })
      outputs.push(`$ ${r}\n${res.stdout}${res.stderr}`)
      if (res.code !== 0) code = res.code
    } catch (e: any) {
      outputs.push(`$ ${r}\n${e?.message || e}`)
      code = -1
    }
  }
  refreshPane(tabId, side, true)
  if (cmd.showOutput || code !== 0) {
    const { outputDialog } = await import('../components/dialogs/OutputDialog')
    outputDialog(commandName(cmd), outputs.join('\n'), code)
  }
}

export async function runCommandDialog(tabId: string, side: Side) {
  const tab = sessionTab(tabId)
  if (!tab) return
  const p = tab.panes[side]
  const command = await prompt({ title: t('cmd.run'), label: t('cmd.placeholder', { dir: p.path }), value: '', mono: true })
  if (!command) return
  return runCustomCommand(tabId, side, { id: 'adhoc', name: command, command, local: false, perFile: false, applyDirs: true, recursive: false, showOutput: true })
}
