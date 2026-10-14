import { useEffect, useRef, type ReactNode } from 'react'
import { X } from 'lucide-react'

interface Props {
  title?: ReactNode
  icon?: ReactNode
  iconTone?: 'accent' | 'danger' | 'warn'
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
  onClose: () => void
  onSubmit?: () => void
  footer?: ReactNode
  children: ReactNode
  closeOnOverlay?: boolean
  bodyClass?: string
  noPadding?: boolean
  headExtra?: ReactNode
}

// Dialog renders a modal panel with keyboard handling (Esc / Enter).
export function Dialog({ title, icon, iconTone = 'accent', size = 'md', onClose, onSubmit, footer, children, closeOnOverlay = false, bodyClass, noPadding, headExtra }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const prevFocus = useRef<Element | null>(null)

  useEffect(() => {
    prevFocus.current = document.activeElement
    const el = ref.current
    if (el) {
      const target = el.querySelector<HTMLElement>('[data-autofocus]') ||
        el.querySelector<HTMLElement>('input:not([type=checkbox]):not([type=radio]):not([disabled]), textarea, select') ||
        el.querySelector<HTMLElement>('.btn.primary')
      setTimeout(() => {
        target?.focus()
        if (target instanceof HTMLInputElement && target.dataset.selectStem !== undefined) {
          const v = target.value
          const dot = v.lastIndexOf('.')
          target.setSelectionRange(0, dot > 0 ? dot : v.length)
        } else if (target instanceof HTMLInputElement && target.type !== 'password') {
          target.select()
        }
      }, 10)
    }
    return () => {
      const p = prevFocus.current as HTMLElement | null
      setTimeout(() => p?.focus?.(), 0)
    }
  }, [])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      e.preventDefault()
      onClose()
    } else if (e.key === 'Enter' && onSubmit && !e.shiftKey) {
      const tag = (e.target as HTMLElement).tagName
      if (tag === 'TEXTAREA' || (e.target as HTMLElement).closest('.cm-editor')) return
      if (tag === 'BUTTON' && !(e.target as HTMLElement).classList.contains('primary')) return
      e.preventDefault()
      e.stopPropagation()
      onSubmit()
    }
    e.stopPropagation()
  }

  const cls = size === 'md' ? 'dialog' : `dialog ${size}`
  return (
    <div className="overlay" onMouseDown={(e) => closeOnOverlay && e.target === e.currentTarget && onClose()} onKeyDown={onKeyDown}>
      <div className={cls} ref={ref} role="dialog" aria-modal="true" onContextMenu={(e) => e.stopPropagation()}>
        {(title || icon) && (
          <div className="dialog-head">
            {icon && <div className={`dialog-icon ${iconTone !== 'accent' ? iconTone : ''}`}>{icon}</div>}
            <h3 className="grow ellipsis">{title}</h3>
            {headExtra}
            <button className="icon-btn" onClick={onClose} aria-label="Close" tabIndex={-1}>
              <X />
            </button>
          </div>
        )}
        <div className={`dialog-body ${bodyClass || ''}`} style={noPadding ? { padding: 0 } : undefined}>
          {children}
        </div>
        {footer && <div className="dialog-foot">{footer}</div>}
      </div>
    </div>
  )
}
