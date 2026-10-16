// Types mirroring the Go API.

export type PathStyle = 'posix' | 'windows'

export interface Entry {
  name: string
  size: number
  mode: number
  mtime: string
  isDir: boolean
  isLink?: boolean
  linkTarget?: string
  owner?: string
  group?: string
  uid?: number
  gid?: number
  hidden?: boolean
  mime?: string
  etag?: string
}

export interface Caps {
  protocol: string
  pathStyle: PathStyle
  permissions: boolean
  chmod: boolean
  chown: boolean
  owners: boolean
  symlink: boolean
  hardlink: boolean
  setMtime: boolean
  exec: boolean
  shell: boolean
  serverCopy: boolean
  checksum: boolean
  space: boolean
  resumeDownload: boolean
  resumeUpload: boolean
  tunnels: boolean
  mtimePrecision: number
}

export interface AppInfo {
  version: string
  mode: 'desktop' | 'web'
  os: string
  arch: string
  hostname: string
  configDir: string
  portable: boolean
  localHome: string
  localStyle: PathStyle
  localCaps: Caps
  locked: boolean
  hasMaster: boolean
  localRoot: string
  agent: boolean
  localTerminal: boolean
  startupUrl?: string
}

export interface Proxy {
  type?: string
  host?: string
  port?: number
  user?: string
  command?: string
  remoteDns?: boolean
}

export interface Jump {
  siteId?: string
  host?: string
  port?: number
  user?: string
  keyFile?: string
  hostKey?: string
}

export interface Forward {
  id: string
  type: 'local' | 'remote' | 'dynamic'
  bindHost?: string
  bindPort: number
  destHost?: string
  destPort?: number
  autoStart?: boolean
  label?: string
}

export type Protocol = 'sftp' | 'scp' | 'ftp' | 'webdav' | 's3' | 'container'

export interface Site {
  id: string
  name: string
  folder?: string
  color?: string
  notes?: string
  protocol: Protocol
  host: string
  port?: number
  user?: string
  keyFile?: string
  useAgent: boolean
  kbdInteractive: boolean
  hostKey?: string
  sftpServer?: string
  shell?: string
  listCommand?: string
  legacyAlgos?: boolean
  ciphers?: string[]
  kex?: string[]
  macs?: string[]
  hostKeyAlgos?: string[]
  ignoreSshConfig?: boolean
  forwardAgent?: boolean
  containerRuntime?: 'docker' | 'podman' | 'kubectl'
  containerCommand?: string
  k8sContext?: string
  k8sNamespace?: string
  k8sContainer?: string
  viaSite?: string
  viaConn?: string
  ftps?: 'none' | 'explicit' | 'implicit'
  ftpActive?: boolean
  ftpActiveIp?: string
  ftpForcePasvIp?: string
  ftpMlsd?: string
  ftpListHidden?: boolean
  ftpAccount?: string
  encoding?: string
  tls?: boolean
  tlsInsecure?: boolean
  tlsFingerprint?: string
  s3Region?: string
  s3PathStyle?: boolean
  remoteDir?: string
  localDir?: string
  syncBrowsing?: boolean
  recycleBin?: string
  resolveSymlinks: boolean
  eol?: string
  timeOffset?: number
  timeout?: number
  keepalive?: number
  ipVersion?: string
  proxy?: Proxy
  jumps?: Jump[]
  forwards?: Forward[]
  parallel?: number
  created?: string
  updated?: string
  lastUsed?: string
  useCount?: number
}

export interface SiteView extends Site {
  hasPassword: boolean
  hasPassphrase: boolean
  hasProxyPassword: boolean
  hasS3SessionToken: boolean
  jumpHasPassword?: boolean[]
}

export interface SecretInput {
  password?: string
  passphrase?: string
  proxyPassword?: string
  s3SessionToken?: string
  jumpPasswords?: Record<number, string>
}

export interface ConnInfo {
  id: string
  siteId?: string
  title: string
  protocol: string
  host: string
  port: number
  user: string
  state: 'connecting' | 'connected' | 'lost' | 'closed'
  error?: string
  caps: Caps
  home: string
  remoteDir?: string
  localDir?: string
  color?: string
  syncBrowsing?: boolean
  url: string
  created: string
  recycleBin?: string
  eol?: string
}

export interface TransferOptions {
  mode: string
  textMask?: string
  preserveTime: boolean
  preservePerms: boolean
  setPerms: boolean
  perms?: number
  addXToDirs: boolean
  overwrite: string
  resumeThreshold: number
  fileMask?: string
  speedLimit?: number
  caseMode?: string
  verify?: boolean
  excludeHidden?: boolean
  replaceInvalid?: boolean
  ignorePermErrors?: boolean
  move?: boolean
  newerOnly?: boolean
  parallel?: number
  retryCount?: number
  retryDelay?: number
  continueOnError?: boolean
}

export type JobStatus = 'queued' | 'scanning' | 'running' | 'paused' | 'done' | 'failed' | 'cancelled' | 'partial'

export interface Job {
  id: string
  label: string
  src: string
  dst: string
  srcTitle: string
  dstTitle: string
  dstDir: string
  move: boolean
  status: JobStatus
  created: string
  started?: string
  finished?: string
  totalBytes: number
  doneBytes: number
  totalFiles: number
  doneFiles: number
  failedFiles: number
  skippedFiles: number
  segmentedFiles?: number
  speed: number
  eta: number
  active: { name: string; size: number; done: number; parts?: number }[]
  error?: string
  errors?: { path: string; error: string }[]
  paths: string[] | null
}

export interface PromptField { label: string; secret: boolean; value?: string }
export interface PromptChoice { id: string; label: string; primary?: boolean; danger?: boolean }
export interface PromptEvent {
  id: string
  kind: 'hostkey' | 'certificate' | 'password' | 'passphrase' | 'kbdint' | 'banner' | 'overwrite' | 'error' | 'confirm'
  conn?: string
  title: string
  message: string
  details?: Record<string, string>
  fields?: PromptField[]
  choices?: PromptChoice[]
  remember?: boolean
  applyAll?: boolean
  danger?: boolean
}

export interface LogLine { t: string; l: string; m: string }

export interface InfoItem { group: string; key: string; value: string }

export interface TerminalInfo { id: string; conn: string; title: string; created: string; exited: boolean }

export interface TunnelInfo {
  id: string; conn: string; connTitle: string; type: string; bind: string; dest?: string; label?: string
  active: number; total: number; bytesIn: number; bytesOut: number; started: string; error?: string; running: boolean
}

export interface WatchEvent { time: string; action: string; path: string; error?: string }
export interface WatchInfo {
  id: string
  req: { localDir: string; remoteConn: string; remoteDir: string; delete: boolean; fileMask?: string }
  started: string; events: WatchEvent[]; uploads: number; deletes: number
}

export interface ExternalEdit { id: string; conn: string; remote: string; local: string; editor: string; opened: string; uploaded?: string; uploads: number; error?: string }

export interface SyncAction {
  id: string
  action: 'upload' | 'download' | 'deleteLocal' | 'deleteRemote' | 'touchLocal' | 'touchRemote' | 'mkdirLocal' | 'mkdirRemote'
  rel: string
  isDir: boolean
  local?: Entry
  remote?: Entry
  reason: string
}
export interface SyncPlan { id: string; req: SyncRequest; actions: SyncAction[]; scanned: number; errors?: string[]; bytes: number }
export interface SyncRequest {
  localConn: string; localDir: string; remoteConn: string; remoteDir: string
  direction: 'remote' | 'local' | 'both'; mode: 'standard' | 'mirror' | 'timestamps'
  criteria: 'time' | 'size' | 'either' | 'checksum' | 'none'
  delete: boolean; existingOnly: boolean; fileMask?: string; excludeHidden?: boolean
}

export interface FileContent { path: string; content: string; encoding: string; bom: boolean; eol: string; size: number; mtime: string; binary: boolean; readOnly: boolean }

export interface Bookmark { name: string; path: string; side: 'local' | 'remote' }
export interface TabState { siteId?: string; url?: string; remote?: string; local?: string }
export interface CustomCommand { id: string; name: string; command: string; local: boolean; perFile: boolean; applyDirs: boolean; recursive: boolean; showOutput: boolean; shortcut?: string }

export interface Prefs {
  language: string
  theme: 'system' | 'light' | 'dark'
  accent: string
  density: 'compact' | 'comfortable' | 'spacious'
  fontSize: number
  interface: 'commander' | 'explorer'
  showHidden: boolean
  dirsFirst: boolean
  sizeFormat: 'iec' | 'si' | 'bytes'
  dateFormat: 'relative' | 'short' | 'iso'
  showFKeyBar: boolean
  showTree: boolean
  doubleClick: 'open' | 'edit' | 'copy'
  showPreview: boolean
  fullRowSelect: boolean
  naturalSort: boolean
  confirmDelete: boolean
  confirmTransfer: boolean
  confirmExit: boolean
  confirmDragDrop: boolean
  confirmOverwrite: boolean
  transfer: {
    mode: string; textMask: string; preserveTime: boolean; preservePerms: boolean; setPerms: boolean; perms: number
    addXToDirs: boolean; overwrite: string; resumeThreshold: number; fileMask: string; speedLimit: number; caseMode: string
    verify: boolean; excludeHidden: boolean; replaceInvalid: boolean; ignorePermErrors: boolean; clearArchive: boolean
  }
  queue: { maxParallel: number; background: boolean; autoPopup: boolean; retryCount: number; retryDelay: number; continueOnError: boolean; segmentSize: number; segments: number }
  editor: { default: 'internal' | 'external'; externalCmd: string; tabSize: number; wordWrap: boolean; fontSize: number; maxSizeMB: number; encoding: string; autoReload: boolean; associations: { mask: string; command: string }[] }
  terminal: { fontSize: number; fontFamily: string; scrollback: number; cursorBlink: boolean; copyOnSelect: boolean; term: string; snippets: Snippet[]
    localShell?: string; copyKeys?: '' | 'ctrl' | 'ctrlShift'; rightClick?: '' | 'copyPaste' | 'menu' }
  logging: { toFile: boolean; level: string; keepDays: number }
  restoreTabs: boolean
  autoReconnect: boolean
  reconnectDelay: number
  bookmarks: Record<string, Bookmark[]>
  workspaces: { name: string; tabs: TabState[] }[]
  lastTabs: TabState[]
  customCommands: CustomCommand[]
  ssh?: { ignoreConfig: boolean; configFile?: string }
  ui: Record<string, any>
}

export interface ImportSource { id: string; name: string; path?: string; available: boolean; needsFile: boolean }
export interface ImportItem { site: Site; password?: string; passphrase?: string; note?: string }

export interface KeyFile { path: string; type: string; fingerprint: string; comment: string; encrypted: boolean; format: string; publicKey?: string }
export interface GeneratedKey { private: string; public: string; fingerprint: string; type: string; path?: string; format: string }
export interface MonitorProc { pid: number; user: string; name: string; state: string; cpu: number; mem: number; threads: number }
export interface MonitorSample {
  time: string; hostname: string; kernel: string; cpus: number; uptime: number; load: [number, number, number]; cpu: number
  memTotal: number; memUsed: number; swapTotal: number; swapUsed: number; netRx: number; netTx: number
  disks: { mount: string; fs: string; total: number; used: number }[]
  procs: MonitorProc[]; procCount: number
}
export interface DevSyncConfig {
  enabled: boolean; siteId: string; path: string; interval: number; device: string
  sites: boolean; keychain: boolean; prefs: boolean; hostKeys: boolean
}
export interface DevSyncView extends DevSyncConfig {
  hasPassphrase: boolean
  state: {
    lastSync?: string; lastOk?: string; lastStatus?: string; lastError?: string; summary?: string; lastDevice?: string; running: boolean
    result?: { uploaded: boolean; created: boolean; sitesIn: number; sitesOut: number; keysIn: number; keysOut: number; prefsIn: boolean; hostKeysIn: number; remoteDevice?: string }
  }
}
export interface TaskSchedule { kind: 'manual' | 'interval' | 'daily' | 'weekly' | 'cron'; every?: number; at?: string; days?: number[]; cron?: string }
export interface Task {
  id: string; name: string; enabled: boolean; kind: 'sync' | 'script'
  siteId?: string; sync?: SyncRequest; script?: string
  schedule: TaskSchedule; catchUp: boolean; notify?: 'failure' | 'always' | 'never'
  created?: string; updated?: string
}
export interface TaskRun {
  id: string; taskId: string; taskName: string; trigger: string; start: string; end?: string
  status: 'running' | 'ok' | 'failed' | 'cancelled'; summary?: string; log?: string[]
  stats?: { files: number; bytes: number; deleted: number; compared: number; upToDate?: boolean; commands?: number }
}
export interface TaskView extends Task { next?: string; last?: TaskRun; running: boolean }
export interface ContainerInfo { name: string; image?: string; status?: string; running: boolean; namespace?: string; containers?: string[] }
export interface Shell { id: string; name: string; kind: 'shell' | 'repl'; path: string; args?: string[]; default?: boolean }
export interface Snippet { id: string; name: string; command: string; run: boolean }
export interface KeychainKey { id: string; name: string; type: string; fingerprint: string; publicKey: string; added: string }
export interface KnownHost { hosts: string; keyType: string; fingerprint: string; line: number }

// OpenSSH client configuration (ssh_config)
export interface SshConfigFile {
  path: string
  scope: 'user' | 'system' | 'include'
  exists: boolean
  error?: string
}

export interface SshHost {
  alias: string
  source: string
  hostName: string
  user?: string
  port: number
}

export interface SshConfigInfo {
  enabled: boolean
  userFile: string
  systemFile: string
  files: SshConfigFile[]
  hosts: SshHost[]
}

export interface SshConfigOption {
  key: string
  value: string
  source: string
  status: 'applied' | 'site' | 'ignored'
}

export interface SshConfigReport {
  enabled: boolean
  alias: string
  options: SshConfigOption[]
  warnings?: string[]
  host: string
  port: number
  user: string
  keyFiles?: string[]
  agent: boolean
  jumps?: string[]
  proxyCommand?: string
  forwards?: string[]
}
