# 13. Scripts et ligne de commande

[← Préférences](12-preferences.md) · [Sommaire](README.md) · [Mode web distant →](14-mode-web.md)

Le binaire `skiff` sert aussi d'outil en ligne de commande. Son langage de script reprend celui de `winscp.com` : les scripts WinSCP existants fonctionnent tels quels dans la plupart des cas.

## 13.1 Vue d'ensemble

```text
skiff [url-de-session]           ouvre la fenêtre (et se connecte éventuellement)
skiff serve [options]            interface web pour un usage distant
skiff run [options] [script]     exécute un script compatible WinSCP
skiff shell [url-de-session]     console interactive
skiff keygen [options]           génère (ou convertit, -convert) une paire de clés SSH
skiff import <source> [fichier]  importe des sessions
skiff version                    affiche la version
skiff help                       affiche l'aide
```

`skiff <commande> -h` détaille les options de chaque commande. Les scripts et la console utilisent les sites, les préférences, les clés d'hôte et les mots de passe enregistrés de la configuration habituelle.

## 13.2 `skiff run`

```bash
skiff run sauvegarde.txt
skiff run -c "open sftp://deploy@prod.example.com -hostkey=SHA256:abc…" \
          -c "synchronize remote -delete ./site /var/www" -c exit
skiff run -log=deploiement.log -batch=continue deploiement.txt
```

| Option | Description |
|---|---|
| `-c "commande"` | Commande à exécuter, répétable. Les commandes `-c` s'exécutent après celles du fichier de script éventuel. |
| `-batch=abort` | Par défaut : la première erreur arrête le script avec le code de sortie 1 |
| `-batch=continue` | Les erreurs sont affichées et le script continue (les fichiers en erreur sont ignorés) |
| `-batch=off` | Mode interactif : les questions (clé d'hôte, écrasement…) sont posées dans le terminal |
| `-log=fichier` | Écrit le journal de session dans ce fichier |

Dans un fichier de script, les lignes vides et les lignes commençant par `#` ou `;` sont ignorées. Le code de sortie vaut 0 en cas de succès et 1 en cas d'erreur.

## 13.3 Commandes de script

### Sessions

| Commande | Description |
|---|---|
| `open <url\|site> [options]` | Ouvre une session. L'argument est une [URL de session](15-protocoles.md#158-url-de-session), le nom d'un site enregistré (`Production/Site vitrine (SFTP)` ou simplement `Site vitrine (SFTP)`) ou un hôte de `~/.ssh/config` (`open recette`, comme `ssh recette` : adresse, port, utilisateur, clés et rebonds viennent du fichier, voir [Configuration OpenSSH](03-sites.md#34-configuration-openssh-ssh_config)). |
| `close [n]` | Ferme la session courante (ou la session `n`) |
| `session [n]` | Liste les sessions ouvertes, ou rend la session `n` courante. Plusieurs sessions peuvent être ouvertes en même temps. |
| `exit` (`bye`, `quit`) | Ferme les sessions et termine le script |

Options de `open` :

| Option | Description |
|---|---|
| `-hostkey=SHA256:…` | Empreinte attendue de la clé d'hôte. `-hostkey=*` accepte n'importe quelle clé (à vos risques). En mode non interactif, une clé inconnue est refusée sans cette option. |
| `-password=…` | Mot de passe (mieux vaut une variable d'environnement : `-password=%MOTDEPASSE%`) |
| `-privatekey=fichier` | Clé privée (OpenSSH ou `.ppk`) |
| `-passphrase=…` | Phrase secrète de la clé |
| `-certificate=empreinte\|*` | Empreinte attendue du certificat TLS (FTPS, WebDAV, S3) |
| `-passive=on\|off` | Mode passif ou actif (FTP) |
| `-explicit`, `-implicit` | TLS explicite ou implicite (FTP) |
| `-timeout=s` | Délai de connexion en secondes |
| `-rawsettings …` | Accepté pour la compatibilité avec WinSCP, sans effet |

### Navigation

| Commande | Description |
|---|---|
| `pwd`, `cd [dossier]`, `ls [dossier\|masque]` (`dir`) | Dossier distant courant, changement, listage |
| `lpwd`, `lcd <dossier>`, `lls [dossier]` (`ldir`) | Équivalents locaux |

### Transferts

| Commande | Description |
|---|---|
| `get [options] <distant>... [local]` | Télécharge des fichiers ou dossiers (masques acceptés : `get /logs/*.log ./logs/`) |
| `put [options] <local>... [distant]` | Envoie des fichiers ou dossiers |

Options de `get` et `put` :

| Option | Description |
|---|---|
| `-delete` | Supprime la source après le transfert (déplacement) |
| `-resume`, `-append` | Reprend ou complète un fichier existant |
| `-preservetime`, `-nopreservetime` | Conserve ou non la date de modification |
| `-permissions=644`, `-nopermissions` | Impose des droits, ou ne transfère pas les droits |
| `-speed=Kio` | Limite de vitesse en Kio/s |
| `-transfer=binary\|ascii\|automatic` | Mode de transfert |
| `-filemask=masque` | [Masque](06-transferts.md#63-masques-de-fichiers) d'inclusion et d'exclusion |
| `-neweronly` | Seulement les fichiers nouveaux ou plus récents |
| `-latest` | Seulement le fichier le plus récent parmi ceux qui correspondent |

### Fichiers

| Commande | Description |
|---|---|
| `mkdir <dossier>` | Crée un dossier |
| `rm <fichier\|masque>...` (`del`), `rmdir <dossier>` | Supprime |
| `mv <source> <destination>` (`rename`) | Renomme ou déplace sur le serveur |
| `cp <source> <destination>` | Copie sur le serveur |
| `chmod <mode> <fichier>... [-recursive]` | Change les droits |
| `ln <cible> <lien>` (`symlink`) | Crée un lien symbolique ; `ln -hard <cible> <lien>` crée un lien physique |
| `stat <fichier>` | Affiche les attributs |
| `checksum <md5\|sha1\|sha256\|sha512> <fichier>` | Calcule une somme de contrôle sur le serveur |
| `call <commande>` (`!`) | Exécute une commande shell sur le serveur (SFTP, SCP) ; un code de sortie non nul est une erreur |

### Synchronisation

| Commande | Description |
|---|---|
| `synchronize local\|remote\|both [options] [dossier local] [dossier distant]` (`sync`) | Synchronise comme la [boîte de synchronisation](07-synchronisation.md) ; un dossier de destination absent est créé. Options : `-delete`, `-mirror`, `-criteria=time\|size\|either\|checksum\|none`, `-filemask=…`, `-transfer=…`, `-preview` (affiche les actions sans les exécuter) |
| `keepuptodate [-delete] [-filemask=…] [dossier local] [dossier distant]` | Surveille le dossier local et envoie les changements jusqu'à `Ctrl+C` |

### Divers

| Commande | Description |
|---|---|
| `option` | Sans argument : affiche les options courantes |
| `option batch off\|on\|abort\|continue` | Gestion des erreurs (voir `-batch` ; `on` équivaut à `abort`) |
| `option confirm on\|off` | Avec `off`, les fichiers existants sont écrasés sans question |
| `option transfer binary\|ascii\|automatic` | Mode de transfert par défaut |
| `option echo on\|off` | Affiche chaque commande avant de l'exécuter |
| `option failonnomatch on\|off` | Une absence de correspondance d'un masque est une erreur |
| `echo <texte>` | Affiche un texte |
| `help` (`?`) | Liste des commandes |

### Variables

| Syntaxe | Remplacée par |
|---|---|
| `%NOM%` | Variable d'environnement `NOM` |
| `%1%`, `%2%`… | Paramètres passés avec `/parameter` |
| `%TIMESTAMP#yyyy-mm-dd%` | Date et heure courantes ; codes `yyyy`, `yy`, `mm`, `dd`, `hh`, `nn` (minutes), `ss`, `zzz` (millisecondes) |
| `%TIMESTAMP-1D#yyyymmdd%` | Date décalée : `+` ou `-` suivi d'un nombre et d'une unité `Y`, `M` (mois), `D`, `H`, `N`, `S` |

### Exemple complet

```text
# sauvegarde.txt — sauvegarde quotidienne des journaux
option batch abort
option confirm off
open sftp://backup@serveur.example.com/ -hostkey=SHA256:Fh7jA0…
lcd /srv/sauvegardes
get -neweronly /var/log/nginx/*.log journaux-%TIMESTAMP#yyyy-mm-dd%/
synchronize local -criteria=either ./site /var/www/site
call df -h /var
close
exit
```

## 13.4 Console interactive : `skiff shell`

```bash
skiff shell sftp://alex@serveur.example.com
```

Une console où l'on tape les commandes de script une à une. L'invite affiche la session et le dossier courants (`skiff@serveur:/var/www> `). Dans un terminal, les questions (clé d'hôte, mot de passe, écrasement) sont posées avec des choix numérotés (`1) Accept  2) Connect once  3) Cancel`). Si l'entrée de la console est redirigée (un fichier, un tube), elle se comporte comme un script : une clé d'hôte inconnue est refusée, d'où l'option `-hostkey=`. `help` liste les commandes, `exit` ou `Ctrl+D` quitte.

## 13.5 Compatibilité `winscp.com`

Skiff accepte la syntaxe de ligne de commande de `winscp.com`, ce qui permet de remplacer WinSCP dans des tâches planifiées existantes :

```bat
skiff /script=sauvegarde.txt /log=sauvegarde.log
skiff /command "open ftpes://depot:secret@ftp.example.com/" "get /exports/*.csv C:\donnees\" "exit"
skiff /script=envoi.txt /parameter // C:\rapports\rapport.pdf /depot/
```

| Option | Description |
|---|---|
| `/script=fichier` | Exécute un fichier de script |
| `/command "cmd1" "cmd2"…` | Exécute les commandes données (après le script éventuel) |
| `/log=fichier` | Journal de session |
| `/ini=dossier` | Dossier de configuration à utiliser (`/ini=nul` : ignoré) |
| `/parameter // a b…` | Paramètres accessibles par `%1%`, `%2%`… |

Le mode d'erreur est `abort` : la première erreur arrête le script avec le code 1, comme `winscp.com` en mode batch.

## 13.6 `skiff import`

```bash
skiff import                         # liste les sources et ce qui a été détecté
skiff import winscp-ini              # WinSCP.ini détecté automatiquement
skiff import winscp-ini C:\Users\alex\Desktop\WinSCP.ini
skiff import ssh-config
skiff import filezilla ~/sitemanager.xml
```

Sources : `winscp-ini`, `winscp-registry` (Windows), `putty`, `filezilla`, `ssh-config`, `skiff-json`, `urls`. Chaque site importé est affiché, avec une remarque si son mot de passe n'a pas pu être récupéré. Voir aussi l'[import dans l'interface](03-sites.md#35-importer-des-sites).

## 13.7 `skiff keygen`

```bash
skiff keygen                               # Ed25519, ~/.ssh/id_ed25519, phrase secrète demandée
skiff keygen -t rsa -b 4096 -C "alex@portable" -f ~/.ssh/id_rsa_travail
skiff keygen -t ecdsa -b 384 -ppk -N       # format PuTTY, sans phrase secrète
skiff keygen -convert ~/.ssh/id_ed25519 -ppk          # OpenSSH → PuTTY (id_ed25519.ppk)
skiff keygen -convert cle.ppk -f ~/.ssh/id_cle        # PuTTY → OpenSSH
```

| Option | Description |
|---|---|
| `-t ed25519\|ecdsa\|rsa` | Type de clé (Ed25519 par défaut) |
| `-b bits` | Taille : RSA 2048 à 8192, ECDSA 256, 384 ou 521 |
| `-C commentaire` | Commentaire (`utilisateur@machine` par défaut) |
| `-f fichier` | Fichier de la clé privée (`~/.ssh/id_<type>` par défaut). La clé publique est écrite dans `<fichier>.pub`. |
| `-ppk` | Format PuTTY `.ppk` v3 au lieu d'OpenSSH |
| `-N` | Pas de phrase secrète |
| `-convert fichier` | Convertit une clé existante au lieu d'en générer une : vers PuTTY avec `-ppk`, vers OpenSSH sinon. Demande la phrase secrète actuelle et, sauf avec `-N`, une nouvelle phrase secrète. |

La commande affiche le chemin de la clé privée, l'empreinte et la clé publique. Un fichier existant n'est jamais écrasé.

## 13.8 `skiff serve`

Voir [Mode web distant](14-mode-web.md).

## 13.9 `skiff task`

Liste les [tâches planifiées](07-synchronisation.md#74-tâches-planifiées) ou en exécute une tout de suite, sans ouvrir Skiff : c'est la commande à confier au planificateur du système (cron, Planificateur de tâches de Windows, launchd) pour qu'une tâche tourne Skiff fermé.

```bash
skiff task list                        # état, nom, type, planification, dernière exécution
skiff task run "Publier le site"       # nom (sans tenir compte de la casse) ou identifiant
skiff task run -q "Publier le site"    # n'affiche que le résultat, pas le journal
```

`run` affiche le journal au fil de l'exécution puis une ligne `OK: …`, `FAILED: …` ou `CANCELLED: …` avec la durée. Le code de retour est 0 en cas de succès, 1 sinon (tâche inconnue comprise). L'exécution est ajoutée à l'historique de la tâche, avec le déclencheur « ligne de commande ».

## 13.10 `skiff desktop-entry`

Sous Linux et BSD, installe (ou met à jour) l'entrée de Skiff dans le menu des applications et son icône, pour l'utilisateur ; la fenêtre le fait d'elle-même à son premier lancement ([§ 1.3](01-installation.md#13-lancer-skiff)).

```bash
skiff desktop-entry           # ~/.local/share/applications/skiff.desktop et l'icône
skiff desktop-entry -remove   # les retire
```

Lancée depuis une AppImage, l'entrée pointe vers le fichier `.AppImage`. Quand le paquet `.deb` est installé, la commande n'ajoute rien (« nothing to do: the package of Skiff installed /usr/share/applications/skiff.desktop ») et retire l'entrée qu'elle avait ajoutée, qui masquerait celle du paquet.

---

[← Préférences](12-preferences.md) · [Sommaire](README.md) · [Mode web distant →](14-mode-web.md)
