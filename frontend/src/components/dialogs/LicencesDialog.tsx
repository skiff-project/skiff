import { useEffect, useMemo, useState } from 'react'
import { Copy, Scale, Search } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { copyToClipboard } from '../../lib/fileops'
import { t } from '../../lib/i18n'

interface Licences { license: string; notice: string; thirdParty: string }

/** The licence of Skiff (Apache-2.0) and those of the software it includes. */
export function openLicences(tab: 'skiff' | 'third' = 'skiff') {
  return openDialog((close) => <LicencesView close={close} initial={tab} />)
}

function LicencesView({ close, initial }: { close: () => void; initial: 'skiff' | 'third' }) {
  const [lic, setLic] = useState<Licences | null>(null)
  const [tab, setTab] = useState(initial)
  const [q, setQ] = useState('')
  useEffect(() => { call<Licences>('app.licences').then(setLic).catch(() => setLic({ license: '', notice: '', thirdParty: '' })) }, [])
  // the components: the list at the top of the notices, then one block each
  const blocks = useMemo(() => (lic?.thirdParty || '').split(/\n={78}\n/), [lic])
  const shown = useMemo(() => {
    if (tab === 'skiff') return `${lic?.notice || ''}\n\n${lic?.license || ''}`
    const needle = q.trim().toLowerCase()
    if (!needle) return lic?.thirdParty || ''
    return blocks.slice(1).filter((b) => b.split('\n', 1)[0].toLowerCase().includes(needle)).join('\n\n' + '='.repeat(78) + '\n')
  }, [lic, tab, q, blocks])
  return (
    <Dialog
      size="lg"
      title={t('lic.title')}
      icon={<Scale />}
      onClose={close}
      footer={<><button className="btn" onClick={() => copyToClipboard(shown)}><Copy /> {t('common.copy')}</button><div className="spacer" /><button className="btn primary" onClick={close}>{t('common.close')}</button></>}
    >
      <div className="form licences">
        <div className="row">
          <div className="segmented">
            <button className={tab === 'skiff' ? 'on' : ''} onClick={() => setTab('skiff')}>{t('lic.skiff')}</button>
            <button className={tab === 'third' ? 'on' : ''} onClick={() => setTab('third')}>{t('lic.thirdParty', { n: Math.max(0, blocks.length - 1) })}</button>
          </div>
          {tab === 'third' && (
            <div className="input-group grow">
              <Search />
              <input className="input" value={q} placeholder={t('lic.search')} onChange={(e) => setQ(e.target.value)} />
            </div>
          )}
        </div>
        <p className="muted" style={{ margin: 0 }}>{tab === 'skiff' ? t('lic.skiffDesc') : t('lic.thirdDesc')}</p>
        {lic === null ? <span className="spinner" /> : <pre className="code-block licence-text selectable">{shown || t('lic.none')}</pre>}
      </div>
    </Dialog>
  )
}
