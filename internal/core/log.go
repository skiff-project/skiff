package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogLine struct {
	Time  time.Time `json:"t"`
	Level string    `json:"l"` // info, cmd, resp, warn, error
	Msg   string    `json:"m"`
}

// Log keeps the recent session log in memory and optionally on disk.
type Log struct {
	conn  string
	bus   *Bus
	mu    sync.Mutex
	lines []LogLine
	max   int
	file  *os.File
	debug bool
}

func newLog(conn string, bus *Bus, dir string, toFile bool, title string) *Log {
	l := &Log{conn: conn, bus: bus, max: 5000}
	if toFile && dir != "" {
		os.MkdirAll(dir, 0o700)
		name := fmt.Sprintf("%s_%s.log", time.Now().Format("20060102-150405"), sanitize(title))
		if f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			l.file = f
		}
	}
	return l
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, s)
}

func (l *Log) add(level, msg string) {
	if l == nil {
		return
	}
	ln := LogLine{Time: time.Now(), Level: level, Msg: msg}
	l.mu.Lock()
	l.lines = append(l.lines, ln)
	if len(l.lines) > l.max {
		l.lines = append([]LogLine(nil), l.lines[len(l.lines)-l.max*4/5:]...)
	}
	if l.file != nil {
		fmt.Fprintf(l.file, "%s %-5s %s\n", ln.Time.Format("2006-01-02 15:04:05.000"), level, msg)
	}
	l.mu.Unlock()
	if l.bus != nil {
		l.bus.Publish("log", map[string]any{"conn": l.conn, "line": ln})
	}
}

func (l *Log) Logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	level := "info"
	switch {
	case strings.HasPrefix(msg, "> "):
		level = "cmd"
	case strings.HasPrefix(msg, "< "):
		level = "resp"
	}
	l.add(level, msg)
}

func (l *Log) Errorf(format string, args ...any) { l.add("error", fmt.Sprintf(format, args...)) }
func (l *Log) Warnf(format string, args ...any)  { l.add("warn", fmt.Sprintf(format, args...)) }

func (l *Log) Lines() []LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogLine(nil), l.lines...)
}

func (l *Log) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}
