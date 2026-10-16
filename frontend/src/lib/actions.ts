import { call, EventStream, setUnauthorizedHandler } from './api'
import {
  get, getTab, newPane, set, toast, errorToast, uid, updatePane, updateTab, useStore,
  type EditorTab, type MonitorTab, type PaneState, type ProjectTab, type SessionTab, type Side, type Tab, type TerminalTab, type TermPaneState,
  barTabs,
} from './store'
import type { AppInfo, ConnInfo, Entry, Job, Prefs, PromptEvent, SiteView, SshConfigInfo, TabState } from './types'
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
  reloadSshConfig()
  if (info.localTerminal) loadShells('local') // for the command palette
  // Re-attach tabs for connections that survived a UI reload.
  for (const c of conns) if (c.state !== 'closed') openSessionTab(c, { activate: false })
  if (info.startupUrl) connectUrl(info.startupUrl)
  else if (!conns.length && prefs.restoreTabs && prefs.lastTabs?.length) restoreTabs(prefs.lastTabs)
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyAppearance(get().prefs!))
}

// the events received while resync waits for its answers: they are newer
// (a question asked meanwhile must not be lost)
let resyncing: { prompts: Map<string, PromptEvent>; promptsClosed: Set<string>; conns: Map<string, ConnInfo>; connsClosed: Set<string> } | null = null

async function resync() {
  const live = { prompts: new Map<string, PromptEvent>(), promptsClosed: new Set<string>(), conns: new Map<string, ConnInfo>(), connsClosed: new Set<string>() }
  resyncing = live
  try {
    const [conns, jobs, pending] = await Promise.all([call<ConnInfo[]>('conn.list'), call<Job[]>('transfer.list'), call<PromptEvent[]>('prompt.pending')])
    const prompts = pending.filter((p) => !live.promptsClosed.has(p.id))
    for (const p of live.prompts.values()) if (!live.promptsClosed.has(p.id) && !prompts.some((x) => x.id === p.id)) prompts.push(p)
    const byId = Object.fromEntries(conns.map((c) => [c.id, c]))
    for (const [id, c] of live.conns) byId[id] = c
    for (const id of live.connsClosed) delete byId[id]
    set({ conns: byId, jobs, prompts })
  } catch {
    /* offline */
  } finally {
    if (resyncing === live) resyncing = null
  }
}

let refreshTimers: Record<string, number> = {}

function onEvent(type: string, data: any) {
  const s = get()
  switch (type) {
    case 'conn.update': {
      const c = data as ConnInfo
      resyncing?.conns.set(c.id, c)
      set({ conns: { ...s.conns, [c.id]: c } })
      break
    }
    case 'conn.closed': {
      resyncing?.connsClosed.add(data)
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
      resyncing?.prompts.set(data.id, data)
      if (!s.prompts.some((p) => p.id === data.id)) set({ prompts: [...s.prompts, data] })
      break
    case 'prompt.close':
      resyncing?.promptsClosed.add(data)
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
      set((st) => ({ fsChanged: { conn, dir, seq: (st.fsChanged?.seq || 0) + 1 } }))
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
      if (f && f.id === data.id) applyFindHits(data)
      else {
        // the search may finish before fs.find returns its id: keep the
        // events until startFind() knows it
        earlyFindHits.set(data.id, [...(earlyFindHits.get(data.id) || []), data])
        if (earlyFindHits.size > 20) earlyFindHits.delete(earlyFindHits.keys().next().value!)
      }
      break
    }
    case 'sites.changed':
      reloadSites()
      break
    case 'devsync.update':
      set({ devsync: data as import('./types').DevSyncView })
      break
    case 'task.update':
      if (get().tasks) reloadTasks()
      break
    case 'task.run': {
      const { run, notify } = data as { run: import('./types').TaskRun; notify: boolean }
      if (notify) import('./summaries').then(({ runSummary }) => toast(run.status === 'ok' ? 'ok' : run.status === 'cancelled' ? 'warn' : 'error', `${run.taskName} — ${t(`tasks.status.${run.status}`)}`, runSummary(run), { timeout: run.status === 'ok' ? 5000 : 12000 }))
      break
    }
    case 'prefs.changed':
      call<Prefs>('prefs.get').then((p) => {
        set({ prefs: p })
        applyAppearance(p)
      })
      reloadSshConfig()
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

/** Re-reads the OpenSSH configuration (files may change outside Skiff). */
export async function reloadSshConfig() {
  try {
    const sshConfig = await call<SshConfigInfo>('sshconfig.info')
    const cur = get().sshConfig
    if (JSON.stringify(cur) !== JSON.stringify(sshConfig)) set({ sshConfig })
  } catch {
    /* keep the previous one */
  }
}

/** Connects to a host of the OpenSSH configuration (resolved by the server). */
export function connectSshHost(alias: string, opts: OpenOpts = {}) {
  openConnection(alias, { site: { name: alias, protocol: 'sftp', host: alias, useAgent: true, kbdInteractive: true, resolveSymlinks: true, keepalive: 30, timeout: 15 } }, opts)
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
  if (tab.kind === 'project') {
    const dirty = tab.files.filter((f) => f.dirty)
    if (dirty.length && !force) {
      const { confirmClose } = await import('../components/editor/closeGuard')
      const msg = dirty.length === 1 ? t('editor.unsavedClose', { name: basename(paneStyle(tab.conn), dirty[0].path) }) : t('project.unsavedClose', { n: dirty.length })
      if (!(await confirmClose(msg, dirty.map((f) => projectSaver(tab.id, f.path))))) return false
    }
    if (tab.terminal) await closeTab(tab.terminal, true)
    const users = get().tabs.filter((x) => x.id !== id && (x.kind === 'session' || x.kind === 'project') && x.conn === tab.conn)
    if (!users.length && tab.conn !== 'local') call('conn.close', { conn: tab.conn }).catch(() => {})
  }
  if (tab.kind === 'terminal' && tab.embeddedIn) {
    updateTab<ProjectTab>(tab.embeddedIn, () => ({ terminal: undefined, showTerminal: false }))
  }
  if (tab.kind === 'session') {
    const others = get().tabs.filter((x) => x.id !== id && (x as any).conn === tab.conn && (x.kind === 'session' || x.kind === 'project'))
    if (!others.length && tab.conn) {
      const running = get().jobs.filter((j) => (j.src === tab.conn || j.dst === tab.conn) && ['queued', 'scanning', 'running', 'paused'].includes(j.status))
      if (running.length && get().prefs?.confirmExit && !force) {
        const { ask } = await import('../components/dialogs/common')
        if (!(await ask({ title: t('tabs.closeTab'), message: t('tabs.runningTransfers', { n: running.length }), danger: true, ok: t('tabs.closeTab') }))) return false
      }
      call('conn.close', { conn: tab.conn }).catch(() => {})
    }
  }
  if (tab.kind === 'terminal') for (const p of terminalPanes(tab)) call('term.close', { id: p.termId }).catch(() => {})
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
  const tabs = barTabs(get().tabs)
  const idx = tabs.findIndex((x) => x.id === get().activeTab)
  const next = tabs[(idx + delta + tabs.length) % tabs.length]
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
    lastTabs.push(...projectStates())
    if (JSON.stringify(lastTabs) !== JSON.stringify(s.prefs.lastTabs || [])) savePrefs({ lastTabs })
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
  return [...out, ...projectStates()]
}

/** The project tabs to reopen, with their connection. */
function projectStates(): TabState[] {
  const s = get()
  const out: TabState[] = []
  for (const tab of s.tabs) {
    if (tab.kind !== 'project') continue
    const project = { root: tab.root, files: tab.files.map((f) => f.path), active: tab.active }
    if (tab.conn === 'local') {
      out.push({ project: { ...project, local: true } })
      continue
    }
    const c = s.conns[tab.conn]
    if (c) out.push({ siteId: c.siteId, url: c.siteId ? undefined : c.url, project })
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
  restoreTabs(ws.tabs, true)
}

export function deleteWorkspace(name: string) {
  savePrefs((p) => ({ workspaces: (p.workspaces || []).filter((w) => w.name !== name) }))
}

async function restoreTabs(list: TabState[], activateFirst = false) {
  // a project is reopened on the connection of the sessions of its server
  const key = (st: TabState) => st.siteId || st.url || ''
  const projects = list.filter((st) => st.project && !st.project.local)
  const openProjectsOf = (k: string) => (c: ConnInfo) => {
    for (const p of projects) if (key(p) === k) openProject(c.id, p.project!.root, p.project!.files, p.project!.active, false)
  }
  let first = activateFirst
  const connect = (st: TabState, afterConnect?: (c: ConnInfo) => void) => {
    const opts = { remote: st.remote, local: st.local, activate: first, afterConnect }
    first = false
    if (st.siteId && get().sites.some((x) => x.id === st.siteId)) connectSite(st.siteId, opts)
    else if (st.url) connectUrl(st.url, undefined, opts) // quick connection: the password is asked again
  }
  const done = new Set<string>()
  for (const st of list) {
    if (st.project) {
      if (st.project.local) openProject('local', st.project.root, st.project.files, st.project.active, false)
      continue
    }
    const k = key(st)
    connect(st, done.has(k) ? undefined : openProjectsOf(k))
    done.add(k)
  }
  // projects whose server had no session tab
  for (const p of projects) {
    const k = key(p)
    if (done.has(k)) continue
    done.add(k)
    connect({ siteId: p.siteId, url: p.url }, openProjectsOf(k))
  }
}

// ---------------------------------------------------------------------------
// Connections

interface OpenOpts {
  remote?: string
  local?: string
  activate?: boolean
  /** called once the session tab is open */
  afterConnect?: (c: ConnInfo) => void
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
    opts.afterConnect?.(c)
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

export function connectUrl(url: string, password?: string, opts: OpenOpts = {}) {
  let title = url
  try {
    const u = new URL(url.includes('://') ? url : 'sftp://' + url)
    title = (u.username ? decodeURIComponent(u.username) + '@' : '') + u.hostname
  } catch {
    /* keep */
  }
  openConnection(title, { url, password }, opts)
}

export function connectAdhoc(site: any, password?: string, opts: OpenOpts = {}) {
  openConnection(site.name || `${site.user ? site.user + '@' : ''}${site.host}`, { site, password }, opts)
}

/** Shows the scheduled tasks tab. */
export function openTasks() {
  if (!get().tabs.some((x) => x.id === 'tasks')) addTab({ id: 'tasks', kind: 'tasks', title: t('tasks.title') } as Tab)
  else activate('tasks')
  reloadTasks()
}

/** Opens the tasks tab with a new task prepared elsewhere (sync dialog). */
export function editNewTask(task: Partial<import('./types').Task>) {
  set({ taskDraft: task })
  openTasks()
}

export async function reloadTasks() {
  try {
    set({ tasks: await call<import('./types').TaskView[]>('tasks.list') })
  } catch {
    /* keep */
  }
}

/** Opens a new site in the editor of the Sites tab. */
export function editNewSite(site: SiteView | import('./types').Site) {
  set({ siteDraft: site as any, activeTab: 'sites' })
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
    if (r.path !== prevPath) persistTabs() // the tabs reopen in their last folders
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

// ---------------------------------------------------------------------------
// Projects: a folder with its tree, its files in tabs and a terminal

/** Shows a folder in a file panel: a session of the connection, or a new one. */
export function openSessionAt(conn: string, path: string) {
  const s = get()
  for (const tab of s.tabs) {
    if (tab.kind !== 'session' || (conn !== 'local' && tab.conn !== conn)) continue
    const side: Side = tab.panes.right.conn === conn ? 'right' : tab.panes.left.conn === conn ? 'left' : tab.active
    if (tab.panes[side].conn !== conn) continue
    activate(tab.id)
    navigate(tab.id, side, path)
    return
  }
  const c = s.conns[conn]
  if (c) openSessionTab(c, { remote: path })
}

/** The saver of a file of a project (see closeGuard). */
export const projectSaver = (tabId: string, path: string) => `${tabId}:${path}`

/** Opens a folder as a project (or shows it when it is already open). */
export function openProject(conn: string, root: string, files: string[] = [], active?: string, activateIt = true) {
  const existing = get().tabs.find((x) => x.kind === 'project' && x.conn === conn && x.root === root)
  if (existing) {
    if (activateIt) activate(existing.id)
    return
  }
  const title = basename(paneStyle(conn), root) || root
  const tab: ProjectTab = { id: uid('p'), kind: 'project', title, conn, root, files: files.map((path) => ({ path, dirty: false })), active: active || files[0], side: 'files' }
  addTab(tab, activateIt)
}

/** Shows a file of a project, opening it first; jump selects a search result. */
export function openProjectFile(tabId: string, path: string, jump?: { line: number; col: number; len: number }) {
  updateTab<ProjectTab>(tabId, (tb) => {
    const files = tb.files.some((f) => f.path === path) ? tb.files : [...tb.files, { path, dirty: false }]
    return { files: jump ? files.map((f) => (f.path === path ? { ...f, jump: { ...jump, seq: Date.now() } } : f)) : files, active: path }
  })
  persistTabs()
}

export async function closeProjectFile(tabId: string, path: string, force = false) {
  const tb = getTab<ProjectTab>(tabId)
  const f = tb?.files.find((x) => x.path === path)
  if (!tb || !f) return
  if (f.dirty && !force) {
    const { confirmClose } = await import('../components/editor/closeGuard')
    if (!(await confirmClose(t('editor.unsavedClose', { name: basename(paneStyle(tb.conn), path) }), [projectSaver(tabId, path)]))) return
  }
  updateTab<ProjectTab>(tabId, (cur) => {
    const i = cur.files.findIndex((x) => x.path === path)
    const files = cur.files.filter((x) => x.path !== path)
    const active = cur.active === path ? files[Math.min(i, files.length - 1)]?.path : cur.active
    return { files, active }
  })
  persistTabs()
}

/** The files of a project moved or renamed: their tabs follow. */
export function renameProjectFiles(tabId: string, from: string, to: string) {
  const style = get().tabs.find((x) => x.id === tabId)?.kind === 'project' ? paneStyle((getTab<ProjectTab>(tabId) as ProjectTab).conn) : 'posix'
  const sep = style === 'windows' ? '\\' : '/'
  const move = (p: string) => (p === from ? to : p.startsWith(from + sep) ? to + p.slice(from.length) : p)
  updateTab<ProjectTab>(tabId, (cur) => ({ files: cur.files.map((f) => ({ ...f, path: move(f.path) })), active: cur.active ? move(cur.active) : cur.active }))
  persistTabs()
}

export async function saveAllProject(tabId: string) {
  const tb = getTab<ProjectTab>(tabId)
  if (!tb) return
  const { runSaver } = await import('../components/editor/closeGuard')
  for (const f of tb.files) if (f.dirty) await runSaver(projectSaver(tabId, f.path))
}

/** Shows or hides the terminal of a project, opening it in its folder. */
export async function toggleProjectTerminal(tabId: string, show?: boolean) {
  const tb = getTab<ProjectTab>(tabId)
  if (!tb) return
  if (tb.terminal && get().tabs.some((x) => x.id === tb.terminal)) {
    updateTab<ProjectTab>(tabId, () => ({ showTerminal: show ?? !tb.showTerminal }))
    return
  }
  if (show === false) return
  try {
    const term = await call<{ id: string; title: string }>('term.open', { conn: tb.conn, cols: 120, rows: 12, cwd: tb.root, shell: '' })
    const tt: TerminalTab = { id: uid('x'), kind: 'terminal', title: term.title, termId: term.id, conn: tb.conn, panes: [{ termId: term.id, conn: tb.conn, title: term.title, cwd: tb.root }], embeddedIn: tabId }
    set((s) => ({ tabs: [...s.tabs, tt] }))
    updateTab<ProjectTab>(tabId, () => ({ terminal: tt.id, showTerminal: true }))
  } catch (e) {
    errorToast(t('term.title'), e)
  }
}

/** Opens (or shows) the server monitor of an SSH connection. */
export function openMonitor(conn: string) {
  const existing = get().tabs.find((x) => x.kind === 'monitor' && x.conn === conn)
  if (existing) return activate(existing.id)
  const title = get().conns[conn]?.title || conn
  addTab({ id: uid('m'), kind: 'monitor', title: t('mon.tabTitle', { title }), conn } as MonitorTab)
}

/** The terminals of a tab (one unless it is split). */
export function terminalPanes(tab: TerminalTab): TermPaneState[] {
  return tab.panes?.length ? tab.panes : [{ termId: tab.termId, conn: tab.conn, title: tab.title }]
}

/** Adds a terminal on conn to a terminal tab, beside or below the others. */
export async function splitTerminal(tabId: string, conn: string, dir: 'columns' | 'rows', cwd?: string, shell?: string) {
  try {
    const term = await call<{ id: string; title: string }>('term.open', { conn, cols: 80, rows: 24, cwd: cwd || '', shell: shell || '' })
    updateTab<TerminalTab>(tabId, (tb) => ({ panes: [...terminalPanes(tb), { termId: term.id, conn, title: term.title, cwd, shell }], split: dir }))
  } catch (e) {
    errorToast(t('term.title'), e)
  }
}

/** Closes one terminal of a split tab (the tab with its last one). */
export function closeTermPane(tabId: string, termId: string) {
  const tab = getTab<TerminalTab>(tabId)
  if (!tab) return
  const rest = terminalPanes(tab).filter((p) => p.termId !== termId)
  if (!rest.length) {
    closeTab(tabId)
    return
  }
  call('term.close', { id: termId }).catch(() => {})
  updateTab<TerminalTab>(tabId, () => ({ panes: rest, termId: rest[0].termId, conn: rest[0].conn, title: rest[0].title, broadcast: rest.length > 1 && getTab<TerminalTab>(tabId)?.broadcast }))
}

function sessionFor(conn: string): { tab: SessionTab; side: Side } | null {
  const s = get()
  const order = [s.tabs.find((x) => x.id === s.activeTab), ...s.tabs]
  for (const tb of order) {
    if (tb?.kind !== 'session') continue
    if (tb.panes.right.conn === conn) return { tab: tb, side: 'right' }
    if (tb.panes.left.conn === conn) return { tab: tb, side: 'left' }
  }
  return null
}

/** The file panel of the terminal's connection follows its current folder. */
export function followTerminalCwd(conn: string, path: string) {
  const found = sessionFor(conn)
  if (found && found.tab.panes[found.side].path !== path) loadDir(found.tab.id, found.side, path)
}

/** Shows the terminal's current folder in a file panel. */
export function revealTerminalCwd(conn: string, path: string) {
  const found = sessionFor(conn)
  if (found) {
    loadDir(found.tab.id, found.side, path)
    activate(found.tab.id)
    return
  }
  const c = get().conns[conn]
  if (c) openSessionTab(c, { remote: path })
}

/** The shells and interpreters a terminal can run on a connection. */
export async function loadShells(conn: string): Promise<import('./types').Shell[]> {
  const cached = get().shells[conn]
  if (cached) return cached
  try {
    const list = await call<import('./types').Shell[]>('term.shells', { conn })
    set((s) => ({ shells: { ...s.shells, [conn]: list } }))
    return list
  } catch {
    return []
  }
}

export async function openTerminal(conn: string, cwd?: string, shell?: string) {
  try {
    const term = await call<{ id: string; title: string }>('term.open', { conn, cols: 120, rows: 32, cwd: cwd || '', shell: shell || '' })
    const tab: TerminalTab = { id: uid('x'), kind: 'terminal', title: term.title, termId: term.id, conn, panes: [{ termId: term.id, conn, title: term.title, cwd, shell }] }
    addTab(tab)
  } catch (e) {
    errorToast(t('term.title'), e)
  }
}

export function attachTerminalTab(termId: string, conn: string, title: string) {
  const existing = get().tabs.find((x) => x.kind === 'terminal' && terminalPanes(x).some((p) => p.termId === termId))
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

const earlyFindHits = new Map<string, any[]>()

function applyFindHits(data: any) {
  const f = get().find
  if (!f || f.id !== data.id) return
  set({ find: { ...f, hits: [...f.hits, ...(data.hits || [])], done: data.done, scanned: data.scanned, error: data.error || undefined } })
}

// startFind shows a search started with fs.find, including the results
// that arrived before its id was known.
export function startFind(find: NonNullable<State['find']>) {
  set({ find })
  const early = earlyFindHits.get(find.id) || []
  earlyFindHits.delete(find.id)
  for (const data of early) applyFindHits(data)
}

// The local folders shown in the panels are watched by Skiff, so that
// changes made outside of it (downloads, other applications) refresh them
// (the server answers with fs.changed events).
let watchedLocal = ''
useStore.subscribe((s) => {
  const dirs = new Set<string>()
  for (const tab of s.tabs) {
    if (tab.kind !== 'session') continue
    for (const side of ['left', 'right'] as Side[]) {
      const p = tab.panes[side]
      if (p.conn === 'local' && p.path) dirs.add(p.path)
    }
  }
  const key = [...dirs].sort().join('\n')
  if (key === watchedLocal) return
  watchedLocal = key
  call('fs.watchLocal', { dirs: [...dirs] }).catch(() => {
    watchedLocal = '' // retry on the next change (e.g. not signed in yet)
  })
})
