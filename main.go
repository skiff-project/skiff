// Skiff — a modern, cross-platform, single-binary file transfer client:
// SFTP, SCP, FTP/FTPS, WebDAV and S3 with a dual-pane commander, transfer
// queue, synchronisation, integrated editor, SSH terminal, tunnels and a
// WinSCP-compatible scripting engine.
package main

import (
	"fmt"
	"os"
	"strings"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Skiff %s — modern cross-platform file transfer client

Usage:
  skiff [session-url]              open the desktop window (optionally connecting)
  skiff serve [flags]              run the web UI for remote/browser use
  skiff run [flags] [script]       run a WinSCP-compatible script
  skiff shell [session-url]        interactive console
  skiff keygen [flags]             generate (or -convert) an SSH key pair (OpenSSH or PuTTY .ppk)
  skiff import <source> [file]     import sessions (winscp-ini, winscp-registry, putty, filezilla, ssh-config)
  skiff task list | run <name>     list the scheduled tasks, or run one now (for the system scheduler)
  skiff desktop-entry [-remove]    install (or remove) the menu entry and the icon (Linux)
  skiff url-handler [-register|-remove]  open (or no longer) the sftp://, ftp://… links with Skiff
  skiff licenses [-full]           the licence of Skiff (Apache-2.0) and of what it includes
  skiff version                    print the version

WinSCP compatibility:
  skiff /script=<file> [/log=<file>] [/parameter // args...]
  skiff /command "open sftp://..." "get *.txt" "exit"

Session URLs: sftp://user@host:22/path, scp://, ftp://, ftps:// (implicit),
ftpes:// (explicit TLS), dav://, davs://, s3://, docker://, podman://, kubectl://

Run "skiff <command> -h" for the flags of a command.
`

func main() {
	args := os.Args[1:]
	if hasWinSCPArgs(args) {
		os.Exit(runWinSCPCompat(args))
	}
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	var code int
	switch cmd {
	case "serve", "web":
		code = cmdServe(args[1:])
	case "run", "script":
		code = cmdRun(args[1:])
	case "shell", "console":
		code = cmdShell(args[1:])
	case "keygen":
		code = cmdKeygen(args[1:])
	case "import":
		code = cmdImport(args[1:])
	case "task", "tasks":
		code = cmdTask(args[1:])
	case "desktop-entry":
		code = cmdDesktopEntry(args[1:])
	case "url-handler":
		code = cmdURLHandler(args[1:])
	case "licenses", "licences", "license", "licence":
		code = cmdLicenses(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("skiff %s\n", version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		if cmd != "" && !strings.Contains(cmd, "://") && !strings.HasPrefix(cmd, "-") {
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
			fmt.Printf(usage, version)
			os.Exit(2)
		}
		code = cmdGUI(args)
	}
	os.Exit(code)
}

func hasWinSCPArgs(args []string) bool {
	for _, a := range args {
		l := strings.ToLower(a)
		if strings.HasPrefix(l, "/script=") || l == "/command" || strings.HasPrefix(l, "/command=") {
			return true
		}
	}
	return false
}
