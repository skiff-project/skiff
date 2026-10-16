package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"skiff/internal/prompt"
)

// Manager owns the queue.
type Manager struct {
	res Resolver
	pub Publisher
	pr  prompt.Prompter

	mu          sync.Mutex
	jobs        []*job
	maxParallel int
	wake        chan struct{}
	stop        chan struct{}
	Logf        func(format string, args ...any)
	// ConnLogf, when set, also writes the job messages to the log of the
	// source and destination connections.
	ConnLogf func(conn, format string, args ...any)
}

func NewManager(res Resolver, pub Publisher, pr prompt.Prompter, maxParallel int) *Manager {
	if maxParallel <= 0 {
		maxParallel = 3
	}
	m := &Manager{res: res, pub: pub, pr: pr, maxParallel: maxParallel, wake: make(chan struct{}, 1), stop: make(chan struct{})}
	go m.scheduler()
	go m.ticker()
	return m
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) SetMaxParallel(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	m.maxParallel = n
	m.mu.Unlock()
	m.kick()
}

func (m *Manager) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Enqueue adds a job; it starts as soon as a slot is free.
func (m *Manager) Enqueue(req Request) string {
	j := newJob(m, req)
	m.mu.Lock()
	m.jobs = append(m.jobs, j)
	m.mu.Unlock()
	m.publish(j)
	m.kick()
	return j.id
}

// Run enqueues a job and waits for it to finish (scripting).
func (m *Manager) Run(ctx context.Context, req Request) (Snapshot, error) {
	id := m.Enqueue(req)
	j := m.find(id)
	select {
	case <-j.done:
	case <-ctx.Done():
		j.cancel()
		<-j.done
	}
	s := j.snapshot()
	switch s.Status {
	case Done:
		return s, nil
	case Cancelled:
		return s, context.Canceled
	}
	if s.Error != "" {
		return s, errors.New(s.Error)
	}
	if len(s.Errors) > 0 {
		return s, errors.New(s.Errors[0].Path + ": " + s.Errors[0].Error)
	}
	return s, nil
}

func (m *Manager) find(id string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

func (m *Manager) scheduler() {
	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
		}
		m.mu.Lock()
		running := 0
		for _, j := range m.jobs {
			st := j.status()
			if st == Scanning || st == Running {
				running++
			}
		}
		for _, j := range m.jobs {
			if running >= m.maxParallel {
				break
			}
			if j.status() == Queued && !j.held {
				j.setStatus(Scanning)
				running++
				go func(j *job) {
					j.run()
					m.kick()
				}(j)
			}
		}
		m.mu.Unlock()
	}
}

func (m *Manager) ticker() {
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		m.mu.Lock()
		jobs := append([]*job(nil), m.jobs...)
		m.mu.Unlock()
		for _, j := range jobs {
			if j.tick() {
				m.publish(j)
			}
		}
	}
}

func (m *Manager) publish(j *job) {
	if m.pub != nil {
		m.pub.Publish("transfer.update", j.snapshot())
	}
}

// Jobs returns snapshots of every job in queue order.
func (m *Manager) Jobs() []Snapshot {
	m.mu.Lock()
	jobs := append([]*job(nil), m.jobs...)
	m.mu.Unlock()
	out := make([]Snapshot, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.snapshot())
	}
	return out
}

func (m *Manager) Cancel(id string) {
	if j := m.find(id); j != nil {
		j.cancel()
	}
}

func (m *Manager) Pause(id string, pause bool) {
	if j := m.find(id); j != nil {
		j.setPaused(pause)
		m.publish(j)
	}
}

// Hold keeps a queued job from starting (WinSCP "suspend").
func (m *Manager) Hold(id string, hold bool) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.id == id {
			j.held = hold
		}
	}
	m.mu.Unlock()
	if j := m.find(id); j != nil {
		m.publish(j)
	}
	m.kick()
}

// Move changes the position of a job in the queue (delta -1 = up).
func (m *Manager) Move(id string, delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.jobs {
		if j.id == id {
			k := i + delta
			if k < 0 || k >= len(m.jobs) {
				return
			}
			m.jobs[i], m.jobs[k] = m.jobs[k], m.jobs[i]
			break
		}
	}
	if m.pub != nil {
		ids := make([]string, len(m.jobs))
		for i, j := range m.jobs {
			ids[i] = j.id
		}
		go m.pub.Publish("transfer.order", ids)
	}
}

// Remove deletes a finished job (or cancels and removes a running one).
func (m *Manager) Remove(id string) {
	j := m.find(id)
	if j == nil {
		return
	}
	j.cancel()
	m.mu.Lock()
	for i, x := range m.jobs {
		if x.id == id {
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	if m.pub != nil {
		m.pub.Publish("transfer.removed", id)
	}
}

// ClearFinished removes every finished job.
func (m *Manager) ClearFinished() {
	m.mu.Lock()
	var keep []*job
	var removed []string
	for _, j := range m.jobs {
		switch j.status() {
		case Done, Cancelled, Failed, Partial:
			removed = append(removed, j.id)
		default:
			keep = append(keep, j)
		}
	}
	m.jobs = keep
	m.mu.Unlock()
	for _, id := range removed {
		if m.pub != nil {
			m.pub.Publish("transfer.removed", id)
		}
	}
}

// Retry re-queues the failed files of a finished job.
func (m *Manager) Retry(id string) (string, error) {
	j := m.find(id)
	if j == nil {
		return "", errors.New("no such job")
	}
	failed := j.failedPairs()
	req := j.req
	if len(failed) > 0 {
		req.Explicit = failed
		req.Paths = nil
	}
	return m.Enqueue(req), nil
}

// Active reports whether any job is still pending or running.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, j := range m.jobs {
		switch j.status() {
		case Queued, Scanning, Running, Paused:
			n++
		}
	}
	return n
}

// CancelConn cancels every job using a connection (on disconnect).
func (m *Manager) CancelConn(conn string) {
	m.mu.Lock()
	jobs := append([]*job(nil), m.jobs...)
	m.mu.Unlock()
	for _, j := range jobs {
		if j.req.Src == conn || j.req.Dst == conn {
			j.cancel()
		}
	}
}

func (m *Manager) Close() {
	close(m.stop)
	m.mu.Lock()
	jobs := append([]*job(nil), m.jobs...)
	m.mu.Unlock()
	for _, j := range jobs {
		j.cancel()
	}
}

func sortStrings(s []string) { sort.Strings(s) }
