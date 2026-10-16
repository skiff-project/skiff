import { useEffect, useState } from 'react'
import {
  ArrowDownUp, Check, CheckSquare, Code2, FileCog, Fingerprint, Info, KeyRound, Keyboard, LayoutPanelLeft, ListOrdered, Palette, Plus, ScrollText,
  Lock, Settings, Shield, SquareTerminal, Trash2, Wand2, KeySquare, Copy, Pencil, Laptop, RefreshCw,
} from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { errorToast, get, toast, useStore } from '../../lib/store'
import { displayPath, savePrefs } from '../../lib/actions'
import { commandName, languages, snippetName, t } from '../../lib/i18n'
import type { CustomCommand, DevSyncConfig, DevSyncView, KeychainKey, KeyFile, KnownHost, Prefs, Shell, Snippet } from '../../lib/types'
import { TransferOptionsForm } from './TransferDialog'
import { formatDate, formatSize } from '../../lib/format'
import { masterPasswordDialog, PasswordMeter } from './MasterPassword'
import { loadShells } from '../../lib/actions'
import { devSyncSummary } from '../../lib/summaries'
import { openConvertKey, openKeygen } from './KeygenDialog'
import { openKeychainAdd } from './KeychainDialog'
import { copyToClipboard } from '../../lib/fileops'
import { ask, prompt } from './common'

export function openPreferences(section?: string) {
  openDialog((close) => <Preferences close={close} initial={section} />)
}

const ACCENTS = ['#4f7cff', '#7c5cff', '#d946ef', '#ec4899', '#ef4444', '#f97316', '#eab308', '#22c55e', '#14b8a6', '#06b6d4', '#64748b']

type Sec = 'appearance' | 'panels' | 'transfer' | 'queue' | 'editor' | 'terminal' | 'ssh' | 'devices' | 'confirm' | 'security' | 'commands' | 'logging' | 'shortcuts' | 'about'

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
    { id: 'ssh', label: 'SSH', icon: FileCog },
    { id: 'devices', label: t('devsync.section'), icon: Laptop },
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
          {sec === 'ssh' && <SshPrefs prefs={prefs} up={up} />}
          {sec === 'devices' && <DevicesSection />}
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
              <div className="form-section">
                <h4>{t('prefs.segments')}</h4>
                <label className="switch"><input type="checkbox" checked={prefs.queue.segmentSize > 0} onChange={(e) => up({ queue: { ...prefs.queue, segmentSize: e.target.checked ? 100 << 20 : 0 } })} /> {t('prefs.segmentsOn')}</label>
                <div className="field-row">
                  <div className="field"><label>{t('prefs.segmentThreshold')}</label><input className="input tnum" type="number" min={1} max={100000} disabled={prefs.queue.segmentSize <= 0} value={prefs.queue.segmentSize > 0 ? Math.round(prefs.queue.segmentSize / (1 << 20)) : ''} onChange={(e) => up({ queue: { ...prefs.queue, segmentSize: Math.max(1, parseInt(e.target.value, 10) || 1) * (1 << 20) } })} /></div>
                  <div className="field"><label>{t('prefs.segmentCount')}</label><input className="input tnum" type="number" min={2} max={8} disabled={prefs.queue.segmentSize <= 0} value={prefs.queue.segments || 4} onChange={(e) => up({ queue: { ...prefs.queue, segments: Math.min(8, Math.max(2, parseInt(e.target.value, 10) || 4)) } })} /></div>
                </div>
                <span className="hint">{t('prefs.segmentsHint')}</span>
              </div>
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
                    <button className="icon-btn sm" data-tip={t('common.delete')} onClick={() => up({ editor: { ...prefs.editor, associations: prefs.editor.associations.filter((_, k) => k !== i) } })}><Trash2 /></button>
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
              <div className="field">
                <label>{t('prefs.copyKeys')}</label>
                <div className="segmented">
                  <button className={prefs.terminal.copyKeys !== 'ctrlShift' ? 'on' : ''} onClick={() => up({ terminal: { ...prefs.terminal, copyKeys: 'ctrl' } })}>{t('prefs.copyKeys.ctrl')}</button>
                  <button className={prefs.terminal.copyKeys === 'ctrlShift' ? 'on' : ''} onClick={() => up({ terminal: { ...prefs.terminal, copyKeys: 'ctrlShift' } })}>{t('prefs.copyKeys.ctrlShift')}</button>
                </div>
                <span className="hint">{t('prefs.copyKeysHint')}</span>
              </div>
              <div className="field">
                <label>{t('prefs.rightClick')}</label>
                <div className="segmented">
                  <button className={prefs.terminal.rightClick !== 'menu' ? 'on' : ''} onClick={() => up({ terminal: { ...prefs.terminal, rightClick: 'copyPaste' } })}>{t('prefs.rightClick.copyPaste')}</button>
                  <button className={prefs.terminal.rightClick === 'menu' ? 'on' : ''} onClick={() => up({ terminal: { ...prefs.terminal, rightClick: 'menu' } })}>{t('prefs.rightClick.menu')}</button>
                </div>
              </div>
              <LocalShellField value={prefs.terminal.localShell || ''} onChange={(v) => up({ terminal: { ...prefs.terminal, localShell: v || undefined } })} />
              <div className="form-section">
                <h4>{t('term.snippets')}</h4>
                <p className="muted" style={{ margin: 0 }}>{t('prefs.snippetsHint')}</p>
                {(prefs.terminal.snippets || []).map((s, i) => {
                  const setS = (patch: Partial<Snippet>) => up({ terminal: { ...prefs.terminal, snippets: prefs.terminal.snippets.map((x, k) => (k === i ? { ...x, ...patch } : x)) } })
                  return (
                    <div key={s.id} className="row snippet-row">
                      <input className="input" style={{ width: 200 }} value={snippetName(s)} placeholder={t('keychain.name')} onChange={(e) => setS({ name: e.target.value })} />
                      <input className="input mono grow" value={s.command} placeholder="systemctl status nginx" onChange={(e) => setS({ command: e.target.value })} />
                      <label className="check" data-tip={t('prefs.snippetRunTip')}><input type="checkbox" checked={s.run} onChange={(e) => setS({ run: e.target.checked })} /> {t('prefs.snippetRun')}</label>
                      <button className="icon-btn sm" data-tip={t('common.delete')} onClick={() => up({ terminal: { ...prefs.terminal, snippets: prefs.terminal.snippets.filter((_, k) => k !== i) } })}><Trash2 /></button>
                    </div>
                  )
                })}
                <div><button className="btn sm" onClick={() => up({ terminal: { ...prefs.terminal, snippets: [...(prefs.terminal.snippets || []), { id: Math.random().toString(36).slice(2), name: '', command: '', run: true }] } })}><Plus /> {t('common.add')}</button></div>
              </div>
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
  const [chain, setChain] = useState<{ keys: KeychainKey[]; locked: boolean }>({ keys: [], locked: false })
  const load = () => {
    call<KnownHost[]>('hostkeys.list').then(setHosts).catch(() => {})
    call<KeyFile[]>('keys.list').then(setKeys).catch(() => {})
    call<{ keys: KeychainKey[]; locked: boolean }>('keychain.list').then(setChain).catch(() => {})
  }
  const toKeychain = async (k: KeyFile) => {
    let passphrase = ''
    if (k.encrypted) {
      const p = await prompt({ title: t('keychain.add'), label: t('keygen.currentPassphrase'), password: true })
      if (p === undefined) return
      passphrase = p
    }
    try {
      const added = await call<KeychainKey>('keychain.add', { path: k.path, passphrase })
      toast('ok', t('keychain.added'), added.name)
      load()
    } catch (e) {
      errorToast(t('keychain.add'), e)
    }
  }
  const inChain = (fp: string) => chain.keys.some((k) => k.fingerprint === fp)
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
        <h4>{t('keychain.title')}</h4>
        <p className="muted" style={{ margin: 0 }}>{t('keychain.desc')}</p>
        {chain.locked && <div className="alert warn"><Lock /><span>{t('keychain.locked')}</span></div>}
        {chain.keys.length > 0 && (
          <table className="table keychain">
            <tbody>
              {chain.keys.map((k) => (
                <tr key={k.id}>
                  <td><KeySquare style={{ width: 14 }} /> <span className="strong">{k.name}</span></td>
                  <td className="muted" style={{ whiteSpace: 'nowrap' }}>{k.type}</td>
                  <td className="mono faint" style={{ fontSize: 11 }}>{k.fingerprint}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="icon-btn sm" data-tip={t('keychain.copyPublic')} onClick={() => copyToClipboard(k.publicKey)}><Copy /></button>
                    <button className="icon-btn sm" data-tip={t('common.rename')} onClick={async () => { const n = await prompt({ title: t('common.rename'), label: t('keychain.name'), value: k.name }); if (n) { await call('keychain.rename', { id: k.id, name: n }); load() } }}><Pencil /></button>
                    <button className="icon-btn sm" data-tip={t('common.delete')} onClick={async () => { if (await ask({ title: t('keychain.remove'), message: t('keychain.removeConfirm', { name: k.name }), danger: true, ok: t('common.delete') })) { await call('keychain.remove', { id: k.id }); load() } }}><Trash2 /></button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div><button className="btn" disabled={chain.locked} onClick={() => openKeychainAdd().then((k) => k && load())}><Plus /> {t('keychain.add')}</button></div>
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
                  <td style={{ whiteSpace: 'nowrap' }}>
                    {info.mode !== 'web' && <button className="btn sm ghost" onClick={() => openConvertKey(k).then(load)}>{t('keygen.convert')}</button>}
                    {info.mode !== 'web' && (inChain(k.fingerprint)
                      ? <span className="badge ok">{t('keychain.inChain')}</span>
                      : <button className="btn sm ghost" disabled={chain.locked} onClick={() => toKeychain(k)}>{t('keychain.toChain')}</button>)}
                  </td>
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
                  <td><button className="icon-btn sm" data-tip={t('common.delete')} onClick={async () => { if (await ask({ title: t('common.delete'), message: h.hosts, danger: true })) { await call('hostkeys.remove', { line: h.line }); load() } }}><Trash2 /></button></td>
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
            <button className="icon-btn sm" data-tip={t('common.delete')} onClick={() => set(cmds.filter((_, k) => k !== i))}><Trash2 /></button>
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

const SCOPES: Record<string, string> = { user: 'sshcfg.scope.user', system: 'sshcfg.scope.system', include: 'sshcfg.scope.include' }

function SshPrefs({ prefs, up }: { prefs: Prefs; up: (p: Partial<Prefs>) => void }) {
  const sshConfig = useStore((s) => s.sshConfig)
  const ssh = prefs.ssh || { ignoreConfig: false }
  const [file, setFile] = useState(ssh.configFile || '')
  const saveFile = () => {
    if (file.trim() !== (ssh.configFile || '')) up({ ssh: { ...ssh, configFile: file.trim() || undefined } })
  }
  return (
    <div className="form">
      <div className="form-section">
        <h4>{t('sshcfg.title')}</h4>
        <label className="switch"><input type="checkbox" checked={!ssh.ignoreConfig} onChange={(e) => up({ ssh: { ...ssh, ignoreConfig: !e.target.checked } })} /> {t('sshcfg.read')}</label>
        <span className="hint">{t('sshcfg.readHint')}</span>
        <div className="field">
          <label>{t('sshcfg.userFile')}</label>
          <input className="input mono" value={file} placeholder={(!ssh.configFile && sshConfig?.files.find((f) => f.scope === 'user')?.path) || t('sshcfg.defaultFile')} onChange={(e) => setFile(e.target.value)} onBlur={saveFile} onKeyDown={(e) => { if (e.key === 'Enter') saveFile() }} spellCheck={false} />
          <span className="hint">{t('sshcfg.userFileHint')}</span>
        </div>
      </div>
      <div className="form-section">
        <h4>{t('sshcfg.files')}</h4>
        <div className="sshcfg-files">
          {(sshConfig?.files || []).map((f) => (
            <div key={f.path} className="sshcfg-file">
              <span className="faint">{t(SCOPES[f.scope])}</span>
              <span className="mono ellipsis selectable" title={f.path}>{f.path}</span>
              {f.error ? <span className="badge danger" data-tip={f.error}>{t('sshcfg.fileError')}</span> : f.exists ? <span className="badge ok">{t('sshcfg.found')}</span> : <span className="badge">{t('sshcfg.missing')}</span>}
            </div>
          ))}
        </div>
        <span className="hint">{t('sshcfg.osHint')}</span>
        {sshConfig?.enabled && <span className="hint">{t('sshcfg.hostsCount', { n: sshConfig.hosts.length })}</span>}
      </div>
    </div>
  )
}

function DevicesSection() {
  const live = useStore((s) => s.devsync)
  const sites = useStore((s) => s.sites)
  const [v, setV] = useState<DevSyncView | null>(live)
  const [conf, setConf] = useState<DevSyncConfig | null>(live)
  const [pass, setPass] = useState('')
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    call<DevSyncView>('devsync.get').then((x) => { useStore.setState({ devsync: x }); setV(x); setConf(x) }).catch(() => {})
  }, [])
  useEffect(() => {
    if (live) setV(live)
  }, [live])
  if (!v || !conf) return null
  const up = (p: Partial<DevSyncConfig>) => { setConf({ ...conf, ...p }); setDirty(true) }
  const save = async (): Promise<boolean> => {
    try {
      const x = await call<DevSyncView>('devsync.set', { config: conf, passphrase: pass || undefined })
      useStore.setState({ devsync: x })
      setV(x)
      setPass('')
      setDirty(false)
      return true
    } catch (e) {
      errorToast(t('devsync.section'), e)
      return false
    }
  }
  const syncNow = async () => {
    if ((dirty || pass) && !(await save())) return
    setBusy(true)
    try {
      await call('devsync.now')
      toast('ok', t('devsync.done'))
    } catch (e) {
      errorToast(t('devsync.now'), e)
    } finally {
      setBusy(false)
    }
  }
  const st = v.state
  return (
    <div className="form">
      <div className="form-section">
        <h4>{t('devsync.title')}</h4>
        <p className="muted" style={{ margin: 0 }}>{t('devsync.desc')}</p>
        <div className="field-row">
          <div className="field">
            <label>{t('devsync.site')}</label>
            <select className="select" value={conf.siteId} onChange={(e) => up({ siteId: e.target.value })}>
              <option value="">—</option>
              {sites.map((x) => <option key={x.id} value={x.id}>{x.name || x.host}</option>)}
            </select>
          </div>
          <div className="field"><label>{t('devsync.path')}</label><input className="input mono" value={conf.path} onChange={(e) => up({ path: e.target.value })} spellCheck={false} /></div>
        </div>
        <div className="field-row">
          <div className="field">
            <label>{t('devsync.passphrase')}</label>
            <input className="input" type="password" autoComplete="new-password" value={pass} placeholder={v.hasPassphrase ? t('site.passwordSaved') : ''} onChange={(e) => { setPass(e.target.value); setDirty(true) }} />
            {pass && <PasswordMeter pw={pass} />}
          </div>
          <div className="field"><label>{t('devsync.device')}</label><input className="input" value={conf.device} onChange={(e) => up({ device: e.target.value })} /></div>
        </div>
        <span className="hint">{t('devsync.passphraseHint')}</span>
        <div className="check-grid">
          <label className="check"><input type="checkbox" checked={conf.sites} onChange={(e) => up({ sites: e.target.checked })} /> {t('devsync.what.sites')}</label>
          <label className="check"><input type="checkbox" checked={conf.keychain} onChange={(e) => up({ keychain: e.target.checked })} /> {t('devsync.what.keychain')}</label>
          <label className="check"><input type="checkbox" checked={conf.prefs} onChange={(e) => up({ prefs: e.target.checked })} /> {t('devsync.what.prefs')}</label>
          <label className="check"><input type="checkbox" checked={conf.hostKeys} onChange={(e) => up({ hostKeys: e.target.checked })} /> {t('devsync.what.hostKeys')}</label>
        </div>
        <label className="switch"><input type="checkbox" checked={conf.enabled} onChange={(e) => up({ enabled: e.target.checked })} /> {t('devsync.auto')}</label>
        {conf.enabled && (
          <div className="field">
            <label>{t('devsync.interval')}</label>
            <select className="select" style={{ width: 260 }} value={conf.interval} onChange={(e) => up({ interval: parseInt(e.target.value, 10) })}>
              {[5, 15, 30, 60].map((n) => <option key={n} value={n}>{t('devsync.every', { n })}</option>)}
              <option value={0}>{t('devsync.onChange')}</option>
            </select>
          </div>
        )}
        <div className="row">
          <button className="btn" disabled={!dirty} onClick={save}>{t('common.save')}</button>
          <button className="btn primary" disabled={busy || st.running || !conf.siteId || (!v.hasPassphrase && !pass)} onClick={syncNow}>{busy || st.running ? <span className="spinner" /> : <RefreshCw />} {t('devsync.now')}</button>
        </div>
      </div>
      <div className="form-section devsync-state">
        <h4>{t('devsync.stateTitle')}</h4>
        {!st.lastSync && <p className="faint" style={{ margin: 0 }}>{t('devsync.never')}</p>}
        {st.lastSync && (
          <dl className="kv">
            <dt>{t('devsync.last')}</dt><dd>{formatDate(st.lastSync, 'short')} · <span className={st.lastStatus === 'ok' ? 'ok-text' : 'danger-text'}>{st.lastStatus === 'ok' ? t('devsync.ok') : t('devsync.failed')}</span></dd>
            {st.lastStatus === 'ok' && (st.result || st.summary) && <><dt>{t('devsync.result')}</dt><dd>{devSyncSummary(st)}</dd></>}
            {st.lastDevice && <><dt>{t('devsync.lastDevice')}</dt><dd>{st.lastDevice}</dd></>}
          </dl>
        )}
        {st.lastStatus === 'failed' && st.lastError && <div className="alert danger"><Laptop /><span className="selectable">{st.lastError}</span></div>}
      </div>
    </div>
  )
}

/** The shell of local terminals, among those installed on this computer. */
function LocalShellField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const info = useStore((s) => s.info)
  const [list, setList] = useState<Shell[]>([])
  useEffect(() => {
    if (info?.localTerminal) loadShells('local').then(setList)
  }, [info?.localTerminal])
  if (!info?.localTerminal) return null
  const def = list.find((s) => s.default)
  return (
    <div className="field">
      <label>{t('prefs.localShell')}</label>
      <select className="select" style={{ width: 320 }} value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">{def ? t('prefs.localShellAuto', { name: def.name }) : t('term.defaultShell')}</option>
        {list.map((s) => <option key={s.id} value={s.id}>{s.name}{s.kind === 'repl' ? ` (${t('term.repl')})` : ''}</option>)}
      </select>
      <span className="hint">{t('prefs.localShellHint')}</span>
    </div>
  )
}
