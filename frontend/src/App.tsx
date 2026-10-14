import { useEffect, useState } from 'react'
import { useStore } from './lib/store'
import { boot } from './lib/actions'
import { onLangChange } from './lib/i18n'
import { Shell } from './components/Shell'
import { Login } from './components/Login'
import { DialogHost } from './lib/dialogs'
import { MenuHost } from './components/ui/Menu'
import { TooltipHost } from './components/ui/Tooltip'
import { Toasts } from './components/ui/Toasts'
import { PromptHost } from './components/dialogs/PromptHost'

export function App() {
  const authed = useStore((s) => s.authed)
  const ready = useStore((s) => !!s.info && !!s.prefs)
  const [, force] = useState(0)

  useEffect(() => {
    boot().catch((e) => console.error(e))
    return onLangChange(() => force((n) => n + 1))
  }, [])

  useEffect(() => {
    const prevent = (e: DragEvent) => {
      // avoid the webview navigating to dropped files
      if (!(e.target as HTMLElement)?.closest?.('[data-dropzone]')) {
        e.preventDefault()
        if (e.dataTransfer) e.dataTransfer.dropEffect = 'none'
      }
    }
    window.addEventListener('dragover', prevent)
    window.addEventListener('drop', prevent)
    const ctx = (e: MouseEvent) => {
      const el = e.target as HTMLElement
      if (!el.closest('input, textarea, .cm-editor, .xterm, .selectable')) e.preventDefault()
    }
    window.addEventListener('contextmenu', ctx)
    return () => {
      window.removeEventListener('dragover', prevent)
      window.removeEventListener('drop', prevent)
      window.removeEventListener('contextmenu', ctx)
    }
  }, [])

  if (authed === false) return <Login />
  if (!ready) return <Splash />
  return (
    <>
      <Shell />
      <PromptHost />
      <DialogHost />
      <MenuHost />
      <TooltipHost />
      <Toasts />
    </>
  )
}

function Splash() {
  return (
    <div className="splash">
      <img src="/favicon.svg" alt="" width={56} height={56} />
      <div className="spinner" />
    </div>
  )
}
