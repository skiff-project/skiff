import { SquareTerminal, Code2, Star } from 'lucide-react'
import { loadShells } from './actions'
import { t } from './i18n'
import type { MenuItem } from '../components/ui/Menu'
import type { Shell } from './types'

/** Menu items to start each shell or interpreter of a connection. */
export function shellItems(list: Shell[], start: (shell: Shell) => void): MenuItem[] {
  const shells = list.filter((s) => s.kind === 'shell')
  const repls = list.filter((s) => s.kind === 'repl')
  const item = (s: Shell): MenuItem => ({
    label: s.name,
    icon: s.default ? <Star /> : s.kind === 'repl' ? <Code2 /> : <SquareTerminal />,
    shortcut: s.default ? t('term.default') : undefined,
    onClick: () => start(s),
  })
  const out: MenuItem[] = []
  if (shells.length) out.push({ header: t('term.shells') }, ...shells.map(item))
  if (repls.length) out.push({ header: t('term.repls') }, ...repls.map(item))
  if (!out.length) out.push({ label: t('term.noShell'), disabled: true })
  return out
}

/** Loads the shells of a connection and returns the menu items. */
export async function shellMenu(conn: string, start: (shell: Shell) => void): Promise<MenuItem[]> {
  return shellItems(await loadShells(conn), start)
}
