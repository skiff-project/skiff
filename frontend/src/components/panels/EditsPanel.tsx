import { useShallow } from 'zustand/react/shallow'
import { CheckCircle2, CircleAlert, Square } from 'lucide-react'
import { useStore } from '../../lib/store'
import { call } from '../../lib/api'
import { formatDate } from '../../lib/format'
import { t } from '../../lib/i18n'

export function EditsPanel() {
  const edits = useStore(useShallow((s) => Object.values(s.edits)))
  const conns = useStore((s) => s.conns)
  return (
    <table className="table">
      <thead>
        <tr>
          <th>{t('common.file')}</th>
          <th>{t('common.remote')}</th>
          <th>Uploads</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {edits.map((e) => (
          <tr key={e.id}>
            <td className="mono ellipsis" style={{ maxWidth: 300 }}>{e.local}</td>
            <td className="mono ellipsis">{conns[e.conn]?.title}:{e.remote}</td>
            <td>
              {e.error ? (
                <span className="danger-text"><CircleAlert style={{ width: 14 }} /> {e.error}</span>
              ) : (
                <span className="muted"><CheckCircle2 className="ok-text" style={{ width: 14 }} /> {e.uploads} · {formatDate(e.uploaded || e.opened, 'relative')}</span>
              )}
            </td>
            <td>
              <button className="btn sm" onClick={() => call('edit.stop', { id: e.id })}><Square /> {t('common.stop')}</button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
