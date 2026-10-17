import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { create } from 'zustand'
import { ChevronRight } from 'lucide-react'

export interface MenuItem {
  label?: ReactNode
  icon?: ReactNode
  shortcut?: string
  onClick?: () => void
  danger?: boolean
  disabled?: boolean
  checked?: boolean
  separator?: boolean
  header?: string
  submenu?: MenuItem[]
  hidden?: boolean
}

interface MenuState {
  open: { x: number; y: number; items: MenuItem[]; key: number; minWidth?: number } | null
}

const useMenu = create<MenuState>(() => ({ open: null }))
let key = 0

export function showMenu(x: number, y: number, items: MenuItem[], minWidth?: number) {
  useMenu.setState({ open: { x, y, items: items.filter((i) => !i.hidden), key: ++key, minWidth } })
}

export function showMenuAt(el: HTMLElement, items: MenuItem[], align: 'left' | 'right' = 'left') {
  const r = el.getBoundingClientRect()
  showMenu(align === 'left' ? r.left : r.right, r.bottom + 4, items, align === 'left' ? r.width : undefined)
  if (align === 'right') {
    // re-align after measuring
    requestAnimationFrame(() => {
      const m = document.querySelector<HTMLElement>('.menu[data-root]')
      if (m) m.style.left = `${Math.max(8, r.right - m.offsetWidth)}px`
    })
  }
}

export function closeMenu() {
  useMenu.setState({ open: null })
}

export function MenuHost() {
  const open = useMenu((s) => s.open)
  if (!open) return null
  return <MenuList key={open.key} x={open.x} y={open.y} items={open.items} root minWidth={open.minWidth} />
}

function MenuList({ x, y, items, root, minWidth, onLeave }: { x: number; y: number; items: MenuItem[]; root?: boolean; minWidth?: number; onLeave?: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x, y })
  const [hot, setHot] = useState(-1)
  const [sub, setSub] = useState<{ i: number; x: number; y: number } | null>(null)
  const visible = items.filter((i) => !i.hidden)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const w = el.offsetWidth
    const h = el.offsetHeight
    let nx = x
    let ny = y
    if (nx + w > window.innerWidth - 8) nx = root ? Math.max(8, window.innerWidth - w - 8) : Math.max(8, x - w - (minWidth ?? 0) - 200)
    if (ny + h > window.innerHeight - 8) ny = Math.max(8, window.innerHeight - h - 8)
    setPos({ x: nx, y: ny })
  }, [x, y])

  useEffect(() => {
    if (!root) return
    const onDown = (e: MouseEvent) => {
      if (!(e.target as HTMLElement).closest('.menu')) closeMenu()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        closeMenu()
        return
      }
      const actionable = visible.map((it, i) => ({ it, i })).filter(({ it }) => !it.separator && !it.header && !it.disabled)
      if (!actionable.length) return
      const idx = actionable.findIndex((a) => a.i === hot)
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        e.stopPropagation()
        setHot(actionable[(idx + 1) % actionable.length].i)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        e.stopPropagation()
        setHot(actionable[(idx - 1 + actionable.length) % actionable.length].i)
      } else if (e.key === 'Enter' && hot >= 0) {
        e.preventDefault()
        e.stopPropagation()
        const it = visible[hot]
        if (it?.onClick) {
          closeMenu()
          it.onClick()
        }
      }
    }
    const onBlur = () => closeMenu()
    window.addEventListener('mousedown', onDown, true)
    window.addEventListener('keydown', onKey, true)
    window.addEventListener('blur', onBlur)
    window.addEventListener('resize', onBlur)
    return () => {
      window.removeEventListener('mousedown', onDown, true)
      window.removeEventListener('keydown', onKey, true)
      window.removeEventListener('blur', onBlur)
      window.removeEventListener('resize', onBlur)
    }
  }, [hot, visible])

  return (
    <div className="menu" role="menu" ref={ref} data-root={root ? '' : undefined} style={{ left: pos.x, top: pos.y, minWidth: minWidth ? Math.max(200, minWidth) : undefined }} onMouseLeave={onLeave} onContextMenu={(e) => e.preventDefault()}>
      {visible.map((it, i) => {
        if (it.separator) return <div key={i} className="menu-sep" role="separator" />
        if (it.header) return <div key={i} className="menu-label" role="presentation">{it.header}</div>
        return (
          <div
            key={i}
            role="menuitem"
            aria-disabled={it.disabled || undefined}
            aria-haspopup={it.submenu ? 'menu' : undefined}
            className={`menu-item${it.danger ? ' danger' : ''}${it.disabled ? ' disabled' : ''}${hot === i ? ' hot' : ''}`}
            onMouseEnter={(e) => {
              setHot(i)
              if (it.submenu) {
                const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
                setSub({ i, x: r.right + 2, y: r.top - 4 })
              } else setSub(null)
            }}
            onClick={() => {
              // an item with a submenu may also have its own action
              if ((it.submenu && !it.onClick) || it.disabled) return
              closeMenu()
              it.onClick?.()
            }}
          >
            {it.checked !== undefined ? (
              <span style={{ width: 15, display: 'inline-flex', justifyContent: 'center', color: 'var(--accent-text)', fontWeight: 700 }}>{it.checked ? '✓' : ''}</span>
            ) : (
              it.icon ?? <span style={{ width: 15 }} />
            )}
            <span className="ellipsis">{it.label}</span>
            {it.shortcut && <span className="shortcut">{it.shortcut}</span>}
            {it.submenu && <ChevronRight className="sub" />}
          </div>
        )
      })}
      {sub && visible[sub.i]?.submenu && <MenuList x={sub.x} y={sub.y} items={visible[sub.i].submenu!} />}
    </div>
  )
}
