package tasks

import (
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestScheduleNext(t *testing.T) {
	for _, tc := range []struct {
		s    Schedule
		ref  string
		want string
	}{
		{Schedule{Kind: "interval", Every: 90}, "2026-10-16 10:00", "2026-10-16 11:30"},
		{Schedule{Kind: "daily", At: "02:30"}, "2026-10-16 10:00", "2026-10-17 02:30"},
		{Schedule{Kind: "daily", At: "02:30"}, "2026-10-16 01:00", "2026-10-16 02:30"},
		// 2026-10-16 is a Friday: next Monday or Wednesday
		{Schedule{Kind: "weekly", At: "08:00", Days: []int{1, 3}}, "2026-10-16 09:00", "2026-10-19 08:00"},
		{Schedule{Kind: "weekly", At: "08:00", Days: []int{5}}, "2026-10-16 07:00", "2026-10-16 08:00"},
		{Schedule{Kind: "cron", Cron: "*/15 * * * *"}, "2026-10-16 10:07", "2026-10-16 10:15"},
		{Schedule{Kind: "cron", Cron: "0 3 1 * *"}, "2026-10-16 10:00", "2026-11-01 03:00"},
		{Schedule{Kind: "cron", Cron: "30 23 * 12 7"}, "2026-10-16 10:00", "2026-12-06 23:30"},     // Sundays of December
		{Schedule{Kind: "cron", Cron: "0 9-17/4 * * 1-5"}, "2026-10-16 13:30", "2026-10-16 17:00"}, // 9, 13 and 17 h
		{Schedule{Kind: "cron", Cron: "0 9-17/4 * * 1-5"}, "2026-10-16 17:30", "2026-10-19 09:00"},
		{Schedule{Kind: "cron", Cron: "0 12 * * 5-7"}, "2026-10-17 13:00", "2026-10-18 12:00"}, // Sat after noon: Sunday
		{Schedule{Kind: "cron", Cron: "0 0 29 2 *"}, "2026-10-16 10:00", "2028-02-29 00:00"},
	} {
		got := tc.s.Next(at(tc.ref))
		if !got.Equal(at(tc.want)) {
			t.Errorf("%+v after %s: %s, want %s", tc.s, tc.ref, got.Format("2006-01-02 15:04"), tc.want)
		}
	}
	if !(Schedule{Kind: "manual"}).Next(at("2026-10-16 10:00")).IsZero() {
		t.Error("a manual task has a next run")
	}
	for _, bad := range []Schedule{{Kind: "interval"}, {Kind: "daily", At: "25:00"}, {Kind: "weekly", At: "08:00"}, {Kind: "cron", Cron: "* * *"}, {Kind: "cron", Cron: "61 * * * *"}, {Kind: "hourly"}} {
		if bad.Validate() == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func testScheduler(t *testing.T) (*Scheduler, *config.Store) {
	cfg, err := config.Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	return NewScheduler(cfg, nil), cfg
}

func waitIdle(t *testing.T, s *Scheduler, id string) {
	deadline := time.Now().Add(10 * time.Second)
	for s.isRunning(id) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSchedulerRunsMissesAndCatchesUp(t *testing.T) {
	s, _ := testScheduler(t)
	defer s.Close()
	now := at("2026-10-16 10:00")
	s.Now = func() time.Time { return now }
	task, err := s.Store.Save(Task{Name: "Écho", Kind: "script", Script: "echo bonjour\n", Enabled: true, Schedule: Schedule{Kind: "interval", Every: 60}})
	if err != nil {
		t.Fatal(err)
	}
	// Save stamps Updated with the real time: make it the fake "now"
	task.Updated = now
	s.ref[task.ID] = now

	s.Tick() // not due yet
	if runs := s.Store.Runs(task.ID); len(runs) != 0 {
		t.Fatalf("ran too early: %+v", runs)
	}
	now = at("2026-10-16 11:00")
	s.Tick()
	waitIdle(t, s, task.ID)
	runs := s.Store.Runs(task.ID)
	if len(runs) != 1 || runs[0].Status != "ok" || runs[0].Trigger != "schedule" || !strings.Contains(strings.Join(runs[0].Log, "\n"), "bonjour") {
		t.Fatalf("scheduled run %+v", runs)
	}
	// Skiff closed for a day: the missed run is skipped…
	now = at("2026-10-17 11:00")
	s.Tick()
	waitIdle(t, s, task.ID)
	if len(s.Store.Runs(task.ID)) != 1 {
		t.Fatal("a missed run was not skipped")
	}
	if next := s.NextRun(task); !next.Equal(at("2026-10-17 12:00")) {
		t.Errorf("next after a skip %s", next)
	}
	// … unless the task catches up
	task.CatchUp = true
	if task, err = s.Store.Save(task); err != nil {
		t.Fatal(err)
	}
	s.ref[task.ID] = at("2026-10-17 11:00")
	now = at("2026-10-18 09:00")
	s.Tick()
	waitIdle(t, s, task.ID)
	runs = s.Store.Runs(task.ID)
	if len(runs) != 2 || runs[0].Trigger != "catch-up" {
		t.Errorf("catch-up %+v", runs)
	}
	// a disabled task never runs
	task.Enabled = false
	s.Store.Save(task)
	now = at("2026-10-19 09:00")
	s.Tick()
	waitIdle(t, s, task.ID)
	if len(s.Store.Runs(task.ID)) != 2 {
		t.Error("a disabled task ran")
	}
}

func TestFailedScriptAndHistory(t *testing.T) {
	s, cfg := testScheduler(t)
	defer s.Close()
	task, _ := s.Store.Save(Task{Name: "Cassé", Kind: "script", Script: "open sftp://nobody@127.0.0.1:1/\n", Schedule: Schedule{Kind: "manual"}})
	if err := s.RunNow(task.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunNow(task.ID, "manual"); err == nil {
		t.Error("ran twice at the same time")
	}
	waitIdle(t, s, task.ID)
	r, ok := s.Store.LastRun(task.ID)
	if !ok || r.Status != "failed" || r.Trigger != "manual" || r.End.Before(r.Start) {
		t.Fatalf("run %+v", r)
	}
	// the history survives a restart
	again := OpenStore(cfg)
	if runs := again.Runs(""); len(runs) != 1 || runs[0].ID != r.ID {
		t.Errorf("history %+v", runs)
	}
	if _, err := s.Store.Save(Task{Name: "x", Kind: "sync"}); err == nil {
		t.Error("a sync without a site was accepted")
	}
	if err := s.Store.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Find("Cassé"); !ok {
		// "again" read the file before the deletion: it still knows the task
		t.Error("Find by name")
	}
}
