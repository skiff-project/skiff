import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { dismissToast, useStore } from '../../lib/store'

export function Toasts() {
  const toasts = useStore((s) => s.toasts)
  return (
    <div className="toasts">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind}`}>
          <div className="toast-icon">
            {t.kind === 'ok' ? <CheckCircle2 /> : t.kind === 'error' ? <XCircle /> : t.kind === 'warn' ? <AlertTriangle /> : <Info />}
          </div>
          <div className="grow">
            <div className="toast-title">{t.title}</div>
            {t.message && <div className="toast-msg selectable">{t.message}</div>}
            {t.action && (
              <button className="btn sm" style={{ marginTop: 8 }} onClick={() => { t.action!.run(); dismissToast(t.id) }}>
                {t.action.label}
              </button>
            )}
          </div>
          <button className="icon-btn sm" onClick={() => dismissToast(t.id)}>
            <X />
          </button>
        </div>
      ))}
    </div>
  )
}
