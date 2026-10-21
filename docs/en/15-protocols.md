# 15. Protocols

[← Remote web mode](14-web-mode.md) · [Contents](README.md) · [Keyboard shortcuts →](16-shortcuts.md) · 🇫🇷 [Français](../15-protocoles.md)

Skiff shows every protocol the same way: the same panels, the same transfers, the same synchronization. This chapter describes their details.

## 15.1 What each protocol allows

| Feature | Local | SFTP | SCP | FTP/FTPS | WebDAV | S3 | Container |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| Unix permissions (display, `chmod`) | Unix | ✓ | ✓ | ✓¹ | | | ✓ |
| Owner and group (`chown`) | Unix | ✓ | ✓ | display | | | ✓ |
| Symbolic links | ✓ | ✓ | ✓ | | | | ✓ |
| Hard links | ✓ | ✓² | ✓ | | | | ✓ |
| Changing the date | ✓ | ✓ | ✓ | ✓³ | Nextcloud/ownCloud | | ✓ |
| Copy on the server side | ✓ | ✓ | ✓ | | ✓ | ✓ | ✓ |
| Checksums without downloading | ✓ | ✓ | ✓ | ✓⁴ | | MD5 | ✓ |
| Free space | ✓ | ✓² | ✓ | | ✓ | | ✓ |
| Resuming downloads | ✓ | ✓ | ✓ | ✓⁵ | ✓ | ✓ | ✓ |
| Resuming uploads | ✓ | ✓ | | ✓ | | | |
| Terminal, shell commands | ✓ | ✓ | ✓ | raw FTP commands | | | ✓ |
| Monitor | | ✓ | ✓ | | | | ✓ |
| Tunnels (port forwarding) | | ✓ | ✓ | | | | |
| Precision of the dates | ns | s | s | s or min | s | s | s |

¹ depending on the server (`SITE CHMOD`). ² when the server offers the extension (`hardlink@openssh.com`, `statvfs@openssh.com`). ³ with `MFMT` or `MDTM`. ⁴ with `HASH`, `XSHA256`, `XSHA1` or `XMD5`. ⁵ with `REST`.

The commands that are not available are hidden or greyed out in the interface. The [Server/protocol information](10-search-log.md#103-server-and-protocol-information) window shows the extensions the server really offers.

## 15.2 SFTP

![SFTP session](images/commander.png)

The default protocol, on port 22. It goes through SSH: password, keyboard-interactive, OpenSSH and PuTTY keys, OpenSSH certificates, SSH agent authentication (see [Sites → SSH](03-sites.md#ssh)).

- Parallel transfers on several channels, pipelined requests for large files.
- OpenSSH extensions used when they exist: `posix-rename` (atomic rename), `statvfs` (free space), `hardlink` (hard links), `fsync`, `limits`.
- **SFTP server command** that can be customized (`sudo /usr/lib/openssh/sftp-server` to browse as root, for one).
- When the server refuses the SFTP subsystem, Skiff switches to SCP automatically.

## 15.3 SCP

![SCP session](images/session-scp.png)

For the SSH servers without SFTP (network devices, embedded systems). The transfers use the `scp -t` / `scp -f` protocol, and the other operations shell commands: listing by `find -printf` or `ls -la` (or the **Listing command (SCP)** of the site), `mkdir`, `rm`, `mv`, `cp`, `chmod`, `chown`, `touch`, `ln`, `df`, `md5sum`/`sha*sum`. The **Shell (SCP)** can be set in the site.

## 15.4 FTP and FTPS

![FTPS session](images/session-ftps.png)

FTP client written for Skiff, port 21 (990 with implicit TLS):

- **FTPS** with explicit (`AUTH TLS`) or implicit TLS, reusing the TLS session on the data connections (required by many servers) and with [certificate checks](11-security.md#112-tls-certificates).
- **Passive** (`PASV`/`EPSV`, with the address corrected behind a NAT) and **active** (`PORT`/`EPRT`, external IP that can be set) modes.
- Standardized **MLSD/MLST** listings, or `LIST` in the Unix and DOS/IIS formats.
- `SITE CHMOD`, `MFMT`/`MDTM` (dates), `REST` and `APPE` (resume), `HASH`/`XSHA256`/`XMD5` (checksums), `ACCT` (account).
- Legacy encodings of the file names (Windows-1252, Latin-1…), keepalive, transparent reconnection when the server cuts an idle connection.
- **Run command** sends a raw FTP command (`SITE …`).

## 15.5 WebDAV

![WebDAV session](images/session-webdav.png)

WebDAV client written for Skiff, over HTTP (80) or HTTPS (443):

- Basic and Digest authentication.
- `PROPFIND` for the listings, `MKCOL`, `MOVE` and `COPY` on the server side, `DELETE`.
- Free space through the quota properties.
- Resumed downloads with `Range` requests.
- Nextcloud and ownCloud: the dates are kept (`X-OC-Mtime`). Remote folder: `/remote.php/dav/files/<user>/`.
- Also works with Apache `mod_dav`, nginx, SharePoint, Box, kDrive, Synology, etc.

## 15.6 S3

![S3 session: the buckets show as folders](images/session-s3.png)

Amazon S3 and the compatible storages: MinIO, Wasabi, Cloudflare R2, Backblaze B2, Scaleway, OVHcloud, Ceph, SeaweedFS, Garage…

- At the root, each **bucket** shows as a folder. Creating a folder at the root creates a bucket.
- The "folders" are key prefixes: Skiff shows them as real folders and creates markers for the empty folders.
- **Multipart** upload for large files, copy and rename on the server side (beyond 5 GB, by assembling parts).
- Virtual-hosted (`bucket.host`) or path-style (`host/bucket`) addressing, region, STS session token.
- No Unix permissions, no date change (the date is that of the upload), MD5 checksum through the ETag (files uploaded in a single part).

## 15.7 Docker, Podman and Kubernetes containers

A Docker or Podman container, or a Kubernetes pod, opens as a server does: its files in a panel, a terminal inside it, the monitor of its processes. Skiff uses the `exec` command of the engine — `docker exec`, `podman exec`, `kubectl exec` — and installs nothing in the container: a POSIX shell and the usual tools are enough (busybox, as in the Alpine images, will do). Reading goes through `cat` (and `tail -c` for a resume), writing through `cat >`, the rest through `ls`/`find`, `mkdir`, `mv`, `chmod`, `sha256sum`…

**Where the engine runs**:

- **this computer**: the `docker`, `podman` or `kubectl` command installed here (with its settings: Docker context, `~/.kube/config`);
- **an SSH server**: the command runs on the server, through the SSH connection — the containers of a remote server open without exposing the Docker daemon nor the Kubernetes API on the network.

**Finding a container**:

- **Containers** on the welcome screen, the palette (`Ctrl+K` → "Containers of this computer") or **⋯ → Containers of this computer** in the site list, for those of the computer;
- in an SFTP or SCP session, the **Containers of this server** button (box icon in the toolbar) lists the containers of the server.

![Containers of this computer](images/conteneurs.png)

The window opens on the first engine installed: Docker, Podman or Kubernetes. Those not installed on the computer or the server are greyed out (a tooltip says so). The **Search** field filters by name, image, project or port.

The containers of the same **Docker Compose** project are grouped under **Compose project …** (the Kubernetes pods, by namespace), the running ones first. Each line shows:

| Column | Content |
|---|---|
| State | **Running** (green), **Stopped**, **Paused**, **Restarting**, **Created**…; for a pod, its phase (**Pending**, **Completed**, **Failed**) |
| Name | The name, then the image and the published ports (`127.0.0.1:18080->80/tcp`) |
| Since | Since when it runs ("for 5 hours", with the health state when it has one), or when it stopped and its exit code ("2 days ago, code 0") |
| Actions | **Files** opens a session on the container, **Terminal** also opens a shell in it (`bash` when it exists, else `sh`); for a stopped container, **Start** |

A double-click on a running container opens its files. The **⋯** menu of each line also offers:

![⋯ menu of a container](images/conteneurs-menu.png)

- **Restart**, **Stop** (`docker stop`: the container has a few seconds to stop cleanly) or **Start**, for Docker and Podman; Kubernetes manages its pods by itself;
- **Save as a site…**, which opens the site editor filled in, to find the container again in the site list (for a server, when its session comes from a saved site);
- **Copy name**.

For Kubernetes, **Context** (empty: the current context) and **Namespace** (empty: all of them) are set above the list, then **Show**.

When it does not work, the window explains the usual cases: Docker missing, daemon stopped (`sudo systemctl start docker`), user outside the `docker` group (`sudo usermod -aG docker $USER`, then log in again), Kubernetes cluster unreachable. The exact message of the engine is shown below.

**A Container site** (**Container** protocol in the site editor) keeps: the **Runtime**, **Runs on** (this computer or a saved SSH site), the name of the **Container** or of the **Pod** (**Choose…** lists them, on this computer or on the chosen SSH server, to which Skiff connects for the time of the choice when no session is open there), the user of the commands (**Run as user**, Docker, Podman), the **Context**, the **Namespace** and the **Container of the pod** (Kubernetes), and the **Command** of the engine when it is not in the `PATH`.

![A Container site](images/site-conteneur.png)

The `docker://container/path`, `podman://container/path` and `kubectl://pod/path?namespace=demo&context=prod&container=web` URLs open a container of this computer ([§ 15.8](#158-session-urls)).

Limits: no resumed uploads nor tunnels; the monitor shows the processes of the container, but the memory and the disks of the host machine. In [web mode](14-web-mode.md), the containers of the machine that runs Skiff are turned off as the local terminal is; those of the SSH servers stay available.

## 15.8 Session URLs

The URLs serve the quick connection, the `Ctrl+K` palette, the command line (`skiff <url>`) and the scripts (`open <url>`).

```text
protocol://[user[:password]@]host[:port][/path]
```

| Scheme | Protocol |
|---|---|
| `sftp://`, `ssh://` | SFTP |
| `scp://` | SCP |
| `ftp://` | FTP without encryption |
| `ftpes://` | FTP + explicit TLS |
| `ftps://` | FTP + implicit TLS |
| `dav://`, `webdav://`, `http://` | WebDAV |
| `davs://`, `webdavs://`, `https://` | WebDAV over HTTPS |
| `s3://` | S3 over HTTPS (default host: `s3.amazonaws.com`) |
| `s3plain://` | S3 over HTTP (local MinIO…) |
| `docker://`, `podman://` | Docker or Podman container of this computer ([§ 15.7](#157-docker-podman-and-kubernetes-containers)) |
| `kubectl://` | Kubernetes pod (`?namespace=`, `&context=`, `&container=`) |

- Without a scheme, `sftp://` is assumed: `alex@server` is enough.
- The path becomes the remote folder opened first. With S3, it starts with the bucket: `s3://KEY@s3.eu-west-3.amazonaws.com/my-bucket/archives/`.
- With S3, the user is the **Access key ID** and the password the **Secret access key**.
- The special characters of the user and of the password are encoded as `%XX` (`@` → `%40`).
- As in WinSCP, the host key fingerprint can be part of the URL: `sftp://alex;fingerprint=SHA256:Fh7jA0…@server/`.

---

[← Remote web mode](14-web-mode.md) · [Contents](README.md) · [Keyboard shortcuts →](16-shortcuts.md) · 🇫🇷 [Français](../15-protocoles.md)
