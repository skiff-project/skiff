# 13. Scripts and command line

[← Preferences](12-preferences.md) · [Contents](README.md) · [Remote web mode →](14-web-mode.md) · 🇫🇷 [Français](../13-scripts-cli.md)

The `skiff` binary is also a command line tool. Its script language follows that of `winscp.com`: existing WinSCP scripts work as they are in most cases.

## 13.1 Overview

```text
skiff [session-url]              opens the window (and connects, if asked)
skiff serve [options]            web interface for a remote use
skiff run [options] [script]     runs a WinSCP-compatible script
skiff shell [session-url]        interactive console
skiff keygen [options]           generates (or converts, -convert) an SSH key pair
skiff import <source> [file]     imports sessions
skiff task list | run <name>     lists the scheduled tasks, or runs one
skiff desktop-entry [-remove]    entry of the application menu (Linux)
skiff url-handler [-register | -remove]  sftp://, ftp://… links opened by Skiff
skiff licenses [-full]           licence of Skiff and of the software it includes
skiff version                    shows the version
skiff help                       shows the help
```

`skiff --link <url>` is the form the system uses for a link clicked elsewhere: the window shows the connection and asks before connecting ([§ 12.14](12-preferences.md#1214-system)). Without `--link`, a URL on the command line connects straight away. When Skiff is already open, the command hands the URL to its window and ends.

`skiff <command> -h` details the options of each command. Scripts and the console use the sites, the preferences, the host keys and the saved passwords of the usual configuration.

## 13.2 `skiff run`

```bash
skiff run backup.txt
skiff run -c "open sftp://deploy@prod.example.com -hostkey=SHA256:abc…" \
          -c "synchronize remote -delete ./site /var/www" -c exit
skiff run -log=deploy.log -batch=continue deploy.txt
```

| Option | Description |
|---|---|
| `-c "command"` | Command to run, can be repeated. The `-c` commands run after those of the script file, if any. |
| `-batch=abort` | The default: the first error stops the script with exit code 1 |
| `-batch=continue` | The errors are shown and the script goes on (the failed files are skipped) |
| `-batch=off` | Interactive mode: the questions (host key, overwrite…) are asked in the terminal |
| `-log=file` | Writes the session log into this file |

In a script file, the empty lines and the lines starting with `#` or `;` are ignored. The exit code is 0 on success and 1 on an error.

## 13.3 Script commands

### Sessions

| Command | Description |
|---|---|
| `open <url\|site> [options]` | Opens a session. The argument is a [session URL](15-protocols.md#158-session-urls), the name of a saved site (`Production/Showcase site (SFTP)` or just `Showcase site (SFTP)`) or a host of `~/.ssh/config` (`open staging`, as `ssh staging`: address, port, user, keys and jumps come from the file, see [OpenSSH configuration](03-sites.md#34-openssh-configuration-ssh_config)). |
| `close [n]` | Closes the current session (or session `n`) |
| `session [n]` | Lists the open sessions, or makes session `n` the current one. Several sessions can be open at the same time. |
| `exit` (`bye`, `quit`) | Closes the sessions and ends the script |

Options of `open`:

| Option | Description |
|---|---|
| `-hostkey=SHA256:…` | Expected host key fingerprint. `-hostkey=*` accepts any key (at your own risk). In non-interactive mode, an unknown key is refused without this option. |
| `-password=…` | Password (an environment variable is better: `-password=%PASSWORD%`) |
| `-privatekey=file` | Private key (OpenSSH or `.ppk`) |
| `-passphrase=…` | Passphrase of the key |
| `-certificate=fingerprint\|*` | Expected TLS certificate fingerprint (FTPS, WebDAV, S3) |
| `-passive=on\|off` | Passive or active mode (FTP) |
| `-explicit`, `-implicit` | Explicit or implicit TLS (FTP) |
| `-timeout=s` | Connection timeout in seconds |
| `-rawsettings …` | Accepted for compatibility with WinSCP, without effect |

### Navigation

| Command | Description |
|---|---|
| `pwd`, `cd [folder]`, `ls [folder\|mask]` (`dir`) | Current remote folder, change, listing |
| `lpwd`, `lcd <folder>`, `lls [folder]` (`ldir`) | Local equivalents |

### Transfers

| Command | Description |
|---|---|
| `get [options] <remote>... [local]` | Downloads files or folders (masks accepted: `get /logs/*.log ./logs/`) |
| `put [options] <local>... [remote]` | Uploads files or folders |

Options of `get` and `put`:

| Option | Description |
|---|---|
| `-delete` | Deletes the source after the transfer (a move) |
| `-resume`, `-append` | Resumes or completes an existing file |
| `-preservetime`, `-nopreservetime` | Keeps the modification date or not |
| `-permissions=644`, `-nopermissions` | Forces permissions, or does not transfer the permissions |
| `-speed=KiB` | Speed limit in KiB/s |
| `-transfer=binary\|ascii\|automatic` | Transfer mode |
| `-filemask=mask` | Inclusion and exclusion [mask](06-transfers.md#63-file-masks) |
| `-neweronly` | Only the new or newer files |
| `-latest` | Only the newest file among those that match |

### Files

| Command | Description |
|---|---|
| `mkdir <folder>` | Creates a folder |
| `rm <file\|mask>...` (`del`), `rmdir <folder>` | Deletes |
| `mv <source> <destination>` (`rename`) | Renames or moves on the server |
| `cp <source> <destination>` | Copies on the server |
| `chmod <mode> <file>... [-recursive]` | Changes the permissions |
| `ln <target> <link>` (`symlink`) | Creates a symbolic link; `ln -hard <target> <link>` creates a hard link |
| `stat <file>` | Shows the attributes |
| `checksum <md5\|sha1\|sha256\|sha512> <file>` | Computes a checksum on the server |
| `call <command>` (`!`) | Runs a shell command on the server (SFTP, SCP); a non-zero exit code is an error |

### Synchronization

| Command | Description |
|---|---|
| `synchronize local\|remote\|both [options] [local folder] [remote folder]` (`sync`) | Synchronizes as the [synchronization dialog](07-synchronization.md) does; a missing destination folder is created. Options: `-delete`, `-mirror`, `-criteria=time\|size\|either\|checksum\|none`, `-filemask=…`, `-transfer=…`, `-preview` (shows the actions without running them) |
| `keepuptodate [-delete] [-filemask=…] [local folder] [remote folder]` | Watches the local folder and uploads the changes until `Ctrl+C` |

### Miscellaneous

| Command | Description |
|---|---|
| `option` | Without an argument: shows the current options |
| `option batch off\|on\|abort\|continue` | Error handling (see `-batch`; `on` is the same as `abort`) |
| `option confirm on\|off` | With `off`, existing files are overwritten without a question |
| `option transfer binary\|ascii\|automatic` | Default transfer mode |
| `option echo on\|off` | Shows each command before running it |
| `option failonnomatch on\|off` | A mask matching nothing is an error |
| `echo <text>` | Shows a text |
| `help` (`?`) | List of the commands |

### Variables

| Syntax | Replaced by |
|---|---|
| `%NAME%` | Environment variable `NAME` |
| `%1%`, `%2%`… | Parameters given with `/parameter` |
| `%TIMESTAMP#yyyy-mm-dd%` | Current date and time; codes `yyyy`, `yy`, `mm`, `dd`, `hh`, `nn` (minutes), `ss`, `zzz` (milliseconds) |
| `%TIMESTAMP-1D#yyyymmdd%` | Shifted date: `+` or `-` followed by a number and a unit `Y`, `M` (months), `D`, `H`, `N`, `S` |

### Complete example

```text
# backup.txt — daily backup of the logs
option batch abort
option confirm off
open sftp://backup@server.example.com/ -hostkey=SHA256:Fh7jA0…
lcd /srv/backups
get -neweronly /var/log/nginx/*.log logs-%TIMESTAMP#yyyy-mm-dd%/
synchronize local -criteria=either ./site /var/www/site
call df -h /var
close
exit
```

## 13.4 Interactive console: `skiff shell`

```bash
skiff shell sftp://alex@server.example.com
```

A console where the script commands are typed one by one. The prompt shows the current session and folder (`skiff@server:/var/www> `). In a terminal, the questions (host key, password, overwrite) are asked with numbered choices (`1) Accept  2) Connect once  3) Cancel`). When the input of the console is redirected (a file, a pipe), it behaves as a script: an unknown host key is refused, hence the `-hostkey=` option. `help` lists the commands, `exit` or `Ctrl+D` quits.

## 13.5 `winscp.com` compatibility

Skiff accepts the command line syntax of `winscp.com`, which allows replacing WinSCP in existing scheduled tasks:

```bat
skiff /script=backup.txt /log=backup.log
skiff /command "open ftpes://depot:secret@ftp.example.com/" "get /exports/*.csv C:\data\" "exit"
skiff /script=upload.txt /parameter // C:\reports\report.pdf /depot/
```

| Option | Description |
|---|---|
| `/script=file` | Runs a script file |
| `/command "cmd1" "cmd2"…` | Runs the commands given (after the script, if any) |
| `/log=file` | Session log |
| `/ini=folder` | Configuration folder to use (`/ini=nul`: ignored) |
| `/parameter // a b…` | Parameters reached with `%1%`, `%2%`… |

The error mode is `abort`: the first error stops the script with code 1, as `winscp.com` does in batch mode.

## 13.6 `skiff import`

```bash
skiff import                         # lists the sources and what was detected
skiff import winscp-ini              # WinSCP.ini detected automatically
skiff import winscp-ini C:\Users\alex\Desktop\WinSCP.ini
skiff import ssh-config
skiff import filezilla ~/sitemanager.xml
```

Sources: `winscp-ini`, `winscp-registry` (Windows), `putty`, `filezilla`, `ssh-config`, `skiff-json`, `urls`. Each site imported is shown, with a remark when its password could not be recovered. See also the [import in the interface](03-sites.md#35-importing-sites).

## 13.7 `skiff keygen`

```bash
skiff keygen                               # Ed25519, ~/.ssh/id_ed25519, passphrase asked for
skiff keygen -t rsa -b 4096 -C "alex@laptop" -f ~/.ssh/id_rsa_work
skiff keygen -t ecdsa -b 384 -ppk -N       # PuTTY format, without a passphrase
skiff keygen -convert ~/.ssh/id_ed25519 -ppk          # OpenSSH → PuTTY (id_ed25519.ppk)
skiff keygen -convert key.ppk -f ~/.ssh/id_key        # PuTTY → OpenSSH
```

| Option | Description |
|---|---|
| `-t ed25519\|ecdsa\|rsa` | Key type (Ed25519 by default) |
| `-b bits` | Size: RSA 2048 to 8192, ECDSA 256, 384 or 521 |
| `-C comment` | Comment (`user@machine` by default) |
| `-f file` | File of the private key (`~/.ssh/id_<type>` by default). The public key is written into `<file>.pub`. |
| `-ppk` | PuTTY `.ppk` v3 format instead of OpenSSH |
| `-N` | No passphrase |
| `-convert file` | Converts an existing key instead of generating one: to PuTTY with `-ppk`, to OpenSSH otherwise. Asks for the current passphrase and, unless `-N` is given, a new passphrase. |

The command shows the path of the private key, the fingerprint and the public key. An existing file is never overwritten.

## 13.8 `skiff serve`

See [Remote web mode](14-web-mode.md).

## 13.9 `skiff task`

Lists the [scheduled tasks](07-synchronization.md#74-scheduled-tasks) or runs one at once, without opening Skiff: it is the command to hand to the scheduler of the system (cron, the Windows Task Scheduler, launchd) for a task to run while Skiff is closed.

```bash
skiff task list                        # state, name, type, schedule, last run
skiff task run "Publish the site"      # name (case ignored) or identifier
skiff task run -q "Publish the site"   # shows only the result, not the log
```

`run` shows the log while running, then an `OK: …`, `FAILED: …` or `CANCELLED: …` line with the duration. The exit code is 0 on success, 1 otherwise (unknown task included). The run is added to the history of the task, with the "command line" trigger.

## 13.10 `skiff desktop-entry`

On Linux and BSD, installs (or updates) the entry of Skiff in the application menu and its icon, for the user; the window does it by itself at its first start ([§ 1.3](01-installation.md#13-starting-skiff)).

```bash
skiff desktop-entry           # ~/.local/share/applications/skiff.desktop and the icon
skiff desktop-entry -remove   # removes them
```

Started from an AppImage, the entry points to the `.AppImage` file. When the `.deb` package is installed, the command adds nothing ("nothing to do: the package of Skiff installed /usr/share/applications/skiff.desktop") and removes the entry it had added, which would hide that of the package.

## 13.11 `skiff url-handler`

Tells which application opens each `sftp://`, `scp://`, `ftp://`, `ftps://`, `ftpes://`, `dav://` and `davs://` link, or makes Skiff that application, as **Preferences → System** does ([§ 12.14](12-preferences.md#1214-system)). The Windows installer uses it.

```bash
skiff url-handler              # sftp:  Skiff, ftp:  Files…
skiff url-handler -register    # the links open with Skiff
skiff url-handler -remove      # they go back to the other applications
```

## 13.12 `skiff licenses`

Shows the notice of Skiff (Apache 2.0 licence) and the list of the libraries included with their licence; `-full` adds the complete text of each licence.

---

[← Preferences](12-preferences.md) · [Contents](README.md) · [Remote web mode →](14-web-mode.md) · 🇫🇷 [Français](../13-scripts-cli.md)
