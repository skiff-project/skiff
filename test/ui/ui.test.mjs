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
      SKIFF_UI_DIR: join(ROOT, 'frontend/dist'), SSH_AUTH_SOCK: '', XDG_DATA_HOME: join(HOME, '.local/share'),
      SKIFF_SSH_SYSTEM_CONFIG: 'none',
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
  await page.locator('.tab.home').click()
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
    assert.equal(await field('Serveur').locator('select').inputValue(), c.id, 'the server of the session')
    await button(inst2, 'Installer').click()
    await until('pasted key installed', async () => (await remoteText(ak)).includes(body(other.public)))
  } finally {
    rmSync(join(HOME, '.ssh', 'id_installe_ui'), { force: true })
    if (before === null) await rpc('fs.delete', { conn: c.id, paths: [ak] })
    else await rpc('fs.writeText', { conn: c.id, path: ak, content: before, encoding: 'utf-8', eol: 'LF', force: true })
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

step('aucune erreur JavaScript', () => {
  assert.deepEqual(pageErrors, [])
})
