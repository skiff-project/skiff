package tasks

import (
	"context"
	"errors"
	"sync"
	"time"

	"skiff/internal/config"
)

// Publisher emits events to the interface.
type Publisher interface {
	Publish(typ string, data any)
}

// View is a task with its state, for the interface.
type View struct {
	Task
	Next    *time.Time `json:"next,omitempty"`
	Last    *Run       `json:"last,omitempty"`
	Running bool       `json:"running"`
}

// Scheduler runs the enabled tasks on time while Skiff is open.
type Scheduler struct {
	Store *Store
	cfg   *config.Store
	bus   Publisher
	// Now is the clock (replaceable in tests).
	Now func() time.Time

	mu      sync.Mutex
	running map[string]context.CancelFunc // task id -> cancel
	ref     map[string]time.Time          // reference of the next run
	stop    chan struct{}
	stopped sync.Once
	wg      sync.WaitGroup
}

func NewScheduler(cfg *config.Store, bus Publisher) *Scheduler {
	return &Scheduler{Store: OpenStore(cfg), cfg: cfg, bus: bus, Now: time.Now, running: map[string]context.CancelFunc{}, ref: map[string]time.Time{}, stop: make(chan struct{})}
}

// Start checks the tasks every few seconds; missed runs of tasks with
// CatchUp are run once.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.Tick()
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.Tick()
			}
		}
	}()
}

// Close stops the scheduler and cancels the running tasks.
func (s *Scheduler) Close() {
	s.stopped.Do(func() {
		close(s.stop)
		s.mu.Lock()
		for _, cancel := range s.running {
			cancel()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
}

// reference is what the next run is computed from: the last run, else
// the last change of the task.
func (s *Scheduler) reference(t Task) time.Time {
	s.mu.Lock()
	ref, ok := s.ref[t.ID]
	s.mu.Unlock()
	if ok {
		return ref
	}
	ref = t.Updated
	if last, ok := s.Store.LastRun(t.ID); ok && last.Start.After(ref) {
		ref = last.Start
	}
	return ref
}

// NextRun returns when the task will run by itself (zero: never).
func (s *Scheduler) NextRun(t Task) time.Time {
	if !t.Enabled {
		return time.Time{}
	}
	return t.Schedule.Next(s.reference(t))
}

// missedAfter is how late a run may start before it counts as missed
// (Skiff was closed at that time).
const missedAfter = 2 * time.Minute

// Tick runs the tasks that are due.
func (s *Scheduler) Tick() {
	now := s.Now()
	for _, t := range s.Store.Tasks() {
		if !t.Enabled || s.isRunning(t.ID) {
			continue
		}
		next := s.NextRun(t)
		if next.IsZero() || next.After(now) {
			continue
		}
		trigger := "schedule"
		if now.Sub(next) > missedAfter {
			if !t.CatchUp {
				// skip the missed runs: start counting from now
				s.mu.Lock()
				s.ref[t.ID] = now
				s.mu.Unlock()
				s.publish(t)
				continue
			}
			trigger = "catch-up"
		}
		s.RunNow(t.ID, trigger)
	}
}

func (s *Scheduler) isRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[id]
	return ok
}

// RunNow starts a task in the background.
func (s *Scheduler) RunNow(id, trigger string) error {
	t, ok := s.Store.Task(id)
	if !ok {
		return errors.New("task not found")
	}
	s.mu.Lock()
	if _, busy := s.running[id]; busy {
		s.mu.Unlock()
		return errors.New("the task is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running[id] = cancel
	s.ref[id] = s.Now()
	s.mu.Unlock()
	s.publish(t)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		r := Execute(ctx, s.cfg, t, trigger, nil)
		cancel()
		s.Store.SaveRun(r)
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
		if s.bus != nil {
			s.bus.Publish("task.run", map[string]any{"run": r, "notify": notifies(t, r)})
		}
		s.publish(t)
	}()
	return nil
}

func notifies(t Task, r Run) bool {
	switch t.Notify {
	case "always":
		return true
	case "never":
		return false
	}
	return r.Status == "failed"
}

// Cancel stops a running task.
func (s *Scheduler) Cancel(id string) {
	s.mu.Lock()
	cancel := s.running[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// View returns the state of a task.
func (s *Scheduler) View(t Task) View {
	v := View{Task: t, Running: s.isRunning(t.ID)}
	if n := s.NextRun(t); !n.IsZero() {
		v.Next = &n
	}
	if last, ok := s.Store.LastRun(t.ID); ok {
		last.Log = nil
		v.Last = &last
	}
	return v
}

func (s *Scheduler) Views() []View {
	out := []View{}
	for _, t := range s.Store.Tasks() {
		out = append(out, s.View(t))
	}
	return out
}

func (s *Scheduler) publish(t Task) {
	if s.bus != nil {
		s.bus.Publish("task.update", s.View(t))
	}
}

// Changed tells the scheduler a task was saved or deleted.
func (s *Scheduler) Changed(id string) {
	s.mu.Lock()
	delete(s.ref, id)
	s.mu.Unlock()
	if t, ok := s.Store.Task(id); ok {
		s.publish(t)
	} else if s.bus != nil {
		s.bus.Publish("task.update", map[string]any{"id": id, "deleted": true})
	}
}
