# 11. Sécurité

[← Recherche, journal et palette](10-recherche-journal.md) · [Sommaire](README.md) · [Préférences →](12-preferences.md)

## 11.1 Clés d'hôte SSH

Chaque serveur SSH possède une clé d'hôte qui prouve son identité. Skiff la vérifie à chaque connexion, pour le serveur comme pour chaque hôte de rebond.

- Les clés acceptées sont enregistrées dans le fichier `known_hosts` de Skiff (dossier de configuration, format OpenSSH).
- Le `~/.ssh/known_hosts` d'OpenSSH est aussi consulté, en lecture seule : un serveur déjà connu d'OpenSSH ne déclenche aucune question.

**Serveur inconnu** : à la première connexion, Skiff affiche l'hôte, le type de clé et les empreintes SHA-256 et MD5, et propose **Accepter** (enregistre la clé), **Se connecter une fois** ou **Annuler** :

![Clé d'hôte inconnue](images/connexion-cle-hote.png)

**Clé d'hôte modifiée** : si un serveur présente une clé différente de celle enregistrée, Skiff affiche une alerte rouge, avec l'ancienne et la nouvelle empreinte. C'est le signe d'une réinstallation du serveur… ou d'une interception. Les choix sont **Mettre à jour la clé**, **Se connecter une fois** et **Annuler**. Dans le doute, annulez et vérifiez auprès de l'administrateur.

**Épinglage** : dans le site (**SSH → Empreinte attendue de la clé d'hôte**), une empreinte `SHA256:…` fixe la clé attendue. Toute autre clé est alors refusée sans question. En script, l'option `-hostkey=` de `open` fait de même.

La liste des clés enregistrées se trouve dans **Préférences → Sécurité → Clés d'hôtes connues**. La corbeille d'une ligne supprime la clé (la question sera reposée à la prochaine connexion).

## 11.2 Certificats TLS

Pour FTPS, WebDAV en HTTPS et S3 en HTTPS, Skiff vérifie le certificat du serveur avec les autorités de certification du système. Un certificat auto-signé, expiré ou émis pour un autre nom déclenche une question :

![Certificat non fiable](images/connexion-certificat.png)

La boîte rappelle la raison du refus (« certificate signed by unknown authority »), puis l'**Hôte**, le **Sujet**, l'**Émetteur**, les **Noms** couverts, la période de **Validité** et l'empreinte SHA-256.

| Bouton | Effet |
|---|---|
| **Toujours faire confiance** | Enregistre le certificat (`trusted_certs.json`) : les connexions suivantes l'acceptent sans question |
| **Faire confiance cette fois** | Accepte le certificat pour cette connexion seulement |
| **Annuler** | Abandonne la connexion |

Dans le site, section **TLS**, l'**Empreinte attendue du certificat** épingle un certificat précis, et **Ne pas vérifier les certificats (non sécurisé)** désactive toute vérification (à réserver aux tests).

## 11.3 Mots de passe enregistrés

Les mots de passe, phrases secrètes, mots de passe de proxy et jetons enregistrés sont chiffrés avec **XChaCha20-Poly1305** avant d'être écrits dans `sites.json`.

- **Sans mot de passe maître**, la clé de chiffrement est un fichier aléatoire, `secret.key`, lisible par vous seul (droits 600). `sites.json` peut être sauvegardé ou synchronisé : sans `secret.key`, les mots de passe restent illisibles.
- **Avec un mot de passe maître**, la clé est dérivée de ce mot de passe par **Argon2id** (3 passes, 64 Mio, 2 fils) et n'est jamais écrite sur le disque.

Dans tous les cas, l'interface ne reçoit jamais les mots de passe enregistrés : elle sait seulement qu'un mot de passe existe (« Enregistré — tapez pour remplacer »). Les exports de sites ne contiennent jamais de mot de passe.

## 11.4 Mot de passe maître

**Préférences → Sécurité → Définir un mot de passe maître** :

![Définir un mot de passe maître](images/mot-de-passe-maitre.png)

Saisissez deux fois le mot de passe (8 caractères au minimum). Une jauge indique sa robustesse. Une fois défini, la section Sécurité affiche **Activé** et propose **Changer le mot de passe maître**, **Supprimer le mot de passe maître** et **Verrouiller les mots de passe maintenant** :

![Section Sécurité avec un mot de passe maître](images/prefs-securite-maitre.png)

Au démarrage suivant (ou après **Verrouiller les mots de passe maintenant**, aussi disponible dans la palette `Ctrl+K`), les mots de passe enregistrés sont **verrouillés**. Un bandeau le signale en haut de la liste des sites :

![Mots de passe verrouillés](images/mots-de-passe-verrouilles.png)

Cliquez sur **Déverrouiller** (ou connectez-vous à un site qui a besoin d'un mot de passe) pour saisir le mot de passe maître :

![Déverrouiller les mots de passe](images/deverrouiller.png)

**Plus tard** ferme la boîte sans déverrouiller (et n'ouvre pas la connexion demandée, le cas échéant). Les sites sans mot de passe enregistré, qui utilisent une clé ou l'agent SSH, restent utilisables tant que les mots de passe sont verrouillés. Changer le mot de passe maître rechiffre tous les secrets. Le supprimer revient au chiffrement par `secret.key`.

Un mot de passe maître oublié ne peut pas être récupéré : il faut alors ressaisir les mots de passe des sites.

## 11.5 Générateur de clés SSH

**Générer une paire de clés…** (écran d'accueil, menu ⋯ des sites, palette, Préférences → Sécurité) crée une nouvelle clé, comme `ssh-keygen` ou PuTTYgen :

![Générateur de clés](images/cles-generateur.png)

| Champ | Description |
|---|---|
| **Type de clé** | **Ed25519 (recommandé)**, **ECDSA** (256, 384 ou 521 bits), **RSA** (2048, 3072, 4096 ou 8192 bits) |
| **Commentaire** | Texte ajouté à la clé publique (`utilisateur@machine` par défaut) |
| **Phrase secrète** | Chiffre la clé privée (recommandé). Une jauge indique sa robustesse. |
| **Format de la clé privée** | **OpenSSH** ou **PuTTY .ppk (v3)** |
| **Enregistrer la clé privée dans** | Chemin du fichier (`~/.ssh/id_ed25519_skiff` par défaut). La clé publique est écrite à côté avec l'extension `.pub`. Un fichier existant n'est jamais écrasé. |

**Générer** affiche le résultat :

![Clé générée](images/cles-resultat.png)

Le type, l'empreinte et l'emplacement de la clé, puis la **Clé publique (pour authorized_keys)** à copier. Si une session SFTP ou SCP est ouverte, **Installer sur le serveur courant** ajoute la clé publique au fichier `~/.ssh/authorized_keys` du serveur (en créant `~/.ssh` avec les bons droits si besoin), comme `ssh-copy-id`.

En ligne de commande : `skiff keygen` (voir [Scripts](13-scripts-cli.md#137-skiff-keygen)).

## 11.6 Clés existantes et conversion

**Préférences → Sécurité → Clés SSH** liste les clés privées de `~/.ssh` avec leur type, leur empreinte, leur format (`openssh` ou `ppk`) et un cadenas si elles sont protégées par une phrase secrète. **Convertir** transforme une clé OpenSSH en clé PuTTY `.ppk`, ou l'inverse :

![Conversion d'une clé OpenSSH en PuTTY .ppk](images/cles-conversion.png)

Indiquez la phrase secrète actuelle (si la clé est chiffrée), la phrase secrète de la clé convertie (vide : aucune) et le fichier de destination. La clé d'origine n'est pas modifiée. En ligne de commande : `skiff keygen -convert`.

Skiff lit directement les deux formats pour se connecter : la conversion ne sert qu'à échanger des clés avec d'autres logiciels (PuTTY, WinSCP, OpenSSH).

## 11.7 Sécurité de l'application

- **Fenêtre de bureau** : l'interface communique avec le moteur de Skiff par un serveur HTTP qui n'écoute que sur `127.0.0.1`, protégé par un jeton aléatoire à chaque lancement, un contrôle de l'en-tête `Host` (contre le *DNS rebinding*) et un en-tête anti-CSRF obligatoire.
- Les fichiers HTML et SVG prévisualisés ne sont jamais exécutés dans le contexte de l'application.
- **Mode web** : voir [Mode web distant](14-mode-web.md#143-sécurité).

---

[← Recherche, journal et palette](10-recherche-journal.md) · [Sommaire](README.md) · [Préférences →](12-preferences.md)
