package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"skiff/internal/mask"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
	"skiff/internal/vfs/localfs"
)

// WatchRequest configures "Keep remote directory up to date".
type WatchRequest struct {
	LocalDir      string `json:"localDir"`
	RemoteConn    string `json:"remoteConn"`
	RemoteDir     string `json:"remoteDir"`
	Delete        bool   `json:"delete"`
	FileMask      string `json:"fileMask,omitempty"`
	ExcludeHidden bool   `json:"excludeHidden,omitempty"`
	InitialSync   bool   `json:"initialSync,omitempty"`
	Recursive     bool   `json:"recursive"`
	Delay         int    `json:"delay,omitempty"` // ms debounce
}

type WatchEvent struct {
	Time   time.Time `json:"time"`
	Action string    `json:"action"` // upload, delete, mkdir, error, info
	Path   string    `json:"path"`
	Error  string    `json:"error,omitempty"`
}

type WatchInfo struct {
	ID      string       `json:"id"`
	Req     WatchRequest `json:"req"`
	Started time.Time    `json:"started"`
	Events  []WatchEvent `json:"events"`
	Uploads int          `json:"uploads"`
	Deletes int          `json:"deletes"`
}

type watcher struct {
	info   WatchInfo
	mu     sync.Mutex
	w      *fsnotify.Watcher
	cancel context.CancelFunc
	m      *mask.Mask
}

type watchers struct {
	core *Core
	mu   sync.Mutex
	m    map[string]*watcher
}

func newWatchers(c *Core) *watchers { return &watchers{core: c, m: map[string]*watcher{}} }

func (w *watcher) log(c *Core, ev WatchEvent) {
	ev.Time = time.Now()
	w.mu.Lock()
	w.info.Events = append(w.info.Events, ev)
	if len(w.info.Events) > 500 {
		w.info.Events = w.info.Events[len(w.info.Events)-400:]
	}
	switch ev.Action {
	case "upload":
		w.info.Uploads++
	case "delete":
		w.info.Deletes++
	}
	w.mu.Unlock()
	c.Bus.Publish("watch.event", map[string]any{"id": w.info.ID, "event": ev})
}

// StartWatch begins keeping a remote directory up to date.
func (c *Core) StartWatch(r WatchRequest) (WatchInfo, error) {
	if c.Local.Root == "" {
		r.LocalDir = localfs.ExpandHome(r.LocalDir)
	}
	if c.Local.Root != "" {
		if _, err := c.Local.Stat(context.Background(), r.LocalDir); err != nil {
			return WatchInfo{}, err
		}
	}
	st, err := os.Stat(r.LocalDir)
	if err != nil || !st.IsDir() {
		return WatchInfo{}, errors.New("local directory does not exist")
	}
	if _, err := c.Conn(r.RemoteConn); err != nil {
		return WatchInfo{}, err
	}
	m, err := mask.Parse(r.FileMask)
	if err != nil {
		return WatchInfo{}, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return WatchInfo{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &watcher{info: WatchInfo{ID: randID(5), Req: r, Started: time.Now()}, w: fw, cancel: cancel, m: m}
	addTree := func(dir string) {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p != dir && !r.Recursive {
				return filepath.SkipDir
			}
			if r.ExcludeHidden && strings.HasPrefix(d.Name(), ".") && p != dir {
				return filepath.SkipDir
			}
			fw.Add(p)
			return nil
		})
	}
	addTree(r.LocalDir)
	c.watchers.mu.Lock()
	c.watchers.m[w.info.ID] = w
	c.watchers.mu.Unlock()
	w.log(c, WatchEvent{Action: "info", Path: r.LocalDir})

	if r.InitialSync {
		go func() {
			plan, err := c.SyncCompare(ctx, SyncRequest{LocalConn: LocalID, LocalDir: r.LocalDir, RemoteConn: r.RemoteConn, RemoteDir: r.RemoteDir, Direction: "remote", Mode: "standard", Criteria: "time", Delete: r.Delete, FileMask: r.FileMask, ExcludeHidden: r.ExcludeHidden})
			if err != nil {
				w.log(c, WatchEvent{Action: "error", Path: r.LocalDir, Error: err.Error()})
				return
			}
			if len(plan.Actions) > 0 {
				c.SyncExecute(ctx, plan.ID, nil, nil)
			}
		}()
	}

	delay := time.Duration(r.Delay) * time.Millisecond
	if delay <= 0 {
		delay = 600 * time.Millisecond
	}
	go func() {
		defer fw.Close()
		pending := map[string]bool{}
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-fw.Events:
				if !ok {
					return
				}
				if strings.HasSuffix(ev.Name, ".filepart") || strings.HasSuffix(ev.Name, "~") || strings.HasSuffix(ev.Name, ".swp") {
					continue
				}
				if ev.Op&fsnotify.Create != 0 && r.Recursive {
					if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
						addTree(ev.Name)
						// files created in the new folder before it was
						// watched (a folder copied or extracted at once)
						filepath.WalkDir(ev.Name, func(p string, d os.DirEntry, err error) error {
							if err == nil {
								pending[p] = true
							}
							return nil
						})
					}
				}
				pending[ev.Name] = true
				timer.Reset(delay)
			case err, ok := <-fw.Errors:
				if !ok {
					return
				}
				w.log(c, WatchEvent{Action: "error", Error: err.Error()})
			case <-timer.C:
				paths := make([]string, 0, len(pending))
				for p := range pending {
					paths = append(paths, p)
				}
				pending = map[string]bool{}
				c.applyWatch(ctx, w, paths)
			}
		}
	}()
	return w.snapshot(), nil
}

func (w *watcher) snapshot() WatchInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	i := w.info
	i.Events = append([]WatchEvent(nil), i.Events...)
	return i
}

func (c *Core) applyWatch(ctx context.Context, w *watcher, paths []string) {
	r := w.info.Req
	rf, err := c.FSFor(ctx, r.RemoteConn)
	if err != nil {
		w.log(c, WatchEvent{Action: "error", Error: err.Error()})
		return
	}
	rs := rf.Caps().PathStyle
	var pairs []transfer.Pair
	for _, p := range paths {
		rel, err := filepath.Rel(r.LocalDir, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		name := filepath.Base(p)
		if r.ExcludeHidden && strings.HasPrefix(name, ".") {
			continue
		}
		rp := vfs.JoinRel(rs, r.RemoteDir, rel)
		st, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) && r.Delete {
				if !w.m.Match(name, rel, false, 0, time.Now()) {
					continue
				}
				e, serr := rf.Stat(ctx, rp)
				if serr != nil {
					continue
				}
				if err := vfs.RemoveAll(ctx, rf, rp, e.IsDir); err != nil {
					w.log(c, WatchEvent{Action: "error", Path: rel, Error: err.Error()})
				} else {
					w.log(c, WatchEvent{Action: "delete", Path: rel})
					c.Changed(r.RemoteConn, vfs.Dir(rs, rp))
				}
			}
			continue
		}
		if !w.m.Match(name, rel, st.IsDir(), st.Size(), st.ModTime()) {
			continue
		}
		if st.IsDir() {
			if err := vfs.MkdirAll(ctx, rf, rp); err != nil {
				w.log(c, WatchEvent{Action: "error", Path: rel, Error: err.Error()})
			} else {
				w.log(c, WatchEvent{Action: "mkdir", Path: rel})
			}
			continue
		}
		pairs = append(pairs, transfer.Pair{Src: p, Dst: rp, Size: st.Size(), MTime: st.ModTime(), Mode: uint32(st.Mode().Perm())})
	}
	if len(pairs) == 0 {
		return
	}
	o := c.DefaultTransferOptions(LocalID, r.RemoteConn)
	o.Overwrite = "overwrite"
	o.PreserveTime = true
	o.ContinueOnError = true
	o.FileMask = ""
	req := transfer.Request{Src: LocalID, Dst: r.RemoteConn, DstDir: r.RemoteDir, Explicit: pairs, Options: o, Label: "Keep up to date: " + filepath.Base(r.LocalDir)}
	req.Options.SrcEOL, req.Options.DstEOL = c.EOL(LocalID), c.EOL(r.RemoteConn)
	snap, err := c.Transfers.Run(ctx, req)
	for _, p := range pairs {
		rel, _ := filepath.Rel(r.LocalDir, p.Src)
		failed := ""
		for _, fe := range snap.Errors {
			if fe.Path == p.Src {
				failed = fe.Error
			}
		}
		if failed != "" {
			w.log(c, WatchEvent{Action: "error", Path: filepath.ToSlash(rel), Error: failed})
		} else if err == nil || len(snap.Errors) > 0 {
			w.log(c, WatchEvent{Action: "upload", Path: filepath.ToSlash(rel)})
		}
	}
}

func (c *Core) StopWatch(id string) {
	c.watchers.mu.Lock()
	w, ok := c.watchers.m[id]
	delete(c.watchers.m, id)
	c.watchers.mu.Unlock()
	if ok {
		w.cancel()
		c.Bus.Publish("watch.closed", id)
	}
}

func (c *Core) Watches() []WatchInfo {
	c.watchers.mu.Lock()
	defer c.watchers.mu.Unlock()
	out := make([]WatchInfo, 0, len(c.watchers.m))
	for _, w := range c.watchers.m {
		out = append(out, w.snapshot())
	}
	return out
}

func (ws *watchers) closeConn(conn string) {
	ws.mu.Lock()
	var ids []string
	for id, w := range ws.m {
		if w.info.Req.RemoteConn == conn {
			ids = append(ids, id)
		}
	}
	ws.mu.Unlock()
	for _, id := range ids {
		ws.core.StopWatch(id)
	}
}
