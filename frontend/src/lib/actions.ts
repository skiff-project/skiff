import { call, EventStream, setUnauthorizedHandler } from './api'
import {
  get, getTab, newPane, set, toast, errorToast, uid, updatePane, updateTab,
  type EditorTab, type PaneState, type SessionTab, type Side, type Tab, type TerminalTab,
} from './store'
import type { AppInfo, ConnInfo, Entry, Job, Prefs, PromptEvent, SiteView, TabState } from './types'
import { setLang, t } from './i18n'
import { basename, dirname, join, normalize, rel as relPath, tildify } from './paths'
import { visibleEntries } from './listing'

// ---------------------------------------------------------------------------
// Boot & events

let stream: EventStream | null = null

export async function boot() {
  setUnauthorizedHandler(() => set({ authed: false }))
  try {
    const st = await fetch('/api/auth/status', { credentials: 'same-origin' }).then((r) => r.json())
    if (!st.authenticated) {
      set({ authed: false })
      return
    }
  } catch {
    set({ authed: false, online: false })
    return
  }
  set({ authed: true })
  const [info, prefs, sites, conns, jobs, terms, tunnels, watches, edits, prompts] = await Promise.all([
    call<AppInfo>('app.info'),
    call<Prefs>('prefs.get'),
    call<{ folders: string[]; sites: SiteView[] }>('sites.list'),
    call<ConnInfo[]>('conn.list'),
    call<Job[]>('transfer.list'),
    call('term.list'),
    call('tunnel.list'),
    call('watch.list'),
    call('edit.list'),
    call<PromptEvent[]>('prompt.pending'),
  ])
  applyAppearance(prefs)
  set({
    info, prefs, sites: sites.sites, folders: sites.folders,
    conns: Object.fromEntries(conns.map((c) => [c.id, c])),
    jobs,
    terminals: Object.fromEntries((terms as any[]).map((x) => [x.id, x])),
    tunnels: Object.fromEntries((tunnels as any[]).map((x) => [x.id, x])),
    watches: Object.fromEntries((watches as any[]).map((x) => [x.id, x])),
    edits: Object.fromEntries((edits as any[]).map((x) => [x.id, x])),
    prompts,
  })
  stream?.close()
  stream = new EventStream(onEvent)
  stream.onStatus = (online) => {
    set({ online })
    if (online) resync()
  }
  // Re-attach tabs for connections that survived a UI reload.
  for (const c of conns) if (c.state !== 'closed') openSessionTab(c, { activate: false })
  if (info.startupUrl) connectUrl(info.startupUrl)
  else if (!conns.length && prefs.restoreTabs && prefs.lastTabs?.length) restoreTabs(prefs.lastTabs)
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyAppearance(get().prefs!))
}

async function resync() {
  try {
    const [conns, jobs, prompts] = await Promise.all([call<ConnInfo[]>('conn.list'), call<Job[]>('transfer.list'), call<PromptEvent[]>('prompt.pending')])
    set({ conns: Object.fromEntries(conns.map((c) => [c.id, c])), jobs, prompts })
  } catch {
    /* offline */
  }
}

let refreshTimers: Record<string, number> = {}

function onEvent(type: string, data: any) {
  const s = get()
  switch (type) {
    case 'conn.update': {
      const c = data as ConnInfo
      set({ conns: { ...s.conns, [c.id]: c } })
      break
    }
    case 'conn.closed': {
      const conns = { ...s.conns }
      delete conns[data]
      set({ conns })
      break
    }
    case 'transfer.update': {
      const j = data as Job
      const idx = s.jobs.findIndex((x) => x.id === j.id)
      const jobs = idx >= 0 ? s.jobs.map((x) => (x.id === j.id ? j : x)) : [...s.jobs, j]
      set({ jobs })
      if (idx >= 0) {
        const prev = s.jobs[idx]
        if (prev.status !== j.status && ['done', 'partial', 'failed'].includes(j.status)) jobFinished(j)
      }
      break
    }
    case 'transfer.removed':
      set({ jobs: s.jobs.filter((x) => x.id !== data) })
      break
    case 'transfer.order': {
      const order: string[] = data
      const by = Object.fromEntries(s.jobs.map((j) => [j.id, j]))
      set({ jobs: order.map((id) => by[id]).filter(Boolean) })
      break
    }
    case 'prompt':
      if (!s.prompts.some((p) => p.id === data.id)) set({ prompts: [...s.prompts, data] })
      break
    case 'prompt.close':
      set({ prompts: s.prompts.filter((p) => p.id !== data) })
      break
    case 'log': {
      const { conn, line } = data
      const cur = s.logs[conn] || []
      const next = cur.length > 3000 ? [...cur.slice(-2500), line] : [...cur, line]
      set({ logs: { ...s.logs, [conn]: next } })
      break
    }
    case 'fs.changed': {
      const { conn, dir } = data
      const key = `${conn}|${dir}`
      clearTimeout(refreshTimers[key])
      refreshTimers[key] = window.setTimeout(() => {
        dirCache.delete(cacheKey(conn, dir))
        for (const tab of get().tabs) {
          if (tab.kind !== 'session') continue
          for (const side of ['left', 'right'] as Side[]) {
            const p = tab.panes[side]
            if (p.conn === conn && (dir === '' || p.path === dir)) refreshPane(tab.id, side, true)
          }
        }
      }, 120)
      break
    }
    case 'term.update':
      set({ terminals: { ...s.terminals, [data.id]: data } })
      break
    case 'term.closed': {
      const terminals = { ...s.terminals }
      delete terminals[data]
      set({ terminals })
      break
    }
    case 'tunnel.update':
      set({ tunnels: { ...s.tunnels, [data.id]: data } })
      break
    case 'tunnel.closed': {
      const tunnels = { ...s.tunnels }
      delete tunnels[data]
      set({ tunnels })
      break
    }
    case 'watch.event': {
      const w = s.watches[data.id]
      if (w) set({ watches: { ...s.watches, [data.id]: { ...w, events: [...w.events.slice(-300), data.event], uploads: w.uploads + (data.event.action === 'upload' ? 1 : 0), deletes: w.deletes + (data.event.action === 'delete' ? 1 : 0) } } })
      break
    }
    case 'watch.closed': {
      const watches = { ...s.watches }
      delete watches[data]
      set({ watches })
      break
    }
    case 'edit.update':
      set({ edits: { ...s.edits, [data.id]: data } })
      if (data.uploads > 0 && !data.error) toast('ok', t('editor.saved'), basename('posix', data.remote), { timeout: 2000 })
      if (data.error) toast('error', t('editor.saved'), data.error)
      break
    case 'edit.closed': {
      const edits = { ...s.edits }
      delete edits[data]
      set({ edits })
      break
    }
    case 'find.hits': {
      const f = s.find
      if (f && f.id === data.id) set({ find: { ...f, hits: [...f.hits, ...(data.hits || [])], done: data.done, scanned: data.scanned, error: data.error || undefined } })
      break
    }
    case 'sites.changed':
      reloadSites()
      break
    case 'prefs.changed':
      call<Prefs>('prefs.get').then((p) => {
        set({ prefs: p })
        applyAppearance(p)
      })
      break
    case 'security.changed':
      call<AppInfo>('app.info').then((info) => set({ info }))
      break
    case 'banner':
      toast('info', data.host, data.message, { timeout: 12000 })
      break
  }
}

function jobFinished(j: Job) {
  const s = get()
  if (j.status === 'failed') toast('error', `${j.label}`, j.error || t('queue.status.failed'))
  else if (j.status === 'partial') toast('warn', j.label, t('queue.errors', { n: j.failedFiles }), { action: { label: t('queue.retryFailed'), run: () => call('transfer.retry', { id: j.id }) } })
  const active = s.jobs.some((x) => ['queued', 'scanning', 'running', 'paused'].includes(x.status))
  if (!active && j.status === 'done' && !s.bottom.open) toast('ok', t('queue.allDone'), j.label, { timeout: 2500 })
}

// ---------------------------------------------------------------------------
// Appearance & preferences

export function applyAppearance(p: Prefs) {
  const root = document.documentElement
  const dark = p.theme === 'dark' || (p.theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  root.dataset.theme = dark ? 'dark' : 'light'
  root.dataset.density = p.density
  root.style.setProperty('--accent', p.accent || '#4f7cff')
  root.style.setProperty('--fs', `${p.fontSize || 13}px`)
  setLang(p.language)
}

let prefsTimer: number | undefined
export async function savePrefs(patch: Partial<Prefs> | ((p: Prefs) => Partial<Prefs>), immediate = false) {
  const cur = get().prefs
  if (!cur) return
  const next = { ...cur, ...(typeof patch === 'function' ? patch(cur) : patch) }
  set({ prefs: next })
  applyAppearance(next)
  clearTimeout(prefsTimer)
  const write = () => call('prefs.set', next).catch((e) => errorToast(t('common.error'), e))
  if (immediate) await write()
  else prefsTimer = window.setTimeout(write, 400)
}

export async function reloadSites() {
  const r = await call<{ folders: string[]; sites: SiteView[] }>('sites.list')
  set({ sites: r.sites, folders: r.folders })
}

// ---------------------------------------------------------------------------
// Tabs

export function activate(id: string) {
  set({ activeTab: id })
}

export function addTab(tab: Tab, activateIt = true) {
  set((s) => {
    const idx = s.tabs.findIndex((x) => x.id === s.activeTab)
    const tabs = [...s.tabs]
    tabs.splice(idx >= 0 ? idx + 1 : tabs.length, 0, tab)
    return { tabs, activeTab: activateIt ? tab.id : s.activeTab }
  })
  persistTabs()
}

export async function closeTab(id: string, force = false): Promise<boolean> {
  const tab = getTab(id)
  if (!tab || tab.kind === 'sites') return false
  if (tab.kind === 'editor' && tab.dirty && !force) {
    const { confirmCloseEditor } = await import('../components/editor/closeGuard')
    const r = await confirmCloseEditor(tab)
    if (!r) return false
  }
  if (tab.kind === 'session') {
    const others = get().tabs.filter((x) => x.id !== id && (x as any).conn === tab.conn && x.kind === 'session')
    if (!others.length && tab.conn) {
      const running = get().jobs.filter((j) => (j.src === tab.conn || j.dst === tab.conn) && ['queued', 'scanning', 'running', 'paused'].includes(j.status))
      if (running.length && get().prefs?.confirmExit && !force) {
        const { ask } = await import('../components/dialogs/common')
        if (!(await ask({ title: t('tabs.closeTab'), message: t('tabs.runningTransfers', { n: running.length }), danger: true, ok: t('tabs.closeTab') }))) return false
      }
      call('conn.close', { conn: tab.conn }).catch(() => {})
    }
  }
  if (tab.kind === 'terminal') call('term.close', { id: tab.termId }).catch(() => {})
  set((s) => {
    const idx = s.tabs.findIndex((x) => x.id === id)
    const tabs = s.tabs.filter((x) => x.id !== id)
    let active = s.activeTab
    if (active === id) active = (tabs[Math.min(idx, tabs.length - 1)] || tabs[0]).id
    return { tabs, activeTab: active }
  })
  persistTabs()
  return true
}

export function moveTab(id: string, toIndex: number) {
  set((s) => {
    const tabs = [...s.tabs]
    const from = tabs.findIndex((x) => x.id === id)
    if (from <= 0 || toIndex <= 0) return {}
    const [tab] = tabs.splice(from, 1)
    tabs.splice(Math.min(toIndex, tabs.length), 0, tab)
    return { tabs }
  })
  persistTabs()
}

export function cycleTab(delta: number) {
  const s = get()
  const idx = s.tabs.findIndex((x) => x.id === s.activeTab)
  const next = s.tabs[(idx + delta + s.tabs.length) % s.tabs.length]
  if (next) activate(next.id)
}

let persistTimer: number | undefined
function persistTabs() {
  clearTimeout(persistTimer)
  persistTimer = window.setTimeout(() => {
    const s = get()
    if (!s.prefs) return
    const lastTabs: TabState[] = []
    for (const tab of s.tabs) {
      if (tab.kind !== 'session') continue
      const c = s.conns[tab.conn]
      if (!c) continue
      const remoteSide: Side = tab.panes.right.conn === tab.conn ? 'right' : 'left'
      const localSide: Side = remoteSide === 'right' ? 'left' : 'right'
      lastTabs.push({
        siteId: c.siteId,
        url: c.siteId ? undefined : c.url,
        remote: tab.panes[remoteSide].path,
        local: tab.panes[localSide].conn === 'local' ? tab.panes[localSide].path : undefined,
      })
    }
    savePrefs({ lastTabs })
  }, 1500)
}

function currentTabStates(): TabState[] {
  const s = get()
  const out: TabState[] = []
  for (const tab of s.tabs) {
    if (tab.kind !== 'session') continue
    const c = s.conns[tab.conn]
    if (!c) continue
    const remoteSide: Side = tab.panes.right.conn === tab.conn ? 'right' : 'left'
    const localSide: Side = remoteSide === 'right' ? 'left' : 'right'
    out.push({ siteId: c.siteId, url: c.siteId ? undefined : c.url, remote: tab.panes[remoteSide].path, local: tab.panes[localSide].conn === 'local' ? tab.panes[localSide].path : undefined })
  }
  return out
}

export async function saveWorkspace(name: string) {
  const tabs = currentTabStates()
  if (!tabs.length) {
    toast('warn', t('ws.title'), t('ws.empty'))
    return
  }
  await savePrefs((p) => ({ workspaces: [...(p.workspaces || []).filter((w) => w.name !== name), { name, tabs }] }), true)
  toast('ok', t('ws.saved'), name)
}

export function openWorkspace(name: string) {
  const ws = get().prefs?.workspaces?.find((w) => w.name === name)
  if (!ws) return
  let first = true
  for (const st of ws.tabs) {
    if (st.siteId && get().sites.some((x) => x.id === st.siteId)) connectSite(st.siteId, { remote: st.remote, local: st.local, activate: first })
    else if (st.url) connectUrl(st.url)
    first = false
  }
}

export function deleteWorkspace(name: string) {
  savePrefs((p) => ({ workspaces: (p.workspaces || []).filter((w) => w.name !== name) }))
}

async function restoreTabs(list: TabState[]) {
  for (const st of list) {
    if (st.siteId && get().sites.some((x) => x.id === st.siteId)) connectSite(st.siteId, { remote: st.remote, local: st.local, activate: false })
  }
}

// ---------------------------------------------------------------------------
// Connections

interface OpenOpts {
  remote?: string
  local?: string
  activate?: boolean
}

export function openSessionTab(c: ConnInfo, opts: OpenOpts = {}, tabId?: string) {
  const info = get().info
  const prefs = get().prefs
  const remoteStart = opts.remote || (c.remoteDir && c.protocol !== 'webdav' ? c.remoteDir : c.home) || c.home || '/'
  const localStart = opts.local || c.localDir || info?.localHome || '/'
  const tab: SessionTab = {
    id: tabId || uid('s'),
    kind: 'session',
    title: c.title,
    conn: c.id,
    panes: { left: newPane('local', localStart), right: newPane(c.id, remoteStart) },
    active: 'right',
    syncBrowsing: !!c.syncBrowsing,
    layout: prefs?.interface === 'explorer' ? 'explorer' : 'commander',
  }
  if (tabId && getTab(tabId)) {
    set((s) => ({ tabs: s.tabs.map((x) => (x.id === tabId ? tab : x)) }))
  } else {
    addTab(tab, opts.activate !== false)
  }
  loadDir(tab.id, 'left', localStart, { history: 'none' })
  loadDir(tab.id, 'right', remoteStart, { history: 'none' })
  persistTabs()
  return tab.id
}

async function openConnection(title: string, params: any, opts: OpenOpts = {}) {
  const token = uid('c')
  const placeholder: SessionTab = {
    id: uid('s'), kind: 'session', title, conn: '', panes: { left: newPane('local', ''), right: newPane('', '') },
    active: 'right', syncBrowsing: false, layout: 'commander',
  }
  addTab(placeholder, opts.activate !== false)
  set((s) => ({ connecting: { ...s.connecting, [placeholder.id]: { title, token } } }))
  try {
    const c = await call<ConnInfo>('conn.open', { ...params, token })
    set((s) => ({ conns: { ...s.conns, [c.id]: c } }))
    if (!getTab(placeholder.id)) {
      call('conn.close', { conn: c.id })
      return
    }
    openSessionTab(c, opts, placeholder.id)
  } catch (e: any) {
    if (getTab(placeholder.id)) {
      updateTab<SessionTab>(placeholder.id, () => ({ conn: '' }))
      set((s) => ({ connecting: { ...s.connecting, [placeholder.id]: { ...s.connecting[placeholder.id], error: e?.message || String(e), params, opts } } }))
      return
    }
  } finally {
    set((s) => {
      const c = { ...s.connecting }
      if (c[placeholder.id] && !c[placeholder.id].error) delete c[placeholder.id]
      return { connecting: c }
    })
  }
}

export function retryConnection(tabId: string) {
  const c = get().connecting[tabId]
  if (!c) return
  closeTabSilently(tabId)
  openConnection(c.title, c.params, c.opts)
}

export function abortConnection(tabId: string) {
  const c = get().connecting[tabId]
  if (c) call('conn.abort', { token: c.token }).catch(() => {})
  closeTabSilently(tabId)
}

function closeTabSilently(tabId: string) {
  set((s) => {
    const connecting = { ...s.connecting }
    delete connecting[tabId]
    const idx = s.tabs.findIndex((x) => x.id === tabId)
    const tabs = s.tabs.filter((x) => x.id !== tabId)
    let active = s.activeTab
    if (active === tabId) active = (tabs[Math.min(idx, tabs.length - 1)] || tabs[0]).id
    return { tabs, activeTab: active, connecting }
  })
}

export function connectSite(siteId: string, opts: OpenOpts = {}) {
  const site = get().sites.find((s) => s.id === siteId)
  if (!site) return
  if (get().info?.locked && (site.hasPassword || site.hasPassphrase)) {
    import('../components/dialogs/MasterPassword').then((m) =>
      m.unlockDialog().then((ok) => {
        if (ok) openConnection(site.name || site.host, { siteId }, opts)
      }),
    )
    return
  }
  openConnection(site.name || `${site.user ? site.user + '@' : ''}${site.host}`, { siteId }, opts)
}

export function connectUrl(url: string, password?: string) {
  let title = url
  try {
    const u = new URL(url.includes('://') ? url : 'sftp://' + url)
    title = (u.username ? decodeURIComponent(u.username) + '@' : '') + u.hostname
  } catch {
    /* keep */
  }
  openConnection(title, { url, password })
}

export function connectAdhoc(site: any, password?: string) {
  openConnection(site.name || `${site.user ? site.user + '@' : ''}${site.host}`, { site, password })
}

export async function reconnect(connId: string) {
  try {
    const c = await call<ConnInfo>('conn.reconnect', { conn: connId })
    set((s) => ({ conns: { ...s.conns, [c.id]: c } }))
    for (const tab of get().tabs) if (tab.kind === 'session' && tab.conn === connId) {
      refreshPane(tab.id, 'left')
      refreshPane(tab.id, 'right')
    }
  } catch (e) {
    errorToast(t('conn.failed'), e)
  }
}

export function duplicateSession(tabId: string) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const c = get().conns[tab.conn]
  if (!c) return
  const id = uid('s')
  const nt: SessionTab = { ...tab, id, panes: { left: { ...tab.panes.left, selected: new Set() }, right: { ...tab.panes.right, selected: new Set() } } }
  addTab(nt)
}

// ---------------------------------------------------------------------------
// Directory navigation

const dirCache = new Map<string, Entry[]>()
const cacheKey = (conn: string, path: string) => `${conn}\u0000${path}`
const loadSeq: Record<string, number> = {}

// displayPath shortens local paths inside the home folder to "~/…".
export function displayPath(conn: string, p: string): string {
  if (conn !== 'local') return p
  const info = get().info
  if (info?.localRoot) return p
  return tildify(info?.localStyle ?? 'posix', p, info?.localHome)
}

export function paneStyle(conn: string) {
  if (conn === 'local') return get().info?.localStyle ?? 'posix'
  return get().conns[conn]?.caps.pathStyle ?? 'posix'
}

interface LoadOpts {
  history?: 'push' | 'none'
  focus?: string
  silent?: boolean
  fromSync?: boolean
}

export async function loadDir(tabId: string, side: Side, path: string, opts: LoadOpts = {}) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const pane = tab.panes[side]
  const conn = pane.conn
  if (!conn) return
  const key = `${tabId}:${side}`
  const seq = (loadSeq[key] = (loadSeq[key] || 0) + 1)
  const cached = dirCache.get(cacheKey(conn, path))
  const prevPath = pane.path
  const navigating = path !== pane.path

  updatePane(tabId, side, (p) => {
    const patch: Partial<PaneState> = { path, loading: true, error: undefined }
    if (navigating) {
      patch.selected = new Set()
      patch.compare = undefined
      patch.focus = opts.focus
      patch.filter = ''
      if (cached) patch.entries = cached
      else if (!opts.silent) patch.entries = []
      if (opts.history !== 'none') {
        const h = p.history.slice(0, p.hIndex + 1)
        if (h[h.length - 1] !== path) h.push(path)
        patch.history = h.slice(-100)
        patch.hIndex = patch.history.length - 1
      }
    }
    return patch
  })

  // synchronized browsing
  if (navigating && !opts.fromSync && tab.syncBrowsing) syncOther(tab, side, prevPath, path)

  try {
    const r = await call<{ path: string; entries: Entry[] }>('fs.list', { conn, path })
    if (loadSeq[key] !== seq) return
    dirCache.set(cacheKey(conn, r.path), r.entries)
    if (dirCache.size > 400) dirCache.delete(dirCache.keys().next().value!)
    updatePane(tabId, side, (p) => {
      const names = new Set(r.entries.map((e) => e.name))
      const selected = new Set([...p.selected].filter((n) => names.has(n)))
      let focus = opts.focus ?? p.focus
      if (focus && !names.has(focus)) focus = undefined
      return { path: r.path, entries: r.entries, loading: false, error: undefined, selected, focus, loadedAt: Date.now() }
    })
  } catch (e: any) {
    if (loadSeq[key] !== seq) return
    updatePane(tabId, side, () => ({ loading: false, error: e?.message || String(e), entries: navigating ? [] : getTab<SessionTab>(tabId)?.panes[side].entries ?? [] }))
  }
}

function syncOther(tab: SessionTab, side: Side, from: string, to: string) {
  const other: Side = side === 'left' ? 'right' : 'left'
  const op = tab.panes[other]
  const st = paneStyle(tab.panes[side].conn)
  const ost = paneStyle(op.conn)
  // entering a subfolder
  const r = relPath(st, from, to)
  if (r !== null && r !== '') {
    let target = op.path
    for (const part of r.split('/')) target = join(ost, target, part)
    loadDir(tab.id, other, target, { fromSync: true })
    return
  }
  // going up
  const up = relPath(st, to, from)
  if (up !== null && up !== '') {
    let target = op.path
    for (let i = 0; i < up.split('/').length; i++) target = dirname(ost, target)
    loadDir(tab.id, other, target, { fromSync: true })
  }
}

export function refreshPane(tabId: string, side: Side, silent = false) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const p = tab.panes[side]
  if (!p.conn) return
  loadDir(tabId, side, p.path, { history: 'none', silent: true, focus: p.focus })
  if (!silent) dirCache.delete(cacheKey(p.conn, p.path))
}

export function navigate(tabId: string, side: Side, path: string) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const p = tab.panes[side]
  const st = paneStyle(p.conn)
  loadDir(tabId, side, normalize(st, path, p.path))
}

export function goUp(tabId: string, side: Side) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const p = tab.panes[side]
  const st = paneStyle(p.conn)
  const parent = dirname(st, p.path)
  if (parent === p.path || (st === 'posix' && p.path === '/')) return
  if (st === 'windows' && p.path === '') return
  loadDir(tabId, side, parent, { focus: basename(st, p.path) })
}

export function goHistory(tabId: string, side: Side, delta: number) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const p = tab.panes[side]
  const i = p.hIndex + delta
  if (i < 0 || i >= p.history.length) return
  updatePane(tabId, side, () => ({ hIndex: i }))
  loadDir(tabId, side, p.history[i], { history: 'none' })
}

export function goHome(tabId: string, side: Side) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const p = tab.panes[side]
  const home = p.conn === 'local' ? get().info?.localHome : get().conns[p.conn]?.home
  if (home) loadDir(tabId, side, home)
}

export function goRoot(tabId: string, side: Side) {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return
  const st = paneStyle(tab.panes[side].conn)
  if (st === 'windows') {
    const p = tab.panes[side].path
    const drive = p.match(/^[A-Za-z]:\\/)?.[0]
    loadDir(tabId, side, drive && p !== drive ? drive : '')
  } else loadDir(tabId, side, '/')
}

// Switch which connection a pane shows (remote ↔ remote transfers).
export function setPaneConn(tabId: string, side: Side, conn: string) {
  const c = conn === 'local' ? null : get().conns[conn]
  const path = conn === 'local' ? get().info?.localHome || '/' : c?.remoteDir && c.protocol !== 'webdav' ? c.remoteDir : c?.home || '/'
  updatePane(tabId, side, () => ({ ...newPane(conn, path) }))
  loadDir(tabId, side, path, { history: 'none' })
}

export function setActiveSide(tabId: string, side: Side) {
  const tab = getTab<SessionTab>(tabId)
  if (tab && tab.kind === 'session' && tab.active !== side) updateTab<SessionTab>(tabId, () => ({ active: side }))
}

export function toggleSyncBrowsing(tabId: string) {
  updateTab<SessionTab>(tabId, (tb) => ({ syncBrowsing: !tb.syncBrowsing }))
}

export function paneVisible(tabId: string, side: Side): Entry[] {
  const tab = getTab<SessionTab>(tabId)
  if (!tab || tab.kind !== 'session') return []
  return visibleEntries(tab.panes[side], get().prefs)
}

// ---------------------------------------------------------------------------
// Editors & terminals

export function openEditor(conn: string, path: string, readOnly = false) {
  const existing = get().tabs.find((x) => x.kind === 'editor' && x.conn === conn && x.path === path) as EditorTab | undefined
  if (existing) return activate(existing.id)
  const st = paneStyle(conn)
  addTab({ id: uid('e'), kind: 'editor', title: basename(st, path), conn, path, dirty: false, readOnly })
}

export function openDiff(left: { conn: string; path: string }, right: { conn: string; path: string }) {
  const lt = basename(paneStyle(left.conn), left.path)
  const rt = basename(paneStyle(right.conn), right.path)
  addTab({ id: uid('d'), kind: 'diff', title: lt === rt ? `${lt} ↔` : `${lt} ↔ ${rt}`, left, right })
}

export async function openTerminal(conn: string, cwd?: string) {
  try {
    const term = await call<{ id: string; title: string }>('term.open', { conn, cols: 120, rows: 32, cwd: cwd || '' })
    const tab: TerminalTab = { id: uid('x'), kind: 'terminal', title: term.title, termId: term.id, conn }
    addTab(tab)
  } catch (e) {
    errorToast(t('term.title'), e)
  }
}

export function attachTerminalTab(termId: string, conn: string, title: string) {
  const existing = get().tabs.find((x) => x.kind === 'terminal' && x.termId === termId)
  if (existing) return activate(existing.id)
  addTab({ id: uid('x'), kind: 'terminal', title, termId, conn })
}

export function setBottom(open: boolean, tab?: State['bottom']['tab']) {
  set((s) => ({ bottom: { ...s.bottom, open, tab: tab ?? s.bottom.tab } }))
}

type State = ReturnType<typeof get>

export function updateLayout(tabId: string) {
  updateTab<SessionTab>(tabId, (tb) => ({ layout: tb.layout === 'commander' ? 'explorer' : 'commander', active: 'right' }))
}
