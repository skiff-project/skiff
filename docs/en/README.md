# Skiff documentation

🇫🇷 [Version française](../README.md) · [README](../../README.en.md)

Skiff is a modern, cross-platform file transfer client, shipped as **a single binary**. It handles SFTP, SCP, FTP/FTPS, WebDAV and S3 and takes up the features of WinSCP: two-panel Commander interface, transfer queue, synchronization, built-in editor, `winscp.com`-compatible scripts and session import. It adds an SSH terminal, the management of tunnels and jump hosts, a visual diff, a command palette and a remote web mode.

![Skiff connected to an SFTP server, in the Commander interface](images/commander.png)

This documentation describes each feature, screen by screen. The screenshots were taken on Skiff 0.1.0 in English, with the light theme, unless stated otherwise.

## Contents

| # | Chapter | Content |
|---|---|---|
| 1 | [Installation and first start](01-installation.md) | Platforms, building, first start, configuration folder, portable mode |
| 2 | [First steps](02-first-steps.md) | Welcome screen, quick connection, first connection, tour of the interface |
| 3 | [Site manager](03-sites.md) | Sites, folders, colors, recent sites, workspaces, all the connection options, OpenSSH configuration (`~/.ssh/config`), import, export, session URL and code |
| 4 | [Interface and navigation](04-interface.md) | Tabs, toolbar, panels, breadcrumb, bookmarks, history, tree, filter, selection, preview, folder comparison, synchronized browsing |
| 5 | [File operations](05-files.md) | Context menus, rename, create, delete, recycle bin, links, properties and permissions, checksums, clipboard, drag and drop, commands, archives, room taken |
| 6 | [Transfers](06-transfers.md) | Copying and moving, transfer options, queue, overwrite, resume, errors, server to server transfers |
| 7 | [Synchronization](07-synchronization.md) | Synchronizing, preview of the changes, keeping the remote folder up to date, scheduled tasks |
| 8 | [Editing, diff and preview](08-editing.md) | Built-in editor, conflicts, search, external editor, visual diff, projects, previous versions |
| 9 | [Terminal, tunnels, proxies and jumps](09-terminal-tunnels.md) | SSH and local terminal, port forwarding, SOCKS, jump hosts, proxies, server monitor, services, live logs, ports |
| 10 | [Search, log and palette](10-search-log.md) | File search, session log, server information, command palette |
| 11 | [Security](11-security.md) | Host keys, TLS certificates, saved passwords, master password, locking, SSH key generator |
| 12 | [Preferences](12-preferences.md) | The fourteen sections of the preferences, themes, density, language, OpenSSH configuration, synchronization between devices |
| 13 | [Scripts and command line](13-scripts-cli.md) | CLI, WinSCP-compatible scripts, interactive console, `keygen`, `import`, `task` |
| 14 | [Remote web mode](14-web-mode.md) | `skiff serve`, authentication, TLS, confinement, deployment |
| 15 | [Protocols](15-protocols.md) | Details of SFTP, SCP, FTP/FTPS, WebDAV and S3, containers, session URLs |
| 16 | [Keyboard shortcuts](16-shortcuts.md) | All the shortcuts |
| 17 | [Troubleshooting](17-troubleshooting.md) | Connection failures, cuts, reconnection, logs, frequently asked questions |
| 18 | [WinSCP → Skiff map](18-winscp.md) | Where to find each feature of WinSCP |
| 19 | [Development](19-development.md) | Architecture, building, tests, making the screenshots again |

## Conventions

- The names of menus, buttons and options are written **in bold**, as they show in the interface.
- Keys are written `Ctrl+K`. On macOS, `Ctrl` stands for `⌘ Cmd`.
- `~` stands for your home folder. Skiff shows it so in the local paths and understands it when you type it.
- The examples use a demonstration site, "Showcase site (SFTP)", whose files are in `~/Projects/showcase-site` locally and `/config/www` on the server.

---

🇫🇷 [Version française](../README.md) · [README](../../README.en.md)
