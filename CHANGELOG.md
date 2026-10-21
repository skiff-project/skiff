# Changelog

All notable changes to Skiff are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Skiff uses
[semantic versioning](https://semver.org/): a 1.x release reads the
configuration of every earlier 1.x release (see "Configuration format"
below).

## [Unreleased]

## [1.0.0] — 2026-10-21

The first public release.

### Protocols and connections
- SFTP (parallel parts for big files, resume, `posix-rename`, custom
  server command, fallback to SCP), SCP, FTP and FTPS (explicit and
  implicit TLS, passive and active modes, MLSD or LIST, legacy encodings),
  WebDAV, S3 and compatible stores, and Docker, Podman and Kubernetes
  containers on this computer or behind SSH.
- SSH keys (OpenSSH and PuTTY `.ppk`), certificates, SSH agent and
  Pageant, the Skiff keychain and agent forwarding; host keys and TLS
  certificates checked and pinned; SOCKS, HTTP and command proxies; jump
  hosts for every protocol; the OpenSSH configuration read as `ssh` does;
  automatic reconnection.

### Files and transfers
- Commander and Explorer layouts, any connection in any panel (server to
  server transfers), tabs, workspaces, a folder tree, bookmarks, history,
  filters, a virtualized list, drag and drop, previews.
- A background queue with parallel jobs, resume, overwrite policies,
  text mode, masks, speed limit, checksums; synchronization with a
  preview; keeping a remote folder up to date.
- Properties (rights, owner, dates, checksums), search (names and
  contents), custom commands with WinSCP patterns, external editors with
  automatic upload, archives, disk usage.

### Beyond WinSCP
- An SSH and local terminal (split, broadcast input, saved commands,
  folder following), a server monitor, systemd services and live logs,
  the ports of a server opened in the browser through a tunnel, a tunnel
  manager (`-L`, `-R`, `-D`).
- Projects: editing the files of a server without installing anything on
  it, with search, quick open, a terminal and version history.
- An editor with conflict detection, a visual diff, scheduled tasks, the
  sync of settings between devices, a web mode (`skiff serve`).
- A key generator and converter; the authorized keys of a server, and a
  key installed or replaced on several sites at once (the old key goes
  only after the new one is checked).
- The `sftp://`, `scp://`, `ftp://`… links of the system opened by Skiff,
  with a confirmation, in the window already open.
- A master password (Argon2id + XChaCha20-Poly1305); imports from WinSCP,
  PuTTY, FileZilla and OpenSSH; WinSCP-compatible scripts and
  `winscp.com` options.

### Packages
- Installers: Windows (for the current user, without administrator
  rights), macOS disk image, Debian package and AppImage; binaries alone
  for every system, and a portable mode.
- French and English interface; the documentation, its nineteen chapters
  and their screenshots in both languages (`docs/` and `docs/en/`).
- A Microsoft Store package (MSIX, made by the CI), Windows signing
  through SignPath prepared (CODE_SIGNING.md), a one-command install for
  macOS without the Gatekeeper warning (install.sh), and a privacy policy
  (PRIVACY.md: Skiff collects nothing).

### Configuration format
- The configuration folder records its format (`format.json`, format 1).
  A folder of a Skiff before 1.0 is backed up in `backups/` and marked;
  a later format change will be migrated the same way, after a backup.
  A Skiff that finds a folder of a newer format refuses to open it rather
  than lose what it does not know.

### Licence
- Apache License 2.0, with the notices of the software Skiff includes
  (`THIRD_PARTY_NOTICES.txt`, Preferences > About, `skiff licenses`).
