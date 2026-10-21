# 15. Protocoles

[← Mode web distant](14-mode-web.md) · [Sommaire](README.md) · [Raccourcis clavier →](16-raccourcis.md) · 🇬🇧 [English](en/15-protocols.md)

Skiff présente tous les protocoles de la même façon : mêmes panneaux, mêmes transferts, même synchronisation. Ce chapitre décrit leurs particularités.

## 15.1 Ce que permet chaque protocole

| Fonction | Local | SFTP | SCP | FTP/FTPS | WebDAV | S3 | Conteneur |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| Droits Unix (affichage, `chmod`) | Unix | ✓ | ✓ | ✓¹ | | | ✓ |
| Propriétaire et groupe (`chown`) | Unix | ✓ | ✓ | affichage | | | ✓ |
| Liens symboliques | ✓ | ✓ | ✓ | | | | ✓ |
| Liens physiques | ✓ | ✓² | ✓ | | | | ✓ |
| Modifier la date | ✓ | ✓ | ✓ | ✓³ | Nextcloud/ownCloud | | ✓ |
| Copie côté serveur | ✓ | ✓ | ✓ | | ✓ | ✓ | ✓ |
| Sommes de contrôle sans téléchargement | ✓ | ✓ | ✓ | ✓⁴ | | MD5 | ✓ |
| Espace libre | ✓ | ✓² | ✓ | | ✓ | | ✓ |
| Reprise des téléchargements | ✓ | ✓ | ✓ | ✓⁵ | ✓ | ✓ | ✓ |
| Reprise des envois | ✓ | ✓ | | ✓ | | | |
| Terminal, commandes shell | ✓ | ✓ | ✓ | commandes FTP brutes | | | ✓ |
| Moniteur | | ✓ | ✓ | | | | ✓ |
| Tunnels (redirection de ports) | | ✓ | ✓ | | | | |
| Précision des dates | ns | s | s | s ou min | s | s | s |

¹ selon le serveur (`SITE CHMOD`). ² si le serveur propose l'extension (`hardlink@openssh.com`, `statvfs@openssh.com`). ³ avec `MFMT` ou `MDTM`. ⁴ avec `HASH`, `XSHA256`, `XSHA1` ou `XMD5`. ⁵ avec `REST`.

Les commandes indisponibles sont masquées ou grisées dans l'interface. La fenêtre [Informations serveur/protocole](10-recherche-journal.md#103-informations-serveur-et-protocole) montre les extensions réellement proposées par le serveur.

## 15.2 SFTP

![Session SFTP](images/commander.png)

Le protocole par défaut, sur le port 22. Il passe par SSH : authentification par mot de passe, clavier-interactif, clés OpenSSH et PuTTY, certificats OpenSSH, agent SSH (voir [Sites → SSH](03-sites.md#ssh)).

- Transferts en parallèle sur plusieurs canaux, requêtes en pipeline pour les gros fichiers.
- Extensions OpenSSH utilisées quand elles existent : `posix-rename` (renommage atomique), `statvfs` (espace libre), `hardlink` (liens physiques), `fsync`, `limits`.
- **Commande du serveur SFTP** personnalisable (par exemple `sudo /usr/lib/openssh/sftp-server` pour naviguer en root).
- Si le serveur refuse le sous-système SFTP, Skiff bascule automatiquement sur SCP.

## 15.3 SCP

![Session SCP](images/session-scp.png)

Pour les serveurs SSH sans SFTP (équipements réseau, systèmes embarqués). Les transferts utilisent le protocole `scp -t` / `scp -f`, et les autres opérations des commandes shell : listage par `find -printf` ou `ls -la` (ou la **Commande de listage** du site), `mkdir`, `rm`, `mv`, `cp`, `chmod`, `chown`, `touch`, `ln`, `df`, `md5sum`/`sha*sum`. Le **Shell** est réglable dans le site.

## 15.4 FTP et FTPS

![Session FTPS](images/session-ftps.png)

Client FTP écrit pour Skiff, port 21 (990 en TLS implicite) :

- **FTPS** en TLS explicite (`AUTH TLS`) ou implicite, avec réutilisation de la session TLS sur les connexions de données (exigée par de nombreux serveurs) et [vérification du certificat](11-securite.md#112-certificats-tls).
- Modes **passif** (`PASV`/`EPSV`, avec correction de l'adresse derrière un NAT) et **actif** (`PORT`/`EPRT`, IP externe réglable).
- Listages **MLSD/MLST** normalisés, ou `LIST` aux formats Unix et DOS/IIS.
- `SITE CHMOD`, `MFMT`/`MDTM` (dates), `REST` et `APPE` (reprise), `HASH`/`XSHA256`/`XMD5` (sommes de contrôle), `ACCT` (compte).
- Encodages hérités des noms de fichiers (Windows-1252, Latin-1…), keepalive, reconnexion transparente quand le serveur coupe une connexion inactive.
- **Exécuter une commande** envoie une commande FTP brute (`SITE …`).

## 15.5 WebDAV

![Session WebDAV](images/session-webdav.png)

Client WebDAV écrit pour Skiff, en HTTP (80) ou HTTPS (443) :

- Authentification Basic et Digest.
- `PROPFIND` pour les listages, `MKCOL`, `MOVE` et `COPY` côté serveur, `DELETE`.
- Espace libre par les propriétés de quota.
- Reprise des téléchargements par requêtes `Range`.
- Nextcloud et ownCloud : conservation des dates (`X-OC-Mtime`). Dossier distant : `/remote.php/dav/files/<utilisateur>/`.
- Fonctionne aussi avec Apache `mod_dav`, nginx, SharePoint, Box, kDrive, Synology, etc.

## 15.6 S3

![Session S3 : les buckets apparaissent comme des dossiers](images/session-s3.png)

Amazon S3 et les stockages compatibles : MinIO, Wasabi, Cloudflare R2, Backblaze B2, Scaleway, OVHcloud, Ceph, SeaweedFS, Garage…

- À la racine, chaque **bucket** apparaît comme un dossier. Créer un dossier à la racine crée un bucket.
- Les « dossiers » sont des préfixes de clés : Skiff les affiche comme de vrais dossiers et crée des marqueurs pour les dossiers vides.
- Envoi **multipart** pour les gros fichiers, copie et renommage côté serveur (au-delà de 5 Go, par assemblage de parties).
- Adressage virtuel (`bucket.hôte`) ou par chemin (`hôte/bucket`), région, jeton de session STS.
- Pas de droits Unix, pas de modification de date (la date est celle de l'envoi), somme de contrôle MD5 par l'ETag (fichiers envoyés en une seule partie).

## 15.7 Conteneurs (Docker, Podman, Kubernetes)

Un conteneur Docker ou Podman, ou un pod Kubernetes, s'ouvre comme un serveur : ses fichiers dans un panneau, un terminal dedans, le moniteur de ses processus. Skiff utilise la commande `exec` du moteur — `docker exec`, `podman exec`, `kubectl exec` — et n'installe rien dans le conteneur : un shell POSIX et les outils habituels suffisent (busybox, comme dans les images Alpine, convient). La lecture passe par `cat` (et `tail -c` pour une reprise), l'écriture par `cat >`, le reste par `ls`/`find`, `mkdir`, `mv`, `chmod`, `sha256sum`…

**Où tourne le moteur** :

- **cet ordinateur** : la commande `docker`, `podman` ou `kubectl` installée ici (avec ses réglages : contexte Docker, `~/.kube/config`) ;
- **un serveur SSH** : la commande est lancée sur le serveur, par la connexion SSH — les conteneurs d'un serveur distant s'ouvrent sans exposer le démon Docker ni l'API Kubernetes sur le réseau.

**Trouver un conteneur** :

- **Conteneurs** sur l'écran d'accueil, la palette (`Ctrl+K` → « Conteneurs de cet ordinateur ») ou **⋯ → Conteneurs de cet ordinateur** dans la liste des sites, pour ceux de l'ordinateur ;
- dans une session SFTP ou SCP, le bouton **Conteneurs de ce serveur** (icône de boîte dans la barre d'outils) liste les conteneurs du serveur.

![Conteneurs de cet ordinateur](images/conteneurs.png)

La fenêtre s'ouvre sur le premier moteur installé : Docker, Podman ou Kubernetes. Ceux qui ne sont pas installés sur l'ordinateur ou le serveur sont grisés (une bulle le dit). Le champ **Rechercher** filtre par nom, image, projet ou port.

Les conteneurs d'un même projet **Docker Compose** sont regroupés sous **Projet Compose …** (les pods Kubernetes, par espace de noms), ceux qui tournent en premier. Chaque ligne montre :

| Colonne | Contenu |
|---|---|
| État | **En marche** (vert), **Arrêté**, **En pause**, **Redémarre**, **Créé**… ; pour un pod, sa phase (**En attente**, **Terminé**, **En échec**) |
| Nom | Le nom, puis l'image et les ports publiés (`127.0.0.1:18080->80/tcp`) |
| Depuis | Depuis quand il tourne (« depuis 5 heures », avec l'état de santé s'il en a un), ou quand il s'est arrêté et son code de sortie (« il y a 2 jours, code 0 ») |
| Actions | **Fichiers** ouvre une session sur le conteneur, **Terminal** y ouvre en plus un shell (`bash` s'il existe, sinon `sh`) ; pour un conteneur arrêté, **Démarrer** |

Un double-clic sur un conteneur en marche ouvre ses fichiers. Le menu **⋯** de chaque ligne propose aussi :

![Menu ⋯ d'un conteneur](images/conteneurs-menu.png)

- **Redémarrer**, **Arrêter** (`docker stop` : le conteneur a quelques secondes pour s'arrêter proprement) ou **Démarrer**, pour Docker et Podman ; Kubernetes gère seul ses pods ;
- **Enregistrer comme site…**, qui ouvre l'éditeur de site prérempli pour retrouver le conteneur dans la liste des sites (pour un serveur, quand sa session vient d'un site enregistré) ;
- **Copier le nom**.

Pour Kubernetes, **Contexte** (vide : le contexte courant) et **Espace de noms** (vide : tous) se règlent au-dessus de la liste, puis **Afficher**.

Quand ça ne marche pas, la fenêtre explique les cas courants : Docker absent, démon arrêté (`sudo systemctl start docker`), utilisateur hors du groupe `docker` (`sudo usermod -aG docker $USER`, puis reconnectez-vous), cluster Kubernetes injoignable. Le message exact du moteur est affiché en dessous.

**Un site de type Conteneur** (protocole **Conteneur** dans l'éditeur de site) garde : le **moteur**, **S'exécute sur** (cet ordinateur ou un site SSH enregistré), le nom du **conteneur** ou du **pod** (**Choisir…** les liste, sur cet ordinateur ou sur le serveur SSH choisi, auquel Skiff se connecte le temps du choix si aucune session n'y est ouverte), l'**utilisateur** des commandes (Docker, Podman), le **contexte**, l'**espace de noms** et le **conteneur du pod** (Kubernetes), et la **commande** du moteur si elle n'est pas dans le `PATH`.

![Un site de type Conteneur](images/site-conteneur.png)

Les URL `docker://conteneur/chemin`, `podman://conteneur/chemin` et `kubectl://pod/chemin?namespace=demo&context=prod&container=web` ouvrent un conteneur de cet ordinateur ([§ 15.8](#158-url-de-session)).

Limites : pas de reprise des envois ni de tunnels ; le moniteur montre les processus du conteneur, mais la mémoire et les disques de la machine hôte. En [mode web](14-mode-web.md), les conteneurs de la machine qui exécute Skiff sont désactivés comme le terminal local ; ceux des serveurs SSH restent disponibles.

## 15.8 URL de session

Les URL servent à la connexion rapide, à la palette `Ctrl+K`, à la ligne de commande (`skiff <url>`) et aux scripts (`open <url>`).

```text
protocole://[utilisateur[:mot-de-passe]@]hôte[:port][/chemin]
```

| Schéma | Protocole |
|---|---|
| `sftp://`, `ssh://` | SFTP |
| `scp://` | SCP |
| `ftp://` | FTP sans chiffrement |
| `ftpes://` | FTP + TLS explicite |
| `ftps://` | FTP + TLS implicite |
| `dav://`, `webdav://`, `http://` | WebDAV |
| `davs://`, `webdavs://`, `https://` | WebDAV en HTTPS |
| `s3://` | S3 en HTTPS (hôte par défaut : `s3.amazonaws.com`) |
| `s3plain://` | S3 en HTTP (MinIO local…) |
| `docker://`, `podman://` | Conteneur Docker ou Podman de cet ordinateur ([§ 15.7](#157-conteneurs-docker-podman-kubernetes)) |
| `kubectl://` | Pod Kubernetes (`?namespace=`, `&context=`, `&container=`) |

- Sans schéma, `sftp://` est supposé : `alex@serveur` suffit.
- Le chemin devient le dossier distant d'ouverture. En S3, il commence par le bucket : `s3://CLÉ@s3.eu-west-3.amazonaws.com/mon-bucket/archives/`.
- En S3, l'utilisateur est l'**ID de clé d'accès** et le mot de passe la **clé secrète**.
- Les caractères spéciaux de l'utilisateur et du mot de passe s'encodent en `%XX` (`@` → `%40`).
- Comme dans WinSCP, l'empreinte de la clé d'hôte peut figurer dans l'URL : `sftp://alex;fingerprint=SHA256:Fh7jA0…@serveur/`.

---

[← Mode web distant](14-mode-web.md) · [Sommaire](README.md) · [Raccourcis clavier →](16-raccourcis.md) · 🇬🇧 [English](en/15-protocols.md)
