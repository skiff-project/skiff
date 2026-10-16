//go:build integration

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/core"
	"skiff/internal/tasks"
)

// testTasks saves a synchronization and a script as tasks, runs them and
// reads their history.
func testTasks(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	local := filepath.Join(e.home, uniq("task"))
	writeTree(t, local, map[string]string{"index.html": "<h1>tâche</h1>\n", "css/site.css": "body{}\n"})
	site := e.saveSite(t, testSite("sftp"), "skiff")

	runOf := func(id string) tasks.Run {
		t.Helper()
		var r tasks.Run
		eventually(t, "the run to finish", 60*time.Second, func() bool {
			var runs []tasks.Run
			e.must(t, "tasks.runs", map[string]string{"id": id}, &runs)
			if len(runs) > 0 && runs[0].Status != "running" {
				r = runs[0]
				return true
			}
			return false
		})
		return r
	}

	var sync tasks.View
	e.must(t, "tasks.save", tasks.Task{
		Name: "Publier le site", Kind: "sync", Enabled: true, SiteID: site.ID,
		Sync:     core.SyncRequest{LocalDir: local, RemoteDir: root + "/www", Direction: "remote", Mode: "standard", Criteria: "time"},
		Schedule: tasks.Schedule{Kind: "daily", At: "03:00"}, Notify: "always",
	}, &sync)
	if sync.ID == "" || sync.Next == nil || sync.Next.Hour() != 3 || sync.Next.Before(time.Now()) {
		t.Errorf("saved %+v", sync)
	}
	e.must(t, "tasks.run", map[string]string{"id": sync.ID}, nil)
	r := runOf(sync.ID)
	if r.Status != "ok" || r.Trigger != "manual" || !strings.Contains(r.Summary, "2 file(s) transferred") {
		t.Fatalf("sync run %+v", r)
	}
	if got := e.readText(t, ci.ID, root+"/www/css/site.css"); got != "body{}\n" {
		t.Errorf("synchronized file %q", got)
	}
	// nothing new: already up to date
	e.must(t, "tasks.run", map[string]string{"id": sync.ID}, nil)
	time.Sleep(200 * time.Millisecond)
	if r := runOf(sync.ID); !strings.HasPrefix(r.Summary, "Already up to date") {
		t.Errorf("second run %+v", r)
	}

	var script tasks.View
	e.must(t, "tasks.save", tasks.Task{
		Name: "Nettoyer", Kind: "script", Enabled: false, Schedule: tasks.Schedule{Kind: "cron", Cron: "0 4 * * 1"},
		Script: "open sftp://skiff:skiff@127.0.0.1:2222/ -hostkey=*\ncd " + root + "/www\nrm index.html\nls\nexit\n",
	}, &script)
	if script.Next != nil {
		t.Error("a disabled task has a next run")
	}
	e.must(t, "tasks.run", map[string]string{"id": script.ID}, nil)
	if r := runOf(script.ID); r.Status != "ok" || !strings.Contains(strings.Join(r.Log, "\n"), "css") {
		t.Errorf("script run %+v", r)
	}
	if e.exists(ci.ID, root+"/www/index.html") {
		t.Error("the script did not delete the file")
	}
	// a failure is recorded with its reason
	var broken tasks.View
	bad := testSite("sftp")
	bad.Port = 1
	e.must(t, "tasks.save", tasks.Task{Name: "En échec", Kind: "sync", SiteID: e.saveSite(t, bad, "skiff").ID, Sync: core.SyncRequest{LocalDir: local, Direction: "remote"}, Schedule: tasks.Schedule{Kind: "manual"}}, &broken)
	e.must(t, "tasks.run", map[string]string{"id": broken.ID}, nil)
	if r := runOf(broken.ID); r.Status != "failed" || !strings.Contains(strings.ToLower(r.Summary), "refused") {
		t.Errorf("failed run %+v", r)
	}
	if err := e.mustFail(t, "tasks.save", tasks.Task{Name: "Sans horaire", Kind: "script", Script: "ls", Schedule: tasks.Schedule{Kind: "weekly", At: "08:00"}}); err == nil {
		t.Error("a weekly task without days was accepted")
	}
	// a long task can be cancelled
	var slow tasks.View
	e.must(t, "tasks.save", tasks.Task{Name: "Lente", Kind: "script", Script: "open sftp://skiff:skiff@127.0.0.1:2222/ -hostkey=*\ncall sleep 30\n", Schedule: tasks.Schedule{Kind: "manual"}}, &slow)
	e.must(t, "tasks.run", map[string]string{"id": slow.ID}, nil)
	time.Sleep(800 * time.Millisecond)
	e.must(t, "tasks.cancel", map[string]string{"id": slow.ID}, nil)
	if r := runOf(slow.ID); r.Status != "cancelled" {
		t.Errorf("cancelled run %+v", r)
	}
	var list []tasks.View
	e.must(t, "tasks.list", nil, &list)
	if len(list) != 4 || list[0].Last == nil || list[0].Last.Status != "ok" {
		t.Errorf("list %+v", list)
	}
	for _, v := range list {
		e.must(t, "tasks.delete", map[string]string{"id": v.ID}, nil)
	}
	var all []tasks.Run
	e.must(t, "tasks.runs", nil, &all)
	if len(all) < 5 {
		t.Errorf("history %d runs", len(all))
	}
	os.RemoveAll(local)
}
