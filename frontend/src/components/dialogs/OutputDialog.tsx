import { TerminalSquare } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { t } from '../../lib/i18n'
import { copyToClipboard } from '../../lib/fileops'

export function outputDialog(title: string, output: string, code: number) {
  openDialog((close) => (
    <Dialog
      size="lg"
      title={title}
      icon={<TerminalSquare />}
      iconTone={code === 0 ? 'accent' : 'danger'}
      onClose={() => close()}
      footer={
        <>
          <span className={code === 0 ? 'muted' : 'danger-text'}>{t('cmd.exit', { code })}</span>
          <div className="spacer" />
          <button className="btn" onClick={() => copyToClipboard(output)}>{t('common.copy')}</button>
          <button className="btn primary" onClick={() => close()}>{t('common.close')}</button>
        </>
      }
    >
      <pre className="code-block" style={{ maxHeight: 480, margin: 0 }}>{output || '(no output)'}</pre>
    </Dialog>
  ))
}
