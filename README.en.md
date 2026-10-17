# Skiff

🇫🇷 [Version française](README.md)

**A modern, cross-platform file transfer client in a single binary.**
SFTP · SCP · FTP/FTPS · WebDAV · S3 — an alternative to WinSCP for Windows, macOS, Linux and FreeBSD.

Skiff takes up the features of WinSCP (two-panel Commander interface, transfer queue, synchronization, editor, `winscp.com`-compatible scripts, session import…) with a modern, fluid interface, and adds a built-in SSH terminal, tunnel and jump host management, a visual diff, a command palette and a remote web mode.

> 📖 **[Full documentation](docs/README.md)** (in French): every feature described screen by screen, with more than a hundred screenshots. The [installation chapter](docs/en/01-installation.md) is also in English.

![Skiff connected to an SFTP server](docs/images/commander.png)

---

## Contents

- [Features](#features)
- [WinSCP → Skiff](#winscp--skiff)
- [Installation and building](#installation-and-building)
- [Usage](#usage)
- [Scripts (WinSCP-compatible)](#scripts-winscp-compatible)
- [Remote web mode](#remote-web-mode)
- [Security](#security)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Architecture](#architecture)
- [Development and tests](#development-and-tests)
- [Known limitations](#known-limitations)
- [Versions](#versions)
- [Licence](#licence)

---

## Features

### Protocols
| Protocol | Details |
|---|---|
| **SFTP** | Concurrent transfers, resume, `posix-rename`, hard links, `statvfs`, custom SFTP server (e.g. `sudo /usr/lib/openssh/sftp-server` to browse as root), automatic fallback to SCP |
| **SCP** | `scp -t/-f` protocol for transfers, shell commands for the rest (listing with `find -printf` or `ls`, `chmod`, `chown`, `touch`, `cp`, `ln`, `df`, checksums…), customizable shell |
| **FTP / FTPS** | Own client: explicit and implicit TLS (with TLS session reuse), **passive and active** modes (PORT/EPRT), MLSD/MLST or LIST (Unix and DOS/IIS), `SITE CHMOD`, MFMT/MDTM, REST/APPE, HASH/XSHA256/XMD5, legacy encodings (Windows-1252, Latin-1…), keepalive, transparent reconnection |
| **WebDAV** | HTTP/HTTPS, Basic and Digest authentication, PROPFIND, server-side MOVE/COPY, quotas, `Range` for resume, `X-OC-Mtime` (Nextcloud/ownCloud) |
| **S3** | AWS and compatible stores (MinIO, Wasabi, R2, B2, Ceph, SeaweedFS…), path or virtual-host addressing, region, session token, multipart upload, server-side copy (> 5 GB through compose), virtual folders |
| **Containers** | Docker, Podman and Kubernetes pods, on this computer or behind an SSH server, through `docker exec` / `kubectl exec`: files, transfers, terminal and monitor, with nothing installed in the container |

### Connection
- SSH authentication: password, keyboard-interactive (OTP, password change), **OpenSSH and PuTTY `.ppk` v2/v3 keys**, OpenSSH certificates (`-cert.pub`), **SSH agent / Pageant / Windows OpenSSH agent**, default keys of `~/.ssh`
- **Host key** checking (own `known_hosts` plus reading `~/.ssh/known_hosts`), SHA-256/MD5 fingerprints, pinning, warning when a key changes
- **TLS certificate** checking with one-time or permanent trust, fingerprint pinning
- **Proxies**: SOCKS4/4a, SOCKS5, HTTP CONNECT, Telnet, local command (`ProxyCommand`)
- **Jump hosts** (`ProxyJump` chains), usable by **every** protocol (FTP/WebDAV/S3 through an SSH tunnel)
- **OpenSSH configuration read automatically**, as the `ssh` command does: `~/.ssh/config` then `/etc/ssh/ssh_config` (on Windows `%USERPROFILE%\.ssh\config` then `%ProgramData%\ssh\ssh_config`), with `Host`, `Match`, `Include` and the `%h %p %r…` tokens. The declared hosts appear in the site list; `HostName`, `User`, `Port`, `IdentityFile`, `ProxyJump`, `ProxyCommand`, `StrictHostKeyChecking`, `HostKeyAlias`, `LocalForward`… complete what a site leaves empty, and the site editor shows each detected value with its source line
- Configurable algorithms, a "legacy algorithms" mode for old servers, IPv4/IPv6, timeouts, keepalive, **automatic reconnection**

### Interface
- **Commander** (two panels) or **Explorer** (remote only); each panel can show **any connection**, which allows **server → server** transfers
- Session, editor, diff and terminal tabs, reorderable, restored at startup, **workspaces**
- Virtualized list (100,000 files without slowing down), natural sort, resizable and adaptive columns, type-ahead search, filter (WinSCP masks), selection by mask (`Num +`, `Num −`, `Num *`)
- **Folder tree**, editable breadcrumb, history, bookmarks, synchronized browsing, **folder comparison**
- **Drag and drop** between panels, onto folders, from the system (folders included), and to the desktop: local files everywhere, remote files with Chromium engines (WebView2, Chrome, Edge)
- File clipboard (Ctrl+C / X / V), copy of paths, names and URLs
- **Preview** (images, video, audio, PDF, Markdown, text), information panel
- **Command palette** (Ctrl+K): commands, sites, workspaces, files, quick-connect URLs
- Light and dark themes, accent colour, three densities, font size, **French and English**, function key bar
- Accessible: every control has a name for screen readers, keyboard navigation, visible focus

### Transfers
- Background queue, **parallelism** (per job and overall), pause, resume, cancel, reorder, **retry of failed files**
- **Resume** through `.filepart` files, automatic retries on network errors
- Overwrite policies: ask, overwrite, skip, if newer, if the size differs, resume, keep both (with "apply to all")
- **Text / binary / automatic** mode (LF ↔ CRLF conversion), include and exclude masks (size, date, paths, folders), **speed limit**, name case, replacement of characters forbidden on Windows
- Dates and rights kept, forced rights (+X for folders), **checksum verification** after transfer
- Move (copy then delete), server-side duplication

### Synchronization
- **Synchronize**: local → remote, remote → local or both ways; standard, mirror and dates-only modes; criteria date, size, date or size, **checksum**, existence; deletion of orphans; masks
- **Preview with checkboxes** before running
- **Keep the remote folder up to date** (watch of the local file system, live upload and deletion, log)

### File operations
- Inline rename (F2), creation of files, folders and links (symbolic and hard), deletion (**system trash** locally, configurable **remote trash**)
- **Properties**: rights (rwx grid, octal, setuid/setgid/sticky), owner and group (with suggestions), modification date, recursive application, folder size, MD5/SHA-1/SHA-256/SHA-512 checksums
- Recursive **search** (mask, content through `grep` on the server, size, date), live results
- **Custom commands** with WinSCP patterns (`!`, `!&`, `!/`, `!@`, `!U`, `!?question?default!`), one-off commands
- Opening with the default application, external editing with **automatic upload** at every save
- Server and protocol information, session log (searchable, filterable, exportable, optionally written to disk)

### Beyond WinSCP
- **Built-in SSH terminal** (xterm.js) with history replay, search and copy/paste, plus a **local terminal**; several terminals per tab (side by side, stacked, in a grid), **broadcast input** to several servers, **saved commands**, and the file panel that **follows the current folder** (OSC 7)
- **Projects**: edit the files of a server without installing anything on it, with a tree, tabs, search, quick open, a terminal and version history
- **Server monitor**: processor, memory, load, network, disks and processes (with kill), read from `/proc` with nothing installed; **systemd services** and live logs; **server ports** opened in the browser through an automatic tunnel; **disk usage**
- **Skiff keychain**: your private keys kept encrypted by Skiff, offered to every server, and **agent forwarding** (`ForwardAgent`) to SSH terminals
- **Scheduled tasks**: synchronizations and scripts run every night, every hour or by a cron expression, with history and log; `skiff task run` for the system scheduler
- **Sync between devices**: sites, passwords, keychain, preferences and host keys shared between your computers through an encrypted file on your own server
- **Big SFTP files in parallel parts**, each on its own channel: much faster to a distant server
- **Tunnel manager**: local (`-L`), remote (`-R`) and **dynamic SOCKS** (`-D`) forwarding, live statistics, automatic start per site
- **Built-in editor** CodeMirror 6: highlighting of 100+ languages, search and replace, encodings, BOM, line endings, **conflict detection** on save
- **Visual diff** local ↔ remote or server ↔ server, merge by blocks
- **Remote web mode** (`skiff serve`): Skiff on a server or a NAS, usable from any browser
- **Key generator** (Ed25519, ECDSA, RSA; OpenSSH or `.ppk` v3), OpenSSH ↔ PPK conversion, **installation of the public key** on a server (the equivalent of `ssh-copy-id`)
- **Authorized keys** of a server: the key of the session flagged, removal, rights fixed; **a key installed or replaced on several sites**, the old one removed only once the new one is checked
- **`sftp://`, `ftp://`… links** of a browser or a mail opened by Skiff, with a confirmation, in the window already open
- **Master password** (Argon2id + XChaCha20-Poly1305)
- Import of **WinSCP (INI and registry, passwords included), PuTTY, FileZilla, `~/.ssh/config`** (including `ProxyJump` and `LocalForward`), URLs; export as JSON, WinSCP.ini, ssh config, URLs
- **URL and code generator** for sessions (URL, command line, script, OpenSSH command)

---

## WinSCP → Skiff

| WinSCP | Skiff |
|---|---|
| Login dialog / site manager | **Sites** tab (tree, folders, recent, search, quick connection by URL) |
| Commander / Explorer interface | Preferences → Interface, or the layout button |
| F2…F10 | The same (key bar at the bottom) |
| Queue | **Transfers** panel (F12) |
| Synchronize / Keep up to date / Compare | Toolbar, Ctrl+S / Ctrl+U / Ctrl+= |
| Open in PuTTY | Built-in terminal (Ctrl+T) |
| Pageant / PuTTYgen | SSH agent supported, built-in key generator |
| `winscp.com /script=… /command …` | `skiff /script=…`, `skiff /command …` or `skiff run` |
| WinSCP.ini (portable mode) | `skiff-data` folder (or `skiff.portable` file) next to the binary |
| .NET assembly | Scripts, CLI, and the HTTP API of the web mode |

---

## Installation and building

| System | Installer |
|---|---|
| Windows 10/11 | `skiff-setup-<version>-windows-amd64.exe` (or `-arm64`): for your account, without administrator rights, Start menu, uninstall from Settings → Apps |
| macOS 11+ | `Skiff-<version>-macos.dmg`: drag Skiff into Applications (universal Intel / Apple Silicon) |
| Debian, Ubuntu… | `sudo apt install ./skiff_<version>_amd64.deb` |
| Other Linux | `Skiff-<version>-x86_64.AppImage`: `chmod +x`, then run it |

Details, first start of the unsigned installers, update and uninstall: [installation chapter](docs/en/01-installation.md).

**Building requirements**: Go ≥ 1.27.2 (the installed `go` fetches that version by itself when needed), Node ≥ 22 and pnpm (only to build the interface).

```bash
./build.sh           # (or make) builds the interface, then the binary in bin/skiff
./build.sh release   # builds every binary into dist/
./build.sh packages  # the installers: .deb, AppImage and Windows on Linux, .dmg on macOS
```

### Native window

| Platform | Window | Requirements |
|---|---|---|
| Windows 10/11 | **Native WebView2** (no CGO, cross-compiled) | WebView2 (preinstalled on Windows 10/11) |
| macOS | **Native WKWebView** (built by the CI on macOS, universal `.app`) | — |
| Linux | **Native WebKitGTK** when built with the headers | to build: `libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config`; to run: `libwebkit2gtk-4.1` |
| All | "Portable" variant without a webview | Opens a Chrome, Edge or Brave app window when one is present, else the default browser |

On Linux, for the native window:

```bash
sudo apt install -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config
./build.sh
```

`build.sh` (or `make`) detects WebKitGTK by itself. The GitHub Actions CI (`.github/workflows/build.yml`) builds the native binaries and the installers of every platform and publishes a release at each `v*` tag.

---

## Usage

```text
skiff [session-url]              opens the window (and connects, optionally)
skiff serve [options]            web interface for remote use
skiff run [options] [script]     runs a WinSCP-compatible script
skiff shell [session-url]        interactive console
skiff keygen [options]           generates (or converts, -convert) an SSH key pair
skiff import <source> [file]     imports sessions (winscp-ini, winscp-registry, putty, filezilla, ssh-config)
skiff url-handler [-register]    sftp://, ftp://… links opened by Skiff (or not: -remove)
skiff licenses [-full]           the licence of Skiff and of the software it includes
skiff version
```

**Session URLs**: `sftp://user@host:22/path`, `scp://`, `ftp://`, `ftps://` (implicit TLS), `ftpes://` (explicit TLS), `dav://`, `davs://`, `s3://`. WinSCP also accepts `sftp://user;fingerprint=SHA256:…@host/`, and so does Skiff.

**Configuration**: `~/.config/Skiff` (Linux), `%APPDATA%\Skiff` (Windows), `~/Library/Application Support/Skiff` (macOS). For the **portable mode**, create a `skiff-data` folder (or an empty `skiff.portable` file) next to the binary. The `SKIFF_HOME` variable forces a folder.

---

## Scripts (WinSCP-compatible)

```bash
skiff run -c "open sftp://deploy@prod.example.com -hostkey=SHA256:abc…" \
          -c "synchronize remote -delete ./site /var/www" -c exit

skiff /script=backup.txt /log=backup.log          # as winscp.com
skiff /command "open ftpes://user:pass@ftp.example.com/" "get /exports/*.csv C:\data\" "exit"
```

Commands: `open`, `close`, `session`, `exit`, `cd`, `pwd`, `ls`, `lcd`, `lpwd`, `lls`, `get`, `put` (`-delete -resume -append -preservetime -nopreservetime -speed= -transfer= -filemask= -neweronly -latest -permissions=`), `mkdir`, `rm`, `rmdir`, `mv`, `cp`, `chmod` (`-recursive`), `ln`, `stat`, `checksum`, `call`, `synchronize` (`local|remote|both -delete -mirror -criteria= -filemask= -preview`), `keepuptodate`, `option` (`batch`, `confirm`, `transfer`, `echo`, `failonnomatch`), `echo`, `help`.
Variables: `%VARIABLE%`, `%TIMESTAMP#yyyy-mm-dd%`, `%TIMESTAMP-1D#yyyymmdd%`. Exit code 1 on error (`option batch abort`, the default).

Non-interactively, an unknown host key is refused: pin it with `-hostkey=…` (or `-hostkey=*` to accept any key, knowingly).

---

## Remote web mode

```bash
skiff serve --set-password                              # choose the password (stored with Argon2id)
skiff serve --listen 0.0.0.0:8443 --root /srv/data      # HTTPS with a self-signed certificate
skiff serve --tls-cert cert.pem --tls-key key.pem
skiff serve --no-tls --listen 127.0.0.1:8080            # behind a TLS reverse proxy
```

- Login with a user name and a password, `HttpOnly` and `SameSite=Strict` session cookie, limited attempts, mandatory anti-CSRF header, `Origin` check of WebSockets.
- `--root` jails the local panel in a folder (symbolic links included); local terminal, local commands and external editors are disabled (option `--allow-local-terminal`).

---

## Security

- **Saved passwords** encrypted with XChaCha20-Poly1305:
  - without a master password, the key is in `secret.key` (mode 600), so the site file can be shared or synchronized without exposing the secrets;
  - with a **master password**, the key is derived with Argon2id and never written.
- The interface never receives the saved passwords (only a "saved" flag).
- Desktop window: server on `127.0.0.1` only, a random token at every start, a check of the `Host` header (against DNS rebinding), an anti-CSRF header, active content (HTML/SVG) never rendered from the origin of the application.
- SSH host keys and TLS certificates checked interactively, with optional pinning.
- Content security policy (CSP): only the scripts and the connections of Skiff itself are allowed.
- A file name sent by a server never writes outside of the destination folder (`../`, and `\` or `:` on Windows).
- Dependencies checked (`govulncheck`, `pnpm audit`), parsers fuzzed; to report a vulnerability: [`SECURITY.md`](SECURITY.md).

---

## Keyboard shortcuts

| Key | Action | Key | Action |
|---|---|---|---|
| `Tab` | Switch panel | `Ctrl+K` | Command palette |
| `Enter` | Open | `Backspace` / `Alt+↑` | Parent folder |
| `F2` | Rename | `F3` | View |
| `F4` / `Shift+F4` | Edit (internal / external) | `F5` / `Shift+F5` | Copy (with options) |
| `F6` | Move | `F7` / `Shift+F7` | New folder / file |
| `F8` / `Del` | Delete | `F9` | Properties |
| `F12` | Transfers | `Space` / `Ins` | Select |
| `Ctrl+F` | Filter | `Ctrl+Shift+F` | Find files |
| `Ctrl+S` | Synchronize | `Ctrl+=` | Compare folders |
| `Ctrl+B` | Synchronized browsing | `Ctrl+U` | Keep up to date |
| `Ctrl+T` | Terminal | `Ctrl+L` | Edit the path |
| `Ctrl+D` | Bookmark | `Ctrl+R` | Refresh |
| `Ctrl+C/X/V` | Copy/cut/paste files | `Ctrl+Tab` | Next tab |

The full list is in Preferences → Keyboard shortcuts.

---

## Architecture

```text
skiff/
├── main.go, cli_*.go         CLI: window, serve, run, shell, keygen, import, winscp.com compatibility
├── internal/
│   ├── vfs/                  unified file system + implementations
│   │   ├── localfs  sftpfs  scpfs  ftpfs (own FTP/FTPS client)  davfs (own WebDAV client)  s3fs
│   ├── sshx/                 SSH: authentication, PPK, agent, known_hosts, jumps, keepalive
│   ├── netx/                 proxies (SOCKS4/5, HTTP, Telnet, command), interactive TLS
│   ├── transfer/             transfer engine (queue, workers, resume, overwrite, text mode…)
│   ├── core/                 connections, operations, synchronization, watching, terminals, tunnels, editors
│   ├── config/               sites, preferences, encrypted secrets, master password, portable mode, format
│   ├── importer/  keys/  mask/  script/  prompt/  instance/  urlhandler/
│   ├── server/               JSON-RPC API, WebSocket (events, terminals), streaming, authentication
│   └── desktop/              Wails window (WebView2/WKWebView/WebKitGTK) or browser fallback
├── frontend/                 React 19 + TypeScript + Vite (zustand, TanStack Virtual, CodeMirror 6, xterm.js)
└── testenv/                  Docker test servers (OpenSSH, old OpenSSH, Pure-FTPd+TLS, vsftpd implicit TLS, WebDAV, S3)
```

The interface talks to the Go core over HTTP and WebSocket. It is the same interface in the native window and in web mode.

---

## Development and tests

```bash
./build.sh dev    # rebuilds the interface on change, Skiff on http://127.0.0.1:8484/?token=dev
./build.sh test   # go vet, unit tests, TypeScript typecheck, translations, notices
./build.sh itest  # end-to-end tests: every API method, web mode, command line and scripts
./build.sh uitest # end-to-end tests of the interface in a browser (Playwright)
```

Test servers (`testenv/docker-compose.yml`): SSH `127.0.0.1:2222`, old SSH `127.0.0.1:2223`, FTP `127.0.0.1:2121` (explicit TLS), FTPS `127.0.0.1:9990` (implicit TLS), WebDAV `http://127.0.0.1:8080`, S3 `http://127.0.0.1:9000` (`skiffadmin` / `skiffsecret`). Credentials: `skiff` / `skiff`.

---

## Known limitations

- The native windows of macOS and Linux are built by the CI (they need CGO): without the WebKitGTK headers, a local Linux build uses the browser fallback.
- SFTP version 3 (the version of OpenSSH); the extensions of versions 4 to 6 are not used.
- FTP `chmod` depends on the server (`SITE CHMOD`), and S3/WebDAV have no Unix rights.
- The transfer queue is not kept after a restart.
- Dragging a **remote** file to the desktop only works with Chromium engines (WebView2, Chrome, Edge): on Linux and macOS, go through the local panel (local files can be dragged anywhere).

---

## Versions

The changes of each version are in [`CHANGELOG.md`](CHANGELOG.md). Skiff follows semantic versioning: every 1.x release reads the configuration of the earlier 1.x releases.

---

## Licence

Skiff is free software, under the [Apache License 2.0](LICENSE): you may use, change and share it, commercially too, keeping the licence and the [`NOTICE`](NOTICE). The licence grants no rights to the name or the logo of Skiff.

Skiff includes libraries under their own licences (MIT, BSD, ISC, Apache-2.0, Unlicense): their list and their texts are in [`THIRD_PARTY_NOTICES.txt`](THIRD_PARTY_NOTICES.txt), in **Preferences → About Skiff** and in `skiff licenses`.

Copyright 2026 Laterna.
