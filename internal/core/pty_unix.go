//go:build !windows

package core

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type localPty struct {
	f   *os.File
	cmd *exec.Cmd
}

func startLocalPty(cols, rows int, cwd, term string) (ptyBackend, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return startLocalPtyCmd(cols, rows, cwd, term, []string{sh, "-l"})
}

// startLocalPtyCmd runs argv in a pseudo-terminal of this computer.
func startLocalPtyCmd(cols, rows int, cwd, term string, argv []string) (ptyBackend, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM="+term)
	if cwd != "" {
		cmd.Dir = cwd
	} else if h, err := os.UserHomeDir(); err == nil {
		cmd.Dir = h
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &localPty{f: f, cmd: cmd}, nil
}

func (l *localPty) Read(p []byte) (int, error)  { return l.f.Read(p) }
func (l *localPty) Write(p []byte) (int, error) { return l.f.Write(p) }
func (l *localPty) Resize(cols, rows int) error {
	return pty.Setsize(l.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
func (l *localPty) Close() error {
	if l.cmd.Process != nil {
		l.cmd.Process.Kill()
	}
	return l.f.Close()
}
func (l *localPty) Wait() error { return l.cmd.Wait() }
