import { useEffect, useState } from 'react'

// Global tooltip: any element with data-tip="text" (and optional data-kbd)
// shows a tooltip after a short hover delay.
export function TooltipHost() {
  const [tip, setTip] = useState<{ text: string; kbd?: string; x: number; y: number; below: boolean } | null>(null)

  useEffect(() => {
    let timer: number | undefined
    let current: HTMLElement | null = null
    const show = (el: HTMLElement) => {
      const r = el.getBoundingClientRect()
      const below = r.top < 60
      setTip({ text: el.dataset.tip || '', kbd: el.dataset.kbd, x: r.left + r.width / 2, y: below ? r.bottom + 6 : r.top - 6, below })
    }
    const over = (e: MouseEvent) => {
      const el = (e.target as HTMLElement).closest<HTMLElement>('[data-tip]')
      if (el === current) return
      current = el
      clearTimeout(timer)
      setTip(null)
      if (el && el.dataset.tip) timer = window.setTimeout(() => show(el), 450)
    }
    const hide = () => {
      clearTimeout(timer)
      current = null
      setTip(null)
    }
    // Give icon-only controls an accessible name from their tooltip.
    const label = (root: ParentNode) =>
      root.querySelectorAll?.('[data-tip]:not([aria-label])').forEach((el) => {
        if (!(el as HTMLElement).innerText?.trim()) el.setAttribute('aria-label', (el as HTMLElement).dataset.tip || '')
      })
    label(document)
    const mo = new MutationObserver((muts) => {
      for (const m of muts) m.addedNodes.forEach((n) => n instanceof HTMLElement && label(n.parentNode || n))
    })
    mo.observe(document.body, { childList: true, subtree: true })
    document.addEventListener('mouseover', over)
    document.addEventListener('mousedown', hide, true)
    document.addEventListener('keydown', hide, true)
    window.addEventListener('blur', hide)
    return () => {
      mo.disconnect()
      document.removeEventListener('mouseover', over)
      document.removeEventListener('mousedown', hide, true)
      document.removeEventListener('keydown', hide, true)
      window.removeEventListener('blur', hide)
    }
  }, [])

  if (!tip) return null
  return (
    <div
      className="tooltip"
      ref={(el) => {
        if (!el) return
        const w = el.offsetWidth
        let left = tip.x - w / 2
        left = Math.max(6, Math.min(left, window.innerWidth - w - 6))
        el.style.left = `${left}px`
        el.style.top = tip.below ? `${tip.y}px` : `${tip.y - el.offsetHeight}px`
      }}
    >
      {tip.text}
      {tip.kbd && <kbd>{tip.kbd}</kbd>}
    </div>
  )
}
