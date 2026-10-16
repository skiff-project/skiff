import { ChevronDown, FileKey2, KeyRound, Lock, X } from 'lucide-react'
import { call } from '../../lib/api'
import { displayPath } from '../../lib/actions'
import { t } from '../../lib/i18n'
import type { KeychainKey, KeyFile } from '../../lib/types'
import { showMenuAt, type MenuItem } from '../ui/Menu'
import { openKeygen } from '../dialogs/KeygenDialog'

/** ssh-ed25519 → Ed25519, ecdsa-sha2-nistp256 → ECDSA… */
function keyKind(type: string) {
  if (/ed25519/i.test(type)) return 'Ed25519'
  if (/ecdsa/i.test(type)) return 'ECDSA'
  if (/rsa/i.test(type)) return 'RSA'
  return type
}

/**
 * The private key file of a site or a jump host: typed, or chosen among
 * the keys of ~/.ssh (or a key generated on the spot).
 */
export function KeyFileInput({ value, onChange, placeholder, icon, compact }: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  icon?: boolean
  compact?: boolean
}) {
  const pick = async (el: HTMLElement) => {
    const [files, chain] = await Promise.all([
      call<KeyFile[]>('keys.list').catch(() => [] as KeyFile[]),
      call<{ keys: KeychainKey[] }>('keychain.list').then((r) => r.keys).catch(() => [] as KeychainKey[]),
    ])
    const items: MenuItem[] = [{ header: '~/.ssh' }]
    if (!files.length) items.push({ label: t('site.noKeys'), disabled: true })
    for (const k of files) {
      const p = displayPath('local', k.path)
      items.push({
        label: <>{p} {k.encrypted && <Lock style={{ width: 12, verticalAlign: -1, color: 'var(--faint)' }} />}</>,
        checked: p === value || k.path === value,
        shortcut: keyKind(k.type),
        onClick: () => onChange(p),
      })
    }
    items.push({ separator: true })
    items.push({
      label: t('prefs.generateKey'),
      icon: <KeyRound />,
      onClick: async () => {
        const path = await openKeygen()
        if (path) onChange(displayPath('local', path))
      },
    })
    if (value) items.push({ label: t('site.keyFileClear'), icon: <X />, onClick: () => onChange('') })
    if (chain.length) items.push({ separator: true }, { label: t('site.keychainAuto', { n: chain.length }), disabled: true })
    showMenuAt(el, items)
  }
  return (
    <div className="row key-file">
      {icon && <FileKey2 style={{ width: 16, color: 'var(--faint)', flex: 'none' }} />}
      <input className="input mono" value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} spellCheck={false} />
      {compact ? (
        <button type="button" className="icon-btn" data-tip={t('site.chooseKeyTip')} onClick={(e) => pick(e.currentTarget)}><KeyRound /></button>
      ) : (
        <button type="button" className="btn" data-tip={t('site.chooseKeyTip')} onClick={(e) => pick(e.currentTarget)}><KeyRound /> {t('site.chooseKey')} <ChevronDown /></button>
      )}
    </div>
  )
}
