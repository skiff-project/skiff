package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func (s *Server) acceptWS(w http.ResponseWriter, r *http.Request) (*websocket.Conn, bool) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin websocket refused", http.StatusForbidden)
		return nil, false
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return nil, false
	}
	ws.SetReadLimit(1 << 20)
	return ws, true
}

// handleEvents streams core events to the UI.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.acceptWS(w, r)
	if !ok {
		return
	}
	defer ws.CloseNow()
	if s.opts.Logf != nil {
		s.opts.Logf("ui attached (%s)", r.UserAgent())
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events, unsub := s.core.Bus.Subscribe()
	defer unsub()

	// Re-send pending prompts to a (re)connecting UI.
	for _, p := range s.core.Broker.Pending() {
		if err := writeEvent(ctx, ws, "prompt", p); err != nil {
			return
		}
	}
	// Reader: detects disconnects and answers pings.
	go func() {
		defer cancel()
		for {
			_, _, err := ws.Read(ctx)
			if err != nil {
				return
			}
		}
	}()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := writeEvent(ctx, ws, ev.Type, ev.Data); err != nil {
				return
			}
		case <-ping.C:
			pctx, pc := context.WithTimeout(ctx, 10*time.Second)
			err := ws.Ping(pctx)
			pc()
			if err != nil {
				return
			}
		}
	}
}

func writeEvent(ctx context.Context, ws *websocket.Conn, typ string, data any) error {
	b, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return nil
	}
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return ws.Write(wctx, websocket.MessageText, b)
}

// handleTerm bridges a terminal: binary frames carry data, text frames
// carry control messages ({"type":"resize","cols":..,"rows":..}).
func (s *Server) handleTerm(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/term/")
	t, err := s.core.Terminal(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	ws, ok := s.acceptWS(w, r)
	if !ok {
		return
	}
	ws.SetReadLimit(4 << 20)
	defer ws.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	replay, out, detach := t.Attach()
	defer detach()
	if len(replay) > 0 {
		if err := ws.Write(ctx, websocket.MessageBinary, replay); err != nil {
			return
		}
	}
	go func() {
		defer cancel()
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if err := t.Write(data); err != nil {
					return
				}
				continue
			}
			var msg struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
				Data string `json:"data"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "resize":
				t.Resize(msg.Cols, msg.Rows)
			case "input":
				t.Write([]byte(msg.Data))
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-out:
			if !ok {
				ws.Close(websocket.StatusNormalClosure, "exited")
				return
			}
			// coalesce bursts
			buf := chunk
		drain:
			for len(buf) < 256*1024 {
				select {
				case more, ok := <-out:
					if !ok {
						break drain
					}
					buf = append(buf, more...)
				default:
					break drain
				}
			}
			wctx, wc := context.WithTimeout(ctx, 15*time.Second)
			err := ws.Write(wctx, websocket.MessageBinary, buf)
			wc()
			if err != nil {
				return
			}
		}
	}
}
