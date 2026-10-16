// Checks the translations: every key used by the code exists in English and
// French, and both dictionaries have the same keys.
//   node scripts/check-i18n.mjs
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const src = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')
const i18n = readFileSync(join(src, 'lib', 'i18n.ts'), 'utf8')

function dict(name) {
  const start = i18n.indexOf(`const ${name}: Dict = {`)
  const end = i18n.indexOf('\n}\n', start)
  const keys = new Set()
  for (const m of i18n.slice(start, end).matchAll(/(?:'([\w.-]+)'|"([\w.-]+)")\s*:/g)) keys.add(m[1] ?? m[2])
  return keys
}
const en = dict('en')
const fr = dict('fr')

const files = []
const walk = (d) => {
  for (const f of readdirSync(d)) {
    const p = join(d, f)
    if (statSync(p).isDirectory()) walk(p)
    else if (/\.(ts|tsx)$/.test(f) && !p.endsWith('i18n.ts')) files.push(p)
  }
}
walk(src)

const problems = []
for (const k of en) if (!fr.has(k)) problems.push(`missing in French: ${k}`)
for (const k of fr) if (!en.has(k)) problems.push(`missing in English: ${k}`)
for (const f of files) {
  const code = readFileSync(f, 'utf8')
  for (const m of code.matchAll(/\bt\(\s*'([^']+)'/g)) {
    const k = m[1]
    if (!en.has(k) || !fr.has(k)) problems.push(`${f.slice(src.length + 1)}: unknown key ${k}`)
  }
  // dynamic keys: t(`prefix.${x}`) needs at least one key with that prefix
  for (const m of code.matchAll(/\bt\(\s*`([^`$]+)\$\{/g)) {
    const prefix = m[1]
    if (![...en].some((k) => k.startsWith(prefix))) problems.push(`${f.slice(src.length + 1)}: no key for ${prefix}…`)
  }
}
if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log(`i18n: ${en.size} keys in English and French, ${files.length} files checked`)
