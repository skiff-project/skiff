# 8. Édition, diff et aperçu

[← Synchronisation](07-synchronisation.md) · [Sommaire](README.md) · [Terminal, tunnels, proxys et rebonds →](09-terminal-tunnels.md)

## 8.1 L'éditeur intégré

`F4` (ou **Éditer**, ou un double-clic si le réglage le prévoit) ouvre un fichier, local ou distant, dans un onglet d'éditeur. L'éditeur repose sur CodeMirror 6.

![Éditeur intégré avec une modification non enregistrée](images/editeur.png)

| Élément | Rôle |
|---|---|
| Onglet | Icône du type de fichier et nom. Un point signale des modifications non enregistrées. |
| Barre du haut | Chemin du fichier, badge **Modifié**, puis **Rechercher / remplacer** (`Ctrl+F`), **Retour à la ligne**, **Recharger**, **Éditer avec un éditeur externe** et **Enregistrer** (`Ctrl+S`) |
| Zone d'édition | Numéros de ligne, repli de code, coloration syntaxique (plus de 100 langages, détectés d'après le nom : HTML, CSS, JavaScript, TypeScript, PHP, Python, Go, Rust, Java, C/C++, SQL, YAML, JSON, Markdown, Dockerfile, Nginx, Shell…), correspondance des parenthèses, fermeture automatique, sélections multiples, sélection rectangulaire (`Alt` + glisser), autocomplétion (`Ctrl+Espace`) |
| Barre d'état | Position (**Ln 3, Col 28**), langage, **Encodage**, case **BOM**, **Fins de ligne**, taille du fichier, **Aller à la ligne** |

Raccourcis de l'éditeur :

| Touche | Effet |
|---|---|
| `Ctrl+S` | Enregistrer (le fichier est renvoyé sur le serveur) |
| `Ctrl+F` | Rechercher et remplacer |
| `F3` ou `Ctrl+G`, `Maj+F3` ou `Ctrl+Maj+G` | Occurrence suivante / précédente |
| `Ctrl+Maj+L` | Sélectionner toutes les occurrences de la sélection |
| `Ctrl+Alt+G` | Aller à la ligne |
| `Ctrl+Z`, `Ctrl+Y` (`Ctrl+Maj+Z`) | Annuler, rétablir |
| `Ctrl+D` | Sélectionner l'occurrence suivante |
| `Tab`, `Maj+Tab` | Indenter, désindenter |
| `Ctrl+/` | Commenter la ligne |
| `Alt+↑`, `Alt+↓` | Déplacer la ligne |
| `Ctrl+Maj+[`, `Ctrl+Maj+]` | Replier, déplier |

### Encodage et fins de ligne

Skiff détecte l'encodage (UTF-8, UTF-16 avec BOM, sinon Windows-1252) et les fins de ligne du fichier, et les conserve à l'enregistrement. Pour les changer :

- **Encodage** : UTF-8, Windows-1252, ISO-8859-1, ISO-8859-15, Windows-1250, Windows-1251, KOI8-R, CP850, UTF-16LE, UTF-16BE. Sans modification en cours, changer d'encodage relit le fichier ; sinon, le fichier sera converti à l'enregistrement.
- **BOM** : ajoute ou retire la marque d'ordre des octets.
- **Fins de ligne** : LF, CRLF ou CR.

Un fichier qui semble binaire affiche un avertissement (« Le modifier risque de le corrompre »). Les fichiers plus gros que la limite (20 Mo par défaut, **Préférences → Éditeur**) ne sont pas ouverts dans l'éditeur.

`F3` (**Afficher**) ouvre le même éditeur en **lecture seule**.

Fermer un onglet modifié propose **Enregistrer**, **Ne pas enregistrer** ou **Annuler**.

## 8.2 Conflits à l'enregistrement

Skiff mémorise la date du fichier à l'ouverture. Si quelqu'un d'autre l'a modifié sur le serveur entre-temps, l'enregistrement est bloqué et un bandeau l'indique :

![Conflit : le fichier a été modifié sur le serveur](images/editeur-conflit.png)

- **Recharger** abandonne vos modifications et relit la version du serveur ;
- **Écraser la version du serveur** enregistre quand même votre version.

Pour comparer les deux versions avant de choisir, copiez votre texte, rechargez, puis utilisez le [diff](#85-diff-visuel).

## 8.3 Rechercher et remplacer

`Ctrl+F` ouvre le panneau de recherche en bas de l'éditeur. Les occurrences sont surlignées dans le texte :

![Recherche dans l'éditeur](images/editeur-recherche.png)

Boutons : **suivant**, **précédent**, **tout** (sélectionne toutes les occurrences pour les modifier ensemble), **remplacer**, **tout remplacer**. Options : **respecter la casse**, **expression régulière** (avec `$1`, `$2` dans le remplacement), **mot entier**. `Échap` ferme le panneau.

## 8.4 Éditeur externe

**Éditer avec un éditeur externe** (`Maj+F4`) ouvre le fichier dans l'application de votre choix : Visual Studio Code, Notepad++, Sublime Text, vim… Pour un fichier distant, Skiff le télécharge dans un dossier temporaire, lance l'éditeur, puis **renvoie le fichier sur le serveur à chaque enregistrement**, comme WinSCP.

L'éditeur utilisé est, dans l'ordre :

1. celui d'une **association** correspondant au fichier (Préférences → Éditeur → Associations, par exemple `*.psd` → `gimp !.!`) ;
2. la **Commande de l'éditeur externe** des préférences (`code --wait !.!`, où `!.!` est remplacé par le chemin du fichier) ;
3. l'application associée par le système.

Les fichiers suivis apparaissent dans l'onglet **Fichiers édités** du panneau du bas (`Ctrl+Maj+Q`) :

![Fichiers ouverts dans un éditeur externe](images/editions-externes.png)

Pour chaque fichier : la copie locale temporaire, le fichier distant, le nombre d'**Envois** (avec une coche verte après le dernier envoi réussi) et **Arrêter**, qui cesse la surveillance. Une erreur d'envoi est signalée par une notification.

Pour faire de l'éditeur externe le choix par défaut de `F4` et du double-clic : **Préférences → Éditeur → Éditeur par défaut → Application externe**. L'édition externe n'existe pas en mode web.

## 8.5 Diff visuel

**Comparer avec le fichier de l'autre panneau** (menu contextuel d'un fichier) ouvre un onglet de comparaison entre deux fichiers : local et distant, ou sur deux serveurs.

![Diff visuel entre la version locale et la version du serveur](images/diff.png)

- En-tête : la connexion et le chemin de chaque fichier, un bouton pour ouvrir chacun dans l'éditeur, le nombre de **modifications** et **Enregistrer** (fichier de droite).
- Les lignes supprimées sont surlignées en rouge à gauche, les lignes ajoutées en vert à droite, avec le détail des mots modifiés.
- Les longues portions identiques sont repliées (cliquez pour les déplier).
- Les flèches entre les deux côtés recopient un bloc de gauche vers la droite (fusion par blocs).
- Le côté droit est modifiable : corrigez-le puis **Enregistrer**. Comme dans l'éditeur, un conflit est détecté si le fichier a changé entre-temps.

## 8.6 Aperçu

Le [panneau d'aperçu](04-interface.md#49-panneau-daperçu) (`Ctrl+Maj+P`) affiche les images, vidéos, sons, PDF, fichiers Markdown et textes, en local comme sur un serveur, sans ouvrir d'onglet.

---

[← Synchronisation](07-synchronisation.md) · [Sommaire](README.md) · [Terminal, tunnels, proxys et rebonds →](09-terminal-tunnels.md)
