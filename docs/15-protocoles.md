# 15. Protocoles

[← Mode web distant](14-mode-web.md) · [Sommaire](README.md) · [Raccourcis clavier →](16-raccourcis.md)

Skiff présente tous les protocoles de la même façon : mêmes panneaux, mêmes transferts, même synchronisation. Ce chapitre décrit leurs particularités.

## 15.1 Ce que permet chaque protocole

| Fonction | Local | SFTP | SCP | FTP/FTPS | WebDAV | S3 |
|---|:-:|:-:|:-:|:-:|:-:|:-:|
| Droits Unix (affichage, `chmod`) | Unix | ✓ | ✓ | ✓¹ | | |
| Propriétaire et groupe (`chown`) | Unix | ✓ | ✓ | affichage | | |
| Liens symboliques | ✓ | ✓ | ✓ | | | |
| Liens physiques | ✓ | ✓² | ✓ | | | |
| Modifier la date | ✓ | ✓ | ✓ | ✓³ | Nextcloud/ownCloud | |
| Copie côté serveur | ✓ | ✓ | ✓ | | ✓ | ✓ |
| Sommes de contrôle sans téléchargement | ✓ | ✓ | ✓ | ✓⁴ | | MD5 |
| Espace libre | ✓ | ✓² | ✓ | | ✓ | |
| Reprise des téléchargements | ✓ | ✓ | ✓ | ✓⁵ | ✓ | ✓ |
| Reprise des envois | ✓ | ✓ | | ✓ | | |
| Terminal, commandes shell | ✓ | ✓ | ✓ | commandes FTP brutes | | |
| Tunnels (redirection de ports) | | ✓ | ✓ | | | |
| Précision des dates | ns | s | s | s ou min | s | s |

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

## 15.7 URL de session

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

- Sans schéma, `sftp://` est supposé : `alex@serveur` suffit.
- Le chemin devient le dossier distant d'ouverture. En S3, il commence par le bucket : `s3://CLÉ@s3.eu-west-3.amazonaws.com/mon-bucket/archives/`.
- En S3, l'utilisateur est l'**ID de clé d'accès** et le mot de passe la **clé secrète**.
- Les caractères spéciaux de l'utilisateur et du mot de passe s'encodent en `%XX` (`@` → `%40`).
- Comme dans WinSCP, l'empreinte de la clé d'hôte peut figurer dans l'URL : `sftp://alex;fingerprint=SHA256:Fh7jA0…@serveur/`.

---

[← Mode web distant](14-mode-web.md) · [Sommaire](README.md) · [Raccourcis clavier →](16-raccourcis.md)
