# 18. Correspondance WinSCP → Skiff

[← Dépannage](17-depannage.md) · [Sommaire](README.md) · [Développement →](19-developpement.md)

Ce chapitre aide les habitués de WinSCP à retrouver chaque fonction dans Skiff.

## 18.1 Migrer

1. **Importez vos sessions** : **Sites → ⋯ → Importer… → WinSCP** (fichier `WinSCP.ini` ou registre), mots de passe compris. En ligne de commande : `skiff import winscp-ini` ou `skiff import winscp-registry`.
2. **Reprenez vos scripts** : remplacez `winscp.com` par `skiff` dans vos tâches planifiées. La syntaxe `/script=`, `/command`, `/log=`, `/parameter` est la même.
3. **Clés PuTTY** : les `.ppk` s'utilisent directement, inutile de les convertir. Pageant est pris en charge.

## 18.2 Fenêtres et commandes

| WinSCP | Skiff |
|---|---|
| Fenêtre de connexion, gestionnaire de sites | Onglet **Sites** ([chapitre 3](03-sites.md)) |
| Avancé (paramètres avancés du site) | Sections du formulaire de site : Dossiers, Environnement, Connexion, Proxy, Hôtes de rebond, SSH, FTP, TLS, S3 |
| Tunnel (onglet Connexion → Tunnel) | **Hôtes de rebond** (plusieurs étages, pour tous les protocoles) |
| Interface Commander / Explorateur | **Disposition**, ou Préférences → Apparence → Interface |
| Barre de touches F2…F10 | Identique, en bas de la fenêtre |
| Barre d'adresse, arborescence, favoris | Fil d'Ariane (`Ctrl+L`), **Arborescence des dossiers**, étoile (`Ctrl+D`) |
| Masque de filtre (`Ctrl+F`) | Identique, même syntaxe |
| Sélectionner / Désélectionner (`Num +` / `Num −` / `Num *`) | Identiques |
| Fichiers → Propriétés (`F9`) | Identique (droits, propriétaire, groupe, date, récursif, sommes de contrôle) |
| File d'attente (`F12`) | Panneau **Transferts** (`F12`) |
| Préférences de transfert, Paramètres de transfert | Boîte de transfert (`Maj+F5`), Préférences → Transferts |
| Commandes → Synchroniser (`Ctrl+S`) | **Synchroniser** (`Ctrl+S`), avec aperçu à cases à cocher |
| Commandes → Garder le dossier distant à jour (`Ctrl+U`) | Identique |
| Commandes → Comparer les dossiers (`Ctrl+=`) | Identique |
| Commandes → Rechercher des fichiers (`F3` dans WinSCP) | **Rechercher des fichiers** (`Ctrl+Maj+F`) |
| Commandes → Navigation synchronisée (`Ctrl+B`) | Identique |
| Commandes → Ouvrir un terminal / Ouvrir dans PuTTY | **Terminal SSH intégré** (`Ctrl+T`) |
| Commandes → Commandes personnalisées | Menu contextuel → **Commandes personnalisées**, mêmes motifs `!`, `!&`, `!/`, `!@`, `!U`, `!?…!` |
| Commandes → Exécuter une commande | **Exécuter une commande** (`Ctrl+Maj+X`) |
| Session → Informations serveur/protocole | Bouton ⓘ de la barre d'outils |
| Session → Journal | Panneau **Journal** |
| Session → Générer URL / code de session | Menu contextuel d'un site → **Générer URL / code…** |
| Éditeur interne | Éditeur CodeMirror (coloration, recherche, encodages, conflits) |
| Éditeur externe avec renvoi automatique | Identique (`Maj+F4`), onglet **Fichiers édités** |
| PuTTYgen | **Générer une paire de clés…**, **Convertir** (Préférences → Sécurité), `skiff keygen` |
| Pageant | Agent SSH / Pageant pris en charge (case **Utiliser l'agent SSH / Pageant**) |
| Mot de passe principal | **Mot de passe maître** (Argon2id + XChaCha20-Poly1305) |
| `WinSCP.ini` (mode portable) | Dossier `skiff-data` ou fichier `skiff.portable` à côté du binaire |
| `winscp.com /script=… /command …` | `skiff /script=…`, `skiff /command …`, `skiff run` |
| Assembly .NET, extensions | Scripts, ligne de commande, API HTTP du [mode web](14-mode-web.md) |
| Thèmes | Clair, sombre, couleur d'accent, densité |

## 18.3 Ce que Skiff ajoute

- Un seul binaire pour Windows, macOS, Linux et FreeBSD, sans installation.
- **S3** et ses compatibles, en plus de SFTP, SCP, FTP/FTPS et WebDAV.
- Chaque panneau peut afficher n'importe quelle connexion : transferts **serveur à serveur**.
- **Terminal SSH** et **terminal local** intégrés, avec historique conservé.
- **Gestionnaire de tunnels** (`-L`, `-R`, SOCKS `-D`) avec statistiques et démarrage automatique.
- **Hôtes de rebond** en chaîne, utilisables par tous les protocoles.
- **Diff visuel** entre deux fichiers, sur n'importe quelles connexions.
- **Palette de commandes** (`Ctrl+K`), **espaces de travail**, panneau d'**aperçu**.
- **Mode web distant** : la même interface dans un navigateur.
- **Conteneurs** Docker, Podman et Kubernetes ouverts comme des serveurs, en local ou par SSH.
- **Moniteur du serveur**, **terminaux divisés** avec saisie diffusée, commandes enregistrées et suivi du dossier.
- **Trousseau Skiff** et **transfert d'agent**, **tâches planifiées** avec historique, **synchronisation des réglages entre appareils**, gros fichiers SFTP en **parties parallèles**.
- **Configuration OpenSSH** (`~/.ssh/config`) lue directement, sur chaque système, comme par la commande `ssh` : alias, clés, rebonds, proxy, tunnels, politique de clés d'hôte.
- Import des sessions **FileZilla**, **PuTTY** et **OpenSSH** (`ProxyJump` et redirections compris), en plus de WinSCP.
- Interface en **français et en anglais**, thème sombre.

---

[← Dépannage](17-depannage.md) · [Sommaire](README.md) · [Développement →](19-developpement.md)
