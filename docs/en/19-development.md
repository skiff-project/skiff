# 19. Development

[← WinSCP → Skiff map](18-winscp.md) · [Contents](README.md) · 🇫🇷 [Français](../19-developpement.md)

## 19.1 Architecture

```text
skiff/
├── main.go, cli_*.go         CLI: window, serve, run, shell, keygen, import, winscp.com compatibility
├── internal/
│   ├── vfs/                  unified file system and its implementations
│   │   ├── localfs  sftpfs  scpfs  ftpfs (FTP/FTPS client)  davfs (WebDAV client)  s3fs
│   ├── sshx/                 SSH: authentication, PPK, agent, known_hosts, jumps, keepalive
│   ├── sshconfig/            reading of the OpenSSH configuration (Host, Match, Include, tokens) by OS
│   ├── tasks/                scheduled tasks: storage, scheduling (interval, day, week, cron), runs without questions
│   ├── devsync/              synchronization of the settings between devices (encrypted file, merge)
│   ├── netx/                 proxies (SOCKS4/5, HTTP, Telnet, command), interactive TLS
│   ├── transfer/             transfer engine (queue, workers, resume, overwrite, text mode…)
│   ├── core/                 connections, operations, synchronization, watching, terminals, tunnels, editors
│   ├── config/               sites, preferences, encrypted secrets, master password, portable mode
│   ├── importer/             WinSCP, PuTTY, FileZilla, OpenSSH, JSON, URL import
│   ├── keys/                 key generation, conversion and installation
│   ├── mask/                 WinSCP file masks
│   ├── script/               WinSCP-compatible script language
│   ├── prompt/               questions asked of the user (host keys, overwrite…)
│   ├── server/               JSON-RPC API, WebSocket (events, terminals), file streams, authentication
│   └── desktop/              Wails window (WebView2, WKWebView, WebKitGTK) or browser fallback
├── frontend/                 React 19 + TypeScript + Vite (zustand, TanStack Virtual, CodeMirror 6, xterm.js)
├── testenv/                  Docker test servers (OpenSSH, old OpenSSH, Pure-FTPd + TLS, vsftpd implicit TLS, WebDAV, S3)
└── docs/                     this documentation (French, and English in docs/en), its screenshots and the tool that makes them
```

The interface talks with the Go core through HTTP and WebSocket:

- `POST /api/rpc/<method>` with a JSON body (`fs.list`, `transfer.start`, `sync.compare`, for instance), the `X-Skiff: 1` header and the session cookie;
- `/api/events` (WebSocket) for the events: transfer progress, log, questions, state of the connections;
- `/api/term/<id>` (WebSocket) for the terminals;
- `/api/download` and `/api/upload` to read and send files as streams.

It is the same interface, and the same API, in the native window and in web mode.

## 19.2 Building and testing

```bash
./build.sh            # interface + binary (bin/skiff), native window when WebKitGTK is present
./build.sh portable   # binary without the native window
./build.sh release    # binaries of every platform into dist/
./build.sh packages   # installers (deb, appimage, setup-windows, dmg; see chapter 1)
./build.sh ui         # interface only (frontend/dist)
./build.sh dev        # rebuilds the interface on the fly; Skiff on http://127.0.0.1:8484/?token=dev
./build.sh test       # go vet, unit tests, check of the TypeScript, the translations and the documentation links
./build.sh itest      # end-to-end tests of the API, the web mode, the command line and the scripts
./build.sh uitest     # end-to-end tests of the interface in a browser
```

The `Makefile` offers the same targets. Several commands can follow each other (`./build.sh deb appimage`).

The files of the installers are in `build/`:

| File | Role |
|---|---|
| `build/appicon.png` | Source icon (1024 × 1024) |
| `build/icons.py` | Makes the icons below again from `appicon.png` (needs Pillow) |
| `build/linux/skiff.desktop`, `build/linux/icons/` | Menu entry and icons of the `.deb` package and of the AppImage; a test checks that the entry is the one Skiff writes itself |
| `build/linux/deb.sh`, `build/linux/copyright` | Debian package |
| `build/linux/appimage.sh`, `build/linux/AppRun` | AppImage; `AppRun` chooses the native window or the browser variant |
| `build/windows/installer.nsi`, `build/windows/icon.ico` | Windows installer (NSIS) |
| `build/darwin/bundle.sh`, `build/darwin/dmg.sh`, `build/darwin/skiff.icns` | `Skiff.app` and its disk image |

### Releasing a version

Skiff follows [semantic versioning](https://semver.org/): a fix raises the third number (1.0.1), a new compatible feature the second (1.1.0), an incompatible change the first. Every 1.x version reads the configuration of the earlier 1.x versions; a change of the format of the configuration goes through a migration, preceded by a backup ([§ 1.7](01-installation.md#17-updating-and-uninstalling)).

1. In `CHANGELOG.md`, the **[Unreleased]** section becomes `## [1.0.0] — 2026-11-02` (version and date), and a new empty **[Unreleased]** section comes before it.
2. `python3 build/notices.py` when the dependencies have changed, then `./build.sh test`, `./build.sh itest` and `./build.sh uitest`.
3. An annotated tag: `git tag -a v1.0.0 -m "Skiff 1.0.0"`, then `git push origin v1.0.0`.

The CI then builds the binaries and the installers of the three systems and publishes the release, with `SHA256SUMS`. The version shown by Skiff (`skiff version`, **About**) comes from the tag (`git describe`); without a tag, it is `0.1.0~git.<commit>`.

A change of the format of the configuration adds a migration in `internal/config/format.go` (`FormatVersion` goes up by 1, `migrations[n]` goes from format n to format n+1) and a test that starts from a folder of the old format.

### Signing and the Microsoft Store

| What | How | Settings of the GitHub repository (**Settings → Secrets and variables → Actions**) |
|---|---|---|
| Windows: programs and installers | Signed by SignPath (SignPath Foundation certificate, [CODE_SIGNING.md](../../CODE_SIGNING.md)), for the `v*` tags only: the CI first sends the two programs (artifact configuration `windows-exe`), then the installers made from them (`windows-setup`), and each request is approved by hand in SignPath. The two configurations to paste into SignPath are in `.signpath/artifact-configurations/`. Without `SIGNPATH_ORGANIZATION_ID`, nothing is signed and the CI publishes the files as they are. | variables `SIGNPATH_ORGANIZATION_ID`, `SIGNPATH_PROJECT_SLUG` (`skiff` by default) and `SIGNPATH_POLICY_SLUG` (`release-signing`); secret `SIGNPATH_API_TOKEN` |
| Microsoft Store | The `msix` job (on Windows) makes `Skiff-<version>-windows.msixbundle` (x64 and ARM64) with `build/windows/msix.ps1` and the manifest `build/windows/msix/AppxManifest.xml`: an artifact of each run, to upload to Partner Center, outside the release. The Store signs it. In this package, the `sftp://`… links are declared by the manifest: Skiff detects it and points to the default apps of Windows. | variables `MSIX_IDENTITY_NAME`, `MSIX_PUBLISHER` and `MSIX_PUBLISHER_NAME`, copied from the **Product identity** page of Partner Center |
| macOS | Ad hoc signature only (Apple's notarization is paid); [`install.sh`](../../install.sh) installs Skiff without quarantine. The CI installs the image it has just made with this script and starts `skiff version`. | — |

The CI also installs the Windows installer silently (`/S`) and starts the installed `skiff.exe`.

### Licences

Skiff is under the Apache 2.0 licence (`LICENSE`, `NOTICE`); a contribution is made under the same licence. `THIRD_PARTY_NOTICES.txt` lists the libraries compiled into the program, for each system (`go list`) and for the interface (`pnpm licenses`), with the text of their licence and their `NOTICE` files. The program embeds it (**About**, `skiff licenses`) and the installers copy it next to `LICENSE`.

```bash
python3 build/notices.py           # after adding or updating a dependency
python3 build/notices.py --check   # in ./build.sh test: fails when the file is no longer up to date
```

A new dependency must have a licence compatible with Apache 2.0 (MIT, BSD, ISC, Apache 2.0, MPL 2.0…); a GPL or LGPL library linked statically is not.

### Accessibility

A button that shows only an icon bears a tooltip (`data-tip`): `frontend/src/lib/a11y.ts` makes it its name for the screen readers (`aria-label`), gives the fields of a `.field` the text of their label and tells the ticked choice of the segmented controls (`aria-pressed`). The "accessibility" interface test fails as soon as a visible control of the main screens has no name.

### Languages

Every text of the interface goes through `frontend/src/lib/i18n.ts`, in English and in French (placeholders and examples included: `~/Projects` / `~/Projets`). `./build.sh test` checks that both languages have the same keys.

## 19.3 The tests

The end-to-end tests run against the Docker servers of `testenv/` (started automatically by `itest` and `uitest`). They also use the OpenSSH client of the machine (`ssh-agent`, `ssh-add`, `ssh-keygen`) for the agent and the certificates.

| Suite | Command | What is checked |
|---|---|---|
| Unit tests | `go test ./...` | Masks, scripts (syntax, variables), PPK keys, OpenSSH configuration (quotes, `Host`/`Match`, `Include`, tokens, paths by OS, algorithms) and its priority over the settings of the sites, reading of `/proc` by the monitor, computing of the scheduled runs (cron included), catching up, encryption and merge of the synchronization between devices, WinSCP/OpenSSH/FileZilla import, secrets and master password, site folders, transfer engine, parsing of the FTP and SCP listings, web mode jail, splitting of the editor commands |
| Translations | `node frontend/scripts/check-i18n.mjs` | Each key used by the interface exists in French and in English, and both languages have the same keys |
| Documentation | `python3 docs/check-links.py` | Every link, anchor and image of the French and English chapters leads somewhere, and each chapter has its counterpart in the other language |
| End-to-end API | `go test -tags integration ./internal/server/` | `TestE2E` starts a real Skiff server and calls **each method of the API** as the interface does, checking their effect on the servers: sites, import and export, the five protocols and the local disk, transfers and all their options, queue, server to server transfers, synchronization (directions, modes, criteria), watching, search, external editor, terminals, `-L`/`-R`/`-D` tunnels, jump hosts (FTP, WebDAV and S3 behind SSH included), SOCKS4/5, HTTP, Telnet and command proxies, keys and conversion, a real SSH agent (`ssh-agent`: connection with the keys of the agent alone, `IdentitiesOnly` filtering the keys of the agent, `IdentityFile` naming the public key or a missing private key, `IdentityAgent` towards another socket, a variable or `none`, passphrase not asked when the agent holds the key), OpenSSH certificates signed by `ssh-keygen` (`<key>-cert.pub` next to the key, `CertificateFile`, key in the agent, certificate of another user refused), host keys, legacy algorithms (refused without the option, connection with **Allow legacy algorithms** or with `KexAlgorithms`/`Ciphers`/`HostKeyAlgorithms` in `ssh_config`), OpenSSH configuration (alias, `IdentityFile` alone, `StrictHostKeyChecking`, `HostKeyAlias`, `ProxyJump`, `ProxyCommand`, `LocalForward` opened at the connection, system file, turning it off), TLS certificates, implicit TLS FTPS (connection by an `ftps://` URL, every file operation, transfers both ways), master password, cut and reconnection, transfers in parallel parts (upload, download, server to server, cancel), keychain and agent forwarding (`ssh-add -l` in a remote terminal), authorized keys (list, key of the session, removal refused without a confirmation or from an outdated list, replacement checked by a second connection, replacement refused without the private key), links of the system (`mimeapps.list`, URL handed to the open window, key and `Origin` header checked, `docker://` links refused), monitor and stopping a process, containers (real Docker, Docker through SSH, Kubernetes), scheduled tasks (synchronization, script, failure, cancel, history), synchronization between two devices (encryption, wrong passphrase, deletions passed on). The test fails when a method of the API has not been exercised. `TestE2EWeb` checks the web mode (authentication, rate limiting, CSRF, origin, `--root` jail, features turned off). |
| Command line | `go test -tags integration .` | Builds the binary and runs every script command, the `winscp.com` compatibility (`/script`, `/command`, `/parameter`, `/log`, `/ini`), the `batch` modes, `keepuptodate`, the console (in a pseudo-terminal), `keygen`, `import`, `serve`, the opening of a host of `~/.ssh/config` by its name, `skiff task` and the opening of a URL at start, the second start that hands its link to the open window (and the start after a crash), the connection with a key that only an `ssh-agent` holds |
| Protocols | `go test -tags integration ./internal/core/` | Basic operations on each protocol |
| Interface | `cd test/ui && pnpm test` | A real browser drives the interface: connection and host key, every file operation, editor and conflicts, clipboard and drag and drop, overwrite, synchronization, watching, search, terminal, tunnels, palette, preferences, sites, hosts of the OpenSSH configuration (detected settings, connection, editor, SSH preferences), split terminal (broadcast input, saved commands, folder following), monitor, containers, scheduled tasks, keychain, authorized keys (checked replacement, removal), key installed on several sites, links (Preferences → System, clicked link confirmed, host key of the link ignored), synchronization between devices, import and export, comparison, bookmarks, filter and selection, tree, preview, layout, diff, checksums, links, commands, properties, transfer queue, server information, log, external editor, tabs, URL and workspaces, keys, master password, cut and reconnection, reopening of the tabs at start (with their folders, password asked again for a quick connection), web mode, portable mode (`skiff.portable` file then `skiff-data` folder next to the binary, nothing in the configuration folder of the user). Each action is checked by its effect on the server, and no JavaScript error must happen. On a failure, a screenshot is saved (`UI_SHOTS=folder`). |

Tests with random data (*fuzzing*) of the parsers of what comes from outside; each also runs for 30 seconds in the CI (`security` job), with `govulncheck` and `pnpm audit`:

```bash
go test -run '^$' -fuzz '^FuzzParseURL$' -fuzztime 1m ./internal/config/
# FuzzParseAuthorized (keys), FuzzListingLines (vfs/ftpfs), FuzzListings (vfs/scpfs),
# FuzzSafeName (vfs), FuzzLines (sshconfig), FuzzTokenize (script), FuzzServerOutputs (core)
```

An input that makes a test fail is kept in the `testdata/fuzz/` folder of the package: `go test` then replays it every time.

The SSH test server is prepared by the scripts of `testenv/ssh-init/`:

- `45-no-penalties` turns `PerSourcePenalties` off (OpenSSH 9.8 and later): the tests fail to connect on purpose, and sshd would then refuse every connection from the machine for a while;
- `50-tcp-forwarding` allows port forwarding, needed by the tunnels and the jump hosts;
- `60-user-ca` creates at the first start a user certification authority (`/config/ssh_ca/user_ca`) that sshd trusts (`TrustedUserCAKeys`); the tests read its private key through SFTP to sign their own keys.

The "old" server (`testenv/ssh-legacy-init/`) accepts only the `diffie-hellman-group14-sha1` and `diffie-hellman-group1-sha1` key exchanges, the `ssh-rsa` signature, the CBC ciphers and `hmac-sha1`. The implicit FTPS server is a vsftpd image built by `docker compose` (`testenv/ftps-implicit/`). Both FTP servers have enough connections and passive ports for the three `itest` suites, which run at the same time: vsftpd gives a session up (`500 OOPS: vsf_sysutil_bind`) when ten random draws find no free passive port. After a change of `testenv/`, create the containers again: `docker compose -f testenv/docker-compose.yml up -d --build`.

Test servers (`testenv/docker-compose.yml`): SSH on `127.0.0.1:2222`, old SSH on `127.0.0.1:2223`, FTP on `127.0.0.1:2121` (explicit TLS, passive ports 30000 to 30029), FTPS on `127.0.0.1:9990` (implicit TLS, in the style of port 990, passive ports 30030 to 30059), WebDAV on `http://127.0.0.1:8080`, S3 on `http://127.0.0.1:9000` (`skiffadmin` / `skiffsecret`). Credentials: `skiff` / `skiff`.

Useful variables during development:

| Variable | Effect |
|---|---|
| `SKIFF_NO_WINDOW=1` | Opens no window: only the local server starts |
| `SKIFF_PORT` | Port of the local server |
| `SKIFF_DEV_TOKEN` | Fixed token (instead of a random one) |
| `SKIFF_UI_DIR` | Serves the interface from this folder instead of the one built into the binary; this Skiff does not coordinate with a window already open (no `instance.json`) |
| `SKIFF_PRINT_URL=1` | Shows the URL of the interface at start |
| `SKIFF_HOME` | Configuration folder |
| `SKIFF_SSH_SYSTEM_CONFIG` | Replaces the `ssh_config` file of the system (`none`: read none); the tests use it so as not to depend on the machine |

## 19.4 Making the screenshots again

All the images of `docs/images` (French) and `docs/en/images` (English) are made automatically by `docs/capture`, which drives the interface of Skiff with Playwright on demonstration data. Just run the tool again after a change of the interface, in both languages.

Requirements: Docker, Node 22 or later, pnpm, Python 3 and a built Skiff binary.

```bash
# 1. test servers
docker compose -f testenv/docker-compose.yml up -d

# 2. capture tool (once)
cd docs/capture && pnpm install && pnpm exec playwright install chromium && cd ../..

# 3. demonstration data: a fake home folder and the content of the servers
./docs/capture/demo-setup.sh /tmp/skiff-demo

# 4. screenshots (all, or only the scenes whose name matches)
DEMO_DIR=/tmp/skiff-demo SKIFF_BIN=bin/skiff node docs/capture/capture.mjs
DEMO_DIR=/tmp/skiff-demo SKIFF_BIN=bin/skiff node docs/capture/capture.mjs '^(connexion-sftp|editeur)$'

# 5. the same in English, into docs/en/images
DEMO_LANG=en ./docs/capture/demo-setup.sh /tmp/skiff-demo-en
CAPTURE_LANG=en DEMO_DIR=/tmp/skiff-demo-en SKIFF_BIN=bin/skiff node docs/capture/capture.mjs

# 6. shrink the images (256-colour palette, needs Pillow)
python3 docs/capture/optimize-images.py
python3 docs/capture/optimize-images.py docs/en/images
```

The tool starts Skiff without a window, with a demonstration home folder and configuration, creates the sites, then plays some forty scenes (sites, connection, menus, transfers, synchronization, editor, terminal, tunnels, preferences, themes, locking, web mode…). The screenshots are taken at 1280 × 800 with a 1.5 scale, with the light theme. A scene that fails leaves a `fail-<scene>.png` screenshot in the demonstration folder; the other scenes go on. The `sites` scene, which creates the sites, is always played, and `connexion-sftp` must come before the scenes that use the SFTP session. The servers keep the content of the last demonstration made: run `demo-setup.sh` again before changing language.

### Two languages

The scenes are written once, in French. In English:

- `demo-setup.sh` with `DEMO_LANG=en` makes the same demonstration, then `translate-demo.py` translates it with `demo-en.json`: the names of the files and folders (`Projets/site-vitrine` → `Projects/showcase-site`), their texts, the containers (`shop-web`), keeping the dates of the files;
- `capture.mjs` with `CAPTURE_LANG=en` starts Skiff in English and translates each text it clicks or types with `T()`: the demonstration data through the same `demo-en.json`, the labels of the interface through `i18n.ts` (the French label gives its key, the key its English label). Where a French label has several English ones (`Connexion`: *Connect* or *Connection*), the scene names the key with `K('site.connection')`. At the end, the run lists the texts it could not translate: add them to `demo-en.json`, or use `K()`.

A new name in the demonstration data goes into `demo-en.json`; a new chapter, or a change of a chapter, is made in both languages: `docs/NN-nom.md` and `docs/en/NN-name.md` (the same number), each with the link to the other at the top and at the bottom. `python3 docs/check-links.py` checks that every link leads somewhere.

---

[← WinSCP → Skiff map](18-winscp.md) · [Contents](README.md) · 🇫🇷 [Français](../19-developpement.md)
