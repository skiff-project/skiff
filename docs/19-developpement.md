# 19. Développement

[← Correspondance WinSCP → Skiff](18-winscp.md) · [Sommaire](README.md)

## 19.1 Architecture

```text
skiff/
├── main.go, cli_*.go         CLI : fenêtre, serve, run, shell, keygen, import, compatibilité winscp.com
├── internal/
│   ├── vfs/                  système de fichiers unifié et ses implémentations
│   │   ├── localfs  sftpfs  scpfs  ftpfs (client FTP/FTPS)  davfs (client WebDAV)  s3fs
│   ├── sshx/                 SSH : authentification, PPK, agent, known_hosts, rebonds, keepalive
│   ├── sshconfig/            lecture de la configuration OpenSSH (Host, Match, Include, jetons) selon l'OS
│   ├── netx/                 proxys (SOCKS4/5, HTTP, Telnet, commande), TLS interactif
│   ├── transfer/             moteur de transferts (file, workers, reprise, écrasement, mode texte…)
│   ├── core/                 connexions, opérations, synchronisation, surveillance, terminaux, tunnels, éditeurs
│   ├── config/               sites, préférences, secrets chiffrés, mot de passe maître, mode portable
│   ├── importer/             import WinSCP, PuTTY, FileZilla, OpenSSH, JSON, URL
│   ├── keys/                 génération, conversion et installation de clés
│   ├── mask/                 masques de fichiers WinSCP
│   ├── script/               langage de script compatible WinSCP
│   ├── prompt/               questions posées à l'utilisateur (clés d'hôte, écrasement…)
│   ├── server/               API JSON-RPC, WebSocket (événements, terminaux), flux de fichiers, authentification
│   └── desktop/              fenêtre Wails (WebView2, WKWebView, WebKitGTK) ou repli navigateur
├── frontend/                 React 19 + TypeScript + Vite (zustand, TanStack Virtual, CodeMirror 6, xterm.js)
├── testenv/                  serveurs Docker de test (OpenSSH, Pure-FTPd + TLS, WebDAV, S3)
└── docs/                     cette documentation, ses captures et l'outil qui les produit
```

L'interface communique avec le cœur Go par HTTP et WebSocket :

- `POST /api/rpc/<méthode>` avec un corps JSON (par exemple `fs.list`, `transfer.start`, `sync.compare`), l'en-tête `X-Skiff: 1` et le cookie de session ;
- `/api/events` (WebSocket) pour les évènements : progression des transferts, journal, questions, état des connexions ;
- `/api/term/<id>` (WebSocket) pour les terminaux ;
- `/api/download` et `/api/upload` pour lire et envoyer des fichiers en flux.

C'est la même interface, et la même API, dans la fenêtre native et en mode web.

## 19.2 Compiler et tester

```bash
./build.sh            # interface + binaire (bin/skiff), fenêtre native si WebKitGTK est présent
./build.sh portable   # binaire sans fenêtre native
./build.sh release    # binaires de toutes les plateformes dans dist/
./build.sh ui         # interface seule (frontend/dist)
./build.sh dev        # recompile l'interface à la volée ; Skiff sur http://127.0.0.1:8484/?token=dev
./build.sh test       # go vet, tests unitaires, vérification TypeScript et des traductions
./build.sh itest      # tests de bout en bout de l'API, du mode web, de la ligne de commande et des scripts
./build.sh uitest     # tests de bout en bout de l'interface dans un navigateur
```

Le `Makefile` propose les mêmes cibles.

## 19.3 Les tests

Les tests de bout en bout s'exécutent contre les serveurs Docker de `testenv/` (lancés automatiquement par `itest` et `uitest`).

| Suite | Commande | Ce qui est vérifié |
|---|---|---|
| Tests unitaires | `go test ./...` | Masques, scripts (syntaxe, variables), clés PPK, configuration OpenSSH (guillemets, `Host`/`Match`, `Include`, jetons, chemins par OS, algorithmes) et sa priorité face aux réglages des sites, import WinSCP/OpenSSH/FileZilla, secrets et mot de passe maître, dossiers de sites, moteur de transferts, analyse des listages FTP et SCP, prison du mode web, découpage des commandes d'éditeur |
| Traductions | `node frontend/scripts/check-i18n.mjs` | Chaque clé utilisée par l'interface existe en français et en anglais, et les deux langues ont les mêmes clés |
| API de bout en bout | `go test -tags integration ./internal/server/` | `TestE2E` démarre un vrai serveur Skiff et appelle **chacune des méthodes de l'API** comme le fait l'interface, en vérifiant leur effet sur les serveurs : sites, import et export, les cinq protocoles et le disque local, transferts et toutes leurs options, file d'attente, transferts de serveur à serveur, synchronisation (sens, modes, critères), surveillance, recherche, éditeur externe, terminaux, tunnels `-L`/`-R`/`-D`, hôtes de rebond (y compris FTP, WebDAV et S3 derrière SSH), proxys SOCKS4/5, HTTP, Telnet et commande, clés et conversion, clés d'hôte, configuration OpenSSH (alias, `IdentityFile` seul, `StrictHostKeyChecking`, `HostKeyAlias`, `ProxyJump`, `ProxyCommand`, `LocalForward` ouvert à la connexion, fichier système, désactivation), certificats TLS, mot de passe maître, coupure et reconnexion. Le test échoue si une méthode de l'API n'a pas été exercée. `TestE2EWeb` vérifie le mode web (authentification, limitation des tentatives, CSRF, origine, prison `--root`, fonctions désactivées). |
| Ligne de commande | `go test -tags integration .` | Compile le binaire et exécute toutes les commandes de script, la compatibilité `winscp.com` (`/script`, `/command`, `/parameter`, `/log`, `/ini`), les modes `batch`, `keepuptodate`, la console (dans un pseudo-terminal), `keygen`, `import`, `serve`, l'ouverture d'un hôte de `~/.ssh/config` par son nom et d'une URL au démarrage |
| Protocoles | `go test -tags integration ./internal/core/` | Opérations de base sur chaque protocole |
| Interface | `cd test/ui && pnpm test` | Un vrai navigateur pilote l'interface : connexion et clé d'hôte, toutes les opérations sur les fichiers, éditeur et conflits, presse-papiers et glisser-déposer, écrasement, synchronisation, surveillance, recherche, terminal, tunnels, palette, préférences, sites, hôtes de la configuration OpenSSH (paramètres détectés, connexion, éditeur, préférences SSH), import et export, comparaison, favoris, filtre et sélection, arborescence, aperçu, disposition, diff, sommes de contrôle, liens, commandes, propriétés, file de transferts, informations serveur, journal, éditeur externe, onglets, URL et espaces de travail, clés, mot de passe maître, coupure et reconnexion, mode web. Chaque action est vérifiée par son effet sur le serveur, et aucune erreur JavaScript ne doit survenir. En cas d'échec, une capture est enregistrée (`UI_SHOTS=dossier`). |

Le serveur SSH de test autorise la redirection de ports (`testenv/ssh-init/50-tcp-forwarding`), nécessaire aux tunnels et aux hôtes de rebond.

Serveurs de test (`testenv/docker-compose.yml`) : SSH sur `127.0.0.1:2222`, FTP sur `127.0.0.1:2121` (TLS explicite), WebDAV sur `http://127.0.0.1:8080`, S3 sur `http://127.0.0.1:9000` (`skiffadmin` / `skiffsecret`). Identifiants : `skiff` / `skiff`.

Variables utiles en développement :

| Variable | Effet |
|---|---|
| `SKIFF_NO_WINDOW=1` | N'ouvre pas de fenêtre : seul le serveur local démarre |
| `SKIFF_PORT` | Port du serveur local |
| `SKIFF_DEV_TOKEN` | Jeton fixe (au lieu d'un jeton aléatoire) |
| `SKIFF_UI_DIR` | Sert l'interface depuis ce dossier au lieu de celle intégrée au binaire |
| `SKIFF_PRINT_URL=1` | Affiche l'URL de l'interface au démarrage |
| `SKIFF_HOME` | Dossier de configuration |
| `SKIFF_SSH_SYSTEM_CONFIG` | Remplace le fichier `ssh_config` du système (`none` : n'en lire aucun) ; les tests s'en servent pour ne pas dépendre de la machine |

## 19.4 Régénérer les captures d'écran

Toutes les images de `docs/images` sont produites automatiquement par `docs/capture`, qui pilote l'interface de Skiff avec Playwright sur des données de démonstration. Il suffit de relancer l'outil après une modification de l'interface.

Prérequis : Docker, Node 22 ou plus, pnpm, et un binaire Skiff compilé.

```bash
# 1. serveurs de test
docker compose -f testenv/docker-compose.yml up -d

# 2. outil de capture (une seule fois)
cd docs/capture && pnpm install && pnpm exec playwright install chromium && cd ../..

# 3. données de démonstration : un faux dossier personnel et le contenu des serveurs
./docs/capture/demo-setup.sh /tmp/skiff-demo

# 4. captures (toutes, ou seulement les scènes dont le nom correspond)
DEMO_DIR=/tmp/skiff-demo SKIFF_BIN=bin/skiff node docs/capture/capture.mjs
DEMO_DIR=/tmp/skiff-demo SKIFF_BIN=bin/skiff node docs/capture/capture.mjs '^(connexion-sftp|editeur)$'

# 5. réduire le poids des images (palette de 256 couleurs, nécessite Pillow)
python3 docs/capture/optimize-images.py
```

L'outil démarre Skiff sans fenêtre, avec un dossier personnel et une configuration de démonstration, crée les sites, puis rejoue une trentaine de scènes (sites, connexion, menus, transferts, synchronisation, éditeur, terminal, tunnels, préférences, thèmes, verrouillage, mode web…). Les captures sont prises en 1280 × 800 à l'échelle 1,5, en français et en thème clair. Une scène qui échoue laisse une capture `fail-<scène>.png` dans le dossier de démonstration ; les autres scènes continuent. La scène `sites`, qui crée les sites, est toujours jouée, et `connexion-sftp` doit précéder les scènes qui utilisent la session SFTP.

---

[← Correspondance WinSCP → Skiff](18-winscp.md) · [Sommaire](README.md)
