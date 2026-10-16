// The clipboard of the system for text. A webview may refuse the page the
// clipboard API (WebKitGTK does): the native window of Skiff reaches it
// instead (app.clipboardRead / app.clipboardWrite). In web mode, only the
// browser's own clipboard is used.
import { call } from './api'
import { get } from './store'

const native = () => get().info?.mode !== 'web'

/** Writes text to the clipboard; false when nothing could. */
export async function writeClipboard(text: string): Promise<boolean> {
  if (native()) {
    try {
      await call('app.clipboardWrite', { text })
      return true
    } catch {
      /* no native window */
    }
  }
  try {
    if (navigator.clipboard?.writeText && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* refused */
  }
  // the old way: a hidden field, selected and copied
  const focused = document.activeElement as HTMLElement | null
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.cssText = 'position:fixed;top:-1000px;opacity:0'
  document.body.appendChild(ta)
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  ta.remove()
  focused?.focus?.()
  return ok
}

/** Reads text from the clipboard; null when nothing may. */
export async function readClipboard(): Promise<string | null> {
  if (native()) {
    try {
      return await call<string>('app.clipboardRead')
    } catch {
      /* no native window */
    }
  }
  try {
    if (navigator.clipboard?.readText && window.isSecureContext) return await navigator.clipboard.readText()
  } catch {
    /* refused */
  }
  return null
}
