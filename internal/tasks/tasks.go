// Package tasks runs saved tasks — synchronizations and scripts — on a
// schedule while Skiff is open, or from the command line (skiff task run)
// for the scheduler of the operating system. Each run has its own Skiff
// core that never asks questions: what needs an answer (unknown host key,
// missing password) makes the run fail with a clear message.
package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
)

// Task is a saved synchronization or script.
type Task struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"` // sync, script

	// sync: the site and the folders, with the options of the sync dialog
	SiteID string           `json:"siteId,omitempty"`
	Sync   core.SyncRequest `json:"sync,omitempty"`
	// script: WinSCP-compatible script commands, one per line
	Script string `json:"script,omitempty"`

	Schedule Schedule `json:"schedule"`
	// CatchUp runs a task once at start-up when Skiff was closed at the
	// time it should have run.
	CatchUp bool   `json:"catchUp"`
	Notify  string `json:"notify,omitempty"` // failure (default), always, never

	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// Run is one execution of a task.
type Run struct {
	ID       string    `json:"id"`
	TaskID   string    `json:"taskId"`
	TaskName string    `json:"taskName"`
	Trigger  string    `json:"trigger"` // schedule, catch-up, manual, command line
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitempty"`
	Status   string    `json:"status"` // running, ok, failed, cancelled
	Summary  string    `json:"summary,omitempty"`
	Stats    *RunStats `json:"stats,omitempty"` // numbers of the summary, for the interface
	Log      []string  `json:"log,omitempty"`
}

// RunStats are the numbers of a successful run.
type RunStats struct {
	Files    int   `json:"files"`
	Bytes    int64 `json:"bytes"`
	Deleted  int   `json:"deleted"`
	Compared int   `json:"compared"`
	UpToDate bool  `json:"upToDate,omitempty"`
	Commands int   `json:"commands,omitempty"` // script
}

const (
	maxRuns    = 200
	maxLogLine = 400
)

// Store keeps the tasks (tasks.json) and their history (task-runs.json)
// in the configuration folder. The history is re-read before each write:
// "skiff task run" may add runs while Skiff is open.
type Store struct {
	cfg   *config.Store
	mu    sync.Mutex
	tasks []Task
}

func OpenStore(cfg *config.Store) *Store {
	s := &Store{cfg: cfg}
	if b, err := os.ReadFile(s.path("tasks.json")); err == nil {
		json.Unmarshal(b, &s.tasks)
	}
	return s
}

func (s *Store) path(name string) string { return filepath.Join(s.cfg.Dir, name) }

func writeAtomic(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *Store) Tasks() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Task(nil), s.tasks...)
}

func (s *Store) Task(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// Find returns the task with this id or name (case-insensitive).
func (s *Store) Find(idOrName string) (Task, bool) {
	if t, ok := s.Task(idOrName); ok {
		return t, true
	}
	for _, t := range s.Tasks() {
		if strings.EqualFold(t.Name, idOrName) {
			return t, true
		}
	}
	return Task{}, false
}

// Save adds or updates a task.
func (s *Store) Save(t Task) (Task, error) {
	if strings.TrimSpace(t.Name) == "" {
		return Task{}, errors.New("the task needs a name")
	}
	switch t.Kind {
	case "sync":
		if t.SiteID == "" || t.Sync.LocalDir == "" {
			return Task{}, errors.New("a synchronization needs a site and a local folder")
		}
	case "script":
		if strings.TrimSpace(t.Script) == "" {
			return Task{}, errors.New("the script is empty")
		}
	default:
		return Task{}, errors.New("unknown kind of task")
	}
	if err := t.Schedule.Validate(); err != nil {
		return Task{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Updated = time.Now()
	found := false
	for i := range s.tasks {
		if s.tasks[i].ID == t.ID && t.ID != "" {
			t.Created = s.tasks[i].Created
			s.tasks[i], found = t, true
		}
	}
	if !found {
		t.ID, t.Created = config.NewID(), t.Updated
		s.tasks = append(s.tasks, t)
	}
	return t, writeAtomic(s.path("tasks.json"), s.tasks)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return writeAtomic(s.path("tasks.json"), s.tasks)
		}
	}
	return errors.New("task not found")
}

func (s *Store) readRuns() []Run {
	var runs []Run
	if b, err := os.ReadFile(s.path("task-runs.json")); err == nil {
		json.Unmarshal(b, &runs)
	}
	return runs
}

// Runs returns the history, the newest first (of one task when id is set).
func (s *Store) Runs(id string) []Run {
	s.mu.Lock()
	runs := s.readRuns()
	s.mu.Unlock()
	out := []Run{}
	for i := len(runs) - 1; i >= 0; i-- {
		if id == "" || runs[i].TaskID == id {
			out = append(out, runs[i])
		}
	}
	return out
}

// LastRun returns the latest run of a task.
func (s *Store) LastRun(id string) (Run, bool) {
	if r := s.Runs(id); len(r) > 0 {
		return r[0], true
	}
	return Run{}, false
}

// SaveRun adds a finished run to the history (or replaces it by ID).
func (s *Store) SaveRun(r Run) error {
	if len(r.Log) > maxLogLine {
		r.Log = append([]string{"…"}, r.Log[len(r.Log)-maxLogLine:]...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runs := s.readRuns()
	replaced := false
	for i := range runs {
		if runs[i].ID == r.ID {
			runs[i], replaced = r, true
		}
	}
	if !replaced {
		runs = append(runs, r)
	}
	sort.SliceStable(runs, func(a, b int) bool { return runs[a].Start.Before(runs[b].Start) })
	if len(runs) > maxRuns {
		runs = runs[len(runs)-maxRuns:]
	}
	return writeAtomic(s.path("task-runs.json"), runs)
}
