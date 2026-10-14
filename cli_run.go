package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"skiff/internal/core"
	"skiff/internal/script"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, "; ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func newScriptCore(r *script.Runner, logFile string) (*core.Core, func(), error) {
	store, err := openStore()
	if err != nil {
		return nil, nil, err
	}
	c := core.New(store, core.Options{Prompter: r.Prompter()})
	r.SetCore(c)
	cleanup := func() { c.Shutdown() }
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, err
		}
		events, unsub := c.Bus.Subscribe()
		go func() {
			for ev := range events {
				if ev.Type != "log" {
					continue
				}
				m, _ := ev.Data.(map[string]any)
				if ln, ok := m["line"].(core.LogLine); ok {
					fmt.Fprintf(f, "%s %-5s %s\n", ln.Time.Format("2006-01-02 15:04:05.000"), ln.Level, ln.Msg)
				}
			}
		}()
		cleanup = func() { c.Shutdown(); unsub(); f.Close() }
	}
	return c, cleanup, nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// cmdRun runs a script file and/or -c commands.
func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var cmds multiFlag
	fs.Var(&cmds, "c", "command to run (repeatable)")
	batch := fs.String("batch", "abort", "error handling: abort, continue or off (interactive prompts)")
	logFile := fs.String("log", "", "write the session log to this file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: skiff run [flags] [script-file]\n\nRuns WinSCP-compatible commands. Example:\n  skiff run -c \"open sftp://me@host -hostkey=*\" -c \"put *.zip /backups/\" -c exit\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	var lines []string
	if fs.NArg() > 0 {
		l, err := script.ReadScript(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		lines = l
	}
	lines = append(lines, cmds...)
	if len(lines) == 0 {
		fs.Usage()
		return 2
	}
	return runLines(lines, *batch, *logFile)
}

func runLines(lines []string, batch, logFile string) int {
	r := script.NewRunner(os.Stdout)
	r.SetBatch(batch)
	_, cleanup, err := newScriptCore(r, logFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()
	ctx, cancel := signalContext()
	defer cancel()
	if err := r.RunLines(ctx, lines); err != nil {
		return 1
	}
	return 0
}

// cmdShell starts an interactive console.
func cmdShell(args []string) int {
	r := script.NewRunner(os.Stdout)
	_, cleanup, err := newScriptCore(r, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fmt.Printf("Skiff %s interactive console. Type \"help\" for commands.\n", version)
	if len(args) > 0 {
		if err := r.Exec(ctx, "open "+quoteArg(args[0])); err != nil {
			fmt.Println("Error:", err)
		}
	}
	r.Interactive(ctx)
	return 0
}

func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// runWinSCPCompat accepts winscp.com style arguments:
//   /script=file /command "cmd" ... /log=file /ini=... /parameter // a b
func runWinSCPCompat(args []string) int {
	var lines []string
	logFile := ""
	var params []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		l := strings.ToLower(a)
		switch {
		case strings.HasPrefix(l, "/script="):
			sl, err := script.ReadScript(a[len("/script="):])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			lines = append(lines, sl...)
		case l == "/command":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "/") {
				i++
				lines = append(lines, args[i])
			}
		case strings.HasPrefix(l, "/log="):
			logFile = a[len("/log="):]
		case strings.HasPrefix(l, "/ini="):
			v := a[len("/ini="):]
			if v != "nul" && v != "" {
				if st, err := os.Stat(v); err == nil && st.IsDir() {
					os.Setenv("SKIFF_HOME", v)
				}
			}
		case l == "/parameter":
			for i+1 < len(args) {
				i++
				if args[i] == "//" {
					continue
				}
				params = append(params, args[i])
			}
		}
	}
	for i, p := range params {
		os.Setenv(fmt.Sprint(i+1), p)
	}
	return runLines(lines, "abort", logFile)
}

var _ = io.Discard
