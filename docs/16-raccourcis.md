# 16. Raccourcis clavier

[← Protocoles](15-protocoles.md) · [Sommaire](README.md) · [Dépannage →](17-depannage.md) · 🇬🇧 [English](en/16-shortcuts.md)

Sous macOS, `Ctrl` correspond à `⌘ Cmd`. La liste est aussi affichée dans **Préférences → Raccourcis clavier**.

## 16.1 Partout

| Touche | Action |
|---|---|
| `Ctrl+K` | Palette de commandes |
| `Ctrl+,` | Préférences |
| `Ctrl+N` | Nouvelle session (onglet Sites) |
| `Ctrl+Tab`, `Ctrl+PgSuiv` | Onglet suivant |
| `Ctrl+Maj+Tab`, `Ctrl+PgPréc` | Onglet précédent |
| `Alt+1` … `Alt+9` | Aller à l'onglet n |
| `Ctrl+W` | Fermer l'onglet (session, éditeur, diff) |
| `Ctrl+Maj+P` | Panneau d'aperçu |
| `Ctrl+Maj+Q` | Ouvrir ou fermer le panneau du bas |
| `Échap` | Fermer la boîte de dialogue, le menu ou la palette |
| `Entrée` | Valider la boîte de dialogue |

## 16.2 Panneaux de fichiers

| Touche | Action |
|---|---|
| `Tab` | Passer à l'autre panneau |
| `↑` `↓` `PgPréc` `PgSuiv` `Début` `Fin` | Déplacer le curseur |
| `Entrée` | Ouvrir |
| `Retour arrière`, `Alt+↑` | Dossier parent |
| `Alt+←`, `Alt+→` | Précédent, suivant |
| Lettres | Recherche par saisie |
| `Espace` | Sélectionner ou désélectionner l'élément |
| `Inser` | Sélectionner ou désélectionner, puis passer au suivant |
| `Maj+↑`, `Maj+↓`, `Maj+clic` | Étendre la sélection |
| `Ctrl+clic` | Ajouter ou retirer de la sélection |
| `Ctrl+A`, `Ctrl+Maj+A` | Tout sélectionner, tout désélectionner |
| `Num +`, `Num −`, `Num *` | Sélectionner par masque, désélectionner par masque, inverser la sélection |
| `Échap` | Désélectionner, puis effacer le filtre |
| `F2`, `Maj+F6` | Renommer |
| `F3` | Afficher |
| `F4`, `Maj+F4` | Éditer, éditer avec un éditeur externe |
| `F5` | Copier vers l'autre panneau |
| `Maj+F5` | Copier avec les options |
| `F6` | Déplacer vers l'autre panneau |
| `F7`, `Maj+F7` | Nouveau dossier, nouveau fichier |
| `F8`, `Suppr` | Supprimer (corbeille si possible) |
| `Maj+Suppr` | Supprimer définitivement |
| `F9` | Propriétés |
| `F10` | Fermer l'onglet |
| `Maj+F10`, touche Menu | Menu contextuel |
| `F12` | File de transferts |
| `Ctrl+C`, `Ctrl+X`, `Ctrl+V` | Copier, couper, coller des fichiers |
| `Ctrl+Maj+C` | Copier le chemin |
| `Ctrl+R`, `Ctrl+F5` | Actualiser |
| `Ctrl+F` | Filtrer |
| `Ctrl+L` | Saisir un chemin |
| `Ctrl+O` | Ouvrir un dossier |
| `Ctrl+D` | Ajouter ou retirer des favoris |
| `Ctrl+Alt+H` | Afficher les fichiers cachés |
| `Ctrl+S` | Synchroniser |
| `Ctrl+=` | Comparer les dossiers |
| `Ctrl+B` | Navigation synchronisée |
| `Ctrl+U` | Garder le dossier distant à jour |
| `Ctrl+Maj+F` | Rechercher des fichiers |
| `Ctrl+T` | Ouvrir un terminal |
| `Ctrl+Maj+X` | Exécuter une commande |

Pendant un glisser-déposer : `Ctrl` (ou `Alt`) force la copie, `Maj` force le déplacement.

## 16.3 Éditeur et diff

| Touche | Action |
|---|---|
| `Ctrl+S` | Enregistrer |
| `Ctrl+F` | Rechercher et remplacer |
| `F3`, `Ctrl+G` / `Maj+F3`, `Ctrl+Maj+G` | Occurrence suivante / précédente |
| `Ctrl+Maj+L` | Sélectionner toutes les occurrences |
| `Ctrl+D` | Ajouter l'occurrence suivante à la sélection |
| `Ctrl+Alt+G` | Aller à la ligne |
| `Ctrl+Z`, `Ctrl+Y`, `Ctrl+Maj+Z` | Annuler, rétablir |
| `Tab`, `Maj+Tab` | Indenter, désindenter |
| `Ctrl+/` | Commenter ou décommenter |
| `Alt+↑`, `Alt+↓` | Déplacer la ligne |
| `Maj+Alt+↑`, `Maj+Alt+↓` | Dupliquer la ligne |
| `Ctrl+Maj+K` | Supprimer la ligne |
| `Ctrl+Maj+[`, `Ctrl+Maj+]` | Replier, déplier |
| `Ctrl+Espace` | Autocomplétion |
| `Alt` + glisser | Sélection rectangulaire |
| `Échap` | Fermer le panneau de recherche |

### Dans un projet

| Touche | Action |
|---|---|
| `Ctrl+Maj+E` | Ouvrir le dossier du panneau actif comme projet |
| `Ctrl+P` | Aller au fichier |
| `Ctrl+Maj+F` | Rechercher dans le projet |
| `Ctrl+Alt+S` | Tout enregistrer |
| `` Ctrl+` `` | Afficher ou masquer le terminal |
| `Ctrl+W` | Fermer le fichier affiché |

## 16.4 Terminal

| Touche | Action |
|---|---|
| `Ctrl+C` | Copier la sélection ; sans sélection, interrompre le programme |
| `Ctrl+V` | Coller |
| `Ctrl+Maj+C` | Copier la sélection |
| `Ctrl+Maj+V` | Coller |
| Clic droit | Copier la sélection, ou coller s'il n'y en a pas |
| `Ctrl+Maj+F` | Rechercher dans le terminal |

`Ctrl+C` et `Ctrl+V` vont toujours au programme avec le réglage **Ctrl+Maj+C / Ctrl+Maj+V seulement**, et le clic droit ouvre un menu avec **Clic droit → Menu** ([§ 12.6](12-preferences.md#126-terminal)). Les autres touches sont envoyées au shell.

## 16.5 Liste des sites et palette

| Touche | Action |
|---|---|
| `↑`, `↓` | Site ou commande précédent, suivant |
| `Entrée` | Se connecter (à tous les sites sélectionnés), exécuter la commande |
| `Suppr` | Supprimer le site sélectionné, ou les sites sélectionnés |
| `Ctrl+clic`, `Maj+clic` | Ajouter ou retirer un site de la sélection, sélectionner une plage |
| `Maj+↑`, `Maj+↓` | Étendre la sélection |
| `Ctrl+A` | Sélectionner tous les sites affichés |
| `Échap` | Effacer la sélection |
| `Ctrl+S` | Enregistrer le site en cours de modification |

---

[← Protocoles](15-protocoles.md) · [Sommaire](README.md) · [Dépannage →](17-depannage.md) · 🇬🇧 [English](en/16-shortcuts.md)
