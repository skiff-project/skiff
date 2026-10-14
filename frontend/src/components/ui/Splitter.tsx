import { useRef } from 'react'

// Splitter is a draggable divider. onDrag receives the pointer delta since
// the drag start; onEnd fires when released.
export function Splitter({ dir, onDrag, onStart, onEnd, onDoubleClick }: { dir: 'h' | 'v'; onDrag: (delta: number) => void; onStart?: () => void; onEnd?: () => void; onDoubleClick?: () => void }) {
  const start = useRef(0)
  return (
    <div
      className={`splitter ${dir}`}
      onDoubleClick={onDoubleClick}
      onPointerDown={(e) => {
        e.preventDefault()
        ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
        start.current = dir === 'h' ? e.clientX : e.clientY
        onStart?.()
        document.body.style.cursor = dir === 'h' ? 'col-resize' : 'row-resize'
        document.body.classList.add('dragging-split')
      }}
      onPointerMove={(e) => {
        if (!(e.target as HTMLElement).hasPointerCapture(e.pointerId)) return
        onDrag((dir === 'h' ? e.clientX : e.clientY) - start.current)
      }}
      onPointerUp={(e) => {
        ;(e.target as HTMLElement).releasePointerCapture(e.pointerId)
        document.body.style.cursor = ''
        document.body.classList.remove('dragging-split')
        onEnd?.()
      }}
    />
  )
}
