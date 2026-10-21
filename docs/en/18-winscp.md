# 18. WinSCP → Skiff map

[← Troubleshooting](17-troubleshooting.md) · [Contents](README.md) · [Development →](19-development.md) · 🇫🇷 [Français](../18-winscp.md)

This chapter helps WinSCP users find each feature in Skiff.

## 18.1 Migrating

1. **Import your sessions**: **Sites → ⋯ → Import… → WinSCP** (`WinSCP.ini` file or registry), passwords included. On the command line: `skiff import winscp-ini` or `skiff import winscp-registry`.
2. **Take your scripts along**: replace `winscp.com` with `skiff` in your scheduled tasks. The `/script=`, `/command`, `/log=`, `/parameter` syntax is the same.
3. **PuTTY keys**: the `.ppk` files are used directly, no need to convert them. Pageant is supported.

## 18.2 Windows and commands

| WinSCP | Skiff |
|---|---|
| Login dialog, Site manager | **Sites** tab ([chapter 3](03-sites.md)) |
| Advanced (advanced site settings) | Sections of the site form: Directories, Environment, Connection, Proxy, Jump hosts, SSH, FTP, TLS, S3 |
| Tunnel (Connection → Tunnel page) | **Jump hosts** (several levels, for every protocol) |
| Commander / Explorer interface | **Layout**, or Preferences → Appearance → Interface |
| F2…F10 key bar | The same, at the bottom of the window |
| Address bar, tree, bookmarks | Breadcrumb (`Ctrl+L`), **Directory tree**, star (`Ctrl+D`) |
| Filter mask (`Ctrl+F`) | The same, same syntax |
| Select / Unselect (`Num +` / `Num −` / `Num *`) | The same |
| Files → Properties (`F9`) | The same (permissions, owner, group, date, recursive, checksums) |
| Queue (`F12`) | **Queue** panel (`F12`) |
| Transfer preferences, Transfer settings | Transfer dialog (`Shift+F5`), Preferences → Transfers |
| Commands → Synchronize (`Ctrl+S`) | **Synchronize** (`Ctrl+S`), with a preview to tick |
| Commands → Keep Remote Directory up to Date (`Ctrl+U`) | The same |
| Commands → Compare Directories (`Ctrl+=`) | The same |
| Commands → Find Files (`F3` in WinSCP) | **Find files** (`Ctrl+Shift+F`) |
| Commands → Synchronized Browsing (`Ctrl+B`) | The same |
| Commands → Open Terminal / Open in PuTTY | **Built-in SSH terminal** (`Ctrl+T`) |
| Commands → Custom Commands | Context menu → **Custom commands**, same `!`, `!&`, `!/`, `!@`, `!U`, `!?…!` patterns |
| Commands → Execute command | **Run command** (`Ctrl+Shift+X`) |
| Session → Server/Protocol Information | ⓘ button of the toolbar |
| Session log | **Log** panel |
| Session → Generate Session URL/Code | Context menu of a site → **Generate URL / code…** |
| Preferences → Integration → Register to handle URL addresses | **Preferences → System → Open these links with Skiff** (or the box of the Windows installer) |
| Install Public Key into Server | **Install a public key**, and **Authorized keys** to see, remove or replace the keys of the server, on one site or on several |
| Internal editor | CodeMirror editor (highlighting, search, encodings, conflicts) |
| External editor with automatic upload | The same (`Shift+F4`), **Edited files** tab |
| PuTTYgen | **Generate key pair…**, **Convert** (Preferences → Security), `skiff keygen` |
| Pageant | SSH agent / Pageant supported (**Use the Skiff keychain and the SSH agent / Pageant** box) |
| Master password | **Master password** (Argon2id + XChaCha20-Poly1305) |
| `WinSCP.ini` (portable mode) | `skiff-data` folder or `skiff.portable` file next to the binary |
| `winscp.com /script=… /command …` | `skiff /script=…`, `skiff /command …`, `skiff run` |
| .NET assembly, extensions | Scripts, command line, HTTP API of the [web mode](14-web-mode.md) |
| Themes | Light, dark, accent color, density |

## 18.3 What Skiff adds

- A single binary for Windows, macOS, Linux and FreeBSD, without installation.
- **S3** and its compatibles, on top of SFTP, SCP, FTP/FTPS and WebDAV.
- Each panel can show any connection: **server to server** transfers.
- Built-in **SSH terminal** and **local terminal**, with the history kept.
- **Tunnel manager** (`-L`, `-R`, SOCKS `-D`) with statistics and automatic start.
- Chained **jump hosts**, usable by every protocol.
- **Visual diff** between two files, on any connections.
- **Command palette** (`Ctrl+K`), **workspaces**, **preview** panel.
- **Remote web mode**: the same interface in a browser.
- Docker, Podman and Kubernetes **containers** opened as servers, locally or through SSH.
- **Server monitor**, **split terminals** with broadcast input, saved commands and folder following.
- **Skiff keychain** and **agent forwarding**, **scheduled tasks** with history, **synchronization of the settings between devices**, large SFTP files in **parallel parts**.
- **OpenSSH configuration** (`~/.ssh/config`) read directly, on each system, as the `ssh` command does: aliases, keys, jumps, proxy, tunnels, host key policy.
- Import of the **FileZilla**, **PuTTY** and **OpenSSH** sessions (`ProxyJump` and forwardings included), on top of WinSCP.
- Interface in **English and French**, dark theme.

---

[← Troubleshooting](17-troubleshooting.md) · [Contents](README.md) · [Development →](19-development.md) · 🇫🇷 [Français](../18-winscp.md)
