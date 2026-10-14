package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"skiff/internal/prompt"
)

// Event is pushed to the UI over the WebSocket.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Bus is a simple fan-out publisher.
type Bus struct {
	mu   sync.Mutex
	subs map[int]chan Event
	next int
}

func NewBus() *Bus { return &Bus{subs: map[int]chan Event{}} }

func (b *Bus) Publish(typ string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ev := Event{Type: typ, Data: data}
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default: // slow consumer: drop
		}
	}
}

// Subscribe returns a channel of events and a cancel function.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, 4096)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

func (b *Bus) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func randID(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// PromptEvent is a pending question for the UI.
type PromptEvent struct {
	ID string `json:"id"`
	prompt.Request
}

type pendingPrompt struct {
	ev PromptEvent
	ch chan prompt.Response
}

// Broker implements prompt.Prompter by asking the UI through the bus.
type Broker struct {
	bus     *Bus
	mu      sync.Mutex
	pending map[string]*pendingPrompt
	order   []string
}

func NewBroker(bus *Bus) *Broker { return &Broker{bus: bus, pending: map[string]*pendingPrompt{}} }

func (b *Broker) Prompt(ctx context.Context, req prompt.Request) (prompt.Response, error) {
	// Wait a little for a UI to be attached (app start-up).
	for i := 0; i < 50 && b.bus.Subscribers() == 0; i++ {
		select {
		case <-ctx.Done():
			return prompt.Response{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if b.bus.Subscribers() == 0 {
		return prompt.Response{}, prompt.ErrNoUser
	}
	p := &pendingPrompt{ev: PromptEvent{ID: randID(8), Request: req}, ch: make(chan prompt.Response, 1)}
	b.mu.Lock()
	b.pending[p.ev.ID] = p
	b.order = append(b.order, p.ev.ID)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, p.ev.ID)
		for i, id := range b.order {
			if id == p.ev.ID {
				b.order = append(b.order[:i], b.order[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
	}()
	b.bus.Publish("prompt", p.ev)
	select {
	case r := <-p.ch:
		if r.Choice == "cancel" {
			return r, prompt.ErrCancelled
		}
		return r, nil
	case <-ctx.Done():
		b.bus.Publish("prompt.close", p.ev.ID)
		return prompt.Response{}, ctx.Err()
	}
}

// Answer delivers the user's response.
func (b *Broker) Answer(id string, r prompt.Response) error {
	b.mu.Lock()
	p, ok := b.pending[id]
	b.mu.Unlock()
	if !ok {
		return errors.New("prompt expired")
	}
	select {
	case p.ch <- r:
	default:
	}
	b.bus.Publish("prompt.close", id)
	return nil
}

// Pending lists unanswered prompts (sent again when a UI reconnects).
func (b *Broker) Pending() []PromptEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]PromptEvent, 0, len(b.order))
	for _, id := range b.order {
		if p, ok := b.pending[id]; ok {
			out = append(out, p.ev)
		}
	}
	return out
}
