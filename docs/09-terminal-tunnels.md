# 9. Terminal, tunnels, proxys et rebonds

[← Édition, diff et aperçu](08-edition.md) · [Sommaire](README.md) · [Recherche, journal et palette →](10-recherche-journal.md)

## 9.1 Terminal SSH

Pour les sessions SFTP et SCP, **Terminal** (`Ctrl+T`) ouvre un terminal SSH complet dans un onglet, directement dans le dossier courant du panneau. Il utilise la connexion déjà ouverte : pas de nouvelle authentification.

![Terminal SSH intégré](images/terminal-ssh.png)

- Émulation xterm complète (xterm.js) : couleurs, 256 couleurs et vraies couleurs, souris, applications plein écran (`vim`, `htop`, `mc`, `tmux`…), redimensionnement automatique.
- Barre d'outils : **Rechercher** dans le défilement (`Ctrl+Maj+F`), **Copier** la sélection (`Ctrl+Maj+C`), **Coller** (`Ctrl+Maj+V`), **Réduire la police** et **Agrandir la police** (de 9 à 28 px, pour ce terminal).
- Option **Copier à la sélection** (Préférences → Terminal) : sélectionner du texte le copie.
- Le terminal continue de tourner quand vous changez d'onglet. Quand l'interface se recharge (par exemple en mode web), les 256 derniers Kio affichés sont restitués.
- Quand le shell se termine, le terminal affiche « La session est terminée » et un bouton **Rouvrir**.

Le shell est le shell de connexion de l'utilisateur (`$SHELL -l`). La police, sa taille, le nombre de lignes conservées, le curseur et la variable `TERM` se règlent dans **Préférences → Terminal**. Un terminal s'ouvre aussi depuis la liste des sites (**Ouvrir dans un terminal**), qui se connecte et ouvre le terminal en une seule étape.

## 9.2 Terminal local

**Terminal local**, sur l'écran d'accueil ou dans la palette (`Ctrl+K` → « Terminal (Local) »), ouvre un terminal sur l'ordinateur ; **Ouvrir un terminal ici** dans le panneau local l'ouvre dans le dossier affiché.

| Système | Shell |
|---|---|
| Linux, macOS, FreeBSD | `$SHELL` (pseudo-terminal) |
| Windows | PowerShell 7 (`pwsh.exe`) s'il est installé, sinon Windows PowerShell, via ConPTY |

En [mode web](14-mode-web.md), le terminal local est désactivé, sauf avec l'option `--allow-local-terminal`.

## 9.3 Tunnels (redirections de ports)

Les tunnels font passer des connexions réseau à travers une session SSH. **Tunnels** (barre d'outils) ouvre la boîte **Redirection de ports** :

![Création d'un tunnel local vers PostgreSQL](images/tunnel-dialogue.png)

| Type | Équivalent OpenSSH | Usage |
|---|---|---|
| **Locale (L)** | `ssh -L` | Un port de votre ordinateur mène à une destination vue depuis le serveur. Exemple : `127.0.0.1:15432` → `localhost:5432` pour atteindre la base PostgreSQL du serveur avec votre client habituel. |
| **Distante (R)** | `ssh -R` | Un port du serveur mène à une destination vue depuis votre ordinateur. Exemple : montrer au serveur un service de développement local. |
| **SOCKS dynamique (D)** | `ssh -D` | Un proxy SOCKS4/5 local : toute application réglée sur ce proxy sort par le serveur. |

| Champ | Description |
|---|---|
| **SSH** | Session SSH (SFTP ou SCP) à utiliser |
| **Écouter sur**, **Port** | Adresse et port d'écoute (`127.0.0.1` pour un accès réservé à l'ordinateur) |
| **Destination**, **Port** | Hôte et port à joindre (sauf en SOCKS) |
| Raccourcis | **PostgreSQL**, **MySQL / MariaDB**, **Redis**, **MongoDB**, **HTTP**, **HTTPS**, **RDP**, **VNC** : remplissent le port de destination, un port local libre et le libellé |
| **Libellé** | Nom affiché dans la liste des tunnels |

**Démarrer** ouvre le tunnel. Les tunnels actifs sont listés dans l'onglet **Tunnels** du panneau du bas :

![Tunnels actifs](images/tunnels-panneau.png)

Pour chaque tunnel : le type et le libellé, l'adresse d'écoute (avec un bouton pour la copier), la destination, la session utilisée avec le nombre de connexions en cours et au total, le trafic reçu et envoyé, et **Arrêter**. **Nouveau tunnel** ouvre la boîte de création. Les tunnels s'arrêtent quand leur session se ferme.

Pour qu'un tunnel s'ouvre à chaque connexion, définissez-le dans le site (**Redirection de ports**, case **Auto**, voir [Sites](03-sites.md#redirection-de-ports-ssh)). Les `LocalForward`, `RemoteForward` et `DynamicForward` d'un `~/.ssh/config` importé deviennent de tels tunnels.

## 9.4 Hôtes de rebond

Un hôte de rebond (*bastion*, *jump host*) est un serveur SSH par lequel il faut passer pour atteindre les serveurs d'un réseau privé. Dans le site, section **Hôtes de rebond**, ajoutez un ou plusieurs rebonds :

![Hôtes de rebond d'un site](images/site-rebonds.png)

Skiff se connecte au premier rebond, ouvre à travers lui une connexion vers le suivant, et ainsi de suite jusqu'au serveur final, exactement comme `ssh -J bastion1,bastion2 serveur`. Chaque rebond s'authentifie avec ses propres identifiants : utilisez un **site enregistré** (recommandé, il garde son mot de passe, sa clé, sa clé d'hôte) ou un **hôte personnalisé**.

Particularités :

- les clés d'hôte de chaque rebond sont vérifiées comme pour une connexion directe ;
- les rebonds servent à **tous les protocoles** : un FTP, un WebDAV ou un S3 accessible seulement depuis le réseau interne est joint à travers le tunnel SSH ;
- le nombre de rebonds est indiqué dans les [informations serveur](10-recherche-journal.md#103-informations-serveur-et-protocole) ;
- un `ProxyJump` de `~/.ssh/config` est converti en chaîne de rebonds à l'import.

## 9.5 Proxys

Dans le site, section **Proxy** :

![Proxy d'un site](images/site-proxy.png)

| Type | Description |
|---|---|
| **SOCKS4** | SOCKS4, ou SOCKS4a si **Résoudre les noms d'hôte sur le proxy** est coché |
| **SOCKS5** | Avec authentification facultative, résolution des noms locale ou par le proxy |
| **HTTP** | Méthode `CONNECT`, authentification Basic facultative |
| **Telnet** | Envoie une commande de connexion au proxy (proxys d'entreprise anciens) |
| **Commande locale (ProxyCommand)** | Lance une commande dont l'entrée et la sortie servent de connexion, avec les variables `%host`, `%port`, `%user`, `%pass`, `%proxyhost`, `%proxyport`. Exemples : `ssh -W %host:%port passerelle`, `nc -X 5 -x proxy:1080 %host %port`, `cloudflared access ssh --hostname %host` |

Le proxy s'applique à la première connexion réseau : directement au serveur, ou au premier hôte de rebond s'il y en a.

---

[← Édition, diff et aperçu](08-edition.md) · [Sommaire](README.md) · [Recherche, journal et palette →](10-recherche-journal.md)
