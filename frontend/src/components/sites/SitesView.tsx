import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown, ChevronRight, Clock, Copy, Download, Edit3, Folder, FolderOpen, FolderPlus, Import, KeyRound, Link2, Lock, MoreHorizontal,
  Plug, Plus, Search, Server, SquareTerminal, Trash2, Upload, Zap, Code2, LayoutGrid, Save, FileCog, Box, CalendarClock, ServerCog,
} from 'lucide-react'
import { get, useStore } from '../../lib/store'
import { openTasks, connectSite, connectSshHost, connectUrl, openTerminal, reloadSites, reloadSshConfig, saveWorkspace, openWorkspace, deleteWorkspace } from '../../lib/actions'
import { SshHostPanel, SshHostRow } from './SshConfig'
import { openContainers } from '../dialogs/ContainersDialog'
import { openGenerateDialog } from '../dialogs/GenerateDialog'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'
import { formatDate } from '../../lib/format'
import type { Site, SiteView } from '../../lib/types'
import { SiteEditor, newSiteDraft } from './SiteEditor'
import { showMenu, showMenuAt, type MenuItem } from '../ui/Menu'
import { ask, prompt } from '../dialogs/common'
import { openImportDialog, openExportDialog } from '../dialogs/ImportDialog'
import { openInstallKey, openKeygen } from '../dialogs/KeygenDialog'
import { unlockDialog } from '../dialogs/MasterPassword'
import { copyToClipboard } from '../../lib/fileops'
import { errorToast } from '../../lib/store'

const NO_WS: { name: string; tabs: any[] }[] = []

const PROTO_LABEL: Record<string, string> = { sftp: 'SFTP', scp: 'SCP', ftp: 'FTP', webdav: 'DAV', s3: 'S3' }

function protoLabel(s: Site) {
  if (s.protocol === 'container') return s.containerRuntime === 'kubectl' ? 'K8S' : (s.containerRuntime || 'docker').toUpperCase()
  if (s.protocol === 'ftp' && s.ftps && s.ftps !== 'none') return 'FTPS'
  if (s.protocol === 'webdav' && s.tls) return 'DAVS'
  return PROTO_LABEL[s.protocol] || s.protocol
}

interface TreeNode {
  name: string
  path: string
  folders: TreeNode[]
  sites: SiteView[]
}

function buildTree(folders: string[], sites: SiteView[]): TreeNode {
  const root: TreeNode = { name: '', path: '', folders: [], sites: [] }
  const get = (path: string): TreeNode => {
    if (!path) return root
    const parts = path.split('/')
    let node = root
    let acc = ''
    for (const p of parts) {
      acc = acc ? acc + '/' + p : p
      let next = node.folders.find((f) => f.name === p)
      if (!next) {
        next = { name: p, path: acc, folders: [], sites: [] }
        node.folders.push(next)
      }
      node = next
    }
    return node
  }
  for (const f of folders) get(f)
  for (const s of sites) get(s.folder || '').sites.push(s)
  const sort = (n: TreeNode) => {
    n.folders.sort((a, b) => a.name.localeCompare(b.name))
    n.sites.sort((a, b) => (a.name || a.host).localeCompare(b.name || b.host))
    n.folders.forEach(sort)
  }
  sort(root)
  return root
}

export function SitesView() {
  const sites = useStore((s) => s.sites)
  const folders = useStore((s) => s.folders)
  const info = useStore((s) => s.info)
  const sshConfig = useStore((s) => s.sshConfig)
  const siteDraft = useStore((s) => s.siteDraft)
  const workspaces = useStore((s) => s.prefs?.workspaces ?? NO_WS)
  const [sel, setSelSite] = useState<string | null>(null)
  const [sshSel, setSshSel] = useState<string | null>(null)
  const setSel = (id: string | null) => {
    setSelSite(id)
    if (id) setSshSel(null)
  }
  const [draft, setDraft] = useState<Site | null>(null)
  const [q, setQ] = useState('')
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [dropFolder, setDropFolder] = useState<string | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const filtered = useMemo(() => {
    if (!q) return sites
    const lq = q.toLowerCase()
    return sites.filter((s) => [s.name, s.host, s.user, s.folder, s.notes].some((v) => v?.toLowerCase().includes(lq)))
  }, [sites, q])
  const tree = useMemo(() => buildTree(q ? [] : folders, filtered), [folders, filtered, q])
  const sshHosts = useMemo(() => {
    const hosts = sshConfig?.enabled ? sshConfig.hosts : []
    if (!q) return hosts
    const lq = q.toLowerCase()
    return hosts.filter((h) => [h.alias, h.hostName, h.user].some((v) => v?.toLowerCase().includes(lq)))
  }, [sshConfig, q])
  // a site prepared elsewhere (containers, ...)
  useEffect(() => {
    if (!siteDraft) return
    setSel(null)
    setSshSel(null)
    setDraft({ ...newSiteDraft(), ...siteDraft, id: '' })
    useStore.setState({ siteDraft: null })
  }, [siteDraft])
  // the files may change outside Skiff
  useEffect(() => {
    reloadSshConfig()
    window.addEventListener('focus', reloadSshConfig)
    return () => window.removeEventListener('focus', reloadSshConfig)
  }, [])
  const recent = useMemo(() => [...sites].filter((s) => s.lastUsed && new Date(s.lastUsed).getFullYear() > 1971).sort((a, b) => +new Date(b.lastUsed!) - +new Date(a.lastUsed!)).slice(0, 5), [sites])

  const selected = sel ? sites.find((s) => s.id === sel) : undefined
  const editing: Site | SiteView | null = draft ?? selected ?? null
  const sshSelected = !editing && sshSel ? sshConfig?.hosts.find((h) => h.alias === sshSel) : undefined

  const newSite = (folder = '') => {
    setSel(null)
    setSshSel(null)
    setDraft({ ...newSiteDraft(), folder })
  }
  // a site for an ssh_config host keeps the alias: the file still applies
  const siteFromSshHost = (alias: string) => {
    setSel(null)
    setSshSel(null)
    setDraft({ ...newSiteDraft(), name: alias, host: alias })
  }
  const selectSshHost = (alias: string) => {
    setDraft(null)
    setSel(null)
    setSshSel(alias)
  }

  const siteMenu = (e: React.MouseEvent | HTMLElement, s: SiteView) => {
    const items: MenuItem[] = [
      { label: t('common.connect'), icon: <Plug />, shortcut: 'Enter', onClick: () => connectSite(s.id) },
      { label: t('sites.openTerminal'), icon: <SquareTerminal />, hidden: !(s.protocol === 'sftp' || s.protocol === 'scp'), onClick: () => connectAndTerminal(s.id) },
      { separator: true },
      { label: t('common.edit'), icon: <Edit3 />, onClick: () => { setDraft(null); setSel(s.id) } },
      { label: t('common.duplicate'), icon: <Copy />, onClick: async () => { const d = await call<SiteView>('sites.duplicate', { id: s.id, name: t('sites.copyName', { name: s.name || s.host }) }); await reloadSites(); setSel(d.id) } },
      { label: t('common.rename'), icon: <Edit3 />, onClick: async () => {
        const name = await prompt({ title: t('common.rename'), label: t('site.name'), value: s.name || s.host })
        if (name) { await call('sites.save', { site: { ...s, name }, secrets: {} }); reloadSites() }
      } },
      { label: t('sites.copyUrl'), icon: <Link2 />, onClick: async () => copyToClipboard(await call<string>('sites.url', { id: s.id })) },
      { label: t('gen.menu'), icon: <Code2 />, onClick: () => openGenerateDialog(s) },
      {
        label: t('sites.moveToFolder'), icon: <Folder />, submenu: [
          { label: t('sites.rootFolder'), onClick: () => moveSites([s.id], '') },
          ...folders.map((f) => ({ label: f, onClick: () => moveSites([s.id], f) })),
        ],
      },
      { separator: true },
      { label: t('common.delete'), icon: <Trash2 />, danger: true, onClick: () => deleteSites([s.id]) },
    ]
    if (e instanceof HTMLElement) showMenuAt(e, items)
    else showMenu(e.clientX, e.clientY, items)
  }

  const folderMenu = (e: React.MouseEvent, f: TreeNode) => {
    e.preventDefault()
    showMenu(e.clientX, e.clientY, [
      { label: t('sites.newSite'), icon: <Plus />, onClick: () => newSite(f.path) },
      { label: t('sites.newFolder'), icon: <FolderPlus />, onClick: () => newFolder(f.path) },
      { label: t('sites.connectAll'), icon: <Plug />, onClick: () => collectSites(f).forEach((s) => connectSite(s.id)) },
      { separator: true },
      { label: t('common.rename'), icon: <Edit3 />, onClick: async () => {
        const name = await prompt({ title: t('common.rename'), label: t('common.folder'), value: f.name })
        if (name && name !== f.name) {
          const parent = f.path.includes('/') ? f.path.slice(0, f.path.lastIndexOf('/')) : ''
          await call('folders.rename', { name: f.path, to: parent ? `${parent}/${name}` : name })
          reloadSites()
        }
      } },
      { label: t('common.delete'), icon: <Trash2 />, danger: true, onClick: async () => {
        if (await ask({ title: t('common.delete'), message: t('sites.deleteFolderConfirm', { name: f.path }), danger: true })) {
          await call('folders.delete', { name: f.path, withSites: false })
          reloadSites()
        }
      } },
    ])
  }

  const newFolder = async (parent = '') => {
    const name = await prompt({ title: t('sites.newFolder'), label: t('common.folder'), value: '' })
    if (!name) return
    await call('folders.add', { name: parent ? `${parent}/${name}` : name })
    reloadSites()
  }

  const moveSites = async (ids: string[], folder: string) => {
    await call('sites.move', { ids, folder })
    reloadSites()
  }

  const deleteSites = async (ids: string[]) => {
    if (!(await ask({ title: t('common.delete'), message: t('sites.deleteConfirm', { n: ids.length }), danger: true, ok: t('common.delete') }))) return
    await call('sites.delete', { ids })
    if (sel && ids.includes(sel)) setSel(null)
    reloadSites()
  }

  const onKey = (e: React.KeyboardEvent) => {
    if ((e.target as HTMLElement).tagName === 'INPUT' && e.key !== 'ArrowDown' && e.key !== 'Enter') return
    const flat: SiteView[] = []
    const walk = (n: TreeNode) => {
      if (!collapsed[n.path] || q) {
        n.folders.forEach(walk)
        flat.push(...n.sites)
      }
    }
    walk(tree)
    const idx = flat.findIndex((s) => s.id === sel)
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      const n = flat[Math.min(flat.length - 1, idx + 1)]
      if (n) { setDraft(null); setSel(n.id) }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      const n = flat[Math.max(0, idx - 1)]
      if (n) { setDraft(null); setSel(n.id) }
    } else if (e.key === 'Enter' && sel) {
      e.preventDefault()
      connectSite(sel)
    } else if (e.key === 'Enter' && sshSel) {
      e.preventDefault()
      connectSshHost(sshSel)
    } else if (e.key === 'Enter' && flat.length === 1) {
      connectSite(flat[0].id)
    } else if (e.key === 'Delete' && sel) {
      deleteSites([sel])
    }
  }

  const renderNode = (n: TreeNode, depth: number) => (
    <div key={n.path || '__root'}>
      {n.path && (
        <div
          className={`tree-folder${dropFolder === n.path ? ' drop' : ''}`}
          style={{ paddingLeft: 8 + (depth - 1) * 14 }}
          onClick={() => setCollapsed((c) => ({ ...c, [n.path]: !c[n.path] }))}
          onContextMenu={(e) => folderMenu(e, n)}
          onDragOver={(e) => { if (e.dataTransfer.types.includes('application/x-skiff-site')) { e.preventDefault(); setDropFolder(n.path) } }}
          onDragLeave={() => setDropFolder(null)}
          onDrop={(e) => { e.preventDefault(); setDropFolder(null); moveSites([e.dataTransfer.getData('application/x-skiff-site')], n.path) }}
          data-dropzone
        >
          {collapsed[n.path] && !q ? <ChevronRight className="chev" /> : <ChevronDown className="chev" />}
          {collapsed[n.path] && !q ? <Folder className="folder-ic" /> : <FolderOpen className="folder-ic" />}
          <span className="ellipsis">{n.name}</span>
          <span className="faint count">{collectSites(n).length}</span>
        </div>
      )}
      {(!collapsed[n.path] || q) && (
        <>
          {n.folders.map((f) => renderNode(f, depth + 1))}
          {n.sites.map((s) => (
            <SiteRow key={s.id} s={s} depth={depth} selected={sel === s.id && !draft} onSelect={() => { setDraft(null); setSel(s.id) }} onMenu={siteMenu} />
          ))}
        </>
      )}
    </div>
  )

  return (
    <div className="sites" onKeyDown={onKey}>
      <aside className="sites-side">
        <div className="sites-side-head">
          <div className="input-group grow">
            <Search />
            <input ref={searchRef} className="input" placeholder={t('sites.search')} value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
          </div>
          <button className="icon-btn" data-tip={t('sites.newSite')} onClick={() => newSite()}><Plus /></button>
          <button className="icon-btn" data-tip={t('common.more')} onClick={(e) => showMenuAt(e.currentTarget, [
            { label: t('sites.newFolder'), icon: <FolderPlus />, onClick: () => newFolder() },
            { label: t('sites.import'), icon: <Import />, onClick: () => openImportDialog() },
            { label: t('tasks.title'), icon: <CalendarClock />, onClick: () => openTasks() },
            { label: t('ctr.titleLocal'), icon: <Box />, hidden: !info?.localTerminal, onClick: () => openContainers({ onNewSite: (st) => setDraft({ ...newSiteDraft(), ...st }) }) },
            { label: t('sites.export'), icon: <Upload />, onClick: () => openExportDialog() },
            { separator: true },
            { label: t('ws.save'), icon: <Save />, onClick: async () => { const n = await prompt({ title: t('ws.title'), label: t('ws.name'), value: '' }); if (n) saveWorkspace(n) } },
            { separator: true },
            { label: t('prefs.generateKey'), icon: <KeyRound />, onClick: () => openKeygen() },
            { label: t('keys.installTitle') + '…', icon: <ServerCog />, onClick: () => openInstallKey() },
          ], 'right')}><MoreHorizontal /></button>
        </div>
        <div
          className="sites-tree"
          tabIndex={0}
          onDragOver={(e) => { if (e.dataTransfer.types.includes('application/x-skiff-site')) e.preventDefault() }}
          onDrop={(e) => { const id = e.dataTransfer.getData('application/x-skiff-site'); if (id && !dropFolder) moveSites([id], '') }}
          data-dropzone
        >
          {info?.locked && (
            <button className="locked-banner" onClick={() => unlockDialog()}>
              <Lock /> <span className="grow">{t('sites.locked')}</span> <span className="accent-text">{t('sites.unlock')}</span>
            </button>
          )}
          {!q && workspaces.length > 0 && (
            <>
              <div className="tree-label"><LayoutGrid /> {t('ws.title')}</div>
              {workspaces.map((w) => (
                <div
                  key={w.name}
                  className="tree-site"
                  style={{ paddingLeft: 8 }}
                  onDoubleClick={() => openWorkspace(w.name)}
                  onContextMenu={(e) => { e.preventDefault(); showMenu(e.clientX, e.clientY, [
                    { label: t('ws.open'), icon: <Plug />, onClick: () => openWorkspace(w.name) },
                    { label: t('common.delete'), icon: <Trash2 />, danger: true, onClick: () => deleteWorkspace(w.name) },
                  ]) }}
                >
                  <LayoutGrid className="faint" style={{ width: 15, marginLeft: 4 }} />
                  <div className="grow" style={{ minWidth: 0 }}>
                    <div className="ellipsis site-name">{w.name}</div>
                    <div className="ellipsis site-sub">{w.tabs.length} session(s)</div>
                  </div>
                  <button className="btn sm" onClick={() => openWorkspace(w.name)}><Plug /></button>
                </div>
              ))}
            </>
          )}
          {!q && recent.length > 0 && (
            <>
              <div className="tree-label"><Clock /> {t('sites.recent')}</div>
              {recent.map((s) => (
                <SiteRow key={'r' + s.id} s={s} depth={1} selected={false} compact onSelect={() => { setDraft(null); setSel(s.id) }} onMenu={siteMenu} />
              ))}
              <div className="tree-label"><Server /> {t('sites.all')}</div>
            </>
          )}
          {sites.length === 0 ? (
            <div className="empty" style={{ height: 'auto', paddingTop: sshHosts.length ? 20 : 40 }}>
              <Server className="big" />
              <h4>{t('sites.empty')}</h4>
              <p>{t('sites.emptyHint')}</p>
              <div className="row">
                <button className="btn sm primary" onClick={() => newSite()}><Plus /> {t('sites.newSite')}</button>
                <button className="btn sm" onClick={() => openImportDialog()}><Download /> {t('sites.import')}</button>
              </div>
            </div>
          ) : (
            renderNode(tree, 1)
          )}
          {sshHosts.length > 0 && (
            <>
              <div className="tree-label sshcfg-label" onClick={() => setCollapsed((c) => ({ ...c, __ssh: !c.__ssh }))} data-tip={sshConfig?.userFile}>
                {collapsed.__ssh && !q ? <ChevronRight /> : <ChevronDown />}
                <FileCog /> {t('sshcfg.section')}
                <span className="faint count">{sshHosts.length}</span>
              </div>
              {(!collapsed.__ssh || q) && sshHosts.map((h) => (
                <SshHostRow key={h.alias} h={h} selected={sshSel === h.alias && !editing} onSelect={() => selectSshHost(h.alias)} onNewSite={() => siteFromSshHost(h.alias)} />
              ))}
            </>
          )}
        </div>
      </aside>
      <main className="sites-main">
        {editing ? (
          <SiteEditor
            key={draft ? 'draft' + (draft.id || '') : editing.id}
            site={editing}
            isNew={!!draft}
            onSaved={(s) => { setDraft(null); setSel(s.id) }}
            onDeleted={() => { setDraft(null); setSel(null) }}
            onCancel={() => setDraft(null)}
          />
        ) : sshSelected ? (
          <SshHostPanel key={sshSelected.alias} h={sshSelected} onNewSite={() => siteFromSshHost(sshSelected.alias)} />
        ) : (
          <Welcome onNew={() => newSite()} recent={recent} onSelect={(id) => setSel(id)} />
        )}
      </main>
    </div>
  )
}

function collectSites(n: TreeNode): SiteView[] {
  return [...n.sites, ...n.folders.flatMap(collectSites)]
}

async function connectAndTerminal(siteId: string) {
  connectSite(siteId)
  // open a terminal once the connection is up
  const start = Date.now()
  const wait = () => {
    const s = get()
    const c = Object.values(s.conns).find((x) => x.siteId === siteId && x.state === 'connected')
    if (c) return openTerminal(c.id)
    if (Date.now() - start < 60000) setTimeout(wait, 300)
  }
  wait()
}

function SiteRow({ s, depth, selected, onSelect, onMenu, compact }: { s: SiteView; depth: number; selected: boolean; onSelect: () => void; onMenu: (e: any, s: SiteView) => void; compact?: boolean }) {
  const conns = useStore((st) => st.conns)
  const connected = Object.values(conns).some((c) => c.siteId === s.id && c.state === 'connected')
  return (
    <div
      className={`tree-site${selected ? ' sel' : ''}`}
      style={{ paddingLeft: 8 + (depth - 1) * 14 + (compact ? 0 : 0) }}
      onClick={onSelect}
      onDoubleClick={() => connectSite(s.id)}
      onContextMenu={(e) => { e.preventDefault(); onSelect(); onMenu(e, s) }}
      draggable
      onDragStart={(e) => e.dataTransfer.setData('application/x-skiff-site', s.id)}
    >
      <span className="site-color" style={{ background: s.color || 'var(--border-strong)' }} />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="ellipsis site-name">{s.name || s.host}</div>
        {!compact && <div className="ellipsis site-sub">{s.user ? `${s.user}@` : ''}{s.host}{s.port ? `:${s.port}` : ''}</div>}
      </div>
      {connected && <span className="dot ok" />}
      <span className="proto-badge">{protoLabel(s)}</span>
    </div>
  )
}

function Welcome({ onNew, recent, onSelect }: { onNew: () => void; recent: SiteView[]; onSelect: (id: string) => void }) {
  const [url, setUrl] = useState('')
  const info = useStore((s) => s.info)
  const sshConfig = useStore((s) => s.sshConfig)
  return (
    <div className="welcome">
      <div className="welcome-hero">
        <img src="/favicon.svg" alt="" width={64} height={64} />
        <h1>{t('sites.welcome')}</h1>
        <p className="muted">{t('sites.welcomeSub')}</p>
      </div>
      <form
        className="quick-connect"
        onSubmit={(e) => {
          e.preventDefault()
          if (url.trim()) connectUrl(url.trim())
        }}
      >
        <div className="input-group grow">
          <Zap />
          <input className="input lg" value={url} list="ssh-config-hosts" onChange={(e) => setUrl(e.target.value)} placeholder={t('sites.quickPlaceholder')} spellCheck={false} />
          <datalist id="ssh-config-hosts">{(sshConfig?.enabled ? sshConfig.hosts : []).map((h) => <option key={h.alias} value={h.alias}>{h.user ? `${h.user}@` : ''}{h.hostName}</option>)}</datalist>
        </div>
        <button className="btn primary lg" type="submit" disabled={!url.trim()}><Plug /> {t('common.connect')}</button>
      </form>
      <div className="welcome-actions">
        <button className="action-card" onClick={onNew}>
          <Plus />
          <span>{t('sites.newSite')}</span>
        </button>
        <button className="action-card" onClick={() => openImportDialog()}>
          <Download />
          <span>{t('sites.import')}</span>
        </button>
        <button className="action-card" onClick={() => openKeygen()}>
          <KeyRound />
          <span>{t('prefs.generateKey')}</span>
        </button>
        {info?.localTerminal && (
          <button className="action-card" onClick={() => openTerminal('local')}>
            <SquareTerminal />
            <span>{t('welcome.localTerminal')}</span>
          </button>
        )}
      </div>
      {recent.length > 0 && (
        <div className="recent-cards">
          <h4>{t('sites.recent')}</h4>
          <div className="cards">
            {recent.map((s) => (
              <div key={s.id} className="site-card" onClick={() => onSelect(s.id)} onDoubleClick={() => connectSite(s.id)}>
                <span className="site-color" style={{ background: s.color || 'var(--accent)' }} />
                <div className="grow" style={{ minWidth: 0 }}>
                  <div className="ellipsis strong">{s.name || s.host}</div>
                  <div className="ellipsis faint">{t('sites.lastUsed', { when: formatDate(s.lastUsed, 'relative') })}</div>
                </div>
                <button className="btn sm primary" onClick={(e) => { e.stopPropagation(); connectSite(s.id) }}><Plug /></button>
              </div>
            ))}
          </div>
        </div>
      )}
      <div className="welcome-foot faint">
        <kbd>Ctrl</kbd>+<kbd>K</kbd> {t('welcome.k.palette')} · <kbd>F5</kbd> {t('welcome.k.copy')} · <kbd>F6</kbd> {t('welcome.k.move')} · <kbd>Tab</kbd> {t('welcome.k.switch')}
      </div>
    </div>
  )
}

void errorToast
