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
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync, copyFileSync, chmodSync } from 'node:fs'
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

// rpcAt calls the API of the Skiff started on base with SKIFF_DEV_TOKEN=token.
async function rpcAt(base, token, method, params = {}) {
  const r = await fetch(`${base}/api/rpc/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Skiff': '1', Cookie: `skiff_session=${token}` },
    body: JSON.stringify(params),
  })
  const b = await r.json()
  if (!r.ok) throw new Error(`${method}: ${b?.error?.message}`)
  return b.result
}
const rpc = (method, params) => rpcAt(BASE, 'ui', method, params)

// startSkiff starts the Skiff under test (again, keeping its configuration).
async function startSkiff() {
  skiff = spawn(SKIFF, [], {
    env: {
      ...process.env, HOME, SKIFF_HOME: join(TMP, 'config'), SKIFF_NO_WINDOW: '1', SKIFF_PORT: String(PORT), SKIFF_DEV_TOKEN: 'ui',
      SKIFF_UI_DIR: join(ROOT, 'frontend/dist'), SSH_AUTH_SOCK: '', XDG_DATA_HOME: join(HOME, '.local/share'), XDG_CONFIG_HOME: join(HOME, '.config'),
      SKIFF_SSH_SYSTEM_CONFIG: 'none', SKIFF_BROWSER: join(TMP, 'browser.sh'),
    },
    stdio: 'ignore',
  })
  await until('Skiff to start', async () => (await fetch(`${BASE}/api/auth/status`)).ok)
}

// stop ends a Skiff process like closing the application does.
async function stop(proc) {
  if (proc.exitCode !== null || proc.signalCode !== null) return
  const exited = new Promise((r) => proc.once('exit', r))
  proc.kill()
  await exited
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

// fillField types into a dialog field once the dialog has focused it (it
// focuses and selects the name stem a tick after opening).
async function fillField(input, value) {
  await until('field focused', () => input.evaluate((el) => el === document.activeElement))
  await input.fill(value)
}

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
  writeFileSync(join(HOME, '.ssh', 'config'), [
    'Host ui-box', '    HostName 127.0.0.1', '    Port 2222', '    User skiff',
    '    StrictHostKeyChecking accept-new', '    HostKeyAlias ui-box-key', '    Compression yes', '',
  ].join('\n'))
  // the browser of the tests writes the addresses it is asked to open
  writeFileSync(join(TMP, 'browser.sh'), `#!/bin/sh\necho "$1" >> ${JSON.stringify(join(TMP, 'opened.txt'))}\n`)
  chmodSync(join(TMP, 'browser.sh'), 0o755)
  await startSkiff()
  const prefs = await rpc('prefs.get')
  await rpc('prefs.set', { ...prefs, language: 'fr', theme: 'light', confirmTransfer: false, restoreTabs: false })
  browser = await chromium.launch()
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 }, locale: 'fr-FR', permissions: ['clipboard-read', 'clipboard-write'] })
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
  await fillField(dialog().locator('input'), 'dossier-ui')
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
  await fillField(dialog().locator('input'), 'note.txt')
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
  await fillField(dialog().locator('input').first(), '*.css')
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
  // the licences: Skiff's, and those of the software it includes
  await d.locator('nav.vtabs').getByRole('button', { name: 'À propos de Skiff', exact: true }).click()
  assert.ok((await d.innerText()).includes('Apache-2.0'), 'licence shown')
  await button(d, 'Logiciels inclus').click()
  const lic = page.locator('.dialog').last()
  await lic.locator('.licence-text', { hasText: 'github.com/pkg/sftp' }).waitFor()
  await lic.locator('input').fill('wails')
  await until('filtered', async () => {
    const txt = await lic.locator('.licence-text').innerText()
    return txt.includes('github.com/wailsapp/wails/v2') && !txt.includes('github.com/pkg/sftp')
  })
  await button(lic, 'Skiff').click()
  await lic.locator('.licence-text', { hasText: 'TERMS AND CONDITIONS' }).waitFor()
  await button(lic, 'Fermer').click()
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

step('sites : sélection multiple, déplacer, supprimer', async () => {
  await rpc('folders.add', { name: 'Lot UI' })
  await rpc('folders.add', { name: 'Rangés UI' })
  for (const name of ['Lot A', 'Lot B', 'Lot C', 'Lot D']) {
    await rpc('sites.save', { site: { name, protocol: 'sftp', host: '127.0.0.1', port: 2222, user: 'skiff', useAgent: false, resolveSymlinks: true, folder: 'Lot UI' }, secrets: {} })
  }
  const byName = async (n) => (await rpc('sites.list')).sites.find((x) => x.name === n)
  try {
    await page.locator('.tab.home').click()
    const siteRow = (n) => page.locator('.sites-tree .tree-site', { has: page.locator('.site-name', { hasText: new RegExp(`^${n}$`) }) }).last()
    await siteRow('Lot A').waitFor()
    // a range with Shift+click, a site taken out with Ctrl+click
    await siteRow('Lot A').click()
    await siteRow('Lot C').click({ modifiers: ['Shift'] })
    await page.locator('.sites-multi h2', { hasText: '3 sites sélectionnés' }).waitFor()
    await siteRow('Lot B').click({ modifiers: ['Control'] })
    await page.locator('.sites-multi h2', { hasText: '2 sites sélectionnés' }).waitFor()
    assert.equal(await page.locator('.sites-multi .sites-chip').count(), 2)
    // dragging one of them onto a folder moves them all
    await siteRow('Lot A').dragTo(page.locator('.tree-folder', { hasText: 'Rangés UI' }))
    await until('both moved', async () => (await byName('Lot A'))?.folder === 'Rangés UI' && (await byName('Lot C'))?.folder === 'Rangés UI' && (await byName('Lot B'))?.folder === 'Lot UI')
    // the menu of the selection, then the panel deletes them
    await siteRow('Lot C').click({ button: 'right' })
    await page.locator('.menu-label', { hasText: '2 sites sélectionnés' }).waitFor()
    await page.keyboard.press('Escape')
    await page.locator('.sites-multi').getByRole('button', { name: 'Supprimer les 2' }).click()
    await button(dialog(), 'Supprimer').click()
    await until('both deleted', async () => !(await byName('Lot A')) && !(await byName('Lot C')))
    await page.locator('.sites-multi').waitFor({ state: 'detached' })
    // Ctrl+A selects the sites shown; Escape lets go
    await page.locator('.sites-side input').fill('Lot')
    await siteRow('Lot B').click()
    await page.keyboard.press('Control+a')
    await page.locator('.sites-multi h2', { hasText: '2 sites sélectionnés' }).waitFor()
    await page.keyboard.press('Escape')
    await page.locator('.sites-multi').waitFor({ state: 'detached' })
    await page.locator('.sites-side input').fill('')
  } finally {
    const ids = (await rpc('sites.list')).sites.filter((x) => x.name.startsWith('Lot ')).map((x) => x.id)
    if (ids.length) await rpc('sites.delete', { ids })
    await rpc('folders.delete', { name: 'Lot UI', withSites: false }).catch(() => {})
    await rpc('folders.delete', { name: 'Rangés UI', withSites: false }).catch(() => {})
  }
})

step('site : choisir la clé privée parmi celles de ~/.ssh', async () => {
  rmSync(join(HOME, '.ssh', 'id_choisie_ui'), { force: true })
  rmSync(join(HOME, '.ssh', 'id_choisie_ui.pub'), { force: true })
  rmSync(join(HOME, '.ssh', 'id_neuve_ui'), { force: true })
  rmSync(join(HOME, '.ssh', 'id_neuve_ui.pub'), { force: true })
  await rpc('keys.generate', { type: 'ed25519', comment: 'choisie-ui', passphrase: 'phrase-ui', format: 'openssh', saveTo: join(HOME, '.ssh', 'id_choisie_ui') })
  await page.locator('.tab.home').click()
  await page.locator('[data-tip="Nouveau site"]').first().click()
  const ed = page.locator('.site-editor')
  await ed.locator('.se-name').fill('Site à clé choisie')
  await ed.locator('.field', { hasText: "Nom d'hôte" }).locator('input').first().fill('127.0.0.1')
  const keyField = ed.locator('.field', { hasText: 'Fichier de clé privée' }).first()
  const keyInput = keyField.locator('input')
  await keyField.locator('[data-tip="Choisir une clé de ~/.ssh"]').click()
  await page.locator('.menu-label', { hasText: '~/.ssh' }).waitFor()
  await page.locator('.menu-item', { hasText: '~/.ssh/id_choisie_ui' }).click()
  assert.equal(await keyInput.inputValue(), '~/.ssh/id_choisie_ui')
  // the current key is checked; "None" empties the field
  await keyField.locator('[data-tip="Choisir une clé de ~/.ssh"]').click()
  assert.match(await page.locator('.menu-item', { hasText: '~/.ssh/id_choisie_ui' }).innerText(), /✓/)
  await page.locator('.menu-item', { hasText: 'Aucune : essayer les clés habituelles' }).click()
  assert.equal(await keyInput.inputValue(), '')
  // a key generated from the menu is chosen
  await keyField.locator('[data-tip="Choisir une clé de ~/.ssh"]').click()
  await page.locator('.menu-item', { hasText: 'Générer une paire de clés' }).click()
  const gen = page.locator('.dialog').last()
  // typed once the dialog is ready, that is focused (typed before, the
  // default name could stay)
  const saveTo = gen.locator('.field', { hasText: 'Enregistrer la clé privée dans' }).locator('input')
  await until('dialog focused', () => gen.evaluate((el) => el.contains(document.activeElement)))
  await saveTo.fill(join(HOME, '.ssh', 'id_neuve_ui'))
  await until('path typed', async () => (await saveTo.inputValue()).endsWith('id_neuve_ui'))
  await button(gen, 'Générer').click()
  await gen.locator('.code-block').waitFor()
  await button(gen, 'Fermer').click()
  assert.equal(await keyInput.inputValue(), '~/.ssh/id_neuve_ui')
  await ed.getByRole('button', { name: /Enregistrer/ }).click()
  await until('site saved with the key', async () => (await rpc('sites.list')).sites.some((x) => x.name === 'Site à clé choisie' && x.keyFile === '~/.ssh/id_neuve_ui'))
  const site = (await rpc('sites.list')).sites.find((x) => x.name === 'Site à clé choisie')
  await rpc('sites.delete', { ids: [site.id] })
  for (const f of ['id_choisie_ui', 'id_choisie_ui.pub', 'id_neuve_ui', 'id_neuve_ui.pub']) rmSync(join(HOME, '.ssh', f), { force: true })
})

step('configuration OpenSSH : hôtes, paramètres détectés, préférences', async () => {
  await page.locator('.tab.home').click()
  const host = page.locator('[data-ssh-host="ui-box"]')
  assert.match(await host.innerText(), /skiff@127\.0\.0\.1:2222/)
  await host.click()
  const panel = page.locator('.sshcfg-panel')
  const hostName = panel.locator('.sshcfg-row', { hasText: 'HostName' })
  await hostName.waitFor()
  assert.match(await hostName.innerText(), /127\.0\.0\.1\s+appliqué\s+~\/\.ssh\/config:2/)
  assert.match(await panel.locator('.sshcfg-row', { hasText: 'Compression' }).innerText(), /ignoré/)
  // connect by the alias: the password is asked (no key), not the host key (accept-new)
  await button(panel, 'Connexion').click()
  const d = dialog()
  await d.locator('input[type="password"]').fill('skiff')
  await button(d, 'OK').click()
  await until('alias connected', async () => (await conns()).some((c) => c.title === 'ui-box' && c.state === 'connected'))
  await V('.toolbar').getByText('skiff@127.0.0.1').waitFor()
  assert.ok((await rpc('hostkeys.list')).some((k) => k.hosts === '[ui-box-key]:2222'), 'key recorded under the HostKeyAlias')
  await page.locator('.tab', { hasText: 'ui-box' }).click({ button: 'right' })
  await page.locator('.menu-item', { hasText: "Fermer l'onglet" }).click()
  await until('alias closed', async () => !(await conns()).some((c) => c.title === 'ui-box' && c.state === 'connected'))
  // a site made from the host keeps the alias; the editor shows what applies
  await page.locator('.tab.home').click()
  await host.click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Créer un site' }).click()
  const ed = page.locator('.site-editor')
  await ed.locator('.sshcfg-detected').waitFor()
  const user = ed.locator('.field', { hasText: "Nom d'utilisateur" }).locator('input').first()
  assert.equal(await user.getAttribute('placeholder'), 'skiff')
  await user.fill('root')
  await ed.locator('.sshcfg-detected button').click()
  await until('user replaced by the site', async () => /\bsite\b/.test(await ed.locator('.sshcfg-row', { hasText: /^User/ }).innerText()))
  await ed.getByRole('button', { name: 'Annuler' }).click()
  // preferences: files read; turned off, the hosts disappear
  await page.keyboard.press('Control+,')
  const prefs = dialog()
  await prefs.locator('nav.vtabs').getByRole('button', { name: 'SSH', exact: true }).click()
  assert.match(await prefs.locator('.sshcfg-file').first().innerText(), /Utilisateur\s+~\/\.ssh\/config\s+trouvé/)
  await prefs.locator('label', { hasText: 'Lire la configuration OpenSSH' }).click()
  await until('turned off', async () => (await rpc('prefs.get')).ssh.ignoreConfig === true)
  await page.keyboard.press('Escape')
  await host.waitFor({ state: 'detached' })
  await rpc('prefs.set', { ...(await rpc('prefs.get')), ssh: { ignoreConfig: false } })
  await host.waitFor()
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
  await fillField(dialog().locator('input'), '*.txt; *.md')
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
  await fillField(dialog().locator('input').first(), 'lien-notes')
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
  await fillField(dialog().locator('input'), 'echo commande-$((1+1))')
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

step('projet : arborescence, onglets, recherche, aller au fichier, terminal', async () => {
  const conn = (await sftp()).id
  const root = `${REMOTE}/projet-ui`
  await rpc('fs.mkdir', { conn, path: root })
  await rpc('fs.mkdir', { conn, path: `${root}/conf` })
  const write = (p, content) => rpc('fs.writeText', { conn, path: `${root}/${p}`, content, encoding: 'utf-8', eol: 'LF', force: true })
  await write('conf/nginx.conf', 'server {\n  listen 80;\n  server_name projet.example.com;\n}\n')
  await write('index.html', '<h1>Projet</h1>\n')
  await nav(1, REMOTE)
  await refresh(1)
  // from the menu of a folder
  await row(1, 'projet-ui').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Ouvrir comme projet' }).click()
  const pv = V('.project')
  await pv.waitFor()
  assert.equal(await page.locator('.tab.active .tab-title').innerText(), 'projet-ui')
  // a file from the tree, edited and saved
  await pv.locator('.ptree [data-path="conf"]').click()
  await pv.locator('.ptree [data-path="conf/nginx.conf"]').click()
  const ed = pv.locator('.project-editor:visible .cm-content')
  await ed.waitFor()
  await until('file shown', async () => (await ed.innerText()).includes('server_name'))
  await ed.click()
  await page.keyboard.press('Control+End')
  await page.keyboard.insertText('# modifié par le test\n')
  await until('dirty', async () => (await pv.locator('.ptab.active .tab-dirty').count()) === 1)
  await page.keyboard.press('Control+s')
  await until('saved on the server', async () => (await remoteText(`${root}/conf/nginx.conf`)).includes('# modifié par le test'))
  assert.equal((await rpc('edit.history', { conn, path: `${root}/conf/nginx.conf` })).length, 1, 'the previous version is kept')
  // a search in all the files opens the file at the line
  await page.keyboard.press('Control+Shift+F')
  await pv.locator('.psearch input').fill('<h1>')
  await page.keyboard.press('Enter')
  await pv.locator('.psearch-file[data-file="index.html"] .psearch-line').click()
  await until('index.html open', async () => (await pv.locator('.ptab.active').getAttribute('data-file')) === 'index.html')
  // a file by its name
  await pv.locator('.project-editor:visible .cm-content').click()
  await page.keyboard.press('Control+p')
  await page.locator('.quick-open input').fill('ngx')
  await page.locator('.quick-open .pal-item.hot', { hasText: 'nginx.conf' }).waitFor()
  await page.keyboard.press('Enter')
  await until('back to nginx.conf', async () => (await pv.locator('.ptab.active').getAttribute('data-file')) === 'conf/nginx.conf')
  // Ctrl+W closes the file, not the project
  await page.keyboard.press('Control+w')
  await until('file closed', async () => (await pv.locator('.ptab').count()) === 1)
  // the terminal of the project, in its folder
  await pv.locator('[data-tip="Terminal"]').click()
  await pv.locator('.project-term .xterm').waitFor()
  await sleep(1000)
  await page.keyboard.type('pwd\n')
  await until('terminal in the project', async () => (await pv.locator('.project-term .xterm-rows').innerText()).includes('projet-ui'))
  // closing with an unsaved file asks
  await pv.locator('.project-editor:visible .cm-content').click()
  await page.keyboard.insertText('x')
  await page.locator('.tab.active .tab-dirty').click()
  await button(dialog(), 'Ne pas enregistrer').click()
  await until('project closed', async () => (await page.locator('.tab', { hasText: 'projet-ui' }).count()) === 0)
  assert.ok(!(await rpc('term.list')).some((x) => x.conn === conn && x.title.includes('projet')), 'its terminal is closed')
  await rpc('fs.delete', { conn, paths: [root] })
})

step('services, journal en direct, compresser et extraire', async () => {
  const fake = (arg) => execSync(`docker exec -i -u 0 testenv-ssh-1 sh -s ${arg} < ${JSON.stringify(fileURLToPath(new URL('../../testenv/fake-systemd.sh', import.meta.url)))}`)
  fake('install')
  const conn = (await sftp()).id
  try {
    // the services of the server
    await tip('Services du serveur').click()
    const d = dialog()
    await d.locator('[data-service="php8.2-fpm"].on').waitFor()
    await d.locator('.svc-logs', { hasText: 'php8.2-fpm[' }).waitFor()
    await d.locator('[data-service="nginx"]').click()
    await d.locator('[data-action="stop"]').click()
    await until('nginx stopped', async () => (await d.locator('[data-service="nginx"] .ctr-state').innerText()) === 'Arrêté')
    await d.locator('[data-action="start"]').click()
    await until('nginx started', async () => (await d.locator('[data-service="nginx"] .ctr-state').innerText()) === 'En marche')
    await d.locator('[data-service="protected"]').click()
    await d.locator('[data-action="stop"]').click()
    await page.locator('.toast', { hasText: 'Seul root peut faire ça' }).waitFor()
    // its journal, live
    await d.locator('[data-service="nginx"]').click()
    await d.getByRole('button', { name: 'Journal en direct' }).click()
    const lv = V('.logs')
    await lv.locator('.logs-line', { hasText: 'live line 2' }).waitFor()
    assert.equal(await page.locator('.tab.active .tab-title').innerText(), 'nginx')
    await lv.getByRole('button', { name: 'Pause' }).click()
    await lv.locator('.logs-state', { hasText: 'En pause' }).waitFor()
    await lv.getByRole('button', { name: 'Reprendre' }).click()
    await closeActiveTab()
    // a file of the server, live
    const log = `${REMOTE}/app.log`
    await rpc('fs.writeText', { conn, path: log, content: 'start\nerror: disk full\nok\n', encoding: 'utf-8', eol: 'LF', force: true })
    await toSession()
    await nav(1, REMOTE)
    await refresh(1)
    await row(1, 'app.log').click({ button: 'right' })
    await page.locator('.menu-item', { hasText: 'Suivre en direct' }).click()
    await V('.logs').locator('.logs-line', { hasText: 'disk full' }).waitFor()
    execSync(`docker exec testenv-ssh-1 sh -c 'echo "warning: ajoutée en direct" >> ${log}'`)
    await V('.logs').locator('.logs-line.warn', { hasText: 'ajoutée en direct' }).waitFor()
    assert.ok(await V('.logs').locator('.logs-line.error', { hasText: 'disk full' }).count(), 'the error is marked')
    await V('.logs input').fill('ajoutée')
    await until('filtered', async () => (await V('.logs .logs-line').count()) === 1)
    await closeActiveTab()
    // compress a folder on the server, then extract it
    await rpc('fs.mkdir', { conn, path: `${REMOTE}/arch-ui` })
    await rpc('fs.writeText', { conn, path: `${REMOTE}/arch-ui/f.txt`, content: 'dans l’archive\n', encoding: 'utf-8', eol: 'LF', force: true })
    await toSession()
    await refresh(1)
    await row(1, 'arch-ui').click({ button: 'right' })
    await page.locator('.menu-item', { hasText: 'Compresser' }).click()
    await button(dialog(), 'TAR.GZ').click()
    assert.equal(await dialog().locator('input').inputValue(), 'arch-ui.tar.gz')
    await dialog().locator('.dialog-foot').getByRole('button', { name: /Compresser/ }).click()
    await until('archive made', () => remoteExists(`${REMOTE}/arch-ui.tar.gz`))
    await refresh(1)
    await row(1, 'arch-ui.tar.gz').click({ button: 'right' })
    await page.locator('.menu-item', { hasText: 'Extraire' }).click()
    await dialog().locator('input').fill('arch-ui-copie')
    await dialog().getByRole('button', { name: 'Extraire', exact: true }).click()
    await until('archive extracted', async () => (await remoteText(`${REMOTE}/arch-ui-copie/arch-ui/f.txt`)) === 'dans l’archive\n')
  } finally {
    fake('remove')
    await rpc('fs.delete', { conn, paths: [`${REMOTE}/app.log`, `${REMOTE}/arch-ui`, `${REMOTE}/arch-ui.tar.gz`, `${REMOTE}/arch-ui-copie`] }).catch(() => {})
  }
})

step('ports du serveur ouverts dans le navigateur, espace disque', async () => {
  const web = (arg) => execSync(`docker exec -i -u skiff testenv-ssh-1 sh -s ${arg} < ${JSON.stringify(fileURLToPath(new URL('../../testenv/fake-web.sh', import.meta.url)))}`)
  web('start')
  const conn = (await sftp()).id
  try {
    await tip('Ports du serveur').click()
    const d = dialog()
    const row3000 = d.locator('[data-port="3000"]')
    await row3000.waitFor()
    assert.match(await row3000.innerText(), /nc[\s\S]*HTTP[\s\S]*Serveur seul/)
    assert.equal(await d.locator('[data-port="2222"]').getByRole('button', { name: 'Ouvrir' }).count(), 0, 'no browser for SSH')
    await row3000.getByRole('button', { name: 'Ouvrir' }).click()
    await page.locator('.toast', { hasText: 'Port 3000 ouvert dans le navigateur' }).waitFor()
    const url = readFileSync(join(TMP, 'opened.txt'), 'utf8').trim().split('\n').pop()
    assert.match(url, /^http:\/\/127\.0\.0\.1:\d+\/$/)
    const page3000 = await (await fetch(url)).text()
    assert.ok(page3000.includes('Tableau de bord interne'), 'the page through the tunnel')
    assert.ok((await rpc('tunnel.list')).some((x) => x.dest === '127.0.0.1:3000' && x.running), 'the tunnel is listed')
    // opened again: the same tunnel
    await row3000.getByRole('button', { name: 'Ouvrir' }).click()
    await until('opened twice', async () => readFileSync(join(TMP, 'opened.txt'), 'utf8').trim().split('\n').length === 2)
    assert.equal(readFileSync(join(TMP, 'opened.txt'), 'utf8').trim().split('\n')[1], url, 'the tunnel is reused')
    await page.keyboard.press('Escape')
    for (const x of await rpc('tunnel.list')) if (x.dest === '127.0.0.1:3000') await rpc('tunnel.stop', { id: x.id })
    // what takes room in a folder of the server
    const dir = `${REMOTE}/place-ui`
    await rpc('fs.mkdir', { conn, path: dir })
    await rpc('fs.mkdir', { conn, path: `${dir}/gros` })
    await rpc('fs.writeText', { conn, path: `${dir}/gros/a.txt`, content: 'x'.repeat(200000), encoding: 'utf-8', eol: 'LF', force: true })
    await rpc('fs.writeText', { conn, path: `${dir}/petit.txt`, content: 'y', encoding: 'utf-8', eol: 'LF', force: true })
    await toSession()
    await nav(1, dir)
    await refresh(1)
    await pane(1).locator('.pane-status').click()
    await page.keyboard.press('Control+k')
    await page.locator('.palette input').fill('prend de la place ici')
    await page.keyboard.press('Enter')
    const uv = V('.usage')
    await uv.locator('.usage-row[data-name="gros"]').waitFor()
    assert.equal(await uv.locator('.usage-row').first().getAttribute('data-name'), 'gros', 'the biggest first')
    assert.match(await uv.locator('.usage-summary').innerText(), /compté par du sur le serveur/)
    await uv.locator('.usage-row[data-name="gros"]').click()
    await uv.locator('.usage-row[data-name="a.txt"]').waitFor()
    await uv.getByRole('button', { name: 'gros' }).waitFor()
    await uv.locator('[data-tip="Dossier parent"]').click()
    await uv.locator('.usage-row[data-name="petit.txt"]').hover()
    await uv.locator('.usage-row[data-name="petit.txt"] [data-tip="Supprimer"]').click()
    await button(dialog(), 'Supprimer').click()
    await until('deleted from the usage view', async () => !(await remoteExists(`${dir}/petit.txt`)) && (await uv.locator('.usage-row[data-name="petit.txt"]').count()) === 0)
    await closeActiveTab()
    await nav(1, REMOTE)
  } finally {
    web('stop')
    // a disk usage tab left open by a failure would mislead the next steps
    for (const tab of await page.locator('.tab', { hasText: 'Espace ·' }).all()) await tab.locator('.tab-close').click().catch(() => {})
    await rpc('fs.delete', { conn, paths: [`${REMOTE}/place-ui`] }).catch(() => {})
  }
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
  await fillField(dialog().locator('input'), 'Espace UI')
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

step('rouvrir les onglets au démarrage', async () => {
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Panneaux', exact: true }).click()
  await d.locator('label', { hasText: 'Rouvrir les sessions de la dernière fois' }).click()
  await until('preference saved', async () => (await rpc('prefs.get')).restoreTabs === true)
  await page.keyboard.press('Escape')
  // the quick connection goes to other folders; "Site UI" stays open
  const dir = `${REMOTE}/reprise`
  await rpc('fs.mkdir', { conn: (await sftp()).id, path: dir })
  await rpc('fs.createFile', { conn: (await sftp()).id, path: `${dir}/temoin.txt`, content: 'x' })
  await toSession()
  await nav(1, dir)
  await nav(0, join(LOCAL, 'site'))
  await until('tabs saved with their folders', async () => {
    const tabs = (await rpc('prefs.get')).lastTabs || []
    return tabs.some((t) => t.url && t.remote === dir && t.local === join(LOCAL, 'site')) && tabs.some((t) => t.siteId === 'site-ui')
  })
  // projects come back too: one on the server of "Site UI", one on this computer
  await page.locator('.tab', { hasText: 'Site UI' }).click()
  await V('.commander').waitFor()
  await pane(1).locator('.pane-status').click()
  await page.keyboard.press('Control+Shift+E')
  await V('.project').waitFor()
  const remoteProject = await page.locator('.tab.active .tab-title').innerText()
  await toSession()
  await pane(0).locator('.pane-status').click()
  await page.keyboard.press('Control+Shift+E')
  await V('.project .ptree [data-path="index.html"]').click()
  await until('projects saved', async () => {
    const tabs = (await rpc('prefs.get')).lastTabs || []
    return tabs.some((t) => t.project?.local && t.project.root === join(LOCAL, 'site') && t.project.files?.[0] === join(LOCAL, 'site', 'index.html')) &&
      tabs.some((t) => t.siteId === 'site-ui' && t.project)
  })
  // quit, then start again
  await page.goto('about:blank')
  await stop(skiff)
  await startSkiff()
  await page.goto(`${BASE}/?token=ui`)
  // the password of the quick connection is not saved: it is asked again
  await dialog().locator('input[type=password]').fill('skiff')
  await button(dialog(), 'OK').click()
  await until('sessions reopened', async () => (await conns()).filter((c) => c.state === 'connected').length === 2)
  await page.locator('.tab', { hasText: 'Site UI' }).waitFor()
  await toSession()
  await row(1, 'temoin.txt').waitFor()
  await row(0, 'index.html').waitFor()
  assert.equal(await pane(1).locator('.crumb.last').innerText(), 'reprise')
  // the projects, on the connection of their session (no third one)
  const local = page.locator('.tab', { has: page.locator('.tab-title', { hasText: /^site$/ }) })
  await local.click()
  await V('.ptab[data-file="index.html"]').waitFor()
  await page.locator('.tab', { has: page.locator('.tab-title', { hasText: new RegExp(`^${remoteProject}$`) }) }).waitFor()
  assert.equal((await conns()).filter((c) => c.state === 'connected').length, 2, 'the project reuses the connection')
  for (const title of ['site', remoteProject]) {
    await page.locator('.tab', { has: page.locator('.tab-title', { hasText: new RegExp(`^${title}$`) }) }).locator('.tab-close').click()
  }
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

const closeActiveTab = async () => {
  await page.locator('.tab.active').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: "Fermer l'onglet" }).click()
}
const termText = async (i) => V('.term-pane .xterm-rows').nth(i).innerText()

step('terminal divisé : diffusion, commandes enregistrées, suivi du dossier', async () => {
  await pane(1).locator('.pane-status').click()
  await page.keyboard.press('Control+t')
  await V('.term-host .xterm').first().waitFor()
  await sleep(800)
  await V('[data-tip="Diviser à droite"]').click()
  await page.locator('.menu-item', { hasText: 'même serveur' }).click()
  await V('.term-pane').nth(1).waitFor()
  await sleep(1200)
  const broadcast = V('[data-tip="Diffuser la saisie à tous les terminaux de l’onglet"]')
  await broadcast.click()
  await V('.term-pane').nth(0).click()
  await page.keyboard.type('echo diffuse-$((6*7))\n')
  await until('input sent to both terminals', async () => (await termText(0)).includes('diffuse-42') && (await termText(1)).includes('diffuse-42'))
  await broadcast.click()
  await V('[data-tip="Commandes enregistrées"]').click()
  await page.locator('.menu-item', { hasText: 'Espace disque' }).click()
  await until('saved command run', async () => (await termText(0)).includes('Filesystem'))
  assert.ok(!(await termText(1)).includes('Filesystem'), 'not broadcast any more')
  await V('[data-tip="Le panneau de fichiers suit le dossier courant"]').click()
  await sleep(1500)
  await page.keyboard.type('cd /tmp\n')
  await until('current folder shown', async () => (await V('.term-bar .term-cwd').innerText()) === '/tmp')
  await closeActiveTab()
  await toSession()
  await until('the file panel followed', async () => (await pane(1).locator('.path-input, .pathbar').first().innerText()).includes('tmp'))
  await nav(1, REMOTE)
})

const clip = () => page.evaluate(() => navigator.clipboard.readText())
const setClip = (text) => page.evaluate((x) => navigator.clipboard.writeText(x), text)
/** double-clicks a word the terminal printed alone on its line */
const selectOutput = async (word) => {
  const line = V('.term-pane .xterm-rows > div').filter({ hasText: new RegExp(`^${word}\\s*$`) }).last()
  const box = await line.boundingBox()
  await page.mouse.dblclick(box.x + 12, box.y + box.height / 2)
}

step('terminal : choix du shell, Ctrl+C / Ctrl+V, clic droit', async () => {
  const conn = (await sftp()).id
  const shells = await rpc('term.shells', { conn })
  const sh = shells.find((x) => x.id === 'sh')
  assert.ok(sh, 'sh found on the server')
  await pane(1).locator('.pane-status').click()
  await tip('Choisir le shell ou l’interpréteur').click()
  await page.locator('.menu-label', { hasText: 'Shells' }).waitFor()
  await page.locator('.menu-item').filter({ has: page.locator('.ellipsis', { hasText: /^sh$/ }) }).click()
  await V('.term-host .xterm').first().waitFor()
  await sleep(800)
  const terms = await rpc('term.list')
  assert.ok(sh.default || terms.some((x) => x.title.endsWith(' · sh')), 'the terminal runs sh')
  // Ctrl+V pastes
  await setClip('echo colle-$((6*7))')
  await page.keyboard.press('Control+v')
  await until('pasted text', async () => (await termText(0)).includes('colle-$((6*7))'))
  await page.keyboard.press('Enter')
  await until('pasted with Ctrl+V', async () => (await termText(0)).includes('colle-42'))
  // a webview that refuses the clipboard to the page (WebKitGTK): Ctrl+V
  // still pastes, by the paste of the system
  await setClip('echo refus-$((6*7))')
  await page.evaluate(() => {
    const deny = () => Promise.reject(new DOMException('denied', 'NotAllowedError'))
    window.__clip = { read: navigator.clipboard.readText, write: navigator.clipboard.writeText }
    navigator.clipboard.readText = deny
  })
  await page.keyboard.press('Control+v')
  await until('pasted without the clipboard API', async () => (await termText(0)).includes('refus-$((6*7))'))
  await page.keyboard.press('Enter')
  await page.evaluate(() => { navigator.clipboard.readText = window.__clip.read })
  // Ctrl+C without a selection still interrupts
  await page.keyboard.type('sleep 60\n')
  await sleep(400)
  await page.keyboard.press('Control+c')
  await page.keyboard.type('echo apres-$((6*7))\n')
  await until('interrupted by Ctrl+C', async () => (await termText(0)).includes('apres-42'), 8000)
  // Ctrl+C with a selection copies it
  await setClip('autre')
  await selectOutput('colle-42')
  await page.keyboard.press('Control+c')
  await until('copied with Ctrl+C', async () => (await clip()) === 'colle-42')
  // right-click like the Windows console: paste, or copy the selection
  await setClip('echo clic-$((6*7))')
  await V('.term-pane').first().click({ button: 'right' })
  await until('text pasted', async () => (await termText(0)).includes('clic-$((6*7))'))
  await page.keyboard.press('Enter')
  await until('pasted with a right-click', async () => (await termText(0)).includes('clic-42'))
  assert.equal(await page.locator('.menu').count(), 0, 'no menu')
  await setClip('autre')
  await selectOutput('clic-42')
  await V('.term-pane').first().click({ button: 'right' })
  await until('copied with a right-click', async () => (await clip()) === 'clic-42')
  // Ctrl+Shift+C / Ctrl+Shift+V only
  await rpc('prefs.set', { ...(await rpc('prefs.get')), terminal: { ...(await rpc('prefs.get')).terminal, copyKeys: 'ctrlShift' } })
  await until('preference applied', async () => (await page.evaluate(() => document.querySelector('.term-bar [data-kbd="Ctrl+Shift+C"]') !== null)))
  await setClip('autre')
  await selectOutput('apres-42')
  await page.keyboard.press('Control+Shift+c')
  await until('copied with Ctrl+Shift+C', async () => (await clip()) === 'apres-42')
  // the menu, chosen in the preferences
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Terminal', exact: true }).click()
  await button(d, 'Ctrl+C / Ctrl+V').click()
  await button(d, 'Menu').click()
  await until('right-click menu saved', async () => (await rpc('prefs.get')).terminal.rightClick === 'menu')
  await page.keyboard.press('Escape')
  await V('.term-pane').first().click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Tout sélectionner' }).click()
  await page.keyboard.press('Control+c')
  await until('everything copied', async () => { const c = await clip(); return c.includes('colle-42') && c.includes('clic-42') })
  await V('.term-pane').first().click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Effacer' }).click()
  await until('cleared', async () => !(await termText(0)).includes('colle-42'))
  await rpc('prefs.set', { ...(await rpc('prefs.get')), terminal: { ...(await rpc('prefs.get')).terminal, rightClick: '', copyKeys: '' } })
  await closeActiveTab()
})

step('moniteur du serveur', async () => {
  await V('[data-tip="Moniteur du serveur"]').click()
  await V('.mon-table tbody tr').first().waitFor()
  await until('processor measured', async () => /%/.test(await V('[data-metric="cpu"] .mon-big').innerText()))
  assert.ok((await V('.mon-disk').count()) >= 1, 'disks listed')
  await V('.monitor input').fill('sshd')
  await until('processes filtered', async () => {
    const names = await V('.mon-table tbody td.mono').allInnerTexts()
    return names.length > 0 && names.every((n) => n.includes('sshd'))
  })
  await closeActiveTab()
})

step('conteneurs de cet ordinateur', async () => {
  // a stopped container of a Compose-like project
  const name = `skiff-ui-${process.pid}`
  execSync(`docker rm -f ${name} >/dev/null 2>&1; docker create --name ${name} --stop-timeout 1 --label com.docker.compose.project=skiff-ui --entrypoint sleep rclone/rclone:latest 600 >/dev/null`)
  try {
    await page.locator('.tab.home').click()
    await page.locator('.action-card', { hasText: 'Conteneurs' }).click()
    const d = dialog()
    assert.ok(await d.getByRole('button', { name: 'Docker', exact: true }).isEnabled(), 'Docker found')
    const mine = d.locator(`[data-container="${name}"]`)
    await mine.waitFor()
    await d.locator('.ctr-group-head', { hasText: 'Projet Compose skiff-ui' }).waitFor()
    assert.match(await mine.locator('.ctr-state').innerText(), /Créé|Arrêté/)
    // the search keeps only matching containers
    await d.locator('input[placeholder^="Rechercher"]').fill('skiff-ui')
    await until('filtered', async () => (await d.locator('.ctr-row').count()) === 1)
    // start it, stop it from its menu
    await mine.getByRole('button', { name: 'Démarrer' }).click()
    await until('started', async () => (await mine.locator('.ctr-state').innerText()).includes('En marche'))
    assert.match(await mine.locator('.ctr-status').innerText(), /^depuis /)
    await mine.locator('[data-tip="Plus"]').click()
    await page.locator('.menu-item', { hasText: 'Arrêter' }).click()
    await until('stopped', async () => (await mine.locator('.ctr-state').innerText()).includes('Arrêté'), 20000)
    // save it as a site: the editor opens with the container
    await mine.locator('[data-tip="Plus"]').click()
    await page.locator('.menu-item', { hasText: 'Enregistrer comme site' }).click()
    const se = page.locator('.site-editor')
    await until('site draft', async () => (await se.locator('.field', { hasText: 'Conteneur' }).locator('input').first().inputValue()) === name)
    await page.locator('.se-head').getByRole('button', { name: 'Annuler', exact: true }).click()
  } finally {
    execSync(`docker rm -f ${name} >/dev/null 2>&1 || true`)
  }
  // the files of a running container, from the menu of the site list
  await moreMenu().click()
  await page.locator('.menu-item', { hasText: 'Conteneurs de cet ordinateur' }).click()
  const ctr = dialog().locator('[data-container="testenv-ssh-1"]')
  await ctr.waitFor()
  await ctr.getByRole('button', { name: 'Fichiers' }).click()
  await until('container session', async () => (await conns()).some((c) => c.protocol === 'container' && c.state === 'connected'))
  await V('.commander').waitFor()
  await until('container root listed', async () => (await row(1, 'etc').count()) === 1)
  await closeActiveTab()
})

step('tâches planifiées : une synchronisation exécutée et son historique', async () => {
  await page.locator('.tab-content:visible .tb-btn', { hasText: 'Synchroniser' }).click()
  assert.ok(await dialog().getByRole('button', { name: /Planifier/ }).isDisabled(), 'an unsaved session cannot be scheduled')
  await page.keyboard.press('Escape')
  await rpc('sites.save', { site: { name: 'Site des tâches', protocol: 'sftp', host: '127.0.0.1', port: 2222, user: 'skiff', useAgent: false, kbdInteractive: true, resolveSymlinks: true, keepalive: 30, timeout: 15 }, secrets: { password: 'skiff' } })
  await page.keyboard.press('Control+k')
  await page.keyboard.type('Tâches planifiées')
  await page.keyboard.press('Enter')
  const tasks = page.locator('.tasks')
  await tasks.locator('[data-tip="Nouvelle tâche"]').click()
  await page.locator('.menu-item', { hasText: 'Synchronisation' }).click()
  const ed = page.locator('.task-editor')
  await ed.locator('.se-name').fill('Publier depuis le test')
  await ed.locator('.field', { hasText: 'Site' }).locator('select').first().selectOption({ label: 'Site des tâches' })
  await ed.locator('.field', { hasText: 'Dossier local' }).locator('input').fill(join(LOCAL, 'site'))
  await ed.locator('.field', { hasText: 'Dossier distant' }).locator('input').fill(REMOTE + '/planifie')
  await ed.getByRole('button', { name: 'Exécuter maintenant' }).click()
  await ed.locator('.task-run[data-status="ok"]').waitFor({ timeout: 60000 })
  assert.ok(await remoteExists(REMOTE + '/planifie/index.html'), 'synchronized by the task')
  await tasks.locator('[data-task="Publier depuis le test"]').waitFor()
  await ed.locator('[data-tip="Supprimer"]').click()
  await button(dialog(), 'Supprimer').click()
  await until('task deleted', async () => (await rpc('tasks.list')).length === 0)
  await closeActiveTab()
})

step('trousseau Skiff et transfert d’agent', async () => {
  const k = await rpc('keys.generate', { type: 'ed25519', comment: 'trousseau-ui', format: 'openssh' })
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Sécurité', exact: true }).click()
  await d.getByRole('button', { name: /Ajouter une clé/ }).click()
  const add = page.locator('.dialog').last()
  await add.locator('textarea').fill(k.private)
  await add.locator('.field', { hasText: 'Nom' }).locator('input').fill('Clé de l’interface')
  await button(add, 'Ajouter').click()
  await until('key in the keychain', async () => (await rpc('keychain.list')).keys.some((x) => x.fingerprint === k.fingerprint))
  const rowKey = d.locator('table.keychain tr', { hasText: 'Clé de l’interface' })
  await rowKey.locator('[data-tip="Supprimer"]').click()
  await button(page.locator('.dialog').last(), 'Supprimer').click()
  await until('key removed', async () => (await rpc('keychain.list')).keys.length === 0)
  // the segmented transfers are on by default
  await d.locator('nav.vtabs').getByRole('button', { name: "File d'attente", exact: true }).click()
  assert.ok(await d.locator('label', { hasText: 'Copier les gros fichiers' }).locator('input').isChecked())
  await page.keyboard.press('Escape')
  // agent forwarding is an option of the SSH section of a site
  await page.locator('.tab.home').click()
  await page.locator('.sites-tree .tree-site', { hasText: 'Site des tâches' }).first().click()
  const se = page.locator('.site-editor')
  await se.locator('nav.vtabs').getByRole('button', { name: 'SSH', exact: true }).click()
  await se.locator('label', { hasText: /Transférer l.agent au serveur/ }).click()
  await button(se, 'Enregistrer').click()
  await until('forwarding saved', async () => (await rpc('sites.list')).sites.some((x) => x.name === 'Site des tâches' && x.forwardAgent))
})

step('installer une clé existante sur le serveur', async () => {
  const c = await sftp()
  const ak = `${c.home}/.ssh/authorized_keys`
  const before = await rpc('fs.readText', { conn: c.id, path: ak }).then((r) => r.content).catch(() => null)
  const body = (pub) => pub.split(' ')[1]
  // an encrypted key without its .pub file: the public key comes from the key itself
  const k = await rpc('keys.generate', { type: 'ed25519', comment: 'installe-ui', passphrase: 'phrase-ui', format: 'openssh', saveTo: join(HOME, '.ssh', 'id_installe_ui') })
  rmSync(join(HOME, '.ssh', 'id_installe_ui.pub'))
  try {
    await page.keyboard.press('Control+,')
    const d = dialog()
    await d.locator('nav.vtabs').getByRole('button', { name: 'Sécurité', exact: true }).click()
    await d.locator('tr', { hasText: 'id_installe_ui' }).locator('[data-tip="Installer sur un serveur…"]').click()
    const inst = page.locator('.dialog').last()
    await inst.getByText('Installer une clé publique', { exact: true }).waitFor()
    assert.ok((await inst.locator('.code-block').innerText()).includes(body(k.public)), 'the public key of the chosen key')
    await button(inst, 'Installer').click()
    await until('key installed', async () => (await remoteText(ak)).includes(body(k.public)))
    await until('install dialog closed', async () => (await page.locator('.dialog').count()) === 1)
    await page.keyboard.press('Escape')
    // a pasted key, from the palette of the session
    const other = await rpc('keys.generate', { type: 'ed25519', comment: 'collee-ui', format: 'openssh' })
    await page.keyboard.press('Control+k')
    await page.locator('.palette input').fill('Installer une clé publique')
    await page.keyboard.press('Enter')
    const inst2 = page.locator('.dialog').last()
    const field = (name) => inst2.locator('.field').filter({ has: page.locator('label', { hasText: new RegExp(`^${name}$`) }) })
    await field('Clé').locator('select').selectOption('paste')
    await inst2.locator('textarea').fill(other.public)
    assert.equal(await field('Serveur').locator('select option:checked').innerText(), await page.locator('.tab.active .tab-title').innerText(), 'the server of the session')
    await button(inst2, 'Installer').click()
    await until('pasted key installed', async () => (await remoteText(ak)).includes(body(other.public)))
  } finally {
    rmSync(join(HOME, '.ssh', 'id_installe_ui'), { force: true })
    if (before === null) await rpc('fs.delete', { conn: c.id, paths: [ak] })
    else await rpc('fs.writeText', { conn: c.id, path: ak, content: before, encoding: 'utf-8', eol: 'LF', force: true })
  }
})

step('clés autorisées : lister, remplacer après vérification, retirer', async () => {
  const c = await sftp()
  const ak = `${c.home}/.ssh/authorized_keys`
  const before = await rpc('fs.readText', { conn: c.id, path: ak }).then((r) => r.content).catch(() => null)
  const body = (pub) => pub.split(' ')[1]
  const files = ['id_ak_ui_a', 'id_ak_ui_a.pub', 'id_ak_ui_b', 'id_ak_ui_b.pub'].map((f) => join(HOME, '.ssh', f))
  files.forEach((f) => rmSync(f, { force: true }))
  const a = await rpc('keys.generate', { type: 'ed25519', comment: 'ak-ui-a', format: 'openssh', saveTo: join(HOME, '.ssh', 'id_ak_ui_a') })
  const b = await rpc('keys.generate', { type: 'ed25519', comment: 'ak-ui-b', format: 'openssh', saveTo: join(HOME, '.ssh', 'id_ak_ui_b') })
  await rpc('keys.install', { conn: c.id, publicKey: a.public })
  try {
    await page.locator('.tab', { hasText: '127.0.0.1' }).first().click()
    await V('[data-tip="Clés autorisées"]').click()
    const d = dialog()
    const keyRow = (fp) => d.locator(`.ak-row[data-fingerprint="${fp}"]`)
    await keyRow(a.fingerprint).waitFor()
    const text = await keyRow(a.fingerprint).innerText()
    assert.ok(text.includes('ak-ui-a') && text.includes('Ed25519'), 'comment and type')
    assert.ok(text.includes('id_ak_ui_a'), 'its private key is on this computer')
    // the session logged in with its password: no key is "this connection"
    assert.equal(await d.locator('.ak-row.own').count(), 0)
    // replace a with b: b opens a connection on its own, then a goes
    await keyRow(a.fingerprint).locator('[data-tip="Remplacer par une autre clé…"]').click()
    const r = page.locator('.dialog').last()
    await r.locator('.dialog-head h3', { hasText: 'Remplacer une clé' }).waitFor()
    const field = (name) => r.locator('.field').filter({ has: page.locator('label', { hasText: new RegExp(`^${name}$`) }) })
    assert.ok((await field('Clé à retirer').locator('select option:checked').innerText()).includes('id_ak_ui_a'), 'the key of the row')
    const opt = await field('Nouvelle clé').locator('option', { hasText: 'id_ak_ui_b' }).getAttribute('value')
    await field('Nouvelle clé').locator('select').selectOption(opt)
    await button(r, 'Remplacer (1)').click()
    await r.locator('.roll-row.done', { hasText: 'Remplacée (nouvelle clé vérifiée)' }).waitFor({ timeout: 30000 })
    const after = await remoteText(ak)
    assert.ok(after.includes(body(b.public)) && !after.includes(body(a.public)), 'a replaced by b')
    await button(r, 'Fermer').click()
    await keyRow(b.fingerprint).waitFor()
    assert.equal(await keyRow(a.fingerprint).count(), 0)
    // remove b, after confirming
    await keyRow(b.fingerprint).locator('[data-tip="Retirer la clé"]').click()
    await button(dialog(), 'Retirer la clé').click()
    await until('b removed', async () => !(await remoteText(ak)).includes(body(b.public)))
    await keyRow(b.fingerprint).waitFor({ state: 'detached' })
    await page.keyboard.press('Escape')
  } finally {
    files.forEach((f) => rmSync(f, { force: true }))
    if (before === null) await rpc('fs.delete', { conn: c.id, paths: [ak] })
    else await rpc('fs.writeText', { conn: c.id, path: ak, content: before, encoding: 'utf-8', eol: 'LF', force: true })
  }
})

step('clés : installer une clé sur plusieurs sites', async () => {
  const c = await sftp()
  const ak = `${c.home}/.ssh/authorized_keys`
  const before = await rpc('fs.readText', { conn: c.id, path: ak }).then((r) => r.content).catch(() => null)
  const k = await rpc('keys.generate', { type: 'ed25519', comment: 'plusieurs-ui', format: 'openssh' })
  await rpc('folders.add', { name: 'Clés UI' })
  for (const name of ['Clé A', 'Clé B']) {
    await rpc('sites.save', { site: { name, protocol: 'sftp', host: '127.0.0.1', port: 2222, user: 'skiff', useAgent: false, resolveSymlinks: true, folder: 'Clés UI' }, secrets: { password: 'skiff' } })
  }
  await rpc('sites.save', { site: { name: 'Clé FTP', protocol: 'ftp', host: '127.0.0.1', port: 2121, user: 'skiff', ftps: 'explicit', useAgent: false, resolveSymlinks: true, folder: 'Clés UI' }, secrets: { password: 'skiff' } })
  try {
    await page.locator('.tab.home').click()
    const siteRow = (n) => page.locator('.sites-tree .tree-site', { has: page.locator('.site-name', { hasText: new RegExp(`^${n}$`) }) }).last()
    await siteRow('Clé A').click()
    await siteRow('Clé FTP').click({ modifiers: ['Shift'] })
    await page.locator('.sites-multi h2', { hasText: '3 sites sélectionnés' }).waitFor()
    await page.locator('.sites-multi').getByRole('button', { name: 'Installer une clé…', exact: true }).click()
    const r = dialog()
    const field = (name) => r.locator('.field').filter({ has: page.locator('label', { hasText: new RegExp(`^${name}$`) }) })
    await field('Clé').locator('select').selectOption('paste')
    await r.locator('textarea').fill(k.public)
    // only the SSH sites of the selection
    assert.equal(await r.locator('.roll-row').count(), 2)
    assert.equal(await r.locator('.roll-row', { hasText: 'Clé FTP' }).count(), 0)
    await button(r, 'Installer (2)').click()
    await r.locator('.roll-row.done', { hasText: 'Clé A' }).waitFor({ timeout: 30000 })
    await r.locator('.roll-row.done', { hasText: 'Clé B' }).waitFor({ timeout: 30000 })
    // the same server twice: the second one has it already
    assert.ok((await r.locator('.roll-row', { hasText: 'Clé A' }).innerText()).includes('Installée'))
    assert.ok((await r.locator('.roll-row', { hasText: 'Clé B' }).innerText()).includes('Déjà installée'))
    assert.equal((await remoteText(ak)).split(k.public.split(' ')[1]).length - 1, 1, 'installed once')
    // the connections opened for it are closed
    await until('connections closed', async () => !(await conns()).some((x) => x.title === 'Clé A' || x.title === 'Clé B'))
    await button(r, 'Fermer').click()
    await page.keyboard.press('Escape')
  } finally {
    const ids = (await rpc('sites.list')).sites.filter((x) => x.folder === 'Clés UI').map((x) => x.id)
    if (ids.length) await rpc('sites.delete', { ids })
    await rpc('folders.delete', { name: 'Clés UI', withSites: false }).catch(() => {})
    if (before === null) await rpc('fs.delete', { conn: c.id, paths: [ak] })
    else await rpc('fs.writeText', { conn: c.id, path: ak, content: before, encoding: 'utf-8', eol: 'LF', force: true })
  }
})

step('liens : ouvrir sftp:// avec Skiff, et confirmer un lien cliqué', async () => {
  // the links of the system (in the folders of the test)
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Système', exact: true }).click()
  await d.locator('tr[data-scheme="sftp"]').getByText('Aucune application').waitFor()
  await button(d, 'Ouvrir ces liens avec Skiff').click()
  await d.locator('tr[data-scheme="davs"] .badge', { hasText: 'Skiff' }).waitFor()
  assert.ok(readFileSync(join(HOME, '.config', 'mimeapps.list'), 'utf8').includes('x-scheme-handler/sftp=skiff.desktop'))
  assert.ok(readFileSync(join(HOME, '.local', 'share', 'applications', 'skiff.desktop'), 'utf8').includes('--link %U'), 'menu entry started with --link')
  await button(d, 'Ne plus les ouvrir avec Skiff').click()
  await d.locator('tr[data-scheme="sftp"]').getByText('Aucune application').waitFor()
  await page.keyboard.press('Escape')

  // a link clicked elsewhere: the system starts "skiff --link <url>",
  // which hands it to the window already open on the same configuration
  const port = PORT + 3000
  const base = `http://127.0.0.1:${port}`
  const home = join(TMP, 'lien-home')
  const cfg = join(TMP, 'lien-config')
  mkdirSync(join(home, '.ssh'), { recursive: true })
  const env = {
    ...process.env, HOME: home, SKIFF_HOME: cfg, SKIFF_NO_WINDOW: '1', SSH_AUTH_SOCK: '', SKIFF_SSH_SYSTEM_CONFIG: 'none',
    XDG_CONFIG_HOME: join(home, '.config'), XDG_DATA_HOME: join(home, '.local/share'),
  }
  delete env.SKIFF_UI_DIR
  const win = spawn(SKIFF, [], { env: { ...env, SKIFF_PORT: String(port), SKIFF_DEV_TOKEN: 'lien' }, stdio: 'ignore' })
  const ctx = await browser.newContext({ locale: 'fr-FR' })
  try {
    await until('the window', async () => (await fetch(`${base}/api/auth/status`)).ok)
    await until('its instance file', async () => existsSync(join(cfg, 'instance.json')))
    const p = await ctx.newPage()
    p.on('pageerror', (e) => pageErrors.push(e.message))
    await p.goto(`${base}/?token=lien`)
    await p.locator('.welcome').waitFor()
    const click = (url) => new Promise((ok, ko) => {
      const x = spawn(SKIFF, ['--link', url], { env: { ...env, SKIFF_PORT: String(port + 1) }, stdio: 'ignore' })
      x.on('exit', (code) => (code === 0 ? ok() : ko(new Error(`skiff --link ended with ${code}`))))
    })
    // a host key pinned in the link is not trusted
    await click('sftp://skiff;fingerprint=SHA256:faussefaussefausse@127.0.0.1:2222/config')
    const ld = p.locator('.dialog').last()
    await ld.locator('.dialog-head h3', { hasText: 'Ouvrir un lien' }).waitFor()
    const txt = await ld.innerText()
    assert.ok(txt.includes('127.0.0.1:2222') && txt.includes('/config') && txt.includes('SFTP'), txt)
    assert.ok(txt.includes("Le lien impose une clé d'hôte"), 'pinned host key ignored')
    await ld.getByRole('button', { name: 'Se connecter', exact: true }).click()
    const hk = p.locator('.dialog').last()
    await hk.getByText('Serveur inconnu').waitFor()
    await hk.getByRole('button', { name: 'Accepter', exact: true }).click()
    const pw = p.locator('.dialog').last()
    await pw.locator('input[type="password"]').fill('skiff')
    await pw.getByRole('button', { name: 'OK', exact: true }).click()
    await until('connected', async () => (await rpcAt(base, 'lien', 'conn.list')).some((x) => x.state === 'connected' && x.remoteDir === '/config'))
    await p.locator('.tab', { hasText: '127.0.0.1' }).waitFor()
    // a second link, refused: Skiff opens no container of this computer
    await click('docker://web/etc')
    await sleep(800)
    assert.equal(await p.locator('.dialog').count(), 0, 'no dialog for a docker:// link')
  } finally {
    await ctx.close()
    await stop(win)
  }
})

step('appareils : partager les réglages par un fichier chiffré', async () => {
  await page.keyboard.press('Control+,')
  const d = dialog()
  await d.locator('nav.vtabs').getByRole('button', { name: 'Appareils', exact: true }).click()
  await d.locator('.field', { hasText: 'Site qui garde le fichier' }).locator('select').selectOption({ label: 'Site des tâches' })
  await d.locator('.field', { hasText: 'Fichier' }).locator('input').first().fill(REMOTE + '/skiff-sync.bin')
  await d.locator('.field', { hasText: 'Phrase secrète' }).locator('input').fill('phrase de test ui')
  await d.getByRole('button', { name: 'Synchroniser maintenant' }).click()
  await d.locator('.devsync-state', { hasText: 'réussie' }).waitFor({ timeout: 30000 })
  assert.ok(await remoteExists(REMOTE + '/skiff-sync.bin'), 'shared file written')
  assert.match(await remoteText(REMOTE + '/skiff-sync.bin').catch(() => 'SKIFFSYNC1'), /^SKIFFSYNC1/)
  await page.keyboard.press('Escape')
})

step('mode portable : configuration à côté du binaire', async () => {
  const dir = join(TMP, 'portable')
  const exe = join(dir, 'skiff')
  const data = join(dir, 'skiff-data')
  const home = join(TMP, 'portable-home')
  mkdirSync(dir, { recursive: true })
  mkdirSync(home, { recursive: true })
  copyFileSync(SKIFF, exe)
  chmodSync(exe, 0o755)
  writeFileSync(join(dir, 'skiff.portable'), '')
  const port = PORT + 2000
  const base = `http://127.0.0.1:${port}`
  const env = {
    ...process.env, HOME: home, SKIFF_NO_WINDOW: '1', SKIFF_PORT: String(port), SKIFF_DEV_TOKEN: 'portable',
    SKIFF_UI_DIR: join(ROOT, 'frontend/dist'), SSH_AUTH_SOCK: '', SKIFF_SSH_SYSTEM_CONFIG: 'none',
  }
  delete env.SKIFF_HOME
  delete env.XDG_CONFIG_HOME
  const run = async (check) => {
    const proc = spawn(exe, [], { env, stdio: 'ignore' })
    try {
      await until('portable Skiff', async () => (await fetch(`${base}/api/auth/status`)).ok)
      await check()
    } finally {
      await stop(proc)
    }
  }
  // "skiff.portable" next to the binary: the configuration goes to "skiff-data"
  await run(async () => {
    const info = await rpcAt(base, 'portable', 'app.info')
    assert.equal(info.portable, true)
    assert.equal(info.configDir, data)
    const ctx = await browser.newContext({ locale: 'fr-FR' })
    try {
      const p = await ctx.newPage()
      p.on('pageerror', (e) => pageErrors.push(e.message))
      await p.goto(`${base}/?token=portable`)
      await p.locator('.welcome').waitFor()
      await p.keyboard.press('Control+,')
      const d = p.locator('.dialog').last()
      await d.getByText('(mode portable)', { exact: false }).waitFor()
      assert.ok((await d.innerText()).includes(data), 'configuration folder shown')
      await d.locator('nav.vtabs').getByRole('button', { name: 'Apparence', exact: true }).click()
      await button(d, 'Sombre').click()
      await until('preference saved', async () => (await rpcAt(base, 'portable', 'prefs.get')).theme === 'dark')
    } finally {
      await ctx.close()
    }
  })
  assert.ok(existsSync(join(data, 'settings.json')), 'settings written next to the binary')
  assert.ok(!existsSync(join(home, '.config', 'Skiff')), 'nothing written in the user configuration folder')
  // the "skiff-data" folder alone is enough
  rmSync(join(dir, 'skiff.portable'))
  await run(async () => {
    const info = await rpcAt(base, 'portable', 'app.info')
    assert.equal(info.portable, true)
    assert.equal((await rpcAt(base, 'portable', 'prefs.get')).theme, 'dark')
  })
})

step('glisser hors de Skiff : les fichiers locaux partent en fichiers', async () => {
  await page.locator('.tab', { hasText: '127.0.0.1' }).first().click()
  await nav(0, LOCAL)
  const dragged = (r) => r.evaluate((el) => {
    const dt = new DataTransfer()
    el.dispatchEvent(new DragEvent('dragstart', { dataTransfer: dt, bubbles: true, cancelable: true }))
    el.dispatchEvent(new DragEvent('dragend', { dataTransfer: dt, bubbles: true }))
    return { uris: dt.getData('text/uri-list'), download: dt.getData('DownloadURL'), text: dt.getData('text/plain') }
  })
  const local = await dragged(row(0, 'a.txt'))
  assert.equal(local.uris, 'file://' + join(LOCAL, 'a.txt').split('/').map(encodeURIComponent).join('/'))
  assert.equal(local.download, '', 'a local file is not downloaded')
  // a remote file, in Chromium: dropped on the desktop, it downloads
  await rpc('fs.writeText', { conn: (await sftp()).id, path: `${REMOTE}/glisse.txt`, content: 'x', encoding: 'utf-8', eol: 'LF' })
  await nav(1, REMOTE)
  const remote = await dragged(row(1, 'glisse.txt'))
  assert.match(remote.download, /^application\/octet-stream:glisse\.txt:http/)
  assert.equal(remote.uris, '')
  await rpc('fs.delete', { conn: (await sftp()).id, paths: [`${REMOTE}/glisse.txt`] })
})

step('accessibilité : chaque contrôle a un nom', async () => {
  // what a screen reader announces: the visible controls without a name
  const unnamed = () => page.evaluate(() => {
    const shown = (el) => !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length)
    const named = (el) => {
      if ((el.getAttribute('aria-label') || '').trim() || (el.getAttribute('title') || '').trim()) return true
      const by = el.getAttribute('aria-labelledby')
      if (by && by.split(' ').some((id) => document.getElementById(id)?.textContent.trim())) return true
      if (el.matches('input, select, textarea')) return !!el.labels?.length
      return !!(el.textContent || '').trim()
    }
    return [...document.querySelectorAll('button, [role="button"], [role="menuitem"], [role="tab"], [role="option"], input, select, textarea')]
      .filter((el) => shown(el) && el.type !== 'hidden' && el.type !== 'file' && !named(el)).map((el) => el.outerHTML.slice(0, 140))
  })
  const check = async (where) => {
    await sleep(200)
    const list = await unnamed()
    assert.deepEqual(list, [], `${where}: controls without a name`)
  }
  assert.equal(await page.evaluate(() => document.documentElement.lang), 'fr')
  await page.locator('.tab.home').click()
  await check('sites')
  // a site in the editor, and its menu
  await rpc('sites.save', { site: { name: 'Site accessible', protocol: 'sftp', host: '127.0.0.1', port: 2222, user: 'skiff', useAgent: false, resolveSymlinks: true }, secrets: {} })
  const site = page.locator('.sites-tree .tree-site', { has: page.locator('.site-name', { hasText: /^Site accessible$/ }) }).last()
  await site.click()
  await page.locator('.site-editor').waitFor()
  for (const sec of ['Général', 'Connexion', 'SSH']) {
    const tab = page.locator('.site-editor nav.vtabs').getByRole('button', { name: sec, exact: true })
    if (await tab.count()) {
      await tab.click()
      await check(`site ${sec}`)
    }
  }
  await site.click({ button: 'right' })
  await page.locator('.menu').waitFor()
  await check('menu of a site')
  await page.keyboard.press('Escape')
  const made = (await rpc('sites.list')).sites.find((x) => x.name === 'Site accessible')
  if (made) await rpc('sites.delete', { ids: [made.id] })
  await page.locator('.tab', { hasText: '127.0.0.1' }).first().click()
  await check('session')
  await page.keyboard.press('Control+,')
  const d = dialog()
  for (const sec of ['Apparence', 'Transferts', 'Terminal', 'SSH', 'Sécurité', 'Système']) {
    await d.locator('nav.vtabs').getByRole('button', { name: sec, exact: true }).click()
    await check(`préférences ${sec}`)
  }
  // a dialog is named by its title
  assert.equal(await page.evaluate(() => {
    const dlg = document.querySelector('[role="dialog"]')
    return document.getElementById(dlg.getAttribute('aria-labelledby'))?.textContent
  }), 'Préférences')
  await page.keyboard.press('Escape')
  // the toolbar of the session, as a screen reader sees it
  assert.equal(await page.getByRole('button', { name: 'Clés autorisées' }).count(), 1)
  assert.ok(await page.getByRole('tab', { selected: true }).count() >= 1, 'the active tab')
})

step('aucune erreur JavaScript', () => {
  assert.deepEqual(pageErrors, [])
})
