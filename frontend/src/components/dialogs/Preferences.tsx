import { useEffect, useState } from 'react'
import {
  ArrowDownUp, Check, CheckSquare, Code2, FileCog, Fingerprint, Info, KeyRound, Keyboard, LayoutPanelLeft, ListOrdered, Palette, Plus, ScrollText,
  Lock, Settings, Shield, SquareTerminal, Trash2, Wand2,
} from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { get, useStore } from '../../lib/store'
import { displayPath, savePrefs } from '../../lib/actions'
import { commandName, languages, t } from '../../lib/i18n'
import type { CustomCommand, KeyFile, KnownHost, Prefs } from '../../lib/types'
import { TransferOptionsForm } from './TransferDialog'
import { formatDate, formatSize } from '../../lib/format'
import { masterPasswordDialog } from './MasterPassword'
import { openConvertKey, openKeygen } from './KeygenDialog'
import { ask } from './common'

export function openPreferences(section?: string) {
  openDialog((close) => <Preferences close={close} initial={section} />)
}

const ACCENTS = ['#4f7cff', '#7c5cff', '#d946ef', '#ec4899', '#ef4444', '#f97316', '#eab308', '#22c55e', '#14b8a6', '#06b6d4', '#64748b']

type Sec = 'appearance' | 'panels' | 'transfer' | 'queue' | 'editor' | 'terminal' | 'confirm' | 'security' | 'commands' | 'logging' | 'shortcuts' | 'about'

function Preferences({ close, initial }: { close: () => void; initial?: string }) {
  const prefs = useStore((s) => s.prefs)!
  const info = useStore((s) => s.info)!
  const [sec, setSec] = useState<Sec>((initial as Sec) || 'appearance')
  const [sampleDate] = useState(() => new Date(Date.now() - 5 * 60 * 1000).toISOString())
  const up = (p: Partial<Prefs>) => savePrefs(p)
  const sections: { id: Sec; label: string; icon: any }[] = [
    { id: 'appearance', label: t('prefs.appearance'), icon: Palette },
    { id: 'panels', label: t('prefs.panels'), icon: LayoutPanelLeft },
    { id: 'transfer', label: t('prefs.transfer'), icon: ArrowDownUp },
    { id: 'queue', label: t('prefs.queue'), icon: ListOrdered },
    { id: 'editor', label: t('prefs.editor'), icon: Code2 },
    { id: 'terminal', label: t('prefs.terminal'), icon: SquareTerminal },
    { id: 'confirm', label: t('prefs.confirmations'), icon: CheckSquare },
    { id: 'security', label: t('prefs.security'), icon: Shield },
    { id: 'commands', label: t('prefs.commands'), icon: Wand2 },
    { id: 'logging', label: t('prefs.logging'), icon: ScrollText },
    { id: 'shortcuts', label: t('prefs.shortcuts'), icon: Keyboard },
    { id: 'about', label: t('about.title'), icon: Info },
  ]
  return (
    <Dialog size="xl" title={t('prefs.title')} icon={<Settings />} onClose={close} noPadding footer={<><span className="faint">{t('prefs.configDir')}: <span className="mono selectable">{displayPath('local', info.configDir)}</span>{info.portable ? ` (${t('prefs.portable')})` : ''}</span><div className="spacer" /><button className="btn primary" onClick={close}>{t('common.close')}</button></>}>
      <div className="prefs">
        <nav className="vtabs">
          {sections.map((s) => (
            <button key={s.id} className={sec === s.id ? 'on' : ''} onClick={() => setSec(s.id)}>
              <s.icon /> {s.label}
            </button>
          ))}
        </nav>
        <div className="prefs-body">
          {sec === 'appearance' && (
            <div className="form">
              <div className="field">
                <label>{t('prefs.language')}</label>
                <div className="segmented">
                  {languages.map((l) => <button key={l.id} className={prefs.language === l.id ? 'on' : ''} onClick={() => up({ language: l.id })}>{l.label}</button>)}
                </div>
              </div>
              <div className="field">
                <label>{t('prefs.theme')}</label>
                <div className="segmented">
                  {(['system', 'light', 'dark'] as const).map((v) => <button key={v} className={prefs.theme === v ? 'on' : ''} onClick={() => up({ theme: v })}>{t(`prefs.theme.${v}`)}</button>)}
                </div>
              </div>
              <div className="field">
                <label>{t('prefs.accent')}</label>
                <div className="color-row">
                  {ACCENTS.map((c) => (
                    <button key={c} className={`swatch lg${prefs.accent === c ? ' on' : ''}`} style={{ background: c }} onClick={() => up({ accent: c })}>
                      {prefs.accent === c && <Check />}
                    </button>
                  ))}
                  <input type="color" className="color-input" value={prefs.accent} onChange={(e) => up({ accent: e.target.value })} />
                </div>
              </div>
              <div className="field">
                <label>{t('prefs.density')}</label>
                <div className="segmented">
                  {(['compact', 'comfortable', 'spacious'] as const).map((v) => <button key={v} className={prefs.density === v ? 'on' : ''} onClick={() => up({ density: v })}>{t(`prefs.density.${v}`)}</button>)}
                </div>
              </div>
              <div className="field">
                <label>{t('prefs.fontSize')} — {prefs.fontSize}px</label>
                <input type="range" min={11} max={16} step={0.5} value={prefs.fontSize} onChange={(e) => up({ fontSize: parseFloat(e.target.value) })} style={{ width: 260 }} />
              </div>
              <div className="field">
                <label>{t('prefs.interface')}</label>
                <div className="segmented">
                  <button className={prefs.interface === 'commander' ? 'on' : ''} onClick={() => up({ interface: 'commander' })}>{t('prefs.commander')}</button>
                  <button className={prefs.interface === 'explorer' ? 'on' : ''} onClick={() => up({ interface: 'explorer' })}>{t('prefs.explorer')}</button>
                </div>
              </div>
            </div>
          )}

          {sec === 'panels' && (
            <div className="form">
              <label className="switch"><input type="checkbox" checked={prefs.showHidden} onChange={(e) => up({ showHidden: e.target.checked })} /> {t('prefs.showHidden')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.dirsFirst} onChange={(e) => up({ dirsFirst: e.target.checked })} /> {t('prefs.dirsFirst')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.naturalSort} onChange={(e) => up({ naturalSort: e.target.checked })} /> {t('prefs.naturalSort')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.showFKeyBar} onChange={(e) => up({ showFKeyBar: e.target.checked })} /> {t('prefs.fkeys')}</label>
              <div className="field-row">
                <div className="field">
                  <label>{t('prefs.sizeFormat')}</label>
                  <select className="select" value={prefs.sizeFormat} onChange={(e) => up({ sizeFormat: e.target.value as any })}>
                    <option value="iec">{formatSize(1572864, 'iec')} (1024)</option>
                    <option value="si">{formatSize(1572864, 'si')} (1000)</option>
                    <option value="bytes">{formatSize(1572864, 'bytes')}</option>
                  </select>
                </div>
                <div className="field">
                  <label>{t('prefs.dateFormat')}</label>
                  <select className="select" value={prefs.dateFormat} onChange={(e) => up({ dateFormat: e.target.value as any })}>
                    <option value="short">{formatDate(sampleDate, 'short')}</option>
                    <option value="relative">{formatDate(sampleDate, 'relative')}</option>
                    <option value="iso">{formatDate(sampleDate, 'iso')}</option>
                  </select>
                </div>
              </div>
              <div className="field">
                <label>{t('prefs.doubleClick')}</label>
                <div className="segmented">
                  {(['open', 'edit', 'copy'] as const).map((v) => <button key={v} className={prefs.doubleClick === v ? 'on' : ''} onClick={() => up({ doubleClick: v })}>{t(`prefs.dc.${v}`)}</button>)}
                </div>
              </div>
              <label className="switch"><input type="checkbox" checked={prefs.restoreTabs} onChange={(e) => up({ restoreTabs: e.target.checked })} /> {t('prefs.restoreTabs')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.autoReconnect} onChange={(e) => up({ autoReconnect: e.target.checked })} /> {t('prefs.autoReconnect')}</label>
            </div>
          )}

          {sec === 'transfer' && (
            <TransferOptionsForm
              o={{ ...prefs.transfer, parallel: 2 } as any}
              up={(p: any) => {
                const { parallel, move, ...rest } = p
                void parallel
                void move
                up({ transfer: { ...prefs.transfer, ...rest } })
              }}
            />
          )}

          {sec === 'queue' && (
            <div className="form">
              <div className="field"><label>{t('prefs.maxParallel')}</label><input className="input tnum" type="number" min={1} max={16} style={{ width: 120 }} value={prefs.queue.maxParallel} onChange={(e) => up({ queue: { ...prefs.queue, maxParallel: parseInt(e.target.value, 10) || 1 } })} /></div>
              <div className="field"><label>{t('prefs.retry')}</label><input className="input tnum" type="number" min={0} max={20} style={{ width: 120 }} value={prefs.queue.retryCount} onChange={(e) => up({ queue: { ...prefs.queue, retryCount: parseInt(e.target.value, 10) || 0 } })} /></div>
              <label className="switch"><input type="checkbox" checked={prefs.queue.continueOnError} onChange={(e) => up({ queue: { ...prefs.queue, continueOnError: e.target.checked } })} /> {t('prefs.continueOnError')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.queue.autoPopup} onChange={(e) => up({ queue: { ...prefs.queue, autoPopup: e.target.checked } })} /> {t('prefs.autoPopup')}</label>
            </div>
          )}

          {sec === 'editor' && (
            <div className="form">
              <div className="field">
                <label>{t('prefs.editorDefault')}</label>
                <div className="segmented">
                  <button className={prefs.editor.default === 'internal' ? 'on' : ''} onClick={() => up({ editor: { ...prefs.editor, default: 'internal' } })}>{t('prefs.editorInternal')}</button>
                  <button className={prefs.editor.default === 'external' ? 'on' : ''} disabled={info.mode === 'web'} onClick={() => up({ editor: { ...prefs.editor, default: 'external' } })}>{t('prefs.editorExternal')}</button>
                </div>
              </div>
              <div className="field"><label>{t('prefs.externalCmd')}</label><input className="input mono" value={prefs.editor.externalCmd} placeholder="code --wait !.!" onChange={(e) => up({ editor: { ...prefs.editor, externalCmd: e.target.value } })} /><span className="hint">{t('prefs.externalCmdHint')}</span></div>
              <div className="field-row r3">
                <div className="field"><label>{t('prefs.tabSize')}</label><input className="input tnum" type="number" min={1} max={8} value={prefs.editor.tabSize} onChange={(e) => up({ editor: { ...prefs.editor, tabSize: parseInt(e.target.value, 10) || 4 } })} /></div>
                <div className="field"><label>{t('prefs.fontSize')}</label><input className="input tnum" type="number" min={9} max={24} value={prefs.editor.fontSize} onChange={(e) => up({ editor: { ...prefs.editor, fontSize: parseInt(e.target.value, 10) || 13 } })} /></div>
                <div className="field"><label>{t('prefs.maxSize')}</label><input className="input tnum" type="number" min={1} max={200} value={prefs.editor.maxSizeMB} onChange={(e) => up({ editor: { ...prefs.editor, maxSizeMB: parseInt(e.target.value, 10) || 20 } })} /></div>
              </div>
              <label className="switch"><input type="checkbox" checked={prefs.editor.wordWrap} onChange={(e) => up({ editor: { ...prefs.editor, wordWrap: e.target.checked } })} /> {t('prefs.wordWrap')}</label>
              <div className="form-section">
                <h4>{t('prefs.associations')}</h4>
                {(prefs.editor.associations || []).map((a, i) => (
                  <div key={i} className="row">
                    <input className="input mono" style={{ width: 160 }} value={a.mask} placeholder="*.psd" onChange={(e) => up({ editor: { ...prefs.editor, associations: prefs.editor.associations.map((x, k) => (k === i ? { ...x, mask: e.target.value } : x)) } })} />
                    <input className="input mono" value={a.command} placeholder="internal | gimp !.!" onChange={(e) => up({ editor: { ...prefs.editor, associations: prefs.editor.associations.map((x, k) => (k === i ? { ...x, command: e.target.value } : x)) } })} />
                    <button className="icon-btn sm" onClick={() => up({ editor: { ...prefs.editor, associations: prefs.editor.associations.filter((_, k) => k !== i) } })}><Trash2 /></button>
                  </div>
                ))}
                <div><button className="btn sm" onClick={() => up({ editor: { ...prefs.editor, associations: [...(prefs.editor.associations || []), { mask: '', command: '' }] } })}><Plus /> {t('common.add')}</button></div>
              </div>
            </div>
          )}

          {sec === 'terminal' && (
            <div className="form">
              <div className="field-row">
                <div className="field"><label>{t('prefs.fontSize')}</label><input className="input tnum" type="number" min={9} max={24} value={prefs.terminal.fontSize} onChange={(e) => up({ terminal: { ...prefs.terminal, fontSize: parseInt(e.target.value, 10) || 13 } })} /></div>
                <div className="field"><label>{t('prefs.scrollback')}</label><input className="input tnum" type="number" min={500} step={500} value={prefs.terminal.scrollback} onChange={(e) => up({ terminal: { ...prefs.terminal, scrollback: parseInt(e.target.value, 10) || 10000 } })} /></div>
              </div>
              <div className="field"><label>{t('prefs.termFont')}</label><input className="input mono" value={prefs.terminal.fontFamily} placeholder="JetBrains Mono, Cascadia Code, monospace" onChange={(e) => up({ terminal: { ...prefs.terminal, fontFamily: e.target.value } })} /></div>
              <div className="field"><label>TERM</label><input className="input mono" style={{ width: 200 }} value={prefs.terminal.term} onChange={(e) => up({ terminal: { ...prefs.terminal, term: e.target.value } })} /></div>
              <label className="switch"><input type="checkbox" checked={prefs.terminal.cursorBlink} onChange={(e) => up({ terminal: { ...prefs.terminal, cursorBlink: e.target.checked } })} /> {t('prefs.cursorBlink')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.terminal.copyOnSelect} onChange={(e) => up({ terminal: { ...prefs.terminal, copyOnSelect: e.target.checked } })} /> {t('prefs.copyOnSelect')}</label>
            </div>
          )}

          {sec === 'confirm' && (
            <div className="form">
              <label className="switch"><input type="checkbox" checked={prefs.confirmDelete} onChange={(e) => up({ confirmDelete: e.target.checked })} /> {t('prefs.confirmDelete')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.confirmOverwrite} onChange={(e) => up({ confirmOverwrite: e.target.checked })} /> {t('prefs.confirmOverwrite')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.confirmTransfer} onChange={(e) => up({ confirmTransfer: e.target.checked })} /> {t('prefs.confirmTransfer')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.confirmDragDrop} onChange={(e) => up({ confirmDragDrop: e.target.checked })} /> {t('prefs.confirmDrag')}</label>
              <label className="switch"><input type="checkbox" checked={prefs.confirmExit} onChange={(e) => up({ confirmExit: e.target.checked })} /> {t('prefs.confirmExit')}</label>
            </div>
          )}

          {sec === 'security' && <SecuritySection />}
          {sec === 'commands' && <CommandsSection />}

          {sec === 'logging' && (
            <div className="form">
              <label className="switch"><input type="checkbox" checked={prefs.logging.toFile} onChange={(e) => up({ logging: { ...prefs.logging, toFile: e.target.checked } })} /> {t('prefs.logToFile')}</label>
              <div className="field"><label>{t('prefs.logLevel')}</label>
                <div className="segmented">
                  {['normal', 'debug'].map((v) => <button key={v} className={prefs.logging.level === v ? 'on' : ''} onClick={() => up({ logging: { ...prefs.logging, level: v } })}>{v}</button>)}
                </div>
              </div>
              <span className="hint mono">{displayPath('local', info.configDir)}/logs</span>
            </div>
          )}

          {sec === 'shortcuts' && <Shortcuts />}

          {sec === 'about' && (
            <div className="about">
              <img src="/favicon.svg" width={72} height={72} alt="" />
              <h2>Skiff <span className="faint">{info.version}</span></h2>
              <p className="muted">{t('about.desc')}</p>
              <dl className="kv" style={{ margin: '0 auto', textAlign: 'left' }}>
                <dt>{t('about.platform')}</dt><dd>{info.os}/{info.arch}</dd>
                <dt>{t('about.mode')}</dt><dd>{t(`about.mode.${info.mode}`)}</dd>
                <dt>{t('about.config')}</dt><dd className="mono">{displayPath('local', info.configDir)}</dd>
                <dt>{t('about.agent')}</dt><dd>{info.agent ? '✓' : '—'}</dd>
              </dl>
              <p className="faint" style={{ fontSize: 12 }}>SFTP · SCP · FTP/FTPS · WebDAV · S3 — Go + React</p>
            </div>
          )}
        </div>
      </div>
    </Dialog>
  )
}

function SecuritySection() {
  const info = useStore((s) => s.info)!
  const [hosts, setHosts] = useState<KnownHost[]>([])
  const [keys, setKeys] = useState<KeyFile[]>([])
  const load = () => {
    call<KnownHost[]>('hostkeys.list').then(setHosts).catch(() => {})
    call<KeyFile[]>('keys.list').then(setKeys).catch(() => {})
  }
  useEffect(load, [])
  return (
    <div className="form">
      <div className="form-section">
        <h4>{t('prefs.masterPassword')}</h4>
        <p className="muted" style={{ margin: 0 }}>{t('prefs.masterPasswordDesc')}</p>
        <div className="row" style={{ flexWrap: 'wrap' }}>
          {!info.hasMaster ? (
            <button className="btn primary" onClick={() => masterPasswordDialog('set')}><Shield /> {t('prefs.setMaster')}</button>
          ) : (
            <>
              <span className="badge ok"><Shield /> {t('common.on')}</span>
              <button className="btn" onClick={() => masterPasswordDialog('change')}>{t('prefs.changeMaster')}</button>
              <button className="btn" onClick={() => masterPasswordDialog('remove')}>{t('prefs.removeMaster')}</button>
              {!info.locked && <button className="btn" onClick={() => call('security.lock')}><Lock /> {t('prefs.lockNow')}</button>}
            </>
          )}
        </div>
      </div>
      <div className="form-section">
        <h4>{t('prefs.keys')}</h4>
        {keys.length > 0 && (
          <table className="table">
            <tbody>
              {keys.map((k) => (
                <tr key={k.path}>
                  <td><KeyRound style={{ width: 14 }} /> <span className="mono">{displayPath('local', k.path)}</span></td>
                  <td className="muted" style={{ whiteSpace: 'nowrap' }}>{k.type}</td>
                  <td className="mono faint" style={{ fontSize: 11 }}>{k.fingerprint}</td>
                  <td>{k.encrypted ? <span className="badge">🔒</span> : ''} <span className="badge">{k.format}</span></td>
                  <td>{info.mode !== 'web' && <button className="btn sm ghost" onClick={() => openConvertKey(k).then(load)}>{t('keygen.convert')}</button>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div><button className="btn" onClick={() => openKeygen().then(load)}><KeyRound /> {t('prefs.generateKey')}</button></div>
      </div>
      <div className="form-section">
        <h4>{t('prefs.knownHosts')}</h4>
        {hosts.length === 0 ? <p className="faint" style={{ margin: 0 }}>—</p> : (
          <table className="table">
            <tbody>
              {hosts.map((h) => (
                <tr key={h.line}>
                  <td><Fingerprint style={{ width: 14 }} /> <span className="mono">{h.hosts}</span></td>
                  <td className="muted">{h.keyType}</td>
                  <td className="mono faint" style={{ fontSize: 11 }}>{h.fingerprint}</td>
                  <td><button className="icon-btn sm" onClick={async () => { if (await ask({ title: t('common.delete'), message: h.hosts, danger: true })) { await call('hostkeys.remove', { line: h.line }); load() } }}><Trash2 /></button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function CommandsSection() {
  const prefs = useStore((s) => s.prefs)!
  const cmds = prefs.customCommands || []
  const set = (list: CustomCommand[]) => savePrefs({ customCommands: list })
  return (
    <div className="form">
      <p className="muted" style={{ margin: 0, fontSize: 12 }}>
        {t('cmd.patterns')} <code>!</code> {t('cmd.p.file')} · <code>!&amp;</code> {t('cmd.p.selected')} · <code>!/</code> {t('cmd.p.path')} · <code>!@</code> {t('cmd.p.host')} · <code>!U</code> {t('cmd.p.user')} · <code>!?Question?défaut!</code> {t('cmd.p.ask')}
      </p>
      {cmds.map((c, i) => (
        <div key={c.id} className="card" style={{ padding: 12 }}>
          <div className="row" style={{ marginBottom: 8 }}>
            <input className="input" style={{ width: 220 }} value={commandName(c)} onChange={(e) => set(cmds.map((x, k) => (k === i ? { ...x, name: e.target.value } : x)))} />
            <div className="spacer" />
            <button className="icon-btn sm" onClick={() => set(cmds.filter((_, k) => k !== i))}><Trash2 /></button>
          </div>
          <input className="input mono" value={c.command} onChange={(e) => set(cmds.map((x, k) => (k === i ? { ...x, command: e.target.value } : x)))} />
          <div className="row" style={{ marginTop: 8, gap: 16, flexWrap: 'wrap' }}>
            <label className="check"><input type="checkbox" checked={c.perFile} onChange={(e) => set(cmds.map((x, k) => (k === i ? { ...x, perFile: e.target.checked } : x)))} /> {t('cmd.perFile')}</label>
            <label className="check"><input type="checkbox" checked={c.applyDirs} onChange={(e) => set(cmds.map((x, k) => (k === i ? { ...x, applyDirs: e.target.checked } : x)))} /> {t('cmd.dirs')}</label>
            <label className="check"><input type="checkbox" checked={c.local} onChange={(e) => set(cmds.map((x, k) => (k === i ? { ...x, local: e.target.checked } : x)))} /> {t('cmd.local')}</label>
            <label className="check"><input type="checkbox" checked={c.showOutput} onChange={(e) => set(cmds.map((x, k) => (k === i ? { ...x, showOutput: e.target.checked } : x)))} /> {t('cmd.showOutput')}</label>
          </div>
        </div>
      ))}
      <div><button className="btn sm" onClick={() => set([...cmds, { id: Math.random().toString(36).slice(2), name: t('cmd.new'), command: '', local: false, perFile: false, applyDirs: false, recursive: false, showOutput: true }])}><Plus /> {t('cmd.add')}</button></div>
    </div>
  )
}

const SHORTCUTS: [string, string][] = [
  ['Ctrl+K', 'sc.palette'], ['Tab', 'sc.switchPanel'], ['Enter', 'sc.open'], ['Backspace / Alt+↑', 'sc.parent'], ['Alt+← / Alt+→', 'sc.history'],
  ['F2', 'sc.rename'], ['F3', 'sc.view'], ['F4 / Shift+F4', 'sc.edit'], ['F5 / Shift+F5', 'sc.copy'], ['F6', 'sc.move'],
  ['F7 / Shift+F7', 'sc.newItem'], ['F8 / Del', 'sc.delete'], ['Shift+Del', 'sc.deletePerm'], ['F9', 'sc.properties'], ['F10', 'sc.closeTab'],
  ['F12', 'sc.queue'], ['Space / Insert', 'sc.toggleSel'], ['Ctrl+A', 'sc.selectAll'], ['Num + / − / *', 'sc.selMask'], ['Ctrl+C / X / V', 'sc.clipboard'],
  ['Ctrl+F', 'sc.filter'], ['Ctrl+Shift+F', 'sc.find'], ['Ctrl+L', 'sc.editPath'], ['Ctrl+O', 'sc.openDir'], ['Ctrl+D', 'sc.bookmark'],
  ['Ctrl+R', 'sc.refresh'], ['Ctrl+S', 'sc.sync'], ['Ctrl+=', 'sc.compare'], ['Ctrl+B', 'sc.syncBrowse'], ['Ctrl+U', 'sc.keepUp'],
  ['Ctrl+T', 'sc.terminal'], ['Ctrl+Shift+X', 'sc.runCmd'], ['Ctrl+Shift+C', 'sc.copyPath'], ['Ctrl+Shift+P', 'sc.preview'], ['Ctrl+Alt+H', 'sc.hidden'],
  ['Ctrl+Tab', 'sc.nextTab'], ['Ctrl+W', 'sc.closeTab'], ['Ctrl+N', 'sc.newSession'], ['Alt+1…9', 'sc.gotoTab'], ['Ctrl+,', 'sc.prefs'],
]

function Shortcuts() {
  return (
    <table className="table">
      <tbody>
        {SHORTCUTS.map(([k, d]) => (
          <tr key={k}>
            <td style={{ width: 200 }}>{k.split(' / ').map((x, i) => <span key={i}>{i > 0 && ' / '}<kbd>{x}</kbd></span>)}</td>
            <td>{t(d)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

void get
void FileCog
