# Documentation de Skiff

Skiff est un client de transfert de fichiers moderne et multiplateforme, livré en **un seul binaire**. Il gère SFTP, SCP, FTP/FTPS, WebDAV et S3 et reprend les fonctionnalités de WinSCP : interface Commander à deux panneaux, file de transferts, synchronisation, éditeur intégré, scripts compatibles `winscp.com` et import des sessions. Il y ajoute un terminal SSH, la gestion des tunnels et des hôtes de rebond, un diff visuel, une palette de commandes et un mode web distant.

![Skiff connecté à un serveur SFTP, en interface Commander](images/commander.png)

Cette documentation décrit chaque fonctionnalité, écran par écran. Les captures ont été prises sur Skiff 0.1.0 en français, avec le thème clair, sauf mention contraire.

## Sommaire

| # | Chapitre | Contenu |
|---|---|---|
| 1 | [Installation et démarrage](01-installation.md) | Plateformes, compilation, premier lancement, dossier de configuration, mode portable |
| 2 | [Premiers pas](02-premiers-pas.md) | Écran d'accueil, connexion rapide, première connexion, visite de l'interface |
| 3 | [Gestionnaire de sites](03-sites.md) | Sites, dossiers, couleurs, récents, espaces de travail, toutes les options de connexion, configuration OpenSSH (`~/.ssh/config`), import, export, URL et code de session |
| 4 | [Interface et navigation](04-interface.md) | Onglets, barre d'outils, panneaux, fil d'Ariane, favoris, historique, arborescence, filtre, sélection, aperçu, comparaison de dossiers, navigation synchronisée |
| 5 | [Opérations sur les fichiers](05-fichiers.md) | Menus contextuels, renommer, créer, supprimer, corbeille, liens, propriétés et droits, sommes de contrôle, presse-papiers, glisser-déposer, commandes, archives, place prise |
| 6 | [Transferts](06-transferts.md) | Copier et déplacer, options de transfert, file d'attente, écrasement, reprise, erreurs, transferts serveur à serveur |
| 7 | [Synchronisation](07-synchronisation.md) | Synchroniser, aperçu des changements, garder le dossier distant à jour, tâches planifiées |
| 8 | [Édition, diff et aperçu](08-edition.md) | Éditeur intégré, conflits, recherche, éditeur externe, diff visuel, projets, versions précédentes |
| 9 | [Terminal, tunnels, proxys et rebonds](09-terminal-tunnels.md) | Terminal SSH et local, redirections de ports, SOCKS, hôtes de rebond, proxys, moniteur du serveur, services, journaux en direct, ports |
| 10 | [Recherche, journal et palette](10-recherche-journal.md) | Recherche de fichiers, journal de session, informations serveur, palette de commandes |
| 11 | [Sécurité](11-securite.md) | Clés d'hôte, certificats TLS, mots de passe enregistrés, mot de passe maître, verrouillage, générateur de clés SSH |
| 12 | [Préférences](12-preferences.md) | Les quatorze sections des préférences, thèmes, densité, langue, configuration OpenSSH, synchronisation entre appareils |
| 13 | [Scripts et ligne de commande](13-scripts-cli.md) | CLI, scripts compatibles WinSCP, console interactive, `keygen`, `import`, `task` |
| 14 | [Mode web distant](14-mode-web.md) | `skiff serve`, authentification, TLS, confinement, déploiement |
| 15 | [Protocoles](15-protocoles.md) | Particularités de SFTP, SCP, FTP/FTPS, WebDAV et S3, URL de session |
| 16 | [Raccourcis clavier](16-raccourcis.md) | Tous les raccourcis |
| 17 | [Dépannage](17-depannage.md) | Échecs de connexion, coupures, reconnexion, journaux, questions fréquentes |
| 18 | [Correspondance WinSCP → Skiff](18-winscp.md) | Où retrouver chaque fonction de WinSCP |
| 19 | [Développement](19-developpement.md) | Architecture, compilation, tests, régénération des captures d'écran |

## Conventions

- Les noms de menus, de boutons et d'options sont écrits **en gras**, tels qu'ils apparaissent dans l'interface.
- Les touches sont notées `Ctrl+K`. Sous macOS, `Ctrl` correspond à `⌘ Cmd`.
- `~` désigne votre dossier personnel. Skiff l'affiche ainsi dans les chemins locaux et le comprend quand vous le tapez.
- Les exemples utilisent un site de démonstration, « Site vitrine (SFTP) », dont les fichiers sont dans `~/Projets/site-vitrine` en local et `/config/www` sur le serveur.
