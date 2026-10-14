import { useState } from 'react'
import { Link } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { t } from '../../lib/i18n'

export function linkDialog(target: string, name: string): Promise<{ target: string; name: string; hard: boolean } | undefined> {
  return openDialog((close) => <LinkDialog target={target} name={name} close={close} />)
}

function LinkDialog({ target, name, close }: { target: string; name: string; close: (v?: { target: string; name: string; hard: boolean }) => void }) {
  const [tg, setTg] = useState(target)
  const [nm, setNm] = useState(name)
  const [hard, setHard] = useState(false)
  const submit = () => tg && nm && close({ target: tg, name: nm, hard })
  return (
    <Dialog size="sm" title={t('dialog.link')} icon={<Link />} onClose={() => close()} onSubmit={submit} footer={<><div className="spacer" /><button className="btn" onClick={() => close()}>{t('common.cancel')}</button><button className="btn primary" onClick={submit}>{t('common.ok')}</button></>}>
      <div className="form">
        <div className="field"><label>{t('dialog.linkName')}</label><input className="input mono" value={nm} onChange={(e) => setNm(e.target.value)} data-autofocus /></div>
        <div className="field"><label>{t('dialog.linkTarget')}</label><input className="input mono" value={tg} onChange={(e) => setTg(e.target.value)} /></div>
        <label className="check"><input type="checkbox" checked={hard} onChange={(e) => setHard(e.target.checked)} /> {t('dialog.linkHard')}</label>
      </div>
    </Dialog>
  )
}
