# 1. Installation et démarrage

[← Sommaire](README.md) · [Chapitre suivant →](02-premiers-pas.md)

## 1.1 Un seul binaire

Skiff tient dans un exécutable unique : interface, moteur de transferts, clients des protocoles, terminal et serveur web sont inclus. Il n'y a rien à installer à côté, sauf le moteur web système pour la fenêtre native (voir ci-dessous).

| Plateforme | Binaire | Fenêtre |
|---|---|---|
| Windows 10/11 (x64, ARM64) | `skiff-windows-amd64.exe`, `skiff-windows-arm64.exe` | Fenêtre native **WebView2** (préinstallé sur Windows 10/11) |
| macOS (Intel et Apple Silicon) | `Skiff.app` universel | Fenêtre native **WKWebView** |
| Linux (x64, ARM64) | `skiff-linux-amd64-desktop` | Fenêtre native **WebKitGTK** (paquet `libwebkit2gtk-4.1` à l'exécution) |
| Linux, FreeBSD, toutes | `skiff-<os>-<arch>` (variante portable) | Fenêtre d'application Chrome, Edge ou Brave si l'un d'eux est installé, sinon le navigateur par défaut |

La variante portable ne dépend d'aucune bibliothèque : elle sert aussi pour le [mode web distant](14-mode-web.md) sur un serveur ou un NAS.

## 1.2 Compiler depuis les sources

Prérequis : Go 1.27 ou plus récent, Node 22 ou plus récent et pnpm (uniquement pour compiler l'interface).

```bash
./build.sh            # compile l'interface puis bin/skiff (équivalent : make)
./build.sh portable   # bin/skiff sans fenêtre native (aucune dépendance)
./build.sh release    # tous les binaires de toutes les plateformes dans dist/
```

Sous Linux, pour obtenir la fenêtre native, installez d'abord les en-têtes de WebKitGTK. `build.sh` les détecte tout seul :

```bash
sudo apt install -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config
```

Sans ces en-têtes, la compilation réussit quand même et produit la variante qui ouvre une fenêtre de navigateur.

La CI GitHub Actions (`.github/workflows/build.yml`) compile les binaires natifs des trois systèmes et publie une release à chaque tag `v*`.

## 1.3 Lancer Skiff

```bash
skiff                                  # ouvre la fenêtre
skiff sftp://alex@serveur.example.com  # ouvre la fenêtre et se connecte
```

Au premier lancement, aucun site n'est enregistré. L'écran d'accueil propose de créer un site, d'importer vos sessions WinSCP, FileZilla, PuTTY ou OpenSSH, ou de se connecter tout de suite avec une URL.

![Premier lancement : aucun site enregistré](images/accueil-premier-lancement.png)

La suite est décrite au chapitre [Premiers pas](02-premiers-pas.md). Les autres commandes (`serve`, `run`, `shell`, `keygen`, `import`, `version`) sont décrites au chapitre [Scripts et ligne de commande](13-scripts-cli.md).

## 1.4 Dossier de configuration

Skiff range ses réglages dans un seul dossier :

| Système | Emplacement par défaut |
|---|---|
| Linux, FreeBSD | `~/.config/Skiff` (ou `$XDG_CONFIG_HOME/Skiff`) |
| Windows | `%APPDATA%\Skiff` |
| macOS | `~/Library/Application Support/Skiff` |

Le chemin exact est affiché en bas de la fenêtre **Préférences** et dans **Préférences → À propos de Skiff**.

| Fichier | Contenu |
|---|---|
| `sites.json` | Sites, dossiers et espaces de travail. Les mots de passe y sont chiffrés. |
| `settings.json` | Préférences, favoris, historique des dossiers, commandes personnalisées |
| `secret.key` | Clé de chiffrement des mots de passe quand il n'y a pas de mot de passe maître (droits 600) |
| `master.json` | Paramètres de dérivation du mot de passe maître (sel, coût Argon2id), sans le mot de passe |
| `known_hosts` | Clés d'hôte SSH acceptées (format OpenSSH) |
| `trusted_certs.json` | Certificats TLS auxquels vous avez fait confiance |
| `keychain.json` | [Trousseau Skiff](11-securite.md#117-trousseau-skiff-et-transfert-dagent) : clés privées chiffrées comme les mots de passe |
| `tasks.json`, `task-runs.json` | [Tâches planifiées](07-synchronisation.md#74-tâches-planifiées) et leur historique |
| `devices.json`, `sync-tombstones.json` | Réglages de la [synchronisation entre appareils](12-preferences.md#128-appareils) (phrase secrète chiffrée) et suppressions à propager |
| `logs/` | Journaux de session, si l'option est activée |
| `web-cert.pem`, `web-key.pem` | Certificat auto-signé du mode web |

`sites.json` peut être copié entre machines sans exposer les mots de passe : sans `secret.key` (ou sans le mot de passe maître), ils restent illisibles. Pour partager sites, mots de passe et préférences entre vos ordinateurs, utilisez plutôt [Préférences → Appareils](12-preferences.md#128-appareils). Voir [Sécurité](11-securite.md).

## 1.5 Mode portable

Comme le fichier `WinSCP.ini` de WinSCP, Skiff peut garder toute sa configuration à côté de l'exécutable, par exemple sur une clé USB. Il suffit de créer, dans le dossier du binaire :

- soit un dossier `skiff-data` ;
- soit un fichier vide `skiff.portable` (le dossier `skiff-data` est alors créé au premier lancement).

Le bas de la fenêtre des préférences indique alors « (mode portable) ».

## 1.6 Variables d'environnement

| Variable | Effet |
|---|---|
| `SKIFF_HOME` | Force le dossier de configuration (prioritaire sur tout le reste) |
| `SSH_AUTH_SOCK` | Socket de l'agent SSH à utiliser (Linux, macOS) |
| `LANG`, `LC_ALL` | Langue du système, utilisée par l'interface quand la préférence **Langue** est sur **Auto** |

En option `/ini=<dossier>`, la syntaxe de `winscp.com` permet aussi de choisir le dossier de configuration (voir [Scripts](13-scripts-cli.md#135-compatibilité-winscpcom)).

## 1.7 Mise à jour et désinstallation

Pour mettre à jour, remplacez le binaire : la configuration est conservée. Pour désinstaller, supprimez le binaire puis, si vous le souhaitez, le dossier de configuration.

---

[← Sommaire](README.md) · [Chapitre suivant →](02-premiers-pas.md)
