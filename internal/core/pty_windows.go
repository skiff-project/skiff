//go:build windows

package core

import (
	"context"
	"os"
	"strings"

	"github.com/UserExistsError/conpty"
)

type localPty struct{ c *conpty.ConPty }

func startLocalPty(cols, rows int, cwd, term string) (ptyBackend, error) {
	shell := "powershell.exe -NoLogo"
	if _, err := os.Stat(`C:\Program Files\PowerShell\7\pwsh.exe`); err == nil {
		shell = `"C:\Program Files\PowerShell\7\pwsh.exe" -NoLogo`
	}
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	c, err := conpty.Start(shell, conpty.ConPtyDimensions(cols, rows), conpty.ConPtyWorkDir(cwd))
	if err != nil {
		return nil, err
	}
	return &localPty{c: c}, nil
}

// startLocalPtyCmd runs argv in a pseudo-console of this computer.
func startLocalPtyCmd(cols, rows int, cwd, term string, argv []string) (ptyBackend, error) {
	q := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		q[i] = a
	}
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	c, err := conpty.Start(strings.Join(q, " "), conpty.ConPtyDimensions(cols, rows), conpty.ConPtyWorkDir(cwd))
	if err != nil {
		return nil, err
	}
	return &localPty{c: c}, nil
}

func (l *localPty) Read(p []byte) (int, error)  { return l.c.Read(p) }
func (l *localPty) Write(p []byte) (int, error) { return l.c.Write(p) }
func (l *localPty) Resize(cols, rows int) error  { return l.c.Resize(cols, rows) }
func (l *localPty) Close() error                 { return l.c.Close() }
func (l *localPty) Wait() error {
	_, err := l.c.Wait(context.Background())
	return err
}
