import { useEffect, useState } from 'react'
import { Copy } from 'lucide-react'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'
import { copyToClipboard } from '../../lib/fileops'
import { displayPath } from '../../lib/actions'
import type { KeychainKey, KeyFile } from '../../lib/types'

/** A public key Skiff can offer: from its keychain or from ~/.ssh. */
export interface PublicChoice { id: string; label: string; group: string; publicKey: string; fingerprint: string }

/** The same key, whatever the comment. */
export const sameKey = (a: string, b: string) => a.split(/\s+/).slice(0, 2).join(' ') === b.split(/\s+/).slice(0, 2).join(' ')

/**
 * The public keys of the keychain and of ~/.ssh (a key in both is offered
 * once); hidden counts the key files whose public key is unknown.
 */
export function useKeyChoices() {
  const [choices, setChoices] = useState<PublicChoice[] | null>(null)
  const [hidden, setHidden] = useState(0)
  useEffect(() => {
    Promise.all([
      call<{ keys: KeychainKey[] }>('keychain.list').then((r) => r.keys).catch(() => [] as KeychainKey[]),
      call<KeyFile[]>('keys.list').catch(() => [] as KeyFile[]),
    ]).then(([chain, files]) => {
      const list: PublicChoice[] = chain.map((k) => ({ id: 'chain:' + k.id, label: `${k.name} · ${k.type}`, group: t('keychain.title'), publicKey: k.publicKey, fingerprint: k.fingerprint }))
      for (const f of files) {
        if (!f.publicKey || list.some((c) => sameKey(c.publicKey, f.publicKey!))) continue
        list.push({ id: 'file:' + f.path, label: `${displayPath('local', f.path)} · ${f.type}`, group: '~/.ssh', publicKey: f.publicKey, fingerprint: f.fingerprint || '' })
      }
      setHidden(files.filter((f) => !f.publicKey).length)
      setChoices(list)
    })
  }, [])
  return { choices, hidden }
}

/**
 * Chooses a public key among the known ones, or pasted. value is the id of
 * the choice ("paste" for the text).
 */
export function KeyPicker({ label, choices, hidden, value, onChange, pasted, onPaste, placeholder, extra, autoFocus }: {
  label: string
  choices: PublicChoice[] | null
  hidden?: number
  value: string
  onChange: (id: string) => void
  pasted: string
  onPaste: (v: string) => void
  placeholder?: string
  extra?: { value: string; label: string }[]
  autoFocus?: boolean
}) {
  const key = value === 'paste' ? '' : choices?.find((c) => c.id === value)?.publicKey || ''
  const groups = [...new Set((choices || []).map((c) => c.group))]
  return (
    <>
      <div className="field">
        <label>{label}</label>
        {choices === null ? <span className="spinner" /> : (
          <select className="select" value={value} onChange={(e) => onChange(e.target.value)} data-autofocus={autoFocus || undefined}>
            {groups.map((g) => (
              <optgroup key={g} label={g}>
                {choices.filter((c) => c.group === g).map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
              </optgroup>
            ))}
            {extra?.map((x) => <option key={x.value} value={x.value}>{x.label}</option>)}
            <option value="paste">{t('keys.paste')}</option>
          </select>
        )}
        {!!hidden && <span className="hint">{t('keys.noPublic', { n: hidden })}</span>}
      </div>
      {value === 'paste' ? (
        <div className="field">
          <label>{t('keygen.public')}</label>
          <textarea className="input mono" rows={3} value={pasted} placeholder={placeholder || 'ssh-ed25519 AAAA… moi@pc'} onChange={(e) => onPaste(e.target.value)} />
        </div>
      ) : key && (
        <div className="field">
          <div className="code-block key-preview" style={{ cursor: 'copy' }} onClick={() => copyToClipboard(key)} title={t('common.copy')}>{key}</div>
          <div><button className="btn sm" onClick={() => copyToClipboard(key)}><Copy /> {t('common.copy')}</button></div>
        </div>
      )}
    </>
  )
}

/** The public key a picker designates. */
export function pickedKey(choices: PublicChoice[] | null, value: string, pasted: string) {
  return value === 'paste' ? pasted.trim() : choices?.find((c) => c.id === value)?.publicKey || ''
}
