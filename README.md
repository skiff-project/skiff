# Skiff

**Client de transfert de fichiers moderne, multiplateforme, en un seul binaire.**
SFTP · SCP · FTP/FTPS · WebDAV · S3 — une alternative à WinSCP pour Windows, macOS, Linux et FreeBSD.

Skiff reprend les fonctionnalités de WinSCP (interface Commander à deux panneaux, file de transferts, synchronisation, éditeur, scripts compatibles `winscp.com`, import des sessions…) avec une interface moderne et fluide, et ajoute un terminal SSH intégré, la gestion des tunnels et des hôtes de rebond, un diff visuel, une palette de commandes et un mode web distant.

---

## Sommaire

- [Fonctionnalités](#fonctionnalités)
- [WinSCP → Skiff](#winscp--skiff)
- [Installation et compilation](#installation-et-compilation)
- [Utilisation](#utilisation)
- [Scripts (compatibles WinSCP)](#scripts-compatibles-winscp)
- [Mode web distant](#mode-web-distant)
- [Sécurité](#sécurité)
- [Raccourcis clavier](#raccourcis-clavier)
- [Architecture](#architecture)
- [Développement et tests](#développement-et-tests)
- [Limites connues](#limites-connues)

---

## Fonctionnalités

### Protocoles
| Protocole | Détails |
|---|---|
| **SFTP** | Transferts concurrents, reprise, `posix-rename`, liens physiques, `statvfs`, serveur SFTP personnalisé (ex. `sudo /usr/lib/openssh/sftp-server` pour naviguer en root), repli automatique sur SCP |
| **SCP** | Protocole `scp -t/-f` pour les transferts, commandes shell pour le reste (listage via `find -printf` ou `ls`, `chmod`, `chown`, `touch`, `cp`, `ln`, `df`, sommes de contrôle…), shell personnalisable |
| **FTP / FTPS** | Client maison : TLS explicite et implicite (avec réutilisation de session TLS), modes **passif et actif** (PORT/EPRT), MLSD/MLST ou LIST (Unix et DOS/IIS), `SITE CHMOD`, MFMT/MDTM, REST/APPE, HASH/XSHA256/XMD5, encodages hérités (Windows-1252, Latin-1…), keepalive, reconnexion transparente |
| **WebDAV** | HTTP/HTTPS, authentification Basic et Digest, PROPFIND, MOVE/COPY côté serveur, quotas, `Range` pour la reprise, `X-OC-Mtime` (Nextcloud/ownCloud) |
| **S3** | AWS et compatibles (MinIO, Wasabi, R2, B2, Ceph, SeaweedFS…), adressage par chemin ou virtuel, région, jeton de session, upload multipart, copie côté serveur (> 5 Go via compose), dossiers virtuels |

### Connexion
- Authentification SSH : mot de passe, clavier-interactif (OTP, changement de mot de passe), **clés OpenSSH et PuTTY `.ppk` v2/v3**, certificats OpenSSH (`-cert.pub`), **agent SSH / Pageant / agent OpenSSH Windows**, clés par défaut de `~/.ssh`
- Vérification des **clés d'hôte** (`known_hosts` propre + lecture de `~/.ssh/known_hosts`), empreintes SHA-256/MD5, épinglage, alerte en cas de changement
- Vérification des **certificats TLS** avec confiance ponctuelle ou permanente, épinglage d'empreinte
- **Proxys** SOCKS4/4a, SOCKS5, HTTP CONNECT, Telnet, commande locale (`ProxyCommand`)
- **Hôtes de rebond** (chaînes `ProxyJump`), utilisables par **tous** les protocoles (FTP/WebDAV/S3 via tunnel SSH)
- Algorithmes configurables, mode « algorithmes hérités » pour les vieux serveurs, IPv4/IPv6, délais, keepalive, **reconnexion automatique**

### Interface
- **Commander** (deux panneaux) ou **Explorateur** (distant seul) ; chaque panneau peut afficher **n'importe quelle connexion**, ce qui permet les transferts **serveur → serveur**
- Onglets de session, d'éditeur, de diff et de terminal, réordonnables, restaurés au démarrage, **espaces de travail**
- Liste virtualisée (100 000 fichiers sans ralentir), tri naturel, colonnes redimensionnables et adaptatives, recherche par saisie, filtre (masques WinSCP), sélection par masque (`Num +`, `Num −`, `Num *`)
- **Arborescence des dossiers**, fil d'Ariane éditable, historique, favoris, navigation synchronisée, **comparaison de dossiers**
- **Glisser-déposer** entre panneaux, vers des dossiers, depuis le système (dossiers compris) et vers le bureau (Chromium/WebView2)
- Presse-papiers de fichiers (Ctrl+C / X / V), copie de chemins, de noms et d'URL
- **Aperçu** (images, vidéo, audio, PDF, Markdown, texte), panneau d'informations
- **Palette de commandes** (Ctrl+K) : commandes, sites, espaces de travail, fichiers, URL de connexion rapide
- Thèmes clair et sombre, couleur d'accent, trois densités, taille de police, **français et anglais**, barre des touches de fonction

### Transferts
- File d'attente en arrière-plan, **parallélisme** (par job et global), pause, reprise, annulation, réordonnancement, **nouvelle tentative des fichiers en échec**
- **Reprise** via fichiers `.filepart`, nouvelles tentatives automatiques sur erreur réseau
- Politiques d'écrasement : demander, écraser, ignorer, si plus récent, si la taille diffère, reprendre, garder les deux (avec « appliquer à tous »)
- Mode **texte / binaire / automatique** (conversion LF ↔ CRLF), masques d'inclusion et d'exclusion (taille, date, chemins, dossiers), **limite de vitesse**, casse des noms, remplacement des caractères interdits sous Windows
- Conservation des dates et des droits, droits imposés (+X pour les dossiers), **vérification par somme de contrôle** après transfert
- Déplacement (copie puis suppression), duplication côté serveur

### Synchronisation
- **Synchroniser** : local → distant, distant → local ou dans les deux sens ; modes standard, miroir, dates seules ; critères date, taille, date ou taille, **somme de contrôle**, existence ; suppression des orphelins ; masques
- **Aperçu avec cases à cocher** avant exécution
- **Garder le dossier distant à jour** (surveillance du système de fichiers local, envoi et suppression en direct, journal)

### Opérations sur les fichiers
- Renommer en ligne (F2), créer des fichiers, dossiers et liens (symboliques et physiques), supprimer (**corbeille du système** en local, **corbeille distante** configurable)
- **Propriétés** : droits (grille rwx, octal, setuid/setgid/sticky), propriétaire et groupe (avec suggestions), date de modification, application récursive, taille des dossiers, sommes de contrôle MD5/SHA-1/SHA-256/SHA-512
- **Recherche** récursive (masque, contenu via `grep` côté serveur, taille, date), résultats en direct
- **Commandes personnalisées** avec motifs WinSCP (`!`, `!&`, `!/`, `!@`, `!U`, `!?question?défaut!`), commandes ponctuelles
- Ouverture avec l'application par défaut, édition externe avec **renvoi automatique** à chaque enregistrement
- Informations serveur et protocole, journal de session (consultable, filtrable, exportable, écrit sur disque en option)

### Fonctions en plus de WinSCP
- **Terminal SSH intégré** (xterm.js) avec reprise de l'historique, recherche et copier/coller, plus un **terminal local**
- **Gestionnaire de tunnels** : redirections locales (`-L`), distantes (`-R`) et **SOCKS dynamique** (`-D`), statistiques en direct, démarrage automatique par site
- **Éditeur intégré** CodeMirror 6 : coloration de 100+ langages, recherche et remplacement, encodages, BOM, fins de ligne, **détection des conflits** à l'enregistrement
- **Diff visuel** local ↔ distant ou serveur ↔ serveur, fusion par blocs
- **Mode web distant** (`skiff serve`) : Skiff sur un serveur ou un NAS, utilisable depuis n'importe quel navigateur
- **Générateur de clés** (Ed25519, ECDSA, RSA ; OpenSSH ou `.ppk` v3), conversion OpenSSH ↔ PPK, **installation de la clé publique** sur un serveur (équivalent de `ssh-copy-id`)
- **Mot de passe maître** (Argon2id + XChaCha20-Poly1305)
- Import **WinSCP (INI et registre, mots de passe compris), PuTTY, FileZilla, `~/.ssh/config`** (y compris `ProxyJump` et `LocalForward`), URL ; export JSON, WinSCP.ini, ssh config, URL
- **Générateur d'URL et de code** de session (URL, ligne de commande, script, commande OpenSSH)

---

## WinSCP → Skiff

| WinSCP | Skiff |
|---|---|
| Fenêtre de connexion / gestionnaire de sites | Onglet **Sites** (arbre, dossiers, récents, recherche, connexion rapide par URL) |
| Interface Commander / Explorateur | Préférences → Interface, ou bouton de disposition |
| F2…F10 | Identiques (barre des touches en bas) |
| File d'attente | Panneau **Transferts** (F12) |
| Synchroniser / Garder à jour / Comparer | Barre d'outils, Ctrl+S / Ctrl+U / Ctrl+= |
| Ouvrir dans PuTTY | Terminal intégré (Ctrl+T) |
| Pageant / PuTTYgen | Agent SSH pris en charge, générateur de clés intégré |
| `winscp.com /script=… /command …` | `skiff /script=…`, `skiff /command …` ou `skiff run` |
| WinSCP.ini (mode portable) | Dossier `skiff-data` (ou fichier `skiff.portable`) à côté du binaire |
| Assembly .NET | Scripts, CLI, et API HTTP du mode web |

---

## Installation et compilation

**Prérequis** : Go ≥ 1.27, Node ≥ 22 et pnpm (uniquement pour compiler l'interface).

```bash
./build.sh      # (ou make) compile l'interface puis le binaire dans bin/skiff
./build.sh release   # compile tous les binaires dans dist/
```

### Fenêtre native

| Plateforme | Fenêtre | Prérequis |
|---|---|---|
| Windows 10/11 | **WebView2 native** (sans CGO, cross-compilée) | WebView2 (préinstallé sur Windows 10/11) |
| macOS | **WKWebView native** (compilée par la CI sur macOS, `.app` universel) | — |
| Linux | **WebKitGTK native** si compilée avec les en-têtes | à la compilation : `libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config` ; à l'exécution : `libwebkit2gtk-4.1` |
| Toutes | Variante « portable » sans webview | Ouvre une fenêtre d'application Chrome, Edge ou Brave si l'un d'eux est présent, sinon le navigateur par défaut |

Sous Linux, pour avoir la fenêtre native :

```bash
sudo apt install -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config
./build.sh
```

`build.sh` (ou `make`) détecte WebKitGTK automatiquement. La CI GitHub Actions (`.github/workflows/build.yml`) produit les binaires natifs de chaque plateforme et publie une release à chaque tag `v*`.

---

## Utilisation

```text
skiff [url-de-session]           ouvre la fenêtre (et se connecte éventuellement)
skiff serve [options]            interface web pour un usage distant
skiff run [options] [script]     exécute un script compatible WinSCP
skiff shell [url-de-session]     console interactive
skiff keygen [options]           génère une paire de clés SSH
skiff import <source> [fichier]  importe des sessions (winscp-ini, winscp-registry, putty, filezilla, ssh-config)
skiff version
```

**URL de session** : `sftp://user@hôte:22/chemin`, `scp://`, `ftp://`, `ftps://` (TLS implicite), `ftpes://` (TLS explicite), `dav://`, `davs://`, `s3://`. WinSCP accepte aussi `sftp://user;fingerprint=SHA256:…@hôte/`, Skiff également.

**Configuration** : `~/.config/Skiff` (Linux), `%APPDATA%\Skiff` (Windows), `~/Library/Application Support/Skiff` (macOS). Pour le **mode portable**, créez un dossier `skiff-data` (ou un fichier vide `skiff.portable`) à côté du binaire. La variable `SKIFF_HOME` force un dossier précis.

---

## Scripts (compatibles WinSCP)

```bash
skiff run -c "open sftp://deploy@prod.example.com -hostkey=SHA256:abc…" \
          -c "synchronize remote -delete ./site /var/www" -c exit

skiff /script=backup.txt /log=backup.log          # comme winscp.com
skiff /command "open ftpes://user:pass@ftp.example.com/" "get /exports/*.csv C:\data\" "exit"
```

Commandes : `open`, `close`, `session`, `exit`, `cd`, `pwd`, `ls`, `lcd`, `lpwd`, `lls`, `get`, `put` (`-delete -resume -append -preservetime -nopreservetime -speed= -transfer= -filemask= -neweronly -latest -permissions=`), `mkdir`, `rm`, `rmdir`, `mv`, `cp`, `chmod` (`-recursive`), `ln`, `stat`, `checksum`, `call`, `synchronize` (`local|remote|both -delete -mirror -criteria= -filemask= -preview`), `keepuptodate`, `option` (`batch`, `confirm`, `transfer`, `echo`, `failonnomatch`), `echo`, `help`.
Variables : `%VARIABLE%`, `%TIMESTAMP#yyyy-mm-dd%`, `%TIMESTAMP-1D#yyyymmdd%`. Code de sortie 1 en cas d'erreur (`option batch abort`, par défaut).

En mode non interactif, une clé d'hôte inconnue est refusée : épinglez-la avec `-hostkey=…` (ou `-hostkey=*` pour accepter n'importe quelle clé, en connaissance de cause).

---

## Mode web distant

```bash
skiff serve --set-password                              # choisir le mot de passe (stocké en Argon2id)
skiff serve --listen 0.0.0.0:8443 --root /srv/data      # HTTPS avec certificat auto-signé
skiff serve --tls-cert cert.pem --tls-key key.pem
skiff serve --no-tls --listen 127.0.0.1:8080            # derrière un reverse proxy TLS
```

- Connexion par identifiant et mot de passe, cookie de session `HttpOnly` et `SameSite=Strict`, limitation des tentatives, en-tête anti-CSRF obligatoire, vérification de l'`Origin` des WebSockets.
- `--root` confine le panneau local à un dossier (y compris face aux liens symboliques) ; terminal local, commandes locales et éditeurs externes sont désactivés (option `--allow-local-terminal`).

---

## Sécurité

- **Mots de passe enregistrés** chiffrés avec XChaCha20-Poly1305 :
  - sans mot de passe maître, la clé est dans `secret.key` (mode 600). Le fichier des sites peut ainsi être partagé ou synchronisé sans exposer les secrets ;
  - avec un **mot de passe maître**, la clé est dérivée par Argon2id et n'est jamais écrite.
- L'interface ne reçoit jamais les mots de passe enregistrés (seulement un indicateur « enregistré »).
- Fenêtre de bureau : serveur sur `127.0.0.1` uniquement, jeton aléatoire par lancement, contrôle de l'en-tête `Host` (anti DNS rebinding), en-tête anti-CSRF, contenus actifs (HTML/SVG) jamais rendus depuis l'origine de l'application.
- Clés d'hôte SSH et certificats TLS vérifiés de façon interactive, avec épinglage possible.

---

## Raccourcis clavier

| Touche | Action | Touche | Action |
|---|---|---|---|
| `Tab` | Changer de panneau | `Ctrl+K` | Palette de commandes |
| `Entrée` | Ouvrir | `Retour arrière` / `Alt+↑` | Dossier parent |
| `F2` | Renommer | `F3` | Afficher |
| `F4` / `Maj+F4` | Éditer (interne / externe) | `F5` / `Maj+F5` | Copier (avec options) |
| `F6` | Déplacer | `F7` / `Maj+F7` | Nouveau dossier / fichier |
| `F8` / `Suppr` | Supprimer | `F9` | Propriétés |
| `F12` | Transferts | `Espace` / `Inser` | Sélectionner |
| `Ctrl+F` | Filtrer | `Ctrl+Maj+F` | Rechercher des fichiers |
| `Ctrl+S` | Synchroniser | `Ctrl+=` | Comparer les dossiers |
| `Ctrl+B` | Navigation synchronisée | `Ctrl+U` | Garder à jour |
| `Ctrl+T` | Terminal | `Ctrl+L` | Éditer le chemin |
| `Ctrl+D` | Favori | `Ctrl+R` | Actualiser |
| `Ctrl+C/X/V` | Copier/couper/coller des fichiers | `Ctrl+Tab` | Onglet suivant |

La liste complète se trouve dans Préférences → Raccourcis clavier.

---

## Architecture

```text
skiff/
├── main.go, cli_*.go         CLI : fenêtre, serve, run, shell, keygen, import, compat winscp.com
├── internal/
│   ├── vfs/                  système de fichiers unifié + implémentations
│   │   ├── localfs  sftpfs  scpfs  ftpfs (client FTP/FTPS maison)  davfs (client WebDAV maison)  s3fs
│   ├── sshx/                 SSH : authentification, PPK, agent, known_hosts, rebonds, keepalive
│   ├── netx/                 proxys (SOCKS4/5, HTTP, Telnet, commande), TLS interactif
│   ├── transfer/             moteur de transferts (file, workers, reprise, écrasement, mode texte…)
│   ├── core/                 connexions, opérations, synchronisation, surveillance, terminaux, tunnels, éditeurs
│   ├── config/               sites, préférences, secrets chiffrés, mot de passe maître, mode portable
│   ├── importer/  keys/  mask/  script/  prompt/
│   ├── server/               API JSON-RPC, WebSocket (événements, terminaux), streaming, authentification
│   └── desktop/              fenêtre Wails (WebView2/WKWebView/WebKitGTK) ou repli navigateur
├── frontend/                 React 19 + TypeScript + Vite (zustand, TanStack Virtual, CodeMirror 6, xterm.js)
└── testenv/                  serveurs Docker de test (OpenSSH, Pure-FTPd+TLS, WebDAV, S3)
```

L'interface communique avec le cœur Go par HTTP et WebSocket. C'est la même interface dans la fenêtre native et en mode web.

---

## Développement et tests

```bash
./build.sh dev   # recompile l'interface à la volée, Skiff sur http://127.0.0.1:8484/?token=dev
./build.sh test  # go vet, tests unitaires, typecheck TypeScript
./build.sh itest # lance les serveurs Docker et teste SFTP, SCP, FTP, FTPS, WebDAV et S3 de bout en bout
```

Serveurs de test (`testenv/docker-compose.yml`) : SSH `127.0.0.1:2222`, FTP `127.0.0.1:2121` (TLS explicite), WebDAV `http://127.0.0.1:8080`, S3 `http://127.0.0.1:9000` (`skiffadmin` / `skiffsecret`). Identifiants : `skiff` / `skiff`.

---

## Limites connues

- Les fenêtres natives macOS et Linux sont compilées par la CI (elles demandent CGO) : sans les en-têtes WebKitGTK, la build Linux locale utilise le repli navigateur.
- SFTP en version 3 (la version d'OpenSSH) ; les extensions des versions 4 à 6 ne sont pas utilisées.
- Le `chmod` FTP dépend du serveur (`SITE CHMOD`), et S3/WebDAV n'ont pas de droits Unix.
- La file de transferts n'est pas conservée après un redémarrage.
- Le glisser-déposer d'un fichier distant vers le bureau ne fonctionne qu'avec les moteurs Chromium (WebView2, Chrome, Edge).
