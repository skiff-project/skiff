import { Fragment, useEffect, useState } from 'react'
import { Info } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'
import type { InfoItem } from '../../lib/types'
import { copyToClipboard } from '../../lib/fileops'
import { formatDateFull } from '../../lib/format'

export function openServerInfo(conn: string) {
  openDialog((close) => <ServerInfo conn={conn} close={close} />)
}

function ServerInfo({ conn, close }: { conn: string; close: () => void }) {
  const [items, setItems] = useState<InfoItem[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    call<InfoItem[]>('conn.serverInfo', { conn }).then(setItems).catch((e) => setErr(e.message))
  }, [])
  // backend labels are English; translate the known ones
  const tr = (prefix: string, k: string) => {
    const key = `${prefix}.${k}`
    const v = t(key)
    return v === key ? k : v
  }
  const groups: Record<string, InfoItem[]> = {}
  for (const it of items || []) if (it.value) (groups[it.group] ||= []).push(it)
  return (
    <Dialog size="lg" title={t('info.title')} icon={<Info />} onClose={close} footer={<><div className="spacer" /><button className="btn" onClick={() => copyToClipboard((items || []).map((i) => `${i.group}\t${i.key}\t${i.value}`).join('\n'))}>{t('common.copy')}</button><button className="btn primary" onClick={close}>{t('common.close')}</button></>}>
      {err && <div className="alert danger">{err}</div>}
      {!items && !err && <div className="spinner" />}
      {Object.entries(groups).map(([g, list]) => (
        <div key={g} className="form-section" style={{ marginBottom: 16 }}>
          <h4>{tr('info.g', g)}</h4>
          <dl className="kv">
            {list.map((i) => (
              <Fragment key={i.key}>
                <dt>{tr('info.k', i.key)}</dt>
                <dd className="mono selectable" style={{ whiteSpace: 'pre-wrap' }}>{i.key === 'Connected since' ? formatDateFull(i.value) : tr('info.v', i.value)}</dd>
              </Fragment>
            ))}
          </dl>
        </div>
      ))}
    </Dialog>
  )
}
