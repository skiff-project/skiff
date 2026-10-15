// Regenerates every screenshot of the documentation by driving the real
// Skiff UI with Playwright.
//
//   docker compose -f testenv/docker-compose.yml up -d
//   ./docs/capture/demo-setup.sh /tmp/skiff-demo
//   DEMO_DIR=/tmp/skiff-demo SKIFF_BIN=./bin/skiff node docs/capture/capture.mjs [scene-filter]
//
// Screenshots are written to docs/images/. Each scene is independent: a
// failing scene is reported and the run continues.
import { chromium } from 'playwright'
import { spawn, execSync } from 'node:child_process'
import { mkdirSync, rmSync, appendFileSync, writeFileSync, existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(HERE, '../..')
const OUT = join(ROOT, 'docs/images')
const DEMO = process.env.DEMO_DIR
if (!DEMO) throw new Error('set DEMO_DIR (see demo-setup.sh)')
const HOME = join(DEMO, 'home/alex')
const CFG = join(HOME, '.config', 'Skiff')
const SKIFF = resolve(process.env.SKIFF_BIN || join(ROOT, 'bin/skiff'))
const PORT = 8590
const TOKEN = 'doc'
const BASE = `http://127.0.0.1:${PORT}`
const FILTER = process.argv[2] ? new RegExp(process.argv[2]) : null
const SITE = join(HOME, 'Projets/site-vitrine')

mkdirSync(OUT, { recursive: true })
rmSync(CFG, { recursive: true, force: true })

// ---------------------------------------------------------------------------
// Skiff process

function startSkiff(extraArgs = [], env = {}) {
  const p = spawn(SKIFF, extraArgs, {
    env: { ...process.env, HOME, SKIFF_HOME: CFG, SKIFF_NO_WINDOW: '1', SKIFF_PORT: String(PORT), SKIFF_DEV_TOKEN: TOKEN, SSH_AUTH_SOCK: '', LANG: 'fr_FR.UTF-8', ...env },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  p.stdout.on('data', () => {})
  p.stderr.on('data', (d) => appendFileSync(join(DEMO, 'skiff-capture.log'), d))
  return p
}

async function waitHttp(url) {
  for (let i = 0; i < 100; i++) {
    try {
      await fetch(url)
      return
    } catch {
      await sleep(100)
    }
  }
  throw new Error('server did not start')
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function rpc(method, params = {}) {
  const r = await fetch(`${BASE}/api/rpc/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Skiff': '1', Cookie: `skiff_session=${TOKEN}` },
    body: JSON.stringify(params),
  })
  const b = await r.json()
  if (!r.ok) throw new Error(`${method}: ${b?.error?.message}`)
  return b.result
}

// ---------------------------------------------------------------------------
// Demo configuration

const ids = {}

async function seed() {
  // a personal key, as most users have one
  if (!existsSync(join(HOME, '.ssh/id_ed25519'))) {
    await rpc('keys.generate', { type: 'ed25519', comment: 'alex@portable', passphrase: '', format: 'openssh', saveTo: join(HOME, '.ssh/id_ed25519') })
  }
  const prefs = await rpc('prefs.get')
  await rpc('prefs.set', {
    ...prefs, language: 'fr', theme: 'light', accent: '#4f7cff', density: 'comfortable', showFKeyBar: true, restoreTabs: false,
    confirmTransfer: false, queue: { ...prefs.queue, autoPopup: true },
    bookmarks: {
      local: [
        { name: 'site-vitrine', path: SITE, side: 'local' },
        { name: 'Documents', path: join(HOME, 'Documents'), side: 'local' },
        { name: 'Téléchargements', path: join(HOME, 'Téléchargements'), side: 'local' },
      ],
    },
  })
  const base = { useAgent: false, kbdInteractive: true, resolveSymlinks: true, keepalive: 30, timeout: 15 }
  const save = async (site, password) => {
    const s = await rpc('sites.save', { site: { ...base, ...site }, secrets: password ? { password } : {} })
    return s.id
  }
  ids.vitrine = await save({ name: 'Site vitrine (SFTP)', folder: 'Production', protocol: 'sftp', host: '127.0.0.1', port: 2222, user: 'skiff', color: '#22c55e', remoteDir: '/config/www', localDir: '~/Projets/site-vitrine', recycleBin: '/config/.corbeille', notes: 'Serveur web de production.\nDéployer avec Ctrl+S (synchronisation).' }, 'skiff')
  ids.scp = await save({ name: 'Serveur SCP', folder: 'Production', protocol: 'scp', host: '127.0.0.1', port: 2222, user: 'skiff', color: '#14b8a6', remoteDir: '/config' })
  ids.ftp = await save({ name: 'FTP partenaire', folder: 'Partenaires', protocol: 'ftp', ftps: 'explicit', host: '127.0.0.1', port: 2121, user: 'skiff', color: '#f97316' }, 'skiff')
  ids.dav = await save({ name: 'Nuage WebDAV', folder: 'Stockage', protocol: 'webdav', host: '127.0.0.1', port: 8080, user: 'skiff', color: '#3b82f6', remoteDir: '/' }, 'skiff')
  ids.s3 = await save({ name: 'Objets S3', folder: 'Stockage', protocol: 's3', host: '127.0.0.1', port: 9000, user: 'skiffadmin', tls: false, s3PathStyle: true, s3Region: 'us-east-1', color: '#8b5cf6' }, 'skiffsecret')
  ids.bastion = await save({ name: 'Bastion', folder: 'Recette', protocol: 'sftp', host: 'bastion.example.com', user: 'alex', keyFile: '~/.ssh/id_ed25519', color: '#64748b' })
  ids.recette = await save({
    name: 'Recette', folder: 'Recette', protocol: 'sftp', host: 'recette.example.com', user: 'deploy', color: '#eab308', remoteDir: '/srv/recette',
    jumps: [{ siteId: ids.bastion }],
    forwards: [
      { id: 'pg', type: 'local', bindHost: '127.0.0.1', bindPort: 15432, destHost: 'db.internal', destPort: 5432, autoStart: true, label: 'PostgreSQL' },
      { id: 'socks', type: 'dynamic', bindHost: '127.0.0.1', bindPort: 1080, autoStart: false, label: 'Proxy SOCKS' },
    ],
    proxy: { type: 'socks5', host: 'proxy.example.com', port: 1080, user: 'alex', remoteDns: true },
  })
  ids.nas = await save({ name: 'NAS maison', protocol: 'sftp', host: '192.168.1.20', port: 2200, user: 'alex', color: '#ec4899' })
  await rpc('folders.add', { name: 'Clients' })
  const p2 = await rpc('prefs.get')
  await rpc('prefs.set', {
    ...p2,
    workspaces: [
      { name: 'Déploiement du site', tabs: [{ siteId: ids.vitrine, remote: '/config/www', local: '~/Projets/site-vitrine' }] },
      { name: 'Stockage', tabs: [{ siteId: ids.dav }, { siteId: ids.s3 }] },
    ],
  })
  // S3 demo content
  const c = await rpc('conn.open', { siteId: ids.s3 })
  for (const b of ['/medias', '/sauvegardes']) await rpc('fs.mkdir', { conn: c.id, path: b, mode: 0 }).catch(() => {})
  await rpc('fs.mkdir', { conn: c.id, path: '/medias/catalogue-2027', mode: 0 }).catch(() => {})
  await rpc('fs.createFile', { conn: c.id, path: '/medias/catalogue-2027/sommaire.txt', content: 'Catalogue 2027 — sommaire\n' }).catch(() => {})
  await rpc('fs.createFile', { conn: c.id, path: '/sauvegardes/base-2026-10-14.sql.gz', content: 'demo' }).catch(() => {})
  await rpc('conn.close', { conn: c.id })
}

// ---------------------------------------------------------------------------
// Browser helpers

let page
let browser
let context

const V = (sel) => page.locator(`.tab-content:visible ${sel}`)
const pane = (i) => V('.pane').nth(i)
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const row = (i, name) => pane(i).locator('.fl-row', { has: page.locator('.fl-name', { hasText: new RegExp(`^${esc(name)}$`) }) })
const tip = (t) => V(`[data-tip="${t}"]`)
const dialog = () => page.locator('.dialog').last()
const btnIn = (scope, name) => scope.getByRole('button', { name, exact: true })

async function settle(ms = 350) {
  await page.mouse.move(2, 2)
  await sleep(ms)
}

async function shot(name, opts = {}) {
  await settle(opts.wait ?? 350)
  await page.evaluate((show) => document.body.classList.toggle('doc-hide-toasts', !show), !!opts.toasts)
  const file = join(OUT, `${name}.png`)
  if (opts.el) {
    const box = await opts.el.boundingBox()
    if (!box) throw new Error(`${name}: element not visible`)
    const pad = opts.pad ?? 20
    const vp = page.viewportSize()
    const x = Math.max(0, box.x - pad)
    const y = Math.max(0, box.y - pad)
    await page.screenshot({ path: file, animations: 'disabled', clip: { x, y, width: Math.min(vp.width - x, box.width + pad * 2), height: Math.min(vp.height - y, box.height + pad * 2) } })
  } else {
    await page.screenshot({ path: file, animations: 'disabled' })
  }
  console.log('  ✓', name)
}

async function activeSide(i) {
  await pane(i).locator('.pane-status').click()
}

async function nav(i, path) {
  await activeSide(i)
  await page.keyboard.press('Control+l')
  const input = pane(i).locator('.path-input')
  await input.waitFor()
  await input.fill(path)
  await input.press('Enter')
  await waitList(i)
}

async function waitList(i) {
  await sleep(150)
  await pane(i).locator('.pathbar-progress').waitFor({ state: 'detached', timeout: 15000 }).catch(() => {})
  await pane(i).locator('.fl-skeleton').waitFor({ state: 'detached', timeout: 15000 }).catch(() => {})
}

async function select(i, ...names) {
  await row(i, names[0]).click()
  for (const n of names.slice(1)) await row(i, n).click({ modifiers: ['Control'] })
}

async function closeDialogs() {
  for (let k = 0; k < 4 && (await page.locator('.overlay').count()); k++) {
    await page.keyboard.press('Escape')
    await sleep(150)
  }
  if (await page.locator('.menu').count()) await page.keyboard.press('Escape')
}

async function openTab(title) {
  await page.locator('.tab', { has: page.locator('.tab-title', { hasText: title }) }).first().click()
  await sleep(300)
}

async function sitesTab() {
  await page.locator('.tab.home').click()
  await sleep(200)
}

async function site(name) {
  return page.locator('.sites-tree .tree-site', { has: page.locator('.site-name', { hasText: new RegExp(`^${esc(name)}$`) }) }).last()
}

async function connect(name) {
  await sitesTab()
  await (await site(name)).dblclick()
}

async function prompt(kind) {
  const d = page.locator('.dialog').last()
  await d.waitFor({ timeout: 20000 })
  return d
}

async function waitSession(title) {
  await page.locator('.tab.active .tab-title', { hasText: title }).waitFor({ timeout: 30000 })
  await V('.commander .pane').first().waitFor({ timeout: 30000 })
  await waitList(0)
  await waitList(1)
  await sleep(400)
}

// ---------------------------------------------------------------------------
// Scenes

const scenes = []
const scene = (name, fn) => scenes.push({ name, fn })

scene('accueil', async () => {
  await page.locator('.welcome').waitFor()
  await shot('accueil-premier-lancement')
})

scene('sites', async () => {
  await seed()
  await page.reload()
  await page.locator('.sites-tree .tree-site').first().waitFor()
  await shot('sites-liste')
  await (await site('Site vitrine (SFTP)')).click()
  await shot('site-general-sftp')
  const se = page.locator('.site-editor')
  for (const [label, file] of [['Dossiers', 'site-dossiers'], ['Environnement', 'site-environnement'], ['Connexion', 'site-connexion'], ['SSH', 'site-ssh'], ['Notes', 'site-notes']]) {
    await se.locator('nav.vtabs').getByRole('button', { name: label, exact: true }).click()
    await shot(file, { el: se, pad: 0 })
  }
  await (await site('Recette')).click()
  for (const [label, file] of [['Proxy', 'site-proxy'], ['Hôtes de rebond', 'site-rebonds'], ['Redirection de ports', 'site-redirections']]) {
    await se.locator('nav.vtabs').getByRole('button', { name: label, exact: true }).click()
    await shot(file, { el: se, pad: 0 })
  }
  await (await site('FTP partenaire')).click()
  await shot('site-general-ftps', { el: se, pad: 0 })
  for (const [label, file] of [['FTP', 'site-ftp'], ['TLS', 'site-tls']]) {
    await se.locator('nav.vtabs').getByRole('button', { name: label, exact: true }).click()
    await shot(file, { el: se, pad: 0 })
  }
  await (await site('Objets S3')).click()
  await shot('site-general-s3', { el: se, pad: 0 })
  await se.locator('nav.vtabs').getByRole('button', { name: 'S3', exact: true }).click()
  await shot('site-s3', { el: se, pad: 0 })
  await (await site('Nuage WebDAV')).click()
  await shot('site-general-webdav', { el: se, pad: 0 })
  // new site
  await page.locator('.sites-side-head [data-tip="Nouveau site"]').click()
  await shot('site-nouveau')
  await btnIn(page.locator('.se-head'), 'Annuler').click()
})

scene('sites-menus', async () => {
  await sitesTab()
  await (await site('Site vitrine (SFTP)')).click({ button: 'right' })
  await shot('sites-menu-contextuel')
  await page.keyboard.press('Escape')
  await page.locator('.tree-folder', { hasText: 'Production' }).click({ button: 'right' })
  await shot('sites-menu-dossier')
  await page.keyboard.press('Escape')
  await page.locator('.sites-side-head .icon-btn').nth(1).click()
  await shot('sites-menu-plus')
  await page.keyboard.press('Escape')
  await (await site('Site vitrine (SFTP)')).click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Générer URL' }).click()
  await shot('generer-url-code', { el: dialog() })
  await closeDialogs()
})

scene('import-export', async () => {
  await sitesTab()
  await page.locator('.sites-side-head .icon-btn').nth(1).click()
  await page.locator('.menu-item', { hasText: 'Importer' }).click()
  await dialog().locator('.source-item').first().waitFor()
  await shot('import-sources', { el: dialog() })
  await dialog().locator('.source-item', { hasText: 'OpenSSH' }).click()
  await btnIn(dialog(), 'Charger').click()
  await dialog().locator('table').waitFor()
  await shot('import-apercu', { el: dialog() })
  await closeDialogs()
  await page.locator('.sites-side-head .icon-btn').nth(1).click()
  await page.locator('.menu-item', { hasText: 'Exporter' }).click()
  await dialog().locator('pre').waitFor()
  await dialog().getByRole('button', { name: 'WinSCP.ini' }).click()
  await sleep(300)
  await shot('export', { el: dialog() })
  await closeDialogs()
})

scene('keygen', async () => {
  await sitesTab()
  await page.locator('.sites-side-head .icon-btn').nth(1).click()
  await page.locator('.menu-item', { hasText: 'paire de clés' }).click()
  await dialog().waitFor()
  await shot('cles-generateur', { el: dialog() })
  await dialog().locator('input[type=password]').fill('une phrase secrète solide')
  await btnIn(dialog(), 'Générer').click()
  await dialog().locator('.code-block').waitFor()
  await shot('cles-resultat', { el: dialog() })
  await closeDialogs()
})

scene('connexion-sftp', async () => {
  await connect('Site vitrine (SFTP)')
  const d = await prompt()
  await d.getByText('SHA-256').waitFor()
  await shot('connexion-cle-hote')
  await btnIn(d, 'Accepter').click()
  await waitSession('Site vitrine')
  await shot('commander', { wait: 600 })
})

scene('interface', async () => {
  await openTab('Site vitrine')
  await select(0, 'index.html', 'contact.html', 'README.md')
  await shot('selection-statut', { el: pane(0), pad: 0 })
  await page.keyboard.press('Escape')
  // compare directories
  await tip('Comparer les dossiers').click()
  await shot('comparer-dossiers')
  await tip('Comparer les dossiers').click()
  // connection chip menu
  await pane(1).locator('.conn-chip').click()
  await shot('panneau-choix-connexion')
  await page.keyboard.press('Escape')
  // bookmarks and history
  await pane(0).locator('[data-tip="Favoris"]').click()
  await shot('favoris')
  await page.keyboard.press('Escape')
  await pane(0).locator('[data-tip="Dossiers récents"]').click()
  await shot('historique')
  await page.keyboard.press('Escape')
  // filter
  await row(0, 'index.html').click()
  await page.keyboard.press('Control+f')
  await sleep(250)
  await page.keyboard.type('*.html')
  await shot('filtre', { el: pane(0), pad: 0 })
  await page.keyboard.press('Escape')
  // directory tree
  await tip('Arborescence des dossiers').click()
  await sleep(800)
  await shot('arborescence')
  await tip('Arborescence des dossiers').click()
  // explorer layout
  await tip('Disposition').click()
  await shot('disposition-explorateur')
  await tip('Disposition').click()
  await sleep(300)
})

scene('apercu', async () => {
  await openTab('Site vitrine')
  await nav(0, join(SITE, 'images'))
  await row(0, 'banniere.png').click()
  await tip("Panneau d'aperçu").click()
  await sleep(800)
  await shot('apercu-image')
  await nav(0, SITE)
  await row(0, 'README.md').click()
  await sleep(800)
  await shot('apercu-markdown')
  await tip("Panneau d'aperçu").click()
})

scene('menus-fichiers', async () => {
  await openTab('Site vitrine')
  await row(1, 'index.html').click({ button: 'right' })
  await shot('menu-fichier')
  await page.locator('.menu-item', { hasText: 'Commandes personnalisées' }).hover()
  await sleep(300)
  await shot('menu-commandes-personnalisees')
  await page.keyboard.press('Escape')
  const box = await pane(1).locator('.filelist').boundingBox()
  await page.mouse.click(box.x + box.width / 2, box.y + box.height - 40, { button: 'right' })
  await shot('menu-arriere-plan')
  await page.keyboard.press('Escape')
})

scene('operations', async () => {
  await openTab('Site vitrine')
  await row(1, 'a-propos.html').click()
  await page.keyboard.press('F2')
  await sleep(200)
  await shot('renommer', { el: pane(1), pad: 0 })
  await page.keyboard.press('Escape')
  await row(1, 'a-propos.html').click()
  await page.keyboard.press('F7')
  await dialog().locator('input').fill('archives')
  await shot('nouveau-dossier', { el: dialog() })
  await closeDialogs()
  await row(1, 'promo-ete.html').click()
  await page.keyboard.press('F8')
  await shot('supprimer', { el: dialog() })
  await closeDialogs()
  await row(1, 'index.html').click()
  await page.keyboard.press('F9')
  await dialog().getByText('Octal').waitFor()
  await shot('proprietes-fichier', { el: dialog() })
  await btnIn(dialog(), 'Calculer').click()
  await dialog().locator('.code-block').waitFor()
  await shot('proprietes-somme', { el: dialog() })
  await closeDialogs()
  await row(1, 'css').click()
  await page.keyboard.press('F9')
  await btnIn(dialog(), 'Calculer').click()
  await sleep(800)
  await dialog().getByText('Appliquer récursivement').click()
  await shot('proprietes-dossier', { el: dialog() })
  await closeDialogs()
  await row(1, 'index.html').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Somme de contrôle' }).click()
  await dialog().locator('.code-block').waitFor()
  await shot('somme-de-controle', { el: dialog() })
  await closeDialogs()
  const box = await pane(1).locator('.filelist').boundingBox()
  await row(1, 'index.html').click()
  await page.mouse.click(box.x + box.width / 2, box.y + box.height - 40, { button: 'right' })
  await page.locator('.menu-item', { hasText: 'Nouveau lien' }).click()
  await shot('nouveau-lien', { el: dialog() })
  await closeDialogs()
  await row(1, 'index.html').click()
  await page.keyboard.press('Control+Shift+X')
  await dialog().locator('input').fill('df -h / && uptime')
  await page.keyboard.press('Enter')
  await page.locator('.dialog pre').waitFor()
  await shot('commande-resultat', { el: dialog() })
  await closeDialogs()
})

scene('transferts', async () => {
  await openTab('Site vitrine')
  await row(0, 'index.html').click()
  await page.keyboard.press('Control+a')
  await page.keyboard.press('Shift+F5')
  await dialog().locator('.disclosure').click()
  await shot('transfert-options', { el: dialog() })
  await closeDialogs()
  // a slow transfer to show the queue
  await nav(1, '/config/releases')
  await nav(0, join(HOME, 'Téléchargements'))
  await row(0, 'sauvegarde-photos.tar.gz').click()
  await page.keyboard.press('Shift+F5')
  await dialog().locator('.disclosure').click()
  await dialog().locator('input[type=number]').nth(0).fill('3000')
  await btnIn(dialog(), 'OK').click()
  await page.locator('.job').first().waitFor()
  await sleep(2500)
  await page.locator('.job .job-main .icon-btn').first().click()
  await shot('file-transferts', { wait: 500 })
  await page.locator('.job.done').first().waitFor({ timeout: 60000 })
  // overwrite prompt
  await row(0, 'sauvegarde-photos.tar.gz').click()
  await page.keyboard.press('F5')
  const d = await prompt()
  await d.getByText('Source').waitFor()
  await shot('ecraser')
  await btnIn(d, 'Ignorer').click()
  await sleep(800)
  // error prompt: upload into a read-only folder
  await nav(1, '/config/sshd')
  await row(0, 'catalogue-2027.zip').click()
  await page.keyboard.press('F5')
  const e = await prompt()
  await e.getByText('Ignorer').first().waitFor()
  await shot('erreur-transfert')
  await btnIn(e, 'Ignorer').click()
  await sleep(1200)
  await page.locator('.job.partial, .job.failed').first().locator('.job-main .icon-btn').first().click().catch(() => {})
  await shot('file-transferts-erreurs', { el: page.locator('.bottom') , pad: 0 })
  await call('transfer.clear')
  await nav(1, '/config/www')
  await nav(0, SITE)
  await page.locator('.bp-tabs .icon-btn').click()
})

async function call(m, p) {
  return rpc(m, p)
}

scene('synchronisation', async () => {
  await openTab('Site vitrine')
  await row(0, 'index.html').click()
  await page.keyboard.press('Control+s')
  await dialog().getByText('Sens', { exact: true }).waitFor()
  await shot('sync-dialogue', { el: dialog() })
  await btnIn(dialog(), 'Comparer').click()
  await page.locator('.sync-table').waitFor({ timeout: 30000 })
  await shot('sync-apercu', { el: dialog() })
  await closeDialogs()
})

scene('garder-a-jour', async () => {
  await openTab('Site vitrine')
  await row(0, 'index.html').click()
  await page.keyboard.press('Control+u')
  await dialog().waitFor()
  await shot('garder-a-jour', { el: dialog() })
  await btnIn(dialog(), 'Démarrer').click()
  await sleep(1200)
  appendFileSync(join(SITE, 'css/style.css'), '\n.footer { padding: 32px; }\n')
  writeFileSync(join(SITE, 'mentions-legales.html'), '<!doctype html><title>Mentions légales</title>\n')
  await sleep(3500)
  await shot('surveillance-panneau')
  const ws = await rpc('watch.list')
  for (const w of ws) await rpc('watch.stop', { id: w.id })
  await page.locator('.bp-tabs .icon-btn').click().catch(() => {})
})

scene('editeur', async () => {
  await openTab('Site vitrine')
  await nav(1, '/config/www')
  await row(1, 'index.html').dblclick()
  await V('.cm-editor').waitFor()
  await sleep(600)
  await V('.cm-content').click()
  await page.keyboard.press('Control+End')
  await page.keyboard.type('\n<!-- modifié avec Skiff -->')
  await shot('editeur')
  // conflict: the file changes on the server meanwhile
  execSync(`docker exec testenv-ssh-1 sh -c 'echo "<!-- changement serveur -->" >> /config/www/index.html'`)
  await sleep(1100)
  await page.keyboard.press('Control+s')
  await V('.lost-bar.warn').waitFor()
  await shot('editeur-conflit')
  await V('.lost-bar.warn').getByRole('button', { name: 'Recharger' }).click()
  await sleep(500)
  await V('.cm-content').click()
  await page.keyboard.press('Control+f')
  await V('.cm-search').waitFor()
  await page.keyboard.type('Atelier')
  await sleep(300)
  await shot('editeur-recherche')
  await page.keyboard.press('Escape')
  await page.locator('.tab.active .tab-close').click()
})

scene('diff', async () => {
  await openTab('Site vitrine')
  await row(0, 'index.html').click()
  await row(1, 'index.html').click()
  await row(0, 'index.html').click({ button: 'right' })
  await page.locator('.menu-item', { hasText: 'Comparer avec le fichier' }).click()
  await V('.cm-mergeView').waitFor()
  await sleep(600)
  await shot('diff')
  await page.locator('.tab.active .tab-close').click()
})

scene('terminal', async () => {
  await openTab('Site vitrine')
  await row(1, 'index.html').click()
  await tip('Ouvrir un terminal').click()
  await V('.xterm').waitFor()
  await sleep(1500)
  await page.keyboard.type('ls -la --color=always && df -h /config | tail -1\n')
  await sleep(1200)
  await shot('terminal-ssh')
  await page.locator('.tab.active .tab-close').click()
})

scene('tunnels', async () => {
  await openTab('Site vitrine')
  await tip('Tunnels').click()
  await dialog().getByRole('button', { name: 'PostgreSQL' }).click()
  await shot('tunnel-dialogue', { el: dialog() })
  await btnIn(dialog(), 'Démarrer').click()
  await page.locator('.tunnelsp').waitFor()
  await sleep(1200)
  await shot('tunnels-panneau')
  for (const tu of await rpc('tunnel.list')) await rpc('tunnel.stop', { id: tu.id })
  await page.locator('.bp-tabs .icon-btn').click().catch(() => {})
})

scene('recherche', async () => {
  await openTab('Site vitrine')
  await nav(1, '/config')
  await row(1, 'www').click()
  await page.keyboard.press('Control+Shift+F')
  await dialog().locator('input').first().fill('*.html; *.css')
  await shot('recherche-dialogue', { el: dialog() })
  await btnIn(dialog(), 'Rechercher').click()
  await page.locator('.findp table').waitFor()
  await sleep(1500)
  await shot('recherche-resultats')
  await nav(1, '/config/www')
})

scene('journal-infos', async () => {
  await openTab('Site vitrine')
  await tip('Journal de session').click()
  await sleep(800)
  await shot('journal')
  await page.locator('.bp-tabs .icon-btn').click()
  await tip('Informations serveur/protocole').click()
  await dialog().getByText('SSH').first().waitFor()
  await sleep(400)
  await shot('infos-serveur', { el: dialog() })
  await closeDialogs()
})

scene('palette', async () => {
  await openTab('Site vitrine')
  await row(1, 'index.html').click()
  await page.keyboard.press('Control+k')
  await page.locator('.palette').waitFor()
  await shot('palette')
  await page.keyboard.type('sync')
  await shot('palette-recherche')
  await page.keyboard.press('Escape')
})

scene('autres-protocoles', async () => {
  // SCP: password prompt (no saved password)
  await connect('Serveur SCP')
  const d = await prompt()
  await d.locator('input[type=password]').waitFor()
  await shot('connexion-mot-de-passe')
  await d.locator('input[type=password]').fill('skiff')
  await btnIn(d, 'OK').click()
  await waitSession('Serveur SCP')
  await shot('session-scp')
  // FTPS: untrusted certificate
  await connect('FTP partenaire')
  const c = await prompt()
  await c.getByText('SHA-256').waitFor()
  await shot('connexion-certificat')
  await btnIn(c, 'Faire confiance cette fois').click()
  await waitSession('FTP partenaire')
  await shot('session-ftps')
  await connect('Nuage WebDAV')
  await waitSession('Nuage WebDAV')
  await shot('session-webdav')
  await connect('Objets S3')
  await waitSession('Objets S3')
  await shot('session-s3')
  // server to server: show the SFTP connection in the left panel
  await pane(0).locator('.conn-chip').click()
  await page.locator('.menu-item', { hasText: 'Site vitrine' }).click()
  await waitList(0)
  await nav(0, '/config/backups')
  await shot('serveur-a-serveur')
})

scene('echec-connexion', async () => {
  await sitesTab()
  await page.keyboard.press('Control+k')
  await page.locator('.palette input').fill('sftp://alex@10.255.255.1')
  await sleep(200)
  await page.keyboard.press('Enter')
  await V('.state-card').waitFor()
  await shot('connexion-en-cours', { wait: 100 })
  await V('.state-icon.danger').waitFor({ timeout: 40000 })
  await shot('connexion-echec')
  await V('.state-card').getByRole('button', { name: 'Fermer' }).click()
})

scene('connexion-perdue', async () => {
  await openTab('Site vitrine')
  execSync('docker restart -t 1 testenv-ssh-1')
  await V('.lost-bar').waitFor({ timeout: 40000 })
  await shot('connexion-perdue')
  await sleep(9000)
  await V('.lost-bar').getByRole('button', { name: 'Reconnecter' }).click()
  await V('.lost-bar').waitFor({ state: 'detached', timeout: 40000 }).catch(() => {})
  await sleep(800)
})

scene('preferences', async () => {
  await openTab('Site vitrine')
  await page.keyboard.press('Control+,')
  await dialog().locator('.prefs').waitFor()
  const sections = [
    ['Apparence', 'prefs-apparence'], ['Panneaux', 'prefs-panneaux'], ['Transferts', 'prefs-transferts'], ["File d'attente", 'prefs-file'],
    ['Éditeur', 'prefs-editeur'], ['Terminal', 'prefs-terminal'], ['Confirmations', 'prefs-confirmations'], ['Sécurité', 'prefs-securite'],
    ['Commandes personnalisées', 'prefs-commandes'], ['Journalisation', 'prefs-journalisation'], ['Raccourcis clavier', 'prefs-raccourcis'], ['À propos de Skiff', 'prefs-a-propos'],
  ]
  for (const [label, file] of sections) {
    await dialog().locator('nav.vtabs').getByRole('button', { name: label, exact: true }).click()
    await sleep(300)
    await shot(file, { el: dialog() })
  }
  await dialog().locator('nav.vtabs button', { hasText: 'Sécurité' }).click()
  // key conversion (OpenSSH -> PuTTY)
  await dialog().getByRole('button', { name: 'Convertir', exact: true }).first().click()
  const k = page.locator('.dialog').last()
  await k.getByText('PuTTY .ppk (v3)').waitFor()
  await shot('cles-conversion', { el: k })
  await page.keyboard.press('Escape')
  await sleep(300)
  await btnIn(dialog(), 'Définir un mot de passe maître').click()
  const m = page.locator('.dialog').last()
  await m.locator('input[type=password]').nth(0).fill('Mon-Mot-De-Passe-2026!')
  await m.locator('input[type=password]').nth(1).fill('Mon-Mot-De-Passe-2026!')
  await shot('mot-de-passe-maitre', { el: m })
  await closeDialogs()
})

scene('editions-externes', async () => {
  await openTab('Site vitrine')
  await rpc('edit.external', { conn: (await rpc('conn.list')).find((c) => c.title.includes('vitrine')).id, path: '/config/www/contact.html', editor: 'true' })
  await sleep(500)
  await page.keyboard.press('Control+Shift+Q')
  await page.locator('.bp-tab', { hasText: 'Fichiers édités' }).click()
  await sleep(400)
  await shot('editions-externes', { el: page.locator('.bottom'), pad: 0 })
  for (const e of await rpc('edit.list')) await rpc('edit.stop', { id: e.id })
  await page.locator('.bp-tabs .icon-btn').click().catch(() => {})
})

scene('themes', async () => {
  await openTab('Site vitrine')
  await nav(0, SITE)
  await nav(1, '/config/www')
  await rpc('prefs.set', { ...(await rpc('prefs.get')), theme: 'dark' })
  await sleep(800)
  await shot('commander-sombre')
  await page.keyboard.press('Control+k')
  await page.locator('.palette').waitFor()
  await shot('palette-sombre')
  await page.keyboard.press('Escape')
  await rpc('prefs.set', { ...(await rpc('prefs.get')), theme: 'dark', accent: '#14b8a6', density: 'compact' })
  await sleep(800)
  await shot('commander-compact-accent')
  await rpc('prefs.set', { ...(await rpc('prefs.get')), theme: 'light', accent: '#4f7cff', density: 'comfortable', language: 'en' })
  await sleep(800)
  await shot('commander-anglais')
  await rpc('prefs.set', { ...(await rpc('prefs.get')), language: 'fr' })
  await sleep(500)
})

scene('accueil-recents', async () => {
  await page.reload()
  await page.locator('.welcome .recent-cards').waitFor({ timeout: 15000 })
  await page.locator('.sites-tree .tree-site').first().waitFor()
  await shot('accueil-recents')
  await shot('espaces-de-travail', { el: page.locator('.sites-side'), pad: 0 })
})

scene('verrouillage', async () => {
  await rpc('security.setMaster', { old: '', new: 'Mon-Mot-De-Passe-2026!' })
  await rpc('security.lock')
  await page.reload()
  await page.locator('.locked-banner').waitFor()
  await shot('mots-de-passe-verrouilles', { el: page.locator('.sites-side'), pad: 0 })
  await page.locator('.locked-banner').click()
  await dialog().locator('input[type=password]').fill('Mon-Mot-De-Passe-2026!')
  await shot('deverrouiller', { el: dialog() })
  await btnIn(dialog(), 'Déverrouiller').click()
  await sleep(500)
  // security preferences with a master password
  await page.keyboard.press('Control+,')
  await dialog().locator('nav.vtabs').getByRole('button', { name: 'Sécurité', exact: true }).click()
  await dialog().getByText('Verrouiller les mots de passe maintenant').waitFor()
  await shot('prefs-securite-maitre', { el: dialog() })
  await closeDialogs()
  await rpc('security.setMaster', { old: 'Mon-Mot-De-Passe-2026!', new: '' })
})

// ---------------------------------------------------------------------------

async function main() {
  console.log('starting', SKIFF)
  const proc = startSkiff()
  try {
    await waitHttp(`${BASE}/api/auth/status`)
    browser = await chromium.launch()
    context = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5, locale: 'fr-FR', colorScheme: 'light' })
    page = await context.newPage()
    page.setDefaultTimeout(10000)
    page.on('pageerror', (e) => console.log('  page error:', e.message))
    await context.addInitScript(() => {
      document.addEventListener('DOMContentLoaded', () => {
        const st = document.createElement('style')
        st.textContent = '.tooltip{display:none!important} .doc-hide-toasts .toasts{display:none!important} input,textarea{caret-color:transparent}'
        document.head.appendChild(st)
      })
    })
    await page.goto(`${BASE}/?token=${TOKEN}`)
    const failures = []
    for (const s of scenes) {
      if (FILTER && !FILTER.test(s.name) && s.name !== 'sites') continue
      console.log('scene', s.name)
      try {
        await s.fn()
      } catch (e) {
        failures.push(s.name)
        console.log('  ✗', s.name, e.message.split('\n')[0])
        await page.screenshot({ path: join(DEMO, `fail-${s.name}.png`) }).catch(() => {})
        await closeDialogs().catch(() => {})
      }
    }
    // web mode login page (separate instance)
    if (!FILTER || FILTER.test('web')) await webLogin()
    console.log(failures.length ? `failed scenes: ${failures.join(', ')}` : 'all scenes captured')
  } finally {
    await browser?.close()
    proc.kill()
  }
}

async function webLogin() {
  const cfg = join(DEMO, 'skiff-web')
  rmSync(cfg, { recursive: true, force: true })
  const p = spawn(SKIFF, ['serve', '--no-tls', '--listen', '127.0.0.1:8591', '--user', 'alex', '--password-env', 'WEBPW'], {
    env: { ...process.env, HOME, SKIFF_HOME: cfg, WEBPW: 'demo-password-123' }, stdio: 'ignore',
  })
  try {
    await waitHttp('http://127.0.0.1:8591/api/auth/status')
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5, locale: 'fr-FR' })
    const pg = await ctx.newPage()
    await pg.goto('http://127.0.0.1:8591/')
    await pg.locator('.login-card').waitFor()
    await pg.locator('.login-card input').first().fill('alex')
    await sleep(300)
    await pg.screenshot({ path: join(OUT, 'mode-web-connexion.png'), animations: 'disabled' })
    console.log('  ✓ mode-web-connexion')
    // signed in: the same interface, with a sign-out button
    await pg.locator('.login-card input[type=password]').fill('demo-password-123')
    await pg.locator('.login-card button').click()
    await pg.locator('.welcome').waitFor({ timeout: 15000 })
    await sleep(600)
    await pg.screenshot({ path: join(OUT, 'mode-web-session.png'), animations: 'disabled' })
    console.log('  ✓ mode-web-session')
    await ctx.close()
  } finally {
    p.kill()
  }
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})

void existsSync
