import { create } from 'zustand'
import type { ReactNode } from 'react'

interface DialogEntry {
  id: number
  render: (close: (v?: any) => void) => ReactNode
  resolve: (v: any) => void
}

export const useDialogs = create<{ stack: DialogEntry[] }>(() => ({ stack: [] }))

let seq = 0

// openDialog renders a modal and resolves with the value passed to close().
export function openDialog<T = any>(render: (close: (v?: T) => void) => ReactNode): Promise<T | undefined> {
  return new Promise((resolve) => {
    const id = ++seq
    const entry: DialogEntry = {
      id,
      render: render as any,
      resolve: (v) => {
        useDialogs.setState((s) => ({ stack: s.stack.filter((d) => d.id !== id) }))
        resolve(v)
      },
    }
    useDialogs.setState((s) => ({ stack: [...s.stack, entry] }))
  })
}

export function dialogOpen(): boolean {
  return useDialogs.getState().stack.length > 0
}

export function DialogHost() {
  const stack = useDialogs((s) => s.stack)
  return (
    <>
      {stack.map((d) => (
        <DialogSlot key={d.id} entry={d} />
      ))}
    </>
  )
}

function DialogSlot({ entry }: { entry: DialogEntry }) {
  return <>{entry.render(entry.resolve)}</>
}
