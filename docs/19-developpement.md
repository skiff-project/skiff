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
./build.sh test       # go vet, tests unitaires, vérification TypeScript
./build.sh itest      # lance les serveurs Docker et teste SFTP, SCP, FTP, FTPS, WebDAV et S3 de bout en bout
```

Le `Makefile` propose les mêmes cibles.

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

## 19.3 Régénérer les captures d'écran

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
