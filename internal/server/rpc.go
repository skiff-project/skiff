package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"skiff/internal/config"
	"skiff/internal/prompt"
	"skiff/internal/vfs"
)

type handler func(ctx context.Context, raw json.RawMessage) (any, error)

// method registers a typed RPC handler.
func method[P any, R any](s *Server, name string, fn func(ctx context.Context, p P) (R, error)) {
	if s.methods == nil {
		s.methods = map[string]handler{}
	}
	s.methods[name] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, &rpcError{Code: "bad_request", Message: "invalid parameters: " + err.Error(), status: 400}
			}
		}
		return fn(ctx, p)
	}
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	status  int
}

func (e *rpcError) Error() string { return e.Message }

func decodeJSON(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func errorCode(err error) (int, string) {
	var re *rpcError
	switch {
	case errors.As(err, &re):
		st := re.status
		if st == 0 {
			st = 400
		}
		return st, re.Code
	case errors.Is(err, vfs.ErrNotExist):
		return 404, "not_found"
	case errors.Is(err, vfs.ErrExist):
		return 409, "exists"
	case errors.Is(err, vfs.ErrPermission):
		return 403, "permission"
	case errors.Is(err, vfs.ErrUnsupported):
		return 501, "unsupported"
	case errors.Is(err, prompt.ErrCancelled), errors.Is(err, context.Canceled):
		return 499, "cancelled"
	case errors.Is(err, config.ErrLocked):
		return 423, "locked"
	case errors.Is(err, config.ErrWrongPassword):
		return 401, "wrong_password"
	}
	if strings.Contains(err.Error(), "modified on the server") {
		return 409, "conflict"
	}
	return 500, "error"
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/rpc/")
	h, ok := s.methods[name]
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown_method", fmt.Sprintf("unknown method %q", name))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	res, err := h(r.Context(), raw)
	if err != nil {
		st, code := errorCode(err)
		if s.opts.Logf != nil && st >= 500 {
			s.opts.Logf("rpc %s: %v", name, err)
		}
		writeErr(w, st, code, err.Error())
		return
	}
	writeJSON(w, map[string]any{"result": res})
}
