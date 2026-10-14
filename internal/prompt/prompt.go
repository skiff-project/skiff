// Package prompt defines the interactive question/answer channel used by
// the protocol layers (host keys, passwords, overwrite confirmations, ...).
// The GUI implements it through the event bus; the script runner through the
// terminal (or fails in batch mode).
package prompt

import (
	"context"
	"errors"
)

type Kind string

const (
	HostKey        Kind = "hostkey"     // unknown or changed SSH host key
	Certificate    Kind = "certificate" // untrusted TLS certificate
	Password       Kind = "password"
	Passphrase     Kind = "passphrase"
	KbdInteractive Kind = "kbdint"
	Banner         Kind = "banner"
	Overwrite      Kind = "overwrite"
	Error          Kind = "error"
	Confirm        Kind = "confirm"
)

type Field struct {
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
	Value  string `json:"value,omitempty"`
}

type Choice struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Primary bool   `json:"primary,omitempty"`
	Danger  bool   `json:"danger,omitempty"`
}

type Request struct {
	Kind     Kind              `json:"kind"`
	Conn     string            `json:"conn,omitempty"`
	Title    string            `json:"title"`
	Message  string            `json:"message"`
	Details  map[string]string `json:"details,omitempty"`
	Fields   []Field           `json:"fields,omitempty"`
	Choices  []Choice          `json:"choices,omitempty"`
	Remember bool              `json:"remember,omitempty"` // offer a "remember" checkbox
	ApplyAll bool              `json:"applyAll,omitempty"` // offer an "apply to all" checkbox
	Danger   bool              `json:"danger,omitempty"`
}

type Response struct {
	Choice   string   `json:"choice"`
	Values   []string `json:"values,omitempty"`
	Remember bool     `json:"remember,omitempty"`
	ApplyAll bool     `json:"applyAll,omitempty"`
}

// Prompter asks the user something. Implementations must honour ctx.
type Prompter interface {
	Prompt(ctx context.Context, req Request) (Response, error)
}

// ErrCancelled is returned when the user dismisses a prompt.
var ErrCancelled = errors.New("cancelled by user")

// ErrNoUser is returned by non-interactive prompters.
var ErrNoUser = errors.New("interaction required but running non-interactively")

// Func adapts a function to the Prompter interface.
type Func func(ctx context.Context, req Request) (Response, error)

func (f Func) Prompt(ctx context.Context, req Request) (Response, error) { return f(ctx, req) }

// Batch never asks; it answers with the given default choice or fails.
type Batch struct{ Default string }

func (b Batch) Prompt(ctx context.Context, req Request) (Response, error) {
	if b.Default != "" {
		for _, c := range req.Choices {
			if c.ID == b.Default {
				return Response{Choice: b.Default}, nil
			}
		}
	}
	return Response{}, ErrNoUser
}

// WithConn tags every request with a connection id.
func WithConn(p Prompter, conn string) Prompter {
	if p == nil {
		return nil
	}
	return Func(func(ctx context.Context, req Request) (Response, error) {
		if req.Conn == "" {
			req.Conn = conn
		}
		return p.Prompt(ctx, req)
	})
}
