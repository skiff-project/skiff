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

/** Saves what a saver was registered for (a file of a tab). */
export async function runSaver(id: string) {
  const fn = savers.get(id)
  if (fn) await fn()
}

// confirmCloseEditor resolves true when the tab may close.
export function confirmCloseEditor(tab: EditorTab): Promise<boolean> {
  return confirmClose(t('editor.unsavedClose', { name: tab.title }), [tab.id])
}

/** Asks whether to save files before closing them; true when they may close. */
export async function confirmClose(message: string, saverIds: string[]): Promise<boolean> {
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
      {message}
    </Dialog>
  ))
  if (r === 'discard') return true
  if (r === 'save') {
    for (const id of saverIds) await runSaver(id)
    return true
  }
  return false
}
