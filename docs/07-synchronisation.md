# 7. Synchronisation

[← Transferts](06-transferts.md) · [Sommaire](README.md) · [Édition, diff et aperçu →](08-edition.md)

## 7.1 Synchroniser deux dossiers

**Synchroniser** (`Ctrl+S`, barre d'outils ou menu contextuel **Synchroniser ce dossier**) compare récursivement un dossier local et un dossier distant, puis ne transfère que ce qui a changé. Les dossiers proposés sont ceux des deux panneaux.

![Boîte de synchronisation](images/sync-dialogue.png)

| Option | Description |
|---|---|
| **Dossier local**, **Dossier distant** | Dossiers à comparer, modifiables. La connexion de chacun est rappelée entre parenthèses. |
| **Sens** | **Local → Distant** (envoi), **Distant → Local** (téléchargement) ou **Dans les deux sens** |
| **Mode** | **Standard (nouveaux et plus récents)** : transfère les fichiers absents ou plus récents à la source. **Miroir (nouveaux et différents)** : rend la destination identique à la source, même si elle est plus récente. **Dates seulement** : ne transfère rien, met à jour les dates de la destination |
| **Comparer par** | **Date de modification**, **Taille**, **Date ou taille**, **Somme de contrôle** (lit les fichiers des deux côtés), **Existence seulement** |
| **Masque de fichiers** | Fichiers et dossiers pris en compte ([syntaxe](06-transferts.md#63-masques-de-fichiers)), par exemple `* \| .git/; node_modules/; *.tmp` |
| **Supprimer les fichiers absents de la source** | Supprime à destination ce qui n'existe plus à la source. Indisponible dans les deux sens. |
| **Fichiers existants seulement** | Met à jour les fichiers présents des deux côtés, sans en créer de nouveaux |
| **Exclure les fichiers cachés** | Ignore les fichiers et dossiers commençant par un point |

**Comparer** lance l'analyse. Si le dossier de destination n'existe pas encore, il est considéré comme vide et sera créé : c'est le cas d'une première synchronisation vers un nouveau dossier. Dans les deux sens, deux fichiers différents ayant la même date sont signalés comme conflit au lieu d'être transférés.

## 7.2 Vérifier les changements

Skiff affiche ensuite la liste des actions prévues, avant de toucher à quoi que ce soit :

![Aperçu des changements](images/sync-apercu.png)

Chaque ligne indique le fichier local (taille, date), l'action, puis le fichier distant (taille, date). Le titre résume le nombre d'actions et le volume à transférer.

| Icône | Action | Raison possible |
|---|---|---|
| → | **Envoyer** | nouveau, plus récent, différent |
| ← | **Télécharger** | nouveau, plus récent, différent |
| Dossier + | **Créer le dossier distant** / **Créer le dossier local** | nouveau |
| Corbeille | **Supprimer à distance** / **Supprimer en local** | absent de la source |
| Horloge | **Mettre à jour la date distante** / **locale** | date |

Survolez l'icône pour voir l'action et la raison. Décochez les lignes à ignorer (la case d'en-tête coche ou décoche tout). Le bas de la fenêtre affiche le nombre d'actions retenues et le volume correspondant. **Synchroniser N élément(s)** exécute les actions retenues : les transferts rejoignent la [file](06-transferts.md#64-la-file-de-transferts), les suppressions et les mises à jour de dates sont faites immédiatement. **← Synchroniser** revient aux réglages.

Quand tout est déjà à jour, la fenêtre l'indique : « Les dossiers sont synchronisés ».

La synchronisation fonctionne entre n'importe quelles connexions : local et serveur, mais aussi deux serveurs (affichez-les dans les deux panneaux). Les options de transfert par défaut (mode texte, droits, `.filepart`…) s'appliquent.

## 7.3 Garder le dossier distant à jour

**Garder le dossier distant à jour** (`Ctrl+U`, éclair de la barre d'outils) surveille un dossier local et envoie chaque modification sur le serveur dès qu'elle se produit. C'est idéal pour développer en local un site hébergé à distance.

![Garder le dossier distant à jour](images/garder-a-jour.png)

| Option | Description |
|---|---|
| **Dossier local** | Dossier surveillé |
| **Dossier distant** | Dossier de destination |
| **Masque de fichiers** | Fichiers pris en compte, par exemple `* \| .git/; *.swp` |
| **Inclure les sous-dossiers** | Surveille toute l'arborescence |
| **Synchroniser avant de démarrer** | Fait d'abord une synchronisation standard (local → distant) |
| **Supprimer à distance les fichiers supprimés en local** | Répercute les suppressions |

**Démarrer** lance la surveillance. Elle continue en arrière-plan, quel que soit l'onglet affiché, et apparaît dans l'onglet **Surveillances** du panneau du bas :

![Surveillance en cours et journal des envois](images/surveillance-panneau.png)

Chaque surveillance affiche le dossier local, la destination, les compteurs d'envois et de suppressions, le journal horodaté des événements (création, modification, suppression, erreurs) et un bouton **Arrêter**. Les modifications rapprochées sont regroupées (600 ms) pour ne pas envoyer dix fois un fichier en cours d'enregistrement. L'éclair de la barre d'état indique le nombre de surveillances actives.

En script, la commande `keepuptodate` fait la même chose (voir [Scripts](13-scripts-cli.md)).


## 7.4 Tâches planifiées

Une synchronisation ou un script peut s'exécuter **tout seul** : chaque nuit, toutes les heures, en semaine à 8 h… Ouvrez l'onglet **Tâches planifiées** depuis la palette (`Ctrl+K`), le menu **⋯** de la liste des sites, ou directement depuis la boîte de synchronisation : **Planifier…** enregistre ses réglages (site, dossiers, sens, mode, critères, masque) comme une nouvelle tâche.

![Tâches planifiées : une synchronisation et son historique](images/taches.png)

**+** crée une **Synchronisation** (un site enregistré, le dossier local, le dossier distant — vide : celui du site — et les mêmes options que la [boîte de synchronisation](#71-synchroniser-deux-dossiers)) ou un **Script** (des [commandes compatibles WinSCP](13-scripts-cli.md#133-commandes-de-script), une par ligne). Puis :

| Réglage | Effet |
|---|---|
| **Activée** | Décochée, la tâche ne s'exécute qu'avec **Exécuter maintenant** |
| **Quand** | **À la main**, **Intervalle** (toutes les N minutes), **Chaque jour** à une heure, **Chaque semaine** les jours cochés à une heure, ou une expression **Cron** (`minute heure jour mois jour-de-semaine`, avec `*`, `1-5`, `*/15`, `1,3,5` ; 0 et 7 sont le dimanche) |
| **Rattraper au démarrage…** | Si Skiff était fermé à l'heure prévue, la tâche s'exécute une fois à l'ouverture ; sinon l'exécution manquée est sautée |
| **Notification** | **En cas d'échec** (par défaut), **Après chaque exécution** ou **Jamais** : une notification résume le résultat |

La liste indique pour chaque tâche la prochaine exécution et une pastille de la dernière (verte : réussie, rouge : en échec, orange : arrêtée). **Exécuter maintenant** la lance tout de suite, **Arrêter** l'interrompt. L'**Historique** garde les 200 dernières exécutions, avec leur déclencheur (planifiée, rattrapée, à la main, ligne de commande), leur résumé (fichiers transférés, supprimés, erreurs) et leur journal complet, déplié d'un clic.

Une tâche s'exécute **sans poser de question**, dans sa propre connexion : une clé d'hôte inconnue, un mot de passe ou une phrase secrète manquants la font échouer avec un message qui dit quoi faire (se connecter une fois depuis Skiff pour accepter la clé, enregistrer le mot de passe dans le site, ou utiliser le [trousseau](11-securite.md#117-trousseau-skiff-et-transfert-dagent)). Une synchronisation n'écrase jamais en demandant : elle suit ses critères.

**Skiff fermé** : les tâches s'exécutent tant que Skiff est ouvert (fenêtre ou `skiff serve`). Pour qu'elles tournent aussi Skiff fermé, confiez la commande affichée sous la planification au planificateur du système ; le résultat s'ajoute au même historique ([§ 13.9](13-scripts-cli.md#139-skiff-task)) :

```bash
# Linux, macOS (crontab -e) : tous les jours à 2 h
0 2 * * * /usr/local/bin/skiff task run -q "Publier le site"
```

```powershell
# Windows : Planificateur de tâches
schtasks /Create /SC DAILY /ST 02:00 /TN "Skiff - Publier le site" /TR "\"C:\Program Files\Skiff\skiff.exe\" task run -q \"Publier le site\""
```

---

[← Transferts](06-transferts.md) · [Sommaire](README.md) · [Édition, diff et aperçu →](08-edition.md)
