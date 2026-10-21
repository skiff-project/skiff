# Privacy policy

🇫🇷 [Version française](#politique-de-confidentialité) · Last updated: 21 October 2026

Skiff is a file transfer client made by Laterna, free and open source software (Apache License 2.0): https://github.com/skiff-project/skiff. This policy applies to every version of Skiff, whatever the way you installed it (installer, disk image, package, Microsoft Store, binary alone).

**Skiff collects nothing.** It has no account, no telemetry, no statistics, no advertising and no crash reports, and it sends no data to Laterna or to anyone else.

## What Skiff connects to

Only to what you ask it to connect to:

- the servers of your sites and of the links you open (SFTP, SCP, FTP/FTPS, WebDAV, S3, containers), and the jump hosts and proxies you set for them;
- the file you choose on one of your own servers to synchronize your settings between your devices, encrypted on your device before it leaves it;
- the web pages of a server you open from **Ports of the server**, in your browser.

In remote web mode (`skiff serve`), the interface is served by the Skiff you run, to the browsers you sign in from.

## What Skiff keeps on your device

In its configuration folder (see the documentation, chapter 1), on your device only:

- your sites and preferences; the saved passwords, passphrases and keys are encrypted (XChaCha20-Poly1305), with a key kept in that folder or derived from your master password (Argon2id);
- the host keys and certificates you accepted;
- the session logs, only if you turn them on;
- the previous versions of the files you save in its editor (the last 20 of each, for 30 days), which you can turn off;
- temporary copies of the files you edit with another application, while you edit them.

Deleting the configuration folder deletes all of it.

## The stores

When you install Skiff from a store (the Microsoft Store, for one), the store handles the download, the updates and what it records about them under its own privacy policy. Skiff itself adds nothing.

## Contact

Questions about this policy: an issue on https://github.com/skiff-project/skiff/issues. Security problems: [SECURITY.md](SECURITY.md).

---

# Politique de confidentialité

🇬🇧 [English version](#privacy-policy) · Dernière mise à jour : 21 octobre 2026

Skiff est un client de transfert de fichiers édité par Laterna, logiciel libre (licence Apache 2.0) : https://github.com/skiff-project/skiff. Cette politique s'applique à toutes les versions de Skiff, quelle que soit la façon dont vous l'avez installé (installateur, image disque, paquet, Microsoft Store, binaire seul).

**Skiff ne collecte rien.** Il n'a ni compte, ni télémétrie, ni statistiques, ni publicité, ni rapports de plantage, et il n'envoie aucune donnée à Laterna ni à personne d'autre.

## À quoi Skiff se connecte

Seulement à ce que vous lui demandez :

- les serveurs de vos sites et des liens que vous ouvrez (SFTP, SCP, FTP/FTPS, WebDAV, S3, conteneurs), et les hôtes de rebond et proxys que vous leur donnez ;
- le fichier que vous choisissez sur un de vos propres serveurs pour synchroniser vos réglages entre vos appareils, chiffré sur votre appareil avant d'en partir ;
- les pages web d'un serveur que vous ouvrez depuis **Ports du serveur**, dans votre navigateur.

En mode web distant (`skiff serve`), l'interface est servie par le Skiff que vous lancez, aux navigateurs depuis lesquels vous vous connectez.

## Ce que Skiff garde sur votre appareil

Dans son dossier de configuration (voir la documentation, chapitre 1), sur votre appareil seulement :

- vos sites et vos préférences ; les mots de passe, phrases secrètes et clés enregistrés sont chiffrés (XChaCha20-Poly1305), par une clé gardée dans ce dossier ou dérivée de votre mot de passe maître (Argon2id) ;
- les clés d'hôte et les certificats que vous avez acceptés ;
- les journaux de session, seulement si vous les activez ;
- les versions précédentes des fichiers enregistrés dans son éditeur (les 20 dernières de chacun, pendant 30 jours), désactivables ;
- des copies temporaires des fichiers que vous modifiez avec une autre application, le temps de les modifier.

Supprimer le dossier de configuration supprime tout cela.

## Les magasins d'applications

Quand vous installez Skiff depuis un magasin d'applications (le Microsoft Store, par exemple), celui-ci gère le téléchargement, les mises à jour et ce qu'il en enregistre selon sa propre politique de confidentialité. Skiff lui-même n'y ajoute rien.

## Contact

Questions sur cette politique : une *issue* sur https://github.com/skiff-project/skiff/issues. Problèmes de sécurité : [SECURITY.md](SECURITY.md).
