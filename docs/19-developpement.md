# 19. Développement

[← Correspondance WinSCP → Skiff](18-winscp.md) · [Sommaire](README.md) · 🇬🇧 [English](en/19-development.md)

## 19.1 Architecture

```text
skiff/
├── main.go, cli_*.go         CLI : fenêtre, serve, run, shell, keygen, import, compatibilité winscp.com
├── internal/
│   ├── vfs/                  système de fichiers unifié et ses implémentations
│   │   ├── localfs  sftpfs  scpfs  ftpfs (client FTP/FTPS)  davfs (client WebDAV)  s3fs
│   ├── sshx/                 SSH : authentification, PPK, agent, known_hosts, rebonds, keepalive
│   ├── sshconfig/            lecture de la configuration OpenSSH (Host, Match, Include, jetons) selon l'OS
│   ├── tasks/                tâches planifiées : stockage, planification (intervalle, jour, semaine, cron), exécution sans question
│   ├── devsync/              synchronisation des réglages entre appareils (fichier chiffré, fusion)
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
├── testenv/                  serveurs Docker de test (OpenSSH, OpenSSH ancien, Pure-FTPd + TLS, vsftpd TLS implicite, WebDAV, S3)
└── docs/                     cette documentation (en français, et en anglais dans docs/en), ses captures et l'outil qui les produit
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
./build.sh packages   # installateurs (deb, appimage, setup-windows, dmg ; voir le chapitre 1)
./build.sh ui         # interface seule (frontend/dist)
./build.sh dev        # recompile l'interface à la volée ; Skiff sur http://127.0.0.1:8484/?token=dev
./build.sh test       # go vet, tests unitaires, vérification TypeScript, des traductions et des liens de la documentation
./build.sh itest      # tests de bout en bout de l'API, du mode web, de la ligne de commande et des scripts
./build.sh uitest     # tests de bout en bout de l'interface dans un navigateur
```

Le `Makefile` propose les mêmes cibles. Plusieurs commandes peuvent se suivre (`./build.sh deb appimage`).

Les fichiers des installateurs sont dans `build/` :

| Fichier | Rôle |
|---|---|
| `build/appicon.png` | Icône source (1024 × 1024) |
| `build/icons.py` | Refait les icônes ci-dessous depuis `appicon.png` (nécessite Pillow) |
| `build/linux/skiff.desktop`, `build/linux/icons/` | Entrée du menu et icônes du paquet `.deb` et de l'AppImage ; un test vérifie que l'entrée est celle que Skiff écrit lui-même |
| `build/linux/deb.sh`, `build/linux/copyright` | Paquet Debian |
| `build/linux/appimage.sh`, `build/linux/AppRun` | AppImage ; `AppRun` choisit la fenêtre native ou la variante navigateur |
| `build/windows/installer.nsi`, `build/windows/icon.ico` | Installateur Windows (NSIS) |
| `build/darwin/bundle.sh`, `build/darwin/dmg.sh`, `build/darwin/skiff.icns` | `Skiff.app` et son image disque |

### Publier une version

Skiff suit le [versionnage sémantique](https://semver.org/lang/fr/) : un correctif augmente le troisième chiffre (1.0.1), une fonction nouvelle et compatible le deuxième (1.1.0), un changement incompatible le premier. Toute version 1.x lit la configuration des versions 1.x précédentes ; un changement du format de la configuration passe par une migration, précédée d'une sauvegarde ([§ 1.7](01-installation.md#17-mise-à-jour-et-désinstallation)).

1. Dans `CHANGELOG.md`, la section **[Unreleased]** devient `## [1.0.0] — 2026-11-02` (version et date), et une nouvelle section **[Unreleased]** vide la précède.
2. `python3 build/notices.py` si les dépendances ont changé, puis `./build.sh test`, `./build.sh itest` et `./build.sh uitest`.
3. Un tag annoté : `git tag -a v1.0.0 -m "Skiff 1.0.0"`, puis `git push origin v1.0.0`.

La CI compile alors les binaires et les installateurs des trois systèmes et publie la release, avec `SHA256SUMS`. La version affichée par Skiff (`skiff version`, **À propos**) vient du tag (`git describe`) ; sans tag, c'est `0.1.0~git.<commit>`.

Une modification du format de la configuration ajoute une migration dans `internal/config/format.go` (`FormatVersion` augmente de 1, `migrations[n]` passe du format n au format n+1) et un test qui part d'un dossier de l'ancien format.

### Licences

Skiff est sous licence Apache 2.0 (`LICENSE`, `NOTICE`) ; une contribution est faite sous la même licence. `THIRD_PARTY_NOTICES.txt` liste les bibliothèques compilées dans le programme, pour chaque système (`go list`) et pour l'interface (`pnpm licenses`), avec le texte de leur licence et leurs fichiers `NOTICE`. Le programme l'intègre (**À propos**, `skiff licenses`) et les installateurs le copient à côté de `LICENSE`.

```bash
python3 build/notices.py           # après un ajout ou une mise à jour de dépendance
python3 build/notices.py --check   # dans ./build.sh test : échoue si le fichier n'est plus à jour
```

Une nouvelle dépendance doit avoir une licence compatible avec Apache 2.0 (MIT, BSD, ISC, Apache 2.0, MPL 2.0…) ; une bibliothèque GPL ou LGPL liée statiquement ne l'est pas.

### Accessibilité

Un bouton qui ne montre qu'une icône porte une infobulle (`data-tip`) : `frontend/src/lib/a11y.ts` en fait son nom pour les lecteurs d'écran (`aria-label`), donne aux champs d'un `.field` le texte de leur étiquette et indique le choix coché des contrôles segmentés (`aria-pressed`). Le test d'interface « accessibilité » échoue dès qu'un contrôle visible des écrans principaux n'a pas de nom.

### Langues

Chaque texte de l'interface passe par `frontend/src/lib/i18n.ts`, en anglais et en français (textes d'exemple compris : `~/Projects` / `~/Projets`). `./build.sh test` vérifie que les deux langues ont les mêmes clés.

## 19.3 Les tests

Les tests de bout en bout s'exécutent contre les serveurs Docker de `testenv/` (lancés automatiquement par `itest` et `uitest`). Ils utilisent aussi le client OpenSSH de la machine (`ssh-agent`, `ssh-add`, `ssh-keygen`) pour l'agent et les certificats.

| Suite | Commande | Ce qui est vérifié |
|---|---|---|
| Tests unitaires | `go test ./...` | Masques, scripts (syntaxe, variables), clés PPK, configuration OpenSSH (guillemets, `Host`/`Match`, `Include`, jetons, chemins par OS, algorithmes) et sa priorité face aux réglages des sites, lecture de `/proc` par le moniteur, calcul des exécutions planifiées (cron compris), rattrapage, chiffrement et fusion de la synchronisation entre appareils, import WinSCP/OpenSSH/FileZilla, secrets et mot de passe maître, dossiers de sites, moteur de transferts, analyse des listages FTP et SCP, prison du mode web, découpage des commandes d'éditeur |
| Traductions | `node frontend/scripts/check-i18n.mjs` | Chaque clé utilisée par l'interface existe en français et en anglais, et les deux langues ont les mêmes clés |
| Documentation | `python3 docs/check-links.py` | Chaque lien, ancre et image des chapitres français et anglais mène quelque part, et chaque chapitre a son pendant dans l'autre langue |
| API de bout en bout | `go test -tags integration ./internal/server/` | `TestE2E` démarre un vrai serveur Skiff et appelle **chacune des méthodes de l'API** comme le fait l'interface, en vérifiant leur effet sur les serveurs : sites, import et export, les cinq protocoles et le disque local, transferts et toutes leurs options, file d'attente, transferts de serveur à serveur, synchronisation (sens, modes, critères), surveillance, recherche, éditeur externe, terminaux, tunnels `-L`/`-R`/`-D`, hôtes de rebond (y compris FTP, WebDAV et S3 derrière SSH), proxys SOCKS4/5, HTTP, Telnet et commande, clés et conversion, agent SSH réel (`ssh-agent` : connexion par les clés de l'agent seul, `IdentitiesOnly` qui filtre les clés de l'agent, `IdentityFile` désignant la clé publique ou une clé privée absente, `IdentityAgent` vers une autre socket, une variable ou `none`, phrase secrète non demandée quand l'agent détient la clé), certificats OpenSSH signés par `ssh-keygen` (`<clé>-cert.pub` à côté de la clé, `CertificateFile`, clé dans l'agent, certificat d'un autre utilisateur refusé), clés d'hôte, anciens algorithmes (refus sans l'option, connexion avec **Autoriser les anciens algorithmes** ou avec `KexAlgorithms`/`Ciphers`/`HostKeyAlgorithms` dans `ssh_config`), configuration OpenSSH (alias, `IdentityFile` seul, `StrictHostKeyChecking`, `HostKeyAlias`, `ProxyJump`, `ProxyCommand`, `LocalForward` ouvert à la connexion, fichier système, désactivation), certificats TLS, FTPS à TLS implicite (connexion par URL `ftps://`, toutes les opérations sur les fichiers, transferts dans les deux sens), mot de passe maître, coupure et reconnexion, transferts en parties parallèles (envoi, téléchargement, serveur à serveur, annulation), trousseau et transfert d'agent (`ssh-add -l` dans un terminal distant), clés autorisées (liste, clé de la session, retrait refusé sans confirmation ou depuis une liste périmée, remplacement vérifié par une seconde connexion, remplacement refusé sans la clé privée), liens du système (`mimeapps.list`, URL passée à la fenêtre ouverte, clé et en-tête `Origin` contrôlés, liens `docker://` refusés), moniteur et arrêt d'un processus, conteneurs (Docker réel, Docker par SSH, Kubernetes), tâches planifiées (synchronisation, script, échec, annulation, historique), synchronisation entre deux appareils (chiffrement, mauvaise phrase secrète, suppressions propagées). Le test échoue si une méthode de l'API n'a pas été exercée. `TestE2EWeb` vérifie le mode web (authentification, limitation des tentatives, CSRF, origine, prison `--root`, fonctions désactivées). |
| Ligne de commande | `go test -tags integration .` | Compile le binaire et exécute toutes les commandes de script, la compatibilité `winscp.com` (`/script`, `/command`, `/parameter`, `/log`, `/ini`), les modes `batch`, `keepuptodate`, la console (dans un pseudo-terminal), `keygen`, `import`, `serve`, l'ouverture d'un hôte de `~/.ssh/config` par son nom, `skiff task` et l'ouverture d'une URL au démarrage, le second lancement qui passe son lien à la fenêtre ouverte (et le lancement après un plantage), la connexion par une clé que seul un `ssh-agent` détient |
| Protocoles | `go test -tags integration ./internal/core/` | Opérations de base sur chaque protocole |
| Interface | `cd test/ui && pnpm test` | Un vrai navigateur pilote l'interface : connexion et clé d'hôte, toutes les opérations sur les fichiers, éditeur et conflits, presse-papiers et glisser-déposer, écrasement, synchronisation, surveillance, recherche, terminal, tunnels, palette, préférences, sites, hôtes de la configuration OpenSSH (paramètres détectés, connexion, éditeur, préférences SSH), terminal divisé (saisie diffusée, commandes enregistrées, suivi du dossier), moniteur, conteneurs, tâches planifiées, trousseau, clés autorisées (remplacement vérifié, retrait), clé installée sur plusieurs sites, liens (Préférences → Système, lien cliqué confirmé, clé d'hôte du lien ignorée), synchronisation entre appareils, import et export, comparaison, favoris, filtre et sélection, arborescence, aperçu, disposition, diff, sommes de contrôle, liens, commandes, propriétés, file de transferts, informations serveur, journal, éditeur externe, onglets, URL et espaces de travail, clés, mot de passe maître, coupure et reconnexion, réouverture des onglets au démarrage (avec leurs dossiers, mot de passe redemandé pour une connexion rapide), mode web, mode portable (fichier `skiff.portable` puis dossier `skiff-data` à côté du binaire, rien dans le dossier de configuration de l'utilisateur). Chaque action est vérifiée par son effet sur le serveur, et aucune erreur JavaScript ne doit survenir. En cas d'échec, une capture est enregistrée (`UI_SHOTS=dossier`). |

Tests à données aléatoires (*fuzzing*) des analyseurs de ce qui vient de l'extérieur ; chacun tourne aussi 30 secondes dans la CI (tâche `security`), avec `govulncheck` et `pnpm audit` :

```bash
go test -run '^$' -fuzz '^FuzzParseURL$' -fuzztime 1m ./internal/config/
# FuzzParseAuthorized (keys), FuzzListingLines (vfs/ftpfs), FuzzListings (vfs/scpfs),
# FuzzSafeName (vfs), FuzzLines (sshconfig), FuzzTokenize (script), FuzzServerOutputs (core)
```

Une entrée qui fait échouer un test est gardée dans `testdata/fuzz/` du paquet : `go test` la rejoue ensuite à chaque fois.

Le serveur SSH de test est préparé par les scripts de `testenv/ssh-init/` :

- `45-no-penalties` désactive `PerSourcePenalties` (OpenSSH 9.8 et plus) : les tests échouent volontairement à se connecter, et sshd refuserait ensuite pendant un moment toute connexion venant de la machine ;
- `50-tcp-forwarding` autorise la redirection de ports, nécessaire aux tunnels et aux hôtes de rebond ;
- `60-user-ca` crée au premier démarrage une autorité de certification d'utilisateurs (`/config/ssh_ca/user_ca`) à laquelle sshd fait confiance (`TrustedUserCAKeys`) ; les tests lisent sa clé privée par SFTP pour signer leurs propres clés.

Le serveur « ancien » (`testenv/ssh-legacy-init/`) n'accepte que les échanges de clés `diffie-hellman-group14-sha1` et `diffie-hellman-group1-sha1`, la signature `ssh-rsa`, les chiffrements CBC et `hmac-sha1`. Le serveur FTPS implicite est une image vsftpd construite par `docker compose` (`testenv/ftps-implicit/`). Les deux serveurs FTP ont assez de connexions et de ports passifs pour les trois suites `itest`, qui tournent en même temps : vsftpd abandonne une session (`500 OOPS: vsf_sysutil_bind`) quand dix tirages au hasard ne trouvent aucun port passif libre. Après une modification de `testenv/`, recréez les conteneurs : `docker compose -f testenv/docker-compose.yml up -d --build`.

Serveurs de test (`testenv/docker-compose.yml`) : SSH sur `127.0.0.1:2222`, SSH ancien sur `127.0.0.1:2223`, FTP sur `127.0.0.1:2121` (TLS explicite, ports passifs 30000 à 30029), FTPS sur `127.0.0.1:9990` (TLS implicite, le style du port 990, ports passifs 30030 à 30059), WebDAV sur `http://127.0.0.1:8080`, S3 sur `http://127.0.0.1:9000` (`skiffadmin` / `skiffsecret`). Identifiants : `skiff` / `skiff`.

Variables utiles en développement :

| Variable | Effet |
|---|---|
| `SKIFF_NO_WINDOW=1` | N'ouvre pas de fenêtre : seul le serveur local démarre |
| `SKIFF_PORT` | Port du serveur local |
| `SKIFF_DEV_TOKEN` | Jeton fixe (au lieu d'un jeton aléatoire) |
| `SKIFF_UI_DIR` | Sert l'interface depuis ce dossier au lieu de celle intégrée au binaire ; ce Skiff-là ne se coordonne pas avec une fenêtre déjà ouverte (pas d'`instance.json`) |
| `SKIFF_PRINT_URL=1` | Affiche l'URL de l'interface au démarrage |
| `SKIFF_HOME` | Dossier de configuration |
| `SKIFF_SSH_SYSTEM_CONFIG` | Remplace le fichier `ssh_config` du système (`none` : n'en lire aucun) ; les tests s'en servent pour ne pas dépendre de la machine |

## 19.4 Régénérer les captures d'écran

Toutes les images de `docs/images` (en français) et de `docs/en/images` (en anglais) sont produites automatiquement par `docs/capture`, qui pilote l'interface de Skiff avec Playwright sur des données de démonstration. Il suffit de relancer l'outil après une modification de l'interface, dans les deux langues.

Prérequis : Docker, Node 22 ou plus, pnpm, Python 3 et un binaire Skiff compilé.

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

# 5. la même chose en anglais, dans docs/en/images
DEMO_LANG=en ./docs/capture/demo-setup.sh /tmp/skiff-demo-en
CAPTURE_LANG=en DEMO_DIR=/tmp/skiff-demo-en SKIFF_BIN=bin/skiff node docs/capture/capture.mjs

# 6. réduire le poids des images (palette de 256 couleurs, nécessite Pillow)
python3 docs/capture/optimize-images.py
python3 docs/capture/optimize-images.py docs/en/images
```

L'outil démarre Skiff sans fenêtre, avec un dossier personnel et une configuration de démonstration, crée les sites, puis rejoue une quarantaine de scènes (sites, connexion, menus, transferts, synchronisation, éditeur, terminal, tunnels, préférences, thèmes, verrouillage, mode web…). Les captures sont prises en 1280 × 800 à l'échelle 1,5, en thème clair. Une scène qui échoue laisse une capture `fail-<scène>.png` dans le dossier de démonstration ; les autres scènes continuent. La scène `sites`, qui crée les sites, est toujours jouée, et `connexion-sftp` doit précéder les scènes qui utilisent la session SFTP. Les serveurs gardent le contenu de la dernière démonstration préparée : relancez `demo-setup.sh` avant de changer de langue.

### Deux langues

Les scènes sont écrites une seule fois, en français. En anglais :

- `demo-setup.sh` avec `DEMO_LANG=en` prépare la même démonstration, puis `translate-demo.py` la traduit avec `demo-en.json` : noms des fichiers et des dossiers (`Projets/site-vitrine` → `Projects/showcase-site`), leurs textes, les conteneurs (`shop-web`), en gardant les dates des fichiers ;
- `capture.mjs` avec `CAPTURE_LANG=en` lance Skiff en anglais et traduit par `T()` chaque texte qu'il clique ou tape : les données de démonstration par le même `demo-en.json`, les libellés de l'interface par `i18n.ts` (le libellé français donne sa clé, la clé son libellé anglais). Quand un libellé français en a plusieurs en anglais (`Connexion` : *Connect* ou *Connection*), la scène désigne la clé avec `K('site.connection')`. À la fin, l'outil liste les textes qu'il n'a pas su traduire : ajoutez-les à `demo-en.json`, ou utilisez `K()`.

Un nouveau nom dans les données de démonstration va dans `demo-en.json` ; un nouveau chapitre, ou la modification d'un chapitre, se fait dans les deux langues : `docs/NN-nom.md` et `docs/en/NN-name.md` (même numéro), chacun avec le lien vers l'autre en haut et en bas. `python3 docs/check-links.py` vérifie que chaque lien mène quelque part.

---

[← Correspondance WinSCP → Skiff](18-winscp.md) · [Sommaire](README.md) · 🇬🇧 [English](en/19-development.md)
