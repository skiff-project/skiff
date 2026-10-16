package core

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// localWatch refreshes the local folders shown in the panels when their
// content changes outside Skiff (a download from a browser, a file saved by
// another application), like WinSCP's local panel.
type localWatch struct {
	mu     sync.Mutex
	w      *fsnotify.Watcher
	dirs   map[string]bool
	timers map[string]*time.Timer
}

// WatchLocalDirs replaces the set of local folders to watch.
func (c *Core) WatchLocalDirs(dirs []string) error {
	lw := &c.localWatch
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.w == nil {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return err
		}
		lw.w, lw.dirs, lw.timers = w, map[string]bool{}, map[string]*time.Timer{}
		go c.runLocalWatch(w)
	}
	want := map[string]bool{}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		d = filepath.Clean(d)
		if root := c.Local.Root; root != "" && d != filepath.Clean(root) && !strings.HasPrefix(d, filepath.Clean(root)+string(filepath.Separator)) {
			continue // outside the web mode jail
		}
		want[d] = true
	}
	for d := range lw.dirs {
		if !want[d] {
			lw.w.Remove(d)
			delete(lw.dirs, d)
		}
	}
	for d := range want {
		if !lw.dirs[d] && lw.w.Add(d) == nil {
			lw.dirs[d] = true
		}
	}
	return nil
}

func (c *Core) runLocalWatch(w *fsnotify.Watcher) {
	lw := &c.localWatch
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			dir := filepath.Dir(ev.Name)
			lw.mu.Lock()
			if lw.dirs[dir] {
				// coalesce bursts (a copy writes many events)
				if t := lw.timers[dir]; t != nil {
					t.Stop()
				}
				lw.timers[dir] = time.AfterFunc(300*time.Millisecond, func() { c.Changed(LocalID, dir) })
			}
			lw.mu.Unlock()
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (c *Core) closeLocalWatch() {
	lw := &c.localWatch
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.w != nil {
		lw.w.Close()
		lw.w = nil
	}
}
