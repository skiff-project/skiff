//go:build integration

package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/transfer"
)

func (e *e2e) syncPlan(t *testing.T, req core.SyncRequest) *core.SyncPlan {
	t.Helper()
	var plan core.SyncPlan
	e.must(t, "sync.compare", req, &plan)
	return &plan
}

func actions(p *core.SyncPlan) []string {
	var out []string
	for _, a := range p.Actions {
		out = append(out, a.Action+":"+a.Rel)
	}
	sort.Strings(out)
	return out
}

func (e *e2e) syncRun(t *testing.T, p *core.SyncPlan) {
	t.Helper()
	var res core.SyncResult
	e.must(t, "sync.execute", map[string]any{"planId": p.ID}, &res)
	for _, id := range res.Jobs {
		if j := e.waitJob(t, id); j.Status != transfer.Done {
			t.Fatalf("sync job = %+v", j)
		}
	}
	if len(res.Errors) > 0 {
		t.Fatalf("sync errors: %v", res.Errors)
	}
}

func setLocalTime(t *testing.T, f string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(f, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func testSync(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	local := filepath.Join(e.home, uniq("sync"))
	writeTree(t, local, map[string]string{"index.html": "v1", "css/style.css": "c1", ".cache": "h", "brouillon.tmp": "t"})
	base := core.SyncRequest{LocalConn: "local", LocalDir: local, RemoteConn: ci.ID, RemoteDir: root, Direction: "remote", Mode: "standard", Criteria: "time"}
	rel := func(r string) string { return root + "/" + r }

	// first upload, with a mask and hidden files excluded
	req := base
	req.FileMask, req.ExcludeHidden = "* | *.tmp", true
	p := e.syncPlan(t, req)
	if got := strings.Join(actions(p), " "); got != "mkdirRemote:css upload:css/style.css upload:index.html" {
		t.Fatalf("initial plan = %s", got)
	}
	e.syncRun(t, p)
	if p = e.syncPlan(t, req); len(p.Actions) != 0 {
		t.Fatalf("second compare must be empty: %v", actions(p))
	}
	// a newer local file is sent; a newer remote file is not downloaded
	now := time.Now().Truncate(time.Second)
	os.WriteFile(filepath.Join(local, "index.html"), []byte("v2"), 0o644)
	setLocalTime(t, filepath.Join(local, "index.html"), now.Add(time.Hour))
	e.put(t, ci.ID, rel("css/style.css"), "serveur")
	later := now.Add(2 * time.Hour)
	e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{rel("css/style.css")}, "attrs": core.Attrs{MTime: &later}}, nil)
	p = e.syncPlan(t, req)
	if got := strings.Join(actions(p), " "); got != "upload:index.html" {
		t.Fatalf("newer plan = %s", got)
	}
	if p.Actions[0].Reason != "newer" {
		t.Errorf("reason = %s", p.Actions[0].Reason)
	}
	// mirror: the remote newer file is replaced too
	req.Mode = "mirror"
	if got := strings.Join(actions(e.syncPlan(t, req)), " "); got != "upload:css/style.css upload:index.html" {
		t.Errorf("mirror plan = %s", got)
	}
	// execute only a selection
	p = e.syncPlan(t, req)
	var res core.SyncResult
	var pick string
	for _, a := range p.Actions {
		if a.Rel == "css/style.css" {
			pick = a.ID
		}
	}
	e.must(t, "sync.execute", map[string]any{"planId": p.ID, "ids": []string{pick}}, &res)
	for _, id := range res.Jobs {
		e.waitJob(t, id)
	}
	if got := strings.Join(actions(e.syncPlan(t, req)), " "); got != "upload:index.html" {
		t.Errorf("after a partial run = %s", got)
	}
	e.syncRun(t, e.syncPlan(t, req))
	// orphans: delete on the destination
	e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": rel("orphelin.txt"), "content": "o"}, nil)
	req = base
	req.Delete, req.FileMask, req.ExcludeHidden = true, "* | *.tmp", true
	p = e.syncPlan(t, req)
	if got := strings.Join(actions(p), " "); got != "deleteRemote:orphelin.txt" {
		t.Fatalf("delete plan = %s", got)
	}
	e.syncRun(t, p)
	if e.exists(ci.ID, rel("orphelin.txt")) {
		t.Fatal("orphan not deleted")
	}
	// download direction
	e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": rel("nouveau.txt"), "content": "n"}, nil)
	req = base
	req.Direction, req.ExcludeHidden, req.FileMask = "local", true, "* | *.tmp"
	p = e.syncPlan(t, req)
	if got := strings.Join(actions(p), " "); got != "download:nouveau.txt" {
		t.Fatalf("download plan = %s", got)
	}
	e.syncRun(t, p)
	if b, _ := os.ReadFile(filepath.Join(local, "nouveau.txt")); string(b) != "n" {
		t.Fatal("not downloaded")
	}
	os.WriteFile(filepath.Join(local, "orphelin-local.txt"), []byte("x"), 0o644)
	req.Delete = true
	if got := strings.Join(actions(e.syncPlan(t, req)), " "); got != "deleteLocal:orphelin-local.txt" {
		t.Errorf("local delete plan = %s", got)
	}
	e.syncRun(t, e.syncPlan(t, req))
	if _, err := os.Stat(filepath.Join(local, "orphelin-local.txt")); err == nil {
		t.Error("local orphan not deleted")
	}
	// both directions
	os.WriteFile(filepath.Join(local, "de-local.txt"), []byte("l"), 0o644)
	e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": rel("de-distant.txt"), "content": "d"}, nil)
	req = base
	req.Direction, req.ExcludeHidden, req.FileMask = "both", true, "* | *.tmp"
	if got := strings.Join(actions(e.syncPlan(t, req)), " "); got != "download:de-distant.txt upload:de-local.txt" {
		t.Errorf("both plan = %s", got)
	}
	e.syncRun(t, e.syncPlan(t, req))
	// criteria: same date and size but different content → only checksum sees it
	mt := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	os.WriteFile(filepath.Join(local, "same.txt"), []byte("AAAA"), 0o644)
	setLocalTime(t, filepath.Join(local, "same.txt"), mt)
	e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": rel("same.txt"), "content": "BBBB"}, nil)
	e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{rel("same.txt")}, "attrs": core.Attrs{MTime: &mt}}, nil)
	os.WriteFile(filepath.Join(local, "size.txt"), []byte("longer"), 0o644)
	setLocalTime(t, filepath.Join(local, "size.txt"), mt)
	e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": rel("size.txt"), "content": "s"}, nil)
	e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{rel("size.txt")}, "attrs": core.Attrs{MTime: &mt}}, nil)
	for crit, want := range map[string]string{
		"time":     "",
		"size":     "upload:size.txt",
		"either":   "upload:size.txt",
		"checksum": "upload:same.txt upload:size.txt",
		"none":     "",
	} {
		req = base
		req.Criteria, req.Mode, req.ExcludeHidden, req.FileMask = crit, "mirror", true, "same.txt; size.txt"
		if got := strings.Join(actions(e.syncPlan(t, req)), " "); got != want {
			t.Errorf("criteria %s = %q, want %q", crit, got, want)
		}
	}
	// timestamps only
	other := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	setLocalTime(t, filepath.Join(local, "nouveau.txt"), other)
	req = base
	req.Mode, req.Criteria, req.FileMask = "timestamps", "time", "nouveau.txt"
	p = e.syncPlan(t, req)
	if got := strings.Join(actions(p), " "); got != "touchRemote:nouveau.txt" {
		t.Fatalf("timestamps plan = %s", got)
	}
	e.syncRun(t, p)
	var st struct {
		ModTime time.Time `json:"mtime"`
	}
	e.must(t, "fs.stat", map[string]string{"conn": ci.ID, "path": rel("nouveau.txt")}, &st)
	if !st.ModTime.Equal(other) {
		t.Errorf("timestamp sync = %v", st.ModTime)
	}
	// existing files only
	os.WriteFile(filepath.Join(local, "jamais.txt"), []byte("x"), 0o644)
	req = base
	req.ExistingOnly, req.FileMask = true, "jamais.txt"
	if p = e.syncPlan(t, req); len(p.Actions) != 0 {
		t.Errorf("existingOnly created files: %v", actions(p))
	}
	// a destination folder that does not exist yet is created
	req = base
	req.RemoteDir, req.FileMask, req.ExcludeHidden = root+"/nouveau/dossier", "* | *.tmp", true
	p = e.syncPlan(t, req)
	if len(p.Actions) == 0 {
		t.Fatal("no actions towards a missing folder")
	}
	e.syncRun(t, p)
	if !e.exists(ci.ID, root+"/nouveau/dossier/index.html") {
		t.Error("missing destination folder not created")
	}
	req = base
	req.Direction, req.LocalDir, req.FileMask = "local", filepath.Join(local, "absent", "ici"), "*.html"
	e.syncRun(t, e.syncPlan(t, req))
	if _, err := os.Stat(filepath.Join(local, "absent", "ici", "index.html")); err != nil {
		t.Error("missing local destination folder not created")
	}
	// the source must exist
	req = base
	req.LocalDir = filepath.Join(local, "inexistant")
	e.mustFail(t, "sync.compare", req)
	// remote ↔ remote synchronisation (two servers)
	s3 := e.remoteTarget(t, "s3")
	req = core.SyncRequest{LocalConn: ci.ID, LocalDir: root, RemoteConn: s3.id, RemoteDir: s3.root, Direction: "remote", Mode: "standard", Criteria: "size", FileMask: "*.txt"}
	p = e.syncPlan(t, req)
	if len(p.Actions) == 0 {
		t.Fatal("server-to-server sync plan empty")
	}
	e.syncRun(t, p)
	if e.readText(t, s3.id, s3.root+"/nouveau.txt") != "n" {
		t.Error("server-to-server sync")
	}
}

func testWatch(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	local := filepath.Join(e.home, uniq("watch"))
	writeTree(t, local, map[string]string{"deja.txt": "avant"})
	var w core.WatchInfo
	e.must(t, "watch.start", core.WatchRequest{LocalDir: local, RemoteConn: ci.ID, RemoteDir: root, Delete: true, FileMask: "* | *.swp", InitialSync: true, Recursive: true}, &w)
	eventually(t, "initial sync", 15*time.Second, func() bool { return e.exists(ci.ID, root+"/deja.txt") })
	os.WriteFile(filepath.Join(local, "nouveau.html"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(local, ".fichier.swp"), []byte("ignore"), 0o644)
	os.MkdirAll(filepath.Join(local, "css"), 0o755)
	os.WriteFile(filepath.Join(local, "css", "s.css"), []byte("c"), 0o644)
	eventually(t, "new files", 15*time.Second, func() bool {
		return e.exists(ci.ID, root+"/nouveau.html") && e.exists(ci.ID, root+"/css/s.css")
	})
	os.WriteFile(filepath.Join(local, "nouveau.html"), []byte("v2"), 0o644)
	eventually(t, "modification", 15*time.Second, func() bool { return e.readText(t, ci.ID, root+"/nouveau.html") == "v2" })
	os.Remove(filepath.Join(local, "deja.txt"))
	eventually(t, "deletion", 15*time.Second, func() bool { return !e.exists(ci.ID, root+"/deja.txt") })
	if e.exists(ci.ID, root+"/.fichier.swp") {
		t.Error("masked file uploaded")
	}
	var list []core.WatchInfo
	e.must(t, "watch.list", nil, &list)
	var got core.WatchInfo
	for _, x := range list {
		if x.ID == w.ID {
			got = x
		}
	}
	if got.Uploads < 3 || got.Deletes < 1 || len(got.Events) == 0 {
		t.Errorf("watch info = %+v", got)
	}
	e.must(t, "watch.stop", map[string]string{"id": w.ID}, nil)
	os.WriteFile(filepath.Join(local, "apres.txt"), []byte("x"), 0o644)
	time.Sleep(2 * time.Second)
	if e.exists(ci.ID, root+"/apres.txt") {
		t.Error("stopped watch still uploads")
	}
}

func testFind(t *testing.T, e *e2e) {
	for _, proto := range []string{"local", "sftp", "webdav"} {
		proto := proto
		t.Run(proto, func(t *testing.T) {
			var tg target
			if proto == "local" {
				tg = e.localTarget(t)
			} else {
				tg = e.remoteTarget(t, proto)
			}
			files := map[string]string{"a.html": "aiguille dans la page", "b.html": "rien", "css/c.css": strings.Repeat("x", 5000), "doc/notes.txt": "AIGUILLE majuscule"}
			for n, c := range files {
				e.upload(t, tg.id, tg.root+"/"+n, []byte(c), true)
			}
			search := func(o core.FindOptions) []string {
				var id string
				e.must(t, "fs.find", map[string]any{"conn": tg.id, "dir": tg.root, "opts": o}, &id)
				var hits []string
				// results are pushed as find.hits events; the final state is in the log of events,
				// so poll the search through a second listener
				hits = e.collectFind(t, id)
				sort.Strings(hits)
				return hits
			}
			if got := strings.Join(search(core.FindOptions{Mask: "*.html"}), " "); got != "a.html b.html" {
				t.Errorf("mask = %s", got)
			}
			if got := strings.Join(search(core.FindOptions{Mask: "*", Contains: "aiguille"}), " "); got != "a.html" && got != "a.html notes.txt" {
				t.Errorf("contains = %s", got)
			}
			if got := strings.Join(search(core.FindOptions{Mask: "*", MinSize: 4000}), " "); got != "c.css" {
				t.Errorf("min size = %s", got)
			}
			if got := strings.Join(search(core.FindOptions{Mask: "*", MaxSize: 10}), " "); got != "b.html" {
				t.Errorf("max size = %s", got)
			}
			future := time.Now().Add(time.Hour)
			if got := search(core.FindOptions{Mask: "*", NewerThan: &future}); len(got) != 0 {
				t.Errorf("newer than the future = %v", got)
			}
			if got := strings.Join(search(core.FindOptions{Mask: "c*", IncludeDirs: true}), " "); got != "c.css css" {
				t.Errorf("include dirs = %s", got)
			}
			if got := search(core.FindOptions{Mask: "*", MaxResults: 2}); len(got) != 2 {
				t.Errorf("max results = %v", got)
			}
		})
	}
	// cancel a long search
	ci := e.connect(t, "sftp")
	var id string
	e.must(t, "fs.find", map[string]any{"conn": ci.ID, "dir": "/", "opts": core.FindOptions{Mask: "*", Contains: "jamais-trouve"}}, &id)
	time.Sleep(300 * time.Millisecond)
	e.must(t, "fs.findCancel", map[string]string{"id": id}, nil)
	e.collectFind(t, id) // must terminate
}

// collectFind follows the find.hits events of a search until it is done.
func (e *e2e) collectFind(t *testing.T, id string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e.mu.Lock()
	ch := e.findSubs(id)
	e.mu.Unlock()
	var names []string
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("search %s did not finish", id)
		case batch := <-ch:
			for _, h := range batch.Hits {
				names = append(names, h.Entry.Name)
			}
			if batch.Done {
				return names
			}
		}
	}
}

type findBatch struct {
	ID   string         `json:"id"`
	Hits []core.FindHit `json:"hits"`
	Done bool           `json:"done"`
}

var findChans = map[string]chan findBatch{}
var findBuffered = map[string][]findBatch{}

// findSubs returns the channel receiving the hits of search id (e.mu held).
func (e *e2e) findSubs(id string) chan findBatch {
	if ch, ok := findChans[id]; ok {
		return ch
	}
	ch := make(chan findBatch, 1000)
	findChans[id] = ch
	for _, b := range findBuffered[id] {
		ch <- b
	}
	delete(findBuffered, id)
	return ch
}

// startFindListener records find.hits events (they may arrive before the
// test starts waiting for them).
func (e *e2e) startFindListener() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-e.stop; cancel() }()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.hs.URL, "http")+"/api/events", &websocket.DialOptions{HTTPClient: e.client})
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 20)
	ready := make(chan struct{})
	go func() {
		defer ws.CloseNow()
		close(ready)
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var ev struct {
				Type string    `json:"type"`
				Data findBatch `json:"data"`
			}
			if json.Unmarshal(data, &ev) != nil || ev.Type != "find.hits" {
				continue
			}
			e.mu.Lock()
			if ch, ok := findChans[ev.Data.ID]; ok {
				ch <- ev.Data
			} else {
				findBuffered[ev.Data.ID] = append(findBuffered[ev.Data.ID], ev.Data)
			}
			e.mu.Unlock()
		}
	}()
	<-ready
}

func testEditors(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": root + "/page.html", "content": "<p>avant</p>\n"}, nil)
	// a fake editor that saves the file twice, like a user would
	editor := filepath.Join(e.home, "editeur.sh")
	os.WriteFile(editor, []byte("#!/bin/sh\nsleep 1\necho '<p>modifié</p>' >> \"$1\"\nsleep 2\necho '<p>encore</p>' >> \"$1\"\n"), 0o755)
	var ed core.ExternalEdit
	e.must(t, "edit.external", map[string]string{"conn": ci.ID, "path": root + "/page.html", "editor": editor}, &ed)
	eventually(t, "the edit to be uploaded twice", 20*time.Second, func() bool {
		return e.readText(t, ci.ID, root+"/page.html") == "<p>avant</p>\n<p>modifié</p>\n<p>encore</p>\n"
	})
	var list []core.ExternalEdit
	e.must(t, "edit.list", nil, &list)
	var mine *core.ExternalEdit
	for i := range list {
		if list[i].ID == ed.ID {
			mine = &list[i]
		}
	}
	if mine == nil || mine.Uploads < 2 || mine.Remote != root+"/page.html" || mine.Uploaded.IsZero() {
		t.Errorf("edit.list = %+v", list)
	}
	e.must(t, "edit.stop", map[string]string{"id": ed.ID}, nil)
	e.must(t, "edit.list", nil, &list)
	for _, x := range list {
		if x.ID == ed.ID {
			t.Error("edit not stopped")
		}
	}
	// an editor command with !.!
	f := filepath.Join(e.home, "local-edit.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	cmd := "sh -c 'echo ok >> \"$0\"' !.!"
	e.must(t, "edit.external", map[string]string{"conn": "local", "path": f, "editor": cmd}, nil)
	eventually(t, "the local editor", 5*time.Second, func() bool {
		b, _ := os.ReadFile(f)
		return string(b) == "xok\n"
	})
}

func (e *e2e) termSession(t *testing.T, id string) (send func(string), wait func(string) string, done func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.hs.URL, "http")+"/api/term/"+id, &websocket.DialOptions{HTTPClient: e.client})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	out := make(chan string, 1000)
	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				close(out)
				return
			}
			out <- string(data)
		}
	}()
	var buf strings.Builder
	send = func(s string) {
		msg, _ := json.Marshal(map[string]any{"type": "input", "data": s})
		ws.Write(ctx, websocket.MessageText, msg)
	}
	wait = func(want string) string {
		t.Helper()
		for !strings.Contains(buf.String(), want) {
			select {
			case s, ok := <-out:
				if !ok {
					t.Fatalf("terminal closed before %q; got %q", want, buf.String())
				}
				buf.WriteString(s)
			case <-ctx.Done():
				t.Fatalf("waiting for %q; got %q", want, buf.String())
			}
		}
		return buf.String()
	}
	return send, wait, func() { ws.Close(websocket.StatusNormalClosure, ""); cancel() }
}

func testTerminals(t *testing.T, e *e2e) {
	for _, proto := range []string{"sftp", "scp", "local"} {
		proto := proto
		t.Run(proto, func(t *testing.T) {
			conn, cwd := "local", e.home
			if proto != "local" {
				ci := e.connect(t, proto)
				conn, cwd = ci.ID, e.remoteRoot(t, ci)
			}
			var term termInfo
			e.must(t, "term.open", map[string]any{"conn": conn, "cols": 100, "rows": 30, "cwd": cwd}, &term)
			send, wait, closeWS := e.termSession(t, term.ID)
			send("echo SKIFF_$((6*7)); pwd; tput cols\n")
			wait("SKIFF_42")
			wait(cwd)
			closeWS()
			// re-attach: the scrollback is replayed
			_, wait2, closeWS2 := e.termSession(t, term.ID)
			wait2("SKIFF_42")
			closeWS2()
			var terms []termInfo
			e.must(t, "term.list", nil, &terms)
			found := false
			for _, x := range terms {
				found = found || x.ID == term.ID
			}
			if !found {
				t.Error("term.list does not list the terminal")
			}
			e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
			e.must(t, "term.list", nil, &terms)
			for _, x := range terms {
				if x.ID == term.ID {
					t.Error("terminal not closed")
				}
			}
		})
	}
	t.Run("resize", func(t *testing.T) {
		ci := e.connect(t, "sftp")
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": ci.ID, "cols": 80, "rows": 24}, &term)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.hs.URL, "http")+"/api/term/"+term.ID, &websocket.DialOptions{HTTPClient: e.client})
		if err != nil {
			t.Fatal(err)
		}
		defer ws.CloseNow()
		resize, _ := json.Marshal(map[string]any{"type": "resize", "cols": 132, "rows": 40})
		ws.Write(ctx, websocket.MessageText, resize)
		time.Sleep(300 * time.Millisecond)
		ws.Write(ctx, websocket.MessageBinary, []byte("stty size\n")) // binary input path
		var buf strings.Builder
		for !strings.Contains(buf.String(), "40 132") {
			_, data, err := ws.Read(ctx)
			if err != nil {
				t.Fatalf("stty size: %q (%v)", buf.String(), err)
			}
			buf.Write(data)
		}
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
	})
	t.Run("ftpHasNoTerminal", func(t *testing.T) {
		ci := e.connect(t, "ftp")
		e.mustFail(t, "term.open", map[string]any{"conn": ci.ID, "cols": 80, "rows": 24})
		e.mustFail(t, "term.shells", map[string]string{"conn": ci.ID})
	})
	t.Run("shells", func(t *testing.T) { testShells(t, e) })
}

// shellOf finds a detected shell or interpreter, and checks that exactly
// one of the list is the default.
func shellOf(t *testing.T, list []core.Shell, id string) (core.Shell, bool) {
	t.Helper()
	defaults := 0
	var found core.Shell
	ok := false
	for _, s := range list {
		if s.Default {
			defaults++
		}
		if s.ID == id {
			found, ok = s, true
		}
	}
	if defaults != 1 {
		t.Errorf("%d default shells in %+v", defaults, list)
	}
	return found, ok
}

func testShells(t *testing.T, e *e2e) {
	t.Run("local", func(t *testing.T) {
		var list []core.Shell
		e.must(t, "term.shells", map[string]string{"conn": "local"}, &list)
		if _, ok := shellOf(t, list, "sh"); !ok {
			t.Fatalf("sh not found on this computer: %+v", list)
		}
		// an interpreter: Python when it is installed
		if py, ok := shellOf(t, list, "python"); ok {
			if py.Kind != "repl" {
				t.Errorf("python kind %q", py.Kind)
			}
			var term termInfo
			e.must(t, "term.open", map[string]any{"conn": "local", "cols": 100, "rows": 30, "shell": "python"}, &term)
			send, wait, done := e.termSession(t, term.ID)
			wait(">>>")
			send("print('SKIFF_%d' % (6*7))\r")
			wait("SKIFF_42")
			done()
			e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
			if term.Title != "Python" {
				t.Errorf("title %q", term.Title)
			}
		}
		// the default shell of local terminals comes from the preferences
		var p config.Prefs
		e.must(t, "prefs.get", nil, &p)
		saved := p.Terminal.LocalShell
		p.Terminal.LocalShell = "sh"
		e.must(t, "prefs.set", p, nil)
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": "local", "cols": 100, "rows": 30, "cwd": e.home}, &term)
		send, wait, done := e.termSession(t, term.ID)
		send("echo SKIFF_$((6*7)) $0; pwd\n")
		if out := wait("SKIFF_42"); !strings.Contains(out, e.home) {
			t.Errorf("sh not in %s:\n%s", e.home, out)
		}
		done()
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
		if term.Title != "sh" {
			t.Errorf("default local shell from the preferences: title %q", term.Title)
		}
		p.Terminal.LocalShell = saved
		e.must(t, "prefs.set", p, nil)
	})
	t.Run("sftp", func(t *testing.T) {
		ci := e.connect(t, "sftp")
		var list []core.Shell
		e.must(t, "term.shells", map[string]string{"conn": ci.ID}, &list)
		sh, ok := shellOf(t, list, "sh")
		if !ok || !strings.HasPrefix(sh.Path, "/") {
			t.Fatalf("sh not found on the server: %+v", list)
		}
		cwd := e.remoteRoot(t, ci)
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": ci.ID, "cols": 100, "rows": 30, "cwd": cwd, "shell": "sh"}, &term)
		send, wait, done := e.termSession(t, term.ID)
		send("echo SKIFF_$((6*7)); pwd\n")
		if out := wait("SKIFF_42"); !strings.Contains(out, cwd) {
			t.Errorf("sh not in %s:\n%s", cwd, out)
		}
		done()
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
		if !sh.Default && !strings.HasSuffix(term.Title, " · sh") {
			t.Errorf("title %q", term.Title)
		}
		// a program the server does not have
		if err := e.mustFail(t, "term.open", map[string]any{"conn": ci.ID, "cols": 80, "rows": 24, "shell": "no-such-shell"}); !strings.Contains(err.Error(), "not installed") {
			t.Errorf("unknown shell: %v", err)
		}
	})
}

// termInfo is the JSON view of a terminal (core.Terminal holds a mutex).
type termInfo struct {
	ID    string `json:"id"`
	Conn  string `json:"conn"`
	Title string `json:"title"`
}
