// Package server exposes the core over HTTP: a JSON RPC API, WebSockets for
// events and terminals, file streaming, and the embedded web UI. The same
// server backs the desktop window (loopback + per-launch token) and the
// remote web mode (login + optional TLS).
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"skiff/internal/core"
	"skiff/internal/devsync"
	"skiff/internal/instance"
	"skiff/internal/tasks"
	"skiff/internal/urlhandler"
)

type Mode string

const (
	Desktop Mode = "desktop"
	Web     Mode = "web"
)

type Options struct {
	Mode    Mode
	Version string
	// Token authenticates the desktop window (desktop mode).
	Token string
	// Web mode credentials (PasswordHash: "argon2id$salt$hash" hex).
	User         string
	PasswordHash string
	// Static is the built UI.
	Static fs.FS
	// AllowLocalTerminal in web mode.
	AllowLocalTerminal bool
	Logf               func(string, ...any)
	// HandoffKey lets a second start of the desktop window give it a
	// session URL (see internal/instance); Raise brings the window to the
	// front then.
	HandoffKey string
	Raise      func()
	// Icon of the application, for the menu entry the links may need
	Icon []byte
	// Licences: the one of Skiff, its notice, and those of the software it
	// includes (Preferences > About)
	Licences Licences
}

// Licences are the texts of the licences, shown in the About section.
type Licences struct {
	License    string `json:"license"`
	Notice     string `json:"notice"`
	ThirdParty string `json:"thirdParty"`
}

type Server struct {
	core *core.Core
	opts Options
	mux  *http.ServeMux

	mu       sync.Mutex
	sessions map[string]time.Time
	attempts map[string][]time.Time
	methods  map[string]handler
	port     int
	pending  sync.Map // connection attempts: token -> cancel
	startup  string   // session URL passed on the command line
	link     bool     // ... by a link clicked elsewhere
	opens    []instance.Request
	tasks    *tasks.Scheduler
	devsync  *devsync.Syncer
	// changed tells the sync between devices that the settings changed
	changed func()
}

// SetStartupURL makes the UI connect to url once it is loaded; link: the
// URL comes from a link clicked elsewhere, the UI asks first.
func (s *Server) SetStartupURL(u string, link bool) {
	s.mu.Lock()
	s.startup, s.link = u, link
	s.mu.Unlock()
}

func (s *Server) takeStartupURL() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, link := s.startup, s.link
	s.startup, s.link = "", false
	return u, link
}

// Open gives the UI a session URL to open (from a second start of Skiff or
// from the system), and brings the window to the front.
func (s *Server) Open(req instance.Request) {
	if req.Link && !linkScheme(req.URL) {
		// a link opens the schemes Skiff registers, nothing else (not a
		// container of this computer, for one)
		if s.opts.Logf != nil {
			s.opts.Logf("link refused: %.80s", req.URL)
		}
		req.URL = ""
	}
	if req.URL != "" {
		s.mu.Lock()
		if len(s.opens) < 20 {
			s.opens = append(s.opens, req)
		}
		s.mu.Unlock()
		s.core.Bus.Publish("app.open", map[string]any{})
	}
	if s.opts.Raise != nil {
		s.opts.Raise()
	}
}

func linkScheme(u string) bool {
	scheme, _, ok := strings.Cut(u, "://")
	if !ok {
		return false
	}
	for _, x := range urlhandler.Schemes {
		if strings.EqualFold(scheme, x) {
			return true
		}
	}
	return false
}

func (s *Server) takeOpens() []instance.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.opens
	s.opens = nil
	if o == nil {
		o = []instance.Request{}
	}
	return o
}

// handleHandoff takes the request of a second start: only with the key of
// instance.json, and never from a web page (which sends an Origin).
func (s *Server) handleHandoff(w http.ResponseWriter, r *http.Request) {
	if s.opts.Mode != Desktop || s.opts.HandoffKey == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req, ok := instance.Check(s.opts.HandoffKey, body)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if req.URL != "" && (len(req.URL) > 8192 || !strings.Contains(req.URL, "://")) {
		http.Error(w, "not a session URL", http.StatusBadRequest)
		return
	}
	s.Open(req)
	w.WriteHeader(http.StatusNoContent)
}

func New(c *core.Core, opts Options) *Server {
	s := &Server{core: c, opts: opts, mux: http.NewServeMux(), sessions: map[string]time.Time{}, attempts: map[string][]time.Time{}}
	s.tasks = tasks.NewScheduler(c.Store, c.Bus)
	s.tasks.Start()
	c.OnShutdown(s.tasks.Close)
	s.devsync = devsync.New(c.Store, c.Bus, c.HostKeys.Path)
	changes := make(chan struct{}, 1)
	s.changed = func() {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
	unsub := c.Store.OnSitesChanged(s.changed)
	stopSync := s.devsync.Start(changes)
	c.OnShutdown(func() { unsub(); stopSync() })
	s.registerRPC()
	s.mux.HandleFunc("/api/rpc/", s.handleRPC)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	s.mux.HandleFunc("/api/term/", s.handleTerm)
	s.mux.HandleFunc("/api/download", s.handleDownload)
	s.mux.HandleFunc("/api/upload", s.handleUpload)
	s.mux.HandleFunc("/api/auth/login", s.handleLogin)
	s.mux.HandleFunc("/api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("/", s.handleStatic)
	return s
}

func (s *Server) SetPort(p int) { s.port = p }

const cookieName = "skiff_session"

func randToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// HashPassword returns an argon2id hash string for web mode.
func HashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	h := argon2.IDKey([]byte(pw), salt, 2, 64*1024, 2, 32)
	return "argon2id$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(h)
}

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := hex.DecodeString(parts[1])
	want, err2 := hex.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, 2, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// authenticated checks the session cookie.
func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	if s.opts.Mode == Desktop {
		return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.opts.Token)) == 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[c.Value]
	if !ok || time.Now().After(exp) {
		delete(s.sessions, c.Value)
		return false
	}
	return true
}

// hostAllowed defends the loopback server against DNS rebinding.
func (s *Server) hostAllowed(r *http.Request) bool {
	if s.opts.Mode != Desktop {
		return true
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// sameOrigin rejects cross-site WebSocket/API requests.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	o = strings.TrimPrefix(strings.TrimPrefix(o, "http://"), "https://")
	return o == r.Host
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, v string, maxAge int) {
	// The desktop window reaches the loopback server through a navigation
	// started by the webview's own page (a different site), so a Strict
	// cookie would never be sent: use Lax there. API calls stay protected
	// by the X-Skiff header and the Origin check.
	same := http.SameSiteStrictMode
	if s.opts.Mode == Desktop {
		same = http.SameSiteLaxMode
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: v, Path: "/", HttpOnly: true, SameSite: same,
		Secure: r.TLS != nil, MaxAge: maxAge,
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hostAllowed(r) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	// Desktop bootstrap: exchange the launch token for a cookie.
	if s.opts.Mode == Desktop && r.URL.Path == "/" {
		if t := r.URL.Query().Get("token"); t != "" {
			if subtle.ConstantTimeCompare([]byte(t), []byte(s.opts.Token)) == 1 {
				// Serve the UI directly (no redirect: some webviews drop
				// cookies set on a redirect of a cross-site navigation);
				// the UI removes the token from the address afterwards.
				s.setCookie(w, r, s.opts.Token, 0)
				s.serveFile(w, r, "index.html")
				return
			}
			http.Error(w, "invalid token", http.StatusForbidden)
			return
		}
	}
	if r.URL.Path == "/handoff" {
		s.handleHandoff(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/auth/") {
		if !s.authenticated(r) {
			http.Error(w, `{"error":{"code":"auth","message":"not authenticated"}}`, http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Skiff") != "1" {
			http.Error(w, "missing X-Skiff header", http.StatusForbidden)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.opts.Static == nil {
		http.Error(w, "UI not built", http.StatusNotFound)
		return
	}
	if s.opts.Mode == Desktop && !s.authenticated(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Skiff</title><body style="font-family:system-ui;padding:40px;background:#111;color:#ddd">This Skiff window session is not authorized. Restart Skiff.</body>`))
		return
	}
	s.serveFile(w, r, strings.TrimPrefix(path.Clean(r.URL.Path), "/"))
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, p string) {
	if s.opts.Static == nil {
		http.Error(w, "UI not built", http.StatusNotFound)
		return
	}
	if p == "" {
		p = "index.html"
	}
	f, err := s.opts.Static.Open(p)
	if err != nil {
		p = "index.html" // SPA fallback
		f, err = s.opts.Static.Open(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	st, _ := f.Stat()
	f.Close()
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	data, err := fs.ReadFile(s.opts.Static, p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var mod time.Time
	if st != nil {
		mod = st.ModTime()
	}
	http.ServeContent(w, r, p, mod, strings.NewReader(string(data)))
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.opts.Mode == Desktop {
		writeJSON(w, map[string]bool{"ok": s.authenticated(r)})
		return
	}
	ip := clientIP(r)
	s.mu.Lock()
	var recent []time.Time
	for _, t := range s.attempts[ip] {
		if time.Since(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	s.attempts[ip] = recent
	blocked := len(recent) >= 5
	s.mu.Unlock()
	if blocked {
		writeErr(w, http.StatusTooManyRequests, "rate", "too many attempts, wait a minute")
		return
	}
	var body struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(body.User), []byte(s.opts.User)) == 1
	passOK := checkPassword(s.opts.PasswordHash, body.Password)
	if !userOK || !passOK {
		s.mu.Lock()
		s.attempts[ip] = append(s.attempts[ip], time.Now())
		s.mu.Unlock()
		time.Sleep(400 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "auth", "invalid user name or password")
		return
	}
	tok := randToken()
	s.mu.Lock()
	s.sessions[tok] = time.Now().Add(7 * 24 * time.Hour)
	s.mu.Unlock()
	s.setCookie(w, r, tok, 7*24*3600)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil && s.opts.Mode == Web {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	if s.opts.Mode == Web {
		s.setCookie(w, r, "", -1)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"authenticated": s.authenticated(r), "mode": s.opts.Mode, "version": s.opts.Version})
}

// Serve runs the HTTP server on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener, certFile, keyFile string) error {
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 20 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	var err error
	if certFile != "" {
		err = srv.ServeTLS(ln, certFile, keyFile)
	} else {
		err = srv.Serve(ln)
	}
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
