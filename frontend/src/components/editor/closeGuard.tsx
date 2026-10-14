import { Save } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { t } from '../../lib/i18n'
import type { EditorTab } from '../../lib/store'

const savers = new Map<string, () => Promise<void>>()

export function registerSaver(tabId: string, fn: () => Promise<void>) {
  savers.set(tabId, fn)
  return () => {
    if (savers.get(tabId) === fn) savers.delete(tabId)
  }
}

// confirmCloseEditor resolves true when the tab may close.
export async function confirmCloseEditor(tab: EditorTab): Promise<boolean> {
  const r = await openDialog<'save' | 'discard'>((close) => (
    <Dialog
      size="sm"
      title={t('tabs.closeTab')}
      icon={<Save />}
      iconTone="warn"
      onClose={() => close()}
      onSubmit={() => close('save')}
      footer={
        <>
          <button className="btn" onClick={() => close('discard')}>{t('editor.dontSave')}</button>
          <div className="spacer" />
          <button className="btn" onClick={() => close()}>{t('common.cancel')}</button>
          <button className="btn primary" onClick={() => close('save')}>{t('common.save')}</button>
        </>
      }
    >
      {t('editor.unsavedClose', { name: tab.title })}
    </Dialog>
  ))
  if (r === 'discard') return true
  if (r === 'save') {
    const fn = savers.get(tab.id)
    if (fn) await fn()
    return true
  }
  return false
}
