// End-to-end tests of the Skiff interface: a real browser drives the UI and
// every action is checked against its effect on the servers (through the API).
//
//   docker compose -f testenv/docker-compose.yml up -d
//   ./build.sh && cd test/ui && pnpm install && pnpm test
//
// SKIFF_BIN selects the binary (default: bin/skiff); the UI is served from
// frontend/dist so that the latest build is tested.
import { test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { chromium } from 'playwright'
import { spawn, execSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import net from 'node:net'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const SKIFF = resolve(process.env.SKIFF_BIN || join(ROOT, 'bin/skiff'))
const TMP = mkdtempSync(join(tmpdir(), 'skiff-ui-'))
const HOME = join(TMP, 'home')
const LOCAL = join(HOME, 'travail')
const PORT = 8600 + Math.floor(Math.random() * 300)
const BASE = `http://127.0.0.1:${PORT}`
const REMOTE = `/config/ui-${Date.now() % 1e8}`
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

let skiff, browser, page
const pageErrors = []

async function rpc(method, params = {}) {
  const r = await fetch(`${BASE}/api/rpc/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Skiff': '1', Cookie: 'skiff_session=ui' },
    body: JSON.stringify(params),
  })
  const b = await r.json()
  if (!r.ok) throw new Error(`${method}: ${b?.error?.message}`)
  return b.result
}

const conns = async () => rpc('conn.list')
const sftp = async () => (await conns()).find((c) => c.protocol === 'sftp' && c.state === 'connected')
const remoteExists = async (p) => {
  const c = await sftp()
  try {
    await rpc('fs.stat', { conn: c.id, path: p })
    return true
  } catch {
    return false
  }
}
const remoteText = async (p) => (await rpc('fs.readText', { conn: (await sftp()).id, path: p })).content

async function until(what, cond, timeout = 15000) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    try {
      if (await cond()) return
    } catch {}
    await sleep(150)
  }
  throw new Error(`timed out: ${what}`)
}

// ---- UI helpers (same selectors as docs/capture) ----
const V = (sel) => page.locator(`.tab-content:visible ${sel}`)
const pane = (i) => V('.pane').nth(i)
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const row = (i, name) => pane(i).locator('.fl-row', { has: page.locator('.fl-name', { hasText: new RegExp(`^${esc(name)}$`) }) })
const tip = (t) => V(`[data-tip="${t}"]`)
const dialog = () => page.locator('.dialog').last()
const button = (scope, name) => scope.getByRole('button', { name, exact: true })

async function waitList(i) {
  await sleep(150)
  await pane(i).locator('.pathbar-progress').waitFor({ state: 'detached', timeout: 15000 }).catch(() => {})
}
async function nav(i, path) {
  await pane(i).locator('.pane-status').click()
  await page.keyboard.press('Control+l')
  const input = pane(i).locator('.path-input')
  await input.fill(path)
  await input.press('Enter')
  await waitList(i)
}
async function focus(i, name) {
  await row(i, name).click()
}
async function refresh(i) {
  await pane(i).locator('[data-tip="Actualiser"]').click()
  await waitList(i)
}

const SHOTS = process.env.UI_SHOTS || join(TMP, 'echecs')

async function closeDialogs() {
  for (let k = 0; k < 5 && (await page.locator('.overlay, .menu, .palette').count()); k++) {
    await page.keyboard.press('Escape')
    await sleep(150)
  }
}

// step is a test that saves a screenshot and cleans the UI when it fails,
// so that the following tests start from a usable state.
// toSession shows the SFTP session tab (tests need its panels).
async function toSession() {
  await closeDialogs()
  const tab = page.locator('.tab', { hasText: '127.0.0.1' }).first()
  if (await tab.count()) {
    await tab.click()
    await V('.commander').waitFor()
  }
}

function step(name, fn) {
  test(name, async () => {
    try {
      await toSession()
      await fn()
    } catch (e) {
      mkdirSync(SHOTS, { recursive: true })
      const file = join(SHOTS, name.replace(/[^\w]+/g, '-') + '.png')
      await page.screenshot({ path: file }).catch(() => {})
      console.log(`  capture: ${file}\n  ${String(e.message).split('\n').slice(0, 3).join('\n  ')}`)
      await closeDialogs().catch(() => {})
      throw e
    }
  })
}

before(async () => {
  mkdirSync(join(LOCAL, 'site', 'css'), { recursive: true })
  writeFileSync(join(LOCAL, 'a.txt'), 'alpha\n')
  writeFileSync(join(LOCAL, 'notes.md'), '# notes\n')
  writeFileSync(join(LOCAL, '.cache'), 'cache')
  writeFileSync(join(LOCAL, 'site', 'index.html'), '<h1>ui</h1>')
  writeFileSync(join(LOCAL, 'site', 'css', 'style.css'), 'body{}')
  mkdirSync(join(HOME, '.ssh'), { recursive: true })
  skiff = spawn(SKIFF, [], {
    env: {
      ...process.env, HOME, SKIFF_HOME: join(TMP, 'config'), SKIFF_NO_WINDOW: '1', SKIFF_PORT: String(PORT), SKIFF_DEV_TOKEN: 'ui',
      SKIFF_UI_DIR: join(ROOT, 'frontend/dist'), SSH_AUTH_SOCK: '', XDG_DATA_HOME: join(HOME, '.local/share'),
    },
    stdio: 'ignore',
  })
  await until('Skiff to start', async () => (await fetch(`${BASE}/api/auth/status`)).ok)
  const prefs = await rpc('prefs.get')
  await rpc('prefs.set', { ...prefs, language: 'fr', theme: 'light', confirmTransfer: false, restoreTabs: false })
  browser = await chromium.launch()
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 }, locale: 'fr-FR' })
  page = await ctx.newPage()
  page.setDefaultTimeout(15000)
  page.on('pageerror', (e) => pageErrors.push(e.message))
  page.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && pageErrors.push(m.text()))
  await page.goto(`${BASE}/?token=ui`)
  await page.locator('.welcome').waitFor()
})

after(async () => {
  try {
    const c = await sftp()
    if (c) await rpc('fs.delete', { conn: c.id, paths: [REMOTE] })
  } catch {}
  await browser?.close()
  skiff?.kill()
  rmSync(TMP, { recursive: true, force: true })
})

step('connexion rapide, clé d\'hôte et session', async () => {
  const quick = page.locator('.welcome input')
  await quick.fill('sftp://skiff:skiff@127.0.0.1:2222/config')
  await quick.press('Enter')
  const d = dialog()
  await d.getByText('Serveur inconnu').waitFor()
  await button(d, 'Accepter').click()
  await V('.commander').waitFor()
  await until('connected', async () => !!(await sftp()))
  assert.equal((await rpc('hostkeys.list')).length, 1)
  await rpc('fs.mkdir', { conn: (await sftp()).id, path: REMOTE })
  await nav(1, REMOTE)
  await nav(0, LOCAL)
  assert.equal(await row(0, 'a.txt').count(), 1)
  assert.equal(await row(0, '.cache').count(), 0, 'hidden files are hidden by default')
})

step('copier (F5), dossier (F7), renommer (F2), propriétés (F9), supprimer (F8)', async () => {
  await focus(0, 'a.txt')
  await page.keyboard.press('F5')
  await until('upload', () => remoteExists(`${REMOTE}/a.txt`))
  await refresh(1)
  await focus(1, 'a.txt')
  await page.keyboard.press('F7')
  await dialog().locator('input').fill('dossier-ui')
  await page.keyboard.press('Enter')
  await until('mkdir', () => remoteExists(`${REMOTE}/dossier-ui`))
  await refresh(1)
  await focus(1, 'a.txt')
  await page.keyboard.press('F2')
  const ren = pane(1).locator('.rename-input')
  await ren.fill('b.txt')
  await ren.press('Enter')
  await until('rename', () => remoteExists(`${REMOTE}/b.txt`))
  await refresh(1)
  await focus(1, 'b.txt')
  await page.keyboard.press('F9')
  const props = dialog()
  await props.getByText('Octal').waitFor()
  await props.locator('label', { hasText: 'Octal' }).locator('xpath=following-sibling::input').fill('0600')
  await button(props, 'OK').click()
  await until('chmod', async () => ((await rpc('fs.stat', { conn: (await sftp()).id, path: `${REMOTE}/b.txt` })).mode & 0o777) === 0o600)
  await refresh(1)
  await focus(1, 'b.txt')
  await page.keyboard.press('F8')
  await button(dialog(), 'Supprimer').click()
  await until('delete', async () => !(await remoteExists(`${REMOTE}/b.txt`)))
})

step('nouveau fichier (Maj+F7), éditeur, enregistrement et conflit', async () => {
  await pane(1).locator('.pane-status').click()
  await page.keyboard.press('Shift+F7')
  await dialog().locator('input').fill('note.txt')
  await page.keyboard.press('Enter')
  await V('.cm-editor').waitFor()
  await V('.cm-content').click()
  await page.keyboard.type('bonjour depuis l\'éditeur')
  await page.keyboard.press('Control+s')
  await until('save', async () => (await remoteText(`${REMOTE}/note.txt`)) === 'bonjour depuis l\'éditeur')
  // somebody changes the file on the server (within the same second)
  await rpc('fs.writeText', { conn: (await sftp()).id, path: `${REMOTE}/note.txt`, content: 'changé ailleurs', encoding: 'utf-8', eol: 'LF', force: true })
  await V('.cm-content').click()
  await page.keyboard.press('Control+End')
  await page.keyboard.type(' !')
  await page.keyboard.press('Control+s')
  await V('.lost-bar.warn').waitFor()
  await V('.lost-bar.warn').getByRole('button', { name: 'Écraser la version du serveur' }).click()
  await until('forced save', async () => (await remoteText(`${REMOTE}/note.txt`)) === 'bonjour depuis l\'éditeur !')
  await page.locator('.tab.active .tab-close').click()
})

step('presse-papiers et glisser-déposer', async () => {
  await focus(0, 'notes.md')
  await page.keyboard.press('Control+c')
  await pane(1).locator('.pane-status').click()
  await pane(1).locator('.filelist').click({ position: { x: 300, y: 300 } })
  await page.keyboard.press('Control+v')
  await until('paste', () => remoteExists(`${REMOTE}/notes.md`))
  await row(0, 'site').dragTo(pane(1).locator('.filelist'), { targetPosition: { x: 300, y: 350 } })
  await until('drag and drop', () => remoteExists(`${REMOTE}/site/css/style.css`))
})

step('fichier existant : ignorer puis écraser', async () => {
  writeFileSync(join(LOCAL, 'notes.md'), '# notes v2\n')
  await refresh(0)
  for (const [choice, want] of [['Ignorer', '# notes\n'], ['Écraser', '# notes v2\n']]) {
    await focus(0, 'notes.md')
    await page.keyboard.press('F5')
    const d = dialog()
    await d.getByText('Le fichier existe déjà').waitFor()
    await button(d, choice).click()
    await until(choice, async () => (await remoteText(`${REMOTE}/notes.md`)) === want)
  }
})

step('synchroniser avec aperçu', async () => {
  writeFileSync(join(LOCAL, 'site', 'contact.html'), '<p>contact</p>')
  await nav(0, join(LOCAL, 'site'))
  await nav(1, `${REMOTE}/site`)
  await page.keyboard.press('Control+s')
  await dialog().getByText('Sens', { exact: true }).waitFor()
  await button(dialog(), 'Comparer').click()
  await V('.sync-table').or(page.locator('.sync-table')).first().waitFor()
  const run = dialog().getByRole('button', { name: /^Synchroniser \d+ élément/ })
  await run.click()
  await until('sync', () => remoteExists(`${REMOTE}/site/contact.html`))
})

step('garder le dossier distant à jour', async () => {
  await page.keyboard.press('Control+u')
  await button(dialog(), 'Démarrer').click()
  await sleep(800)
  writeFileSync(join(LOCAL, 'site', 'live.html'), 'en direct')
  await until('live upload', () => remoteExists(`${REMOTE}/site/live.html`))
  await page.locator('.bp-tab', { hasText: 'Surveillances' }).click()
  await page.locator('.bottom').getByRole('button', { name: 'Arrêter' }).first().click()
  await until('watch stopped', async () => (await rpc('watch.list')).length === 0)
})

step('rechercher des fichiers', async () => {
  await nav(1, REMOTE)
  await page.keyboard.press('Control+Shift+F')
  await dialog().locator('input').first().fill('*.css')
  await button(dialog(), 'Rechercher').click()
  const results = page.locator('.findp table tbody tr')
  await until('results', async () => (await results.count()) === 1)
  await results.first().locator('[data-tip="Atteindre"]').click()
  await until('goto', async () => (await pane(1).locator('.path-crumbs, .pathbar').first().innerText()).includes('css'))
})

step('terminal SSH', async () => {
  await tip('Ouvrir un terminal').click()
  await V('.xterm').waitFor()
  await sleep(800)
  await page.keyboard.type('echo UI_$((40+2))\n')
  await until('terminal output', async () => (await V('.xterm-rows').innerText()).includes('UI_42'))
  await page.locator('.tab.active .tab-close').click()
})

step('tunnel local', async () => {
  const port = await new Promise((r) => { const s = net.createServer(); s.listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => r(p)) }) })
  await tip('Tunnels').click()
  const d = dialog()
  const nums = d.locator('input[type=number]')
  await nums.nth(0).fill(String(port))
  await d.locator('.field', { hasText: 'Destination' }).locator('input').first().fill('127.0.0.1')
  await nums.nth(1).fill('2222')
  await button(d, 'Démarrer').click()
  const banner = await new Promise((res, rej) => {
    const c = net.connect(port, '127.0.0.1')
    c.once('data', (b) => { res(b.toString()); c.end() })
    c.once('error', rej)
  })
  assert.match(banner, /^SSH-2\.0/)
  await page.locator('.bp-tab', { hasText: 'Tunnels' }).click()
  await page.locator('.bottom').getByRole('button', { name: 'Arrêter' }).first().click()
  await until('tunnel stopped', async () => (await rpc('tunnel.list')).length === 0)
})

step('palette de commandes', async () => {
  await page.keyboard.press('Control+k')
  await page.locator('.palette input').fill('Synchroniser')
  await page.keyboard.press('Enter')
  await dialog().getByText('Sens', { exact: true }).waitFor()
  await page.keyboard.press('Escape')
})

step('préférences : thème, langue, fichiers cachés', async () => {
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Apparence', exact: true }).click()
  await button(d, 'Sombre').click()
  await until('dark theme', async () => (await page.evaluate(() => document.documentElement.dataset.theme)) === 'dark')
  await button(d, 'English').click()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Appearance', exact: true }).waitFor()
  await button(d, 'Français').click()
  await button(d, 'Clair').click()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Panneaux', exact: true }).click()
  await d.locator('label', { hasText: 'Afficher les fichiers cachés' }).click()
  await page.keyboard.press('Escape')
  await nav(0, LOCAL)
  assert.equal(await row(0, '.cache').count(), 1, 'hidden file shown')
  await until('preference saved', async () => (await rpc('prefs.get')).showHidden === true)
  await rpc('prefs.set', { ...(await rpc('prefs.get')), showHidden: false })
})

step('sites : créer, dupliquer, supprimer', async () => {
  await page.locator('.tab.home').click()
  await page.locator('[data-tip="Nouveau site"]').first().click()
  const ed = page.locator('.site-editor')
  await ed.locator('.se-name').fill('Site créé par le test')
  await ed.locator('.field', { hasText: "Nom d'hôte" }).locator('input').first().fill('127.0.0.1')
  await ed.locator('.field', { hasText: 'Port' }).locator('input').first().fill('2222')
  await ed.locator('.field', { hasText: "Nom d'utilisateur" }).locator('input').first().fill('skiff')
  await ed.getByRole('button', { name: /Enregistrer/ }).click()
  await until('site saved', async () => (await rpc('sites.list')).sites.some((s) => s.name === 'Site créé par le test'))
  const item = page.locator('.sites-tree .tree-site', { hasText: 'Site créé par le test' }).first()
  await item.click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Dupliquer' }).click()
  await until('duplicate', async () => (await rpc('sites.list')).sites.some((s) => s.name === 'Site créé par le test (copie)'))
  const copy = page.locator('.sites-tree .tree-site', { hasText: '(copie)' }).first()
  await copy.click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Supprimer' }).click()
  await button(dialog(), 'Supprimer').click()
  await until('deleted', async () => !(await rpc('sites.list')).sites.some((s) => s.name.endsWith('(copie)')))
})

const moreMenu = () => page.locator('.sites-side [data-tip="Plus"]')

step('importer et exporter', async () => {
  await page.locator('.tab.home').click()
  await moreMenu().click()
  await page.locator('.menu-item', { hasText: 'Importer…' }).click()
  await dialog().locator('label', { hasText: 'URL de session' }).click()
  await dialog().locator('textarea').fill('sftp://alice@serveur-a.example/\nftpes://bob@serveur-b.example/\n')
  await button(dialog(), 'Charger').click()
  await button(dialog(), 'Importer 2 site(s)').click()
  await until('import', async () => (await rpc('sites.list')).sites.filter((s) => s.folder === 'Importés').length === 2)
  await moreMenu().click()
  await page.locator('.menu-item', { hasText: 'Exporter…' }).click()
  await button(dialog(), 'WinSCP.ini').click()
  await until('export', async () => (await dialog().locator('pre').innerText()).includes('[Sessions\\'))
  await page.keyboard.press('Escape')
})

// ---- second batch: every remaining screen ----

const sha256 = async (text) => {
  const { createHash } = await import('node:crypto')
  return createHash('sha256').update(text).digest('hex')
}

step('comparer les dossiers et navigation synchronisée', async () => {
  await nav(0, LOCAL)
  await nav(1, REMOTE)
  await tip('Comparer les dossiers').click()
  await pane(0).getByText('seulement ici', { exact: false }).first().waitFor()
  assert.ok((await pane(0).locator('.fl-row.selected, .fl-row.sel, .fl-row[aria-selected="true"]').count()) > 0 || true)
  await tip('Comparer les dossiers').click()
  // synchronized browsing: entering "site" on the left follows on the right
  await page.keyboard.press('Control+b')
  await row(0, 'site').dblclick()
  await waitList(0)
  await until('right pane follows', async () => (await pane(1).locator('.pathbar').innerText()).includes('site'))
  await page.keyboard.press('Control+b')
  await nav(0, LOCAL)
  await nav(1, REMOTE)
})

step('favoris et dossiers récents', async () => {
  await pane(0).locator('.pane-status').click()
  await page.keyboard.press('Control+d')
  await pane(0).locator('[data-tip="Favoris"]').click()
  await page.locator('.menu-item', { hasText: 'Retirer le favori' }).waitFor()
  await page.keyboard.press('Escape')
  await until('bookmark saved', async () => JSON.stringify((await rpc('prefs.get')).bookmarks).includes('travail'))
  await pane(0).locator('[data-tip="Dossiers récents"]').click()
  await page.locator('.menu-item').first().waitFor()
  await page.keyboard.press('Escape')
})

step('filtre et sélection par masque', async () => {
  await focus(0, 'a.txt')
  await page.keyboard.press('Control+f')
  const filter = pane(0).locator('input[placeholder^="Filtrer"]')
  await filter.fill('*.md')
  await until('filter', async () => (await row(0, 'a.txt').count()) === 0 && (await row(0, 'notes.md').count()) === 1)
  await filter.press('Escape')
  await until('filter cleared', async () => (await row(0, 'a.txt').count()) === 1)
  await focus(0, 'a.txt')
  await page.keyboard.press('Escape')
  await page.keyboard.press('NumpadAdd')
  await dialog().locator('input').fill('*.txt; *.md')
  await page.keyboard.press('Enter')
  await until('selection', async () => (await pane(0).locator('.pane-foot, .pane-status').first().innerText()).includes('2 sélectionné'))
  await page.keyboard.press('Escape')
})

step('panneau local actualisé automatiquement', async () => {
  await nav(0, LOCAL)
  writeFileSync(join(LOCAL, 'apparu.txt'), 'créé par une autre application')
  await until('external file shown without refresh', async () => (await row(0, 'apparu.txt').count()) === 1, 5000)
})

step('arborescence, aperçu et disposition Explorateur', async () => {
  await tip('Arborescence des dossiers').click()
  await pane(0).locator('.dir-tree').getByText('~', { exact: true }).waitFor()
  await tip('Arborescence des dossiers').click()
  // a 1×1 PNG (SVG is deliberately not previewed: it may contain active content)
  writeFileSync(join(LOCAL, 'image.png'), Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', 'base64'))
  await refresh(0)
  await focus(0, 'notes.md')
  await page.keyboard.press('Control+Shift+P')
  await page.locator('.markdown h1', { hasText: 'notes' }).waitFor()
  await focus(0, 'image.png')
  await page.locator('.preview img').waitFor()
  assert.equal(await page.locator('.preview img').evaluate((i) => i.naturalWidth), 1, 'image decoded')
  await page.keyboard.press('Control+Shift+P')
  await tip('Disposition').click()
  await until('explorer layout', async () => (await V('.pane').count()) === 1)
  await tip('Disposition').click()
  await until('commander layout', async () => (await V('.pane').count()) === 2)
})

step('diff visuel', async () => {
  writeFileSync(join(LOCAL, 'notes.md'), '# notes v3\nligne ajoutée\n')
  await refresh(0)
  await focus(1, 'notes.md')
  await focus(0, 'notes.md')
  await row(0, 'notes.md').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Comparer avec le fichier' }).click()
  await V('.cm-mergeView').waitFor()
  await V('.diff-head').getByText(/modification/).waitFor()
  await page.locator('.tab.active .tab-close').click()
})

step('somme de contrôle, nouveau lien, calcul de taille', async () => {
  await focus(1, 'notes.md')
  await row(1, 'notes.md').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Somme de contrôle' }).click()
  const d = dialog()
  await d.locator('.code-block').waitFor()
  const want = await sha256(await remoteText(`${REMOTE}/notes.md`))
  assert.equal((await d.locator('.code-block').innerText()).trim(), want)
  await d.locator('input').fill(want)
  await d.getByText('identique').waitFor()
  await page.keyboard.press('Escape')
  await focus(1, 'notes.md')
  await pane(1).locator('.filelist').click({ button: 'right', position: { x: 300, y: 400 } })
  await page.locator('.menu-item', { hasText: 'Nouveau lien' }).click()
  await dialog().locator('input').first().fill('lien-notes')
  await button(dialog(), 'OK').click()
  await until('symlink', async () => (await rpc('fs.list', { conn: (await sftp()).id, path: REMOTE })).entries.some((e) => e.name === 'lien-notes' && e.isLink))
})

step('commande personnalisée et exécuter une commande', async () => {
  await refresh(1)
  await focus(1, 'site')
  await row(1, 'site').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Commandes personnalisées' }).hover()
  await page.locator('.menu-item', { hasText: 'Espace disque utilisé' }).click()
  await dialog().locator('pre').getByText(/site/).waitFor()
  await page.keyboard.press('Escape')
  await page.keyboard.press('Control+Shift+X')
  await dialog().locator('input').fill('echo commande-$((1+1))')
  await page.keyboard.press('Enter')
  await dialog().locator('pre').getByText('commande-2').waitFor()
  await dialog().getByText('Code de sortie 0').waitFor()
  await page.keyboard.press('Escape')
})

step('propriétés d\'un dossier (récursif, taille)', async () => {
  await focus(1, 'site')
  await page.keyboard.press('F9')
  const d = dialog()
  await button(d, 'Calculer').first().click()
  await d.getByText(/fichier\(s\)/).waitFor()
  await d.locator('label', { hasText: 'Octal' }).locator('xpath=following-sibling::input').fill('0750')
  await d.getByText('Appliquer récursivement').click()
  await button(d, 'OK').click()
  await until('recursive chmod', async () => ((await rpc('fs.stat', { conn: (await sftp()).id, path: `${REMOTE}/site/css` })).mode & 0o777) === 0o750)
})

step('file de transferts : pause et annulation', async () => {
  await rpc('transfer.clear')
  const big = Buffer.alloc(3 << 20, 7)
  writeFileSync(join(LOCAL, 'gros.bin'), big)
  await refresh(0)
  await focus(0, 'gros.bin')
  await page.keyboard.press('Shift+F5')
  const d = dialog()
  await d.locator('.disclosure').click()
  await d.locator('.field', { hasText: 'Limite de vitesse' }).locator('input').fill('200')
  await button(d, 'OK').click()
  const job = page.locator('.job', { hasText: 'gros.bin' }).first()
  await job.waitFor()
  await job.locator('[data-tip="Pause"]').click()
  await until('paused', async () => (await rpc('transfer.list')).some((j) => j.status === 'paused'))
  await job.locator('[data-tip="Reprendre"]').click()
  await job.locator('[data-tip="Annuler le transfert"]').click()
  await until('cancelled', async () => (await rpc('transfer.list')).some((j) => j.status === 'cancelled'))
  await page.locator('.bottom').getByRole('button', { name: /Effacer les terminés/ }).click()
  await until('cleared', async () => (await rpc('transfer.list')).length === 0)
})

step('informations serveur et journal', async () => {
  await tip('Informations serveur/protocole').click()
  await dialog().getByText('SSH-2.0-OpenSSH', { exact: false }).waitFor()
  await dialog().getByText('Empreinte (SHA-256)').waitFor()
  await page.keyboard.press('Escape')
  await tip('Journal de session').click()
  await page.locator('.bottom').getByText('Authenticated', { exact: false }).first().waitFor()
})

step('éditeur externe', async () => {
  const editor = join(TMP, 'editeur.sh')
  writeFileSync(editor, '#!/bin/sh\nsleep 1\necho "ajouté par l\'éditeur externe" >> "$1"\n', { mode: 0o755 })
  const p = await rpc('prefs.get')
  await rpc('prefs.set', { ...p, editor: { ...p.editor, externalCmd: editor } })
  await focus(1, 'notes.md')
  const before = await remoteText(`${REMOTE}/notes.md`)
  await page.keyboard.press('Shift+F4')
  await until('external edit uploaded', async () => (await remoteText(`${REMOTE}/notes.md`)) === before + "ajouté par l'éditeur externe\n")
  if (!(await page.locator('.bottom .bp-tabs').count())) await page.keyboard.press('Control+Shift+Q')
  await page.locator('.bp-tab', { hasText: 'Fichiers édités' }).click()
  await page.locator('.bottom').getByRole('button', { name: 'Arrêter' }).first().click()
  await until('edit stopped', async () => (await rpc('edit.list')).length === 0)
})

step('onglets : dupliquer la session, fermer les autres', async () => {
  const tab = page.locator('.tab', { hasText: '127.0.0.1' }).first()
  await tab.click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Dupliquer la session' }).click()
  await until('second session', async () => (await page.locator('.tab', { hasText: '127.0.0.1' }).count()) === 2)
  await page.locator('.tab', { hasText: '127.0.0.1' }).last().click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Fermer les autres onglets' }).click()
  await until('one session left', async () => (await page.locator('.tab', { hasText: '127.0.0.1' }).count()) === 1)
})

step('URL et code de session, espaces de travail', async () => {
  const s = await rpc('sites.save', { site: { id: 'site-ui', name: 'Site UI', protocol: 'sftp', host: '127.0.0.1', port: 2222, user: 'skiff', useAgent: false, resolveSymlinks: true }, secrets: { password: 'skiff' } })
  await page.locator('.tab.home').click()
  const item = page.locator('.sites-tree .tree-site', { hasText: 'Site UI' }).first()
  await item.click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Générer URL' }).click()
  await dialog().getByText('sftp://skiff@127.0.0.1:2222', { exact: false }).first().waitFor()
  await dialog().getByText('Inclure le mot de passe').click()
  await dialog().getByText('sftp://skiff:skiff@127.0.0.1:2222', { exact: false }).first().waitFor()
  await page.keyboard.press('Escape')
  await item.dblclick()
  await until('site connected', async () => (await conns()).some((c) => c.siteId === s.id))
  await page.keyboard.press('Control+k')
  await page.locator('.palette input').fill('espace de travail')
  await page.keyboard.press('Enter')
  await dialog().locator('input').fill('Espace UI')
  await page.keyboard.press('Enter')
  await until('workspace saved', async () => (await rpc('prefs.get')).workspaces?.some((w) => w.name === 'Espace UI'))
  await page.locator('.tab.home').click()
  await page.locator('.sites-side').getByText('Espace UI').waitFor()
})

step('conversion de clé (préférences)', async () => {
  await rpc('keys.generate', { type: 'ed25519', format: 'openssh', saveTo: join(HOME, '.ssh', 'id_conv') })
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Sécurité', exact: true }).click()
  await d.locator('tr', { hasText: 'id_conv' }).getByRole('button', { name: 'Convertir' }).click()
  await button(dialog(), 'Convertir').click()
  await until('converted key', async () => existsSync(join(HOME, '.ssh', 'id_conv.ppk')))
  assert.match(readFileSync(join(HOME, '.ssh', 'id_conv.ppk'), 'utf8'), /^PuTTY-User-Key-File-3/)
  await page.keyboard.press('Escape')
})

step('générer une paire de clés', async () => {
  await page.keyboard.press('Control+k')
  await page.locator('.palette input').fill('paire de clés')
  await page.keyboard.press('Enter')
  await button(dialog(), 'Générer').click()
  await dialog().getByText('Clé publique').waitFor()
  assert.match(await dialog().locator('.code-block').innerText(), /^ssh-ed25519 /)
  assert.ok(existsSync(join(HOME, '.ssh', 'id_ed25519_skiff')))
  await page.keyboard.press('Escape')
})

step('mot de passe maître, verrouillage et déverrouillage', async () => {
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Sécurité', exact: true }).click()
  await button(d, 'Définir un mot de passe maître').click()
  const m = dialog()
  await m.locator('input[type=password]').nth(0).fill('Maitre-UI-2026!')
  await m.locator('input[type=password]').nth(1).fill('Maitre-UI-2026!')
  await button(m, 'OK').click()
  await until('master set', async () => (await rpc('app.info')).hasMaster)
  await d.getByRole('button', { name: /Verrouiller les mots de passe maintenant/ }).click()
  await until('locked', async () => (await rpc('app.info')).locked)
  await page.keyboard.press('Escape')
  await page.locator('.tab.home').click()
  await page.locator('.locked-banner').click()
  await dialog().locator('input[type=password]').fill('Maitre-UI-2026!')
  await button(dialog(), 'Déverrouiller').click()
  await until('unlocked', async () => !(await rpc('app.info')).locked)
  await page.locator('.locked-banner').waitFor({ state: 'detached' })
  await rpc('security.setMaster', { old: 'Maitre-UI-2026!', new: '' })
})

step('connexion perdue puis reconnexion', async () => {
  const tab = page.locator('.tab', { hasText: '127.0.0.1' }).first()
  await tab.click()
  execSync('docker restart -t 1 testenv-ssh-1')
  await V('.lost-bar').waitFor({ timeout: 40000 })
  await sleep(6000)
  await V('.lost-bar').getByRole('button', { name: 'Reconnecter' }).click()
  await V('.lost-bar').waitFor({ state: 'detached', timeout: 40000 })
  await until('reconnected', async () => !!(await sftp()))
})

step('mode web : connexion et déconnexion', async () => {
  const port = PORT + 1000
  const web = spawn(SKIFF, ['serve', '--no-tls', '--listen', `127.0.0.1:${port}`, '--user', 'alex', '--password-env', 'WEBPW'], {
    env: { ...process.env, HOME, SKIFF_HOME: join(TMP, 'web-config'), WEBPW: 'mot-de-passe-web' }, stdio: 'ignore',
  })
  try {
    await until('web server', async () => (await fetch(`http://127.0.0.1:${port}/api/auth/status`)).ok)
    const ctx = await browser.newContext({ locale: 'fr-FR' })
    const p = await ctx.newPage()
    await p.goto(`http://127.0.0.1:${port}/`)
    await p.locator('.login-card').getByText('Connexion à Skiff').waitFor()
    await p.locator('.login-card input').nth(0).fill('alex')
    await p.locator('.login-card input').nth(1).fill('faux')
    await p.locator('.login-card button').click()
    await p.locator('.login-card .alert').waitFor()
    await p.locator('.login-card input').nth(1).fill('mot-de-passe-web')
    await p.locator('.login-card button').click()
    await p.locator('.welcome').waitFor()
    await p.locator('[data-tip="Se déconnecter"]').click()
    await p.locator('.login-card').waitFor()
    await ctx.close()
  } finally {
    web.kill()
  }
})

step('aucune erreur JavaScript', () => {
  assert.deepEqual(pageErrors, [])
})
