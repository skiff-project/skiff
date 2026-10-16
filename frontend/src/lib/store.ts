import { create } from 'zustand'
import type {
  AppInfo, ConnInfo, Entry, ExternalEdit, Job, LogLine, Prefs, PromptEvent, SiteView, SshConfigInfo, TerminalInfo, TunnelInfo, WatchInfo,
} from './types'

export type Side = 'left' | 'right'
export type SortKey = 'name' | 'size' | 'mtime' | 'type' | 'perm' | 'owner'

export interface PaneState {
  conn: string
  path: string
  entries: Entry[]
  loading: boolean
  error?: string
  selected: Set<string>
  focus?: string
  anchor?: string
  sort: { key: SortKey; dir: 1 | -1 }
  filter: string
  history: string[]
  hIndex: number
  loadedAt: number
  compare?: Record<string, 'newer' | 'older' | 'differs' | 'missing'>
  renaming?: string
}

export interface SessionTab {
  id: string
  kind: 'session'
  title: string
  conn: string
  panes: Record<Side, PaneState>
  active: Side
  syncBrowsing: boolean
  layout: 'commander' | 'explorer'
}

export interface EditorTab {
  id: string
  kind: 'editor'
  title: string
  conn: string
  path: string
  dirty: boolean
  readOnly?: boolean
}

export interface DiffTab {
  id: string
  kind: 'diff'
  title: string
  left: { conn: string; path: string }
  right: { conn: string; path: string }
}

export interface TermPaneState {
  termId: string
  conn: string
  title: string
  cwd?: string // folder it was opened in
  shell?: string // shell or interpreter it runs ("" = the default one)
}

export interface TerminalTab {
  id: string
  kind: 'terminal'
  title: string
  // the first terminal (panes[0] when the tab is split)
  termId: string
  conn: string
  panes?: TermPaneState[]
  split?: 'columns' | 'rows'
  broadcast?: boolean // input goes to every pane
  follow?: boolean // the file panel follows the current folder
  embeddedIn?: string // the project tab it is shown in (not in the tab bar)
}

/** A log followed live: a file, or the journal of a service. */
export interface LogsTab {
  id: string
  kind: 'logs'
  title: string
  conn: string
  path?: string
  unit?: string
}

/** A file open in a project. */
export interface ProjectFile {
  path: string
  dirty: boolean
  /** where to put the cursor (a search result); seq makes a new jump */
  jump?: { line: number; col: number; len: number; seq: number }
}

/** A folder opened with its tree, its files in tabs and a terminal. */
export interface ProjectTab {
  id: string
  kind: 'project'
  title: string
  conn: string
  root: string
  files: ProjectFile[]
  active?: string // path of the shown file
  side: 'files' | 'search'
  terminal?: string // id of its terminal tab
  showTerminal?: boolean
}

export interface MonitorTab {
  id: string
  kind: 'monitor'
  title: string
  conn: string
}

export interface TasksTab {
  id: 'tasks'
  kind: 'tasks'
  title: string
}

export interface SitesTab {
  id: 'sites'
  kind: 'sites'
  title: string
}

export type Tab = SitesTab | SessionTab | EditorTab | DiffTab | TerminalTab | MonitorTab | TasksTab | ProjectTab | LogsTab

export type BottomTab = 'queue' | 'log' | 'find' | 'watch' | 'tunnels' | 'edits'

export interface Toast {
  id: number
  kind: 'ok' | 'error' | 'warn' | 'info'
  title: string
  message?: string
  action?: { label: string; run: () => void }
  timeout?: number
}

export interface FindState {
  id: string
  conn: string
  dir: string
  hits: { path: string; entry: Entry }[]
  done: boolean
  scanned: number
  error?: string
}

export interface State {
  info: AppInfo | null
  prefs: Prefs | null
  sites: SiteView[]
  folders: string[]
  sshConfig: SshConfigInfo | null
  conns: Record<string, ConnInfo>
  tabs: Tab[]
  activeTab: string
  jobs: Job[]
  prompts: PromptEvent[]
  logs: Record<string, LogLine[]>
  terminals: Record<string, TerminalInfo>
  tunnels: Record<string, TunnelInfo>
  watches: Record<string, WatchInfo>
  edits: Record<string, ExternalEdit>
  find: FindState | null
  toasts: Toast[]
  bottom: { open: boolean; tab: BottomTab; height: number }
  online: boolean
  palette: boolean
  authed: boolean | null
  connecting: Record<string, { title: string; token: string; error?: string; params?: any; opts?: any }>
  /** a site to open in the site editor (set from outside the Sites tab) */
  siteDraft: import('./types').Site | null
  tasks: import('./types').TaskView[] | null
  devsync: import('./types').DevSyncView | null
  /** shells and interpreters found per connection ("local" too) */
  shells: Record<string, import('./types').Shell[]>
  /** the latest folder changed on a connection (projects refresh their tree) */
  fsChanged?: { conn: string; dir: string; seq: number }
  /** a task to open in the task editor */
  taskDraft: Partial<import('./types').Task> | null
  previewOpen: boolean
}

export const useStore = create<State>(() => ({
  info: null,
  prefs: null,
  sites: [],
  folders: [],
  sshConfig: null,
  conns: {},
  tabs: [{ id: 'sites', kind: 'sites', title: 'Sites' }],
  activeTab: 'sites',
  jobs: [],
  prompts: [],
  logs: {},
  terminals: {},
  tunnels: {},
  watches: {},
  edits: {},
  find: null,
  toasts: [],
  bottom: { open: false, tab: 'queue', height: 230 },
  online: true,
  palette: false,
  authed: null,
  connecting: {},
  siteDraft: null,
  tasks: null,
  devsync: null,
  shells: {},
  taskDraft: null,
  previewOpen: false,
}))

export const get = () => useStore.getState()
export const set = useStore.setState

export function newPane(conn: string, path: string): PaneState {
  return {
    conn, path, entries: [], loading: true, selected: new Set(), sort: { key: 'name', dir: 1 }, filter: '',
    history: [path], hIndex: 0, loadedAt: 0,
  }
}

let idSeq = 0
export function uid(prefix = 't'): string {
  idSeq++
  return `${prefix}${Date.now().toString(36)}${idSeq}`
}

export function getTab<T extends Tab = Tab>(id: string): T | undefined {
  return get().tabs.find((t) => t.id === id) as T | undefined
}

export function updateTab<T extends Tab>(id: string, fn: (t: T) => Partial<T>) {
  set((s) => ({ tabs: s.tabs.map((t) => (t.id === id ? ({ ...t, ...fn(t as T) } as Tab) : t)) }))
}

export function updatePane(tabId: string, side: Side, fn: (p: PaneState) => Partial<PaneState>) {
  set((s) => ({
    tabs: s.tabs.map((t) => {
      if (t.id !== tabId || t.kind !== 'session') return t
      const p = t.panes[side]
      return { ...t, panes: { ...t.panes, [side]: { ...p, ...fn(p) } } }
    }),
  }))
}

export function activeSession(): SessionTab | undefined {
  const s = get()
  const t = s.tabs.find((x) => x.id === s.activeTab)
  return t && t.kind === 'session' ? t : undefined
}

let toastSeq = 0
export function toast(kind: Toast['kind'], title: string, message?: string, opts: Partial<Toast> = {}) {
  const id = ++toastSeq
  const timeout = opts.timeout ?? (kind === 'error' ? 8000 : 4000)
  set((s) => ({ toasts: [...s.toasts.slice(-4), { id, kind, title, message, ...opts }] }))
  if (timeout > 0) setTimeout(() => dismissToast(id), timeout)
  return id
}

export function dismissToast(id: number) {
  set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
}

export function errorToast(title: string, e: any) {
  if (e?.code === 'cancelled') return
  toast('error', title, e?.message || String(e))
}

/** The tabs of the tab bar (a project's terminal is shown inside it). */
export function barTabs(tabs: Tab[]): Tab[] {
  return tabs.filter((x) => !(x.kind === 'terminal' && x.embeddedIn))
}
