// Package davfs implements vfs.FS over WebDAV (HTTP/HTTPS) with Basic and
// Digest authentication.
package davfs

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"skiff/internal/vfs"
)

type Config struct {
	// BaseURL is the server URL including the initial path, e.g.
	// https://cloud.example.com/remote.php/dav/files/me/
	BaseURL   string
	User      string
	Password  string
	Transport http.RoundTripper
	Timeout   time.Duration
	Logf      func(string, ...any)
}

type FS struct {
	cfg    Config
	base   *url.URL
	client *http.Client

	mu     sync.Mutex
	scheme string // "", "basic", "digest"
	digest map[string]string
	nc     int
	server string
	dav    string
	nextcloud bool
}

func New(ctx context.Context, cfg Config) (*FS, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if u.Path == "" {
		u.Path = "/"
	}
	f := &FS{cfg: cfg, base: u, client: &http.Client{
		Transport: cfg.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}}
	// Probe: authenticates and validates the URL.
	req, _ := http.NewRequestWithContext(ctx, "PROPFIND", f.url(u.Path), strings.NewReader(propfindBody))
	req.Header.Set("Depth", "0")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	resp, err := f.do(req, []byte(propfindBody))
	if err != nil {
		return nil, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 401 {
		return nil, errors.New("webdav: authentication failed")
	}
	if resp.StatusCode != 207 && resp.StatusCode != 200 {
		return nil, fmt.Errorf("webdav: %s does not look like a WebDAV resource (%s)", u.Path, resp.Status)
	}
	f.server = resp.Header.Get("Server")
	f.dav = resp.Header.Get("DAV")
	f.nextcloud = strings.Contains(u.Path, "remote.php") || resp.Header.Get("X-Nextcloud-Request-Id") != "" || strings.Contains(strings.ToLower(f.server), "nextcloud")
	return f, nil
}

func (f *FS) logf(format string, a ...any) {
	if f.cfg.Logf != nil {
		f.cfg.Logf(format, a...)
	}
}

func (f *FS) url(p string) string {
	u := *f.base
	u.Path = p
	u.RawPath = ""
	return u.String()
}

// do sends req handling authentication challenges. body is used to replay
// the request after a 401 (nil when the body is a non-replayable stream).
func (f *FS) do(req *http.Request, body []byte) (*http.Response, error) {
	f.authorize(req)
	f.logf("> %s %s", req.Method, req.URL.Path)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	f.logf("< %s", resp.Status)
	if resp.StatusCode != 401 || f.cfg.User == "" {
		return resp, nil
	}
	chal := resp.Header.Values("WWW-Authenticate")
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !f.learn(chal) {
		return resp, nil
	}
	if req.Body != nil && body == nil && req.ContentLength != 0 {
		return resp, nil
	}
	r2 := req.Clone(req.Context())
	if body != nil {
		r2.Body = io.NopCloser(bytes.NewReader(body))
		r2.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	f.authorize(r2)
	return f.client.Do(r2)
}

func (f *FS) learn(chal []string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range chal {
		low := strings.ToLower(c)
		if strings.HasPrefix(low, "digest") {
			f.scheme = "digest"
			f.digest = parseChallenge(c[len("digest"):])
			f.nc = 0
			return true
		}
	}
	for _, c := range chal {
		if strings.HasPrefix(strings.ToLower(c), "basic") {
			if f.scheme == "basic" {
				return false // credentials rejected
			}
			f.scheme = "basic"
			return true
		}
	}
	return false
}

func parseChallenge(s string) map[string]string {
	out := map[string]string{}
	for _, part := range splitParams(s) {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			out[strings.ToLower(kv[0])] = strings.Trim(kv[1], `"`)
		}
	}
	return out
}

func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for _, r := range s {
		switch {
		case r == '"':
			inq = !inq
			cur.WriteRune(r)
		case r == ',' && !inq:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (f *FS) authorize(req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.scheme {
	case "basic":
		req.SetBasicAuth(f.cfg.User, f.cfg.Password)
	case "digest":
		d := f.digest
		f.nc++
		nc := fmt.Sprintf("%08x", f.nc)
		cb := make([]byte, 8)
		rand.Read(cb)
		cnonce := hex.EncodeToString(cb)
		uri := req.URL.RequestURI()
		h := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
		ha1 := h(f.cfg.User + ":" + d["realm"] + ":" + f.cfg.Password)
		if strings.EqualFold(d["algorithm"], "MD5-sess") {
			ha1 = h(ha1 + ":" + d["nonce"] + ":" + cnonce)
		}
		ha2 := h(req.Method + ":" + uri)
		var resp string
		qop := ""
		if strings.Contains(d["qop"], "auth") {
			qop = "auth"
			resp = h(ha1 + ":" + d["nonce"] + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
		} else {
			resp = h(ha1 + ":" + d["nonce"] + ":" + ha2)
		}
		v := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`, f.cfg.User, d["realm"], d["nonce"], uri, resp)
		if d["algorithm"] != "" {
			v += ", algorithm=" + d["algorithm"]
		}
		if d["opaque"] != "" {
			v += `, opaque="` + d["opaque"] + `"`
		}
		if qop != "" {
			v += ", qop=" + qop + ", nc=" + nc + `, cnonce="` + cnonce + `"`
		}
		req.Header.Set("Authorization", v)
	}
}

func (f *FS) Caps() vfs.Caps {
	return vfs.Caps{
		Protocol:       "webdav",
		PathStyle:      "posix",
		SetMtime:       f.nextcloud,
		ServerCopy:     true,
		Space:          true,
		ResumeDownload: true,
		MtimePrecision: time.Second,
	}
}

func (f *FS) Home(ctx context.Context) (string, error) {
	p := f.base.Path
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p, nil
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:getcontenttype/><d:getetag/><d:quota-available-bytes/><d:quota-used-bytes/></d:prop></d:propfind>`

type multistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string     `xml:"href"`
	Propstats []propstat `xml:"propstat"`
}

type propstat struct {
	Prop   davProp `xml:"prop"`
	Status string  `xml:"status"`
}

type davProp struct {
	ResourceType struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
	ContentLength string `xml:"getcontentlength"`
	LastModified  string `xml:"getlastmodified"`
	ContentType   string `xml:"getcontenttype"`
	ETag          string `xml:"getetag"`
	QuotaAvail    string `xml:"quota-available-bytes"`
	QuotaUsed     string `xml:"quota-used-bytes"`
}

func (f *FS) propfind(ctx context.Context, p string, depth string) ([]davResponse, error) {
	target := p
	if depth == "1" && !strings.HasSuffix(target, "/") {
		target += "/"
	}
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", f.url(target), strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	resp, err := f.do(req, []byte(propfindBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return nil, err
	}
	var ms multistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("webdav: invalid PROPFIND response: %w", err)
	}
	return ms.Responses, nil
}

func statusErr(resp *http.Response) error {
	switch {
	case resp.StatusCode < 300:
		return nil
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return fmt.Errorf("%s: %w", resp.Status, vfs.ErrNotExist)
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return fmt.Errorf("%s: %w", resp.Status, vfs.ErrPermission)
	case resp.StatusCode == 405 || resp.StatusCode == 412:
		return fmt.Errorf("%s: %w", resp.Status, vfs.ErrExist)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(b))
	if i := strings.Index(msg, "<s:message>"); i >= 0 {
		msg = msg[i+11:]
		if j := strings.Index(msg, "<"); j >= 0 {
			msg = msg[:j]
		}
	} else if len(msg) > 200 || strings.HasPrefix(msg, "<") {
		msg = ""
	}
	if msg != "" {
		return fmt.Errorf("webdav: %s (%s)", resp.Status, msg)
	}
	return fmt.Errorf("webdav: %s", resp.Status)
}

func hrefPath(h string) string {
	if u, err := url.Parse(h); err == nil {
		return u.Path
	}
	if p, err := url.PathUnescape(h); err == nil {
		return p
	}
	return h
}

func toEntry(r davResponse) vfs.Entry {
	p := hrefPath(r.Href)
	e := vfs.Entry{Name: path.Base(strings.TrimRight(p, "/"))}
	for _, ps := range r.Propstats {
		if ps.Status != "" && !strings.Contains(ps.Status, " 200") {
			continue
		}
		pr := ps.Prop
		if pr.ResourceType.Collection != nil {
			e.IsDir = true
		}
		if pr.ContentLength != "" {
			e.Size, _ = strconv.ParseInt(pr.ContentLength, 10, 64)
		}
		if pr.LastModified != "" {
			if t, err := http.ParseTime(pr.LastModified); err == nil {
				e.ModTime = t
			}
		}
		if pr.ContentType != "" {
			e.MimeType = pr.ContentType
		}
		if pr.ETag != "" {
			e.ETag = strings.Trim(pr.ETag, `"`)
		}
	}
	if e.IsDir {
		e.Size = 0
		e.Mode = 0o755
	} else {
		e.Mode = 0o644
	}
	e.Hidden = strings.HasPrefix(e.Name, ".")
	return e
}

func (f *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	rs, err := f.propfind(ctx, dir, "1")
	if err != nil {
		return nil, vfs.WrapErr("list", dir, err)
	}
	self := strings.TrimRight(dir, "/")
	out := make([]vfs.Entry, 0, len(rs))
	for _, r := range rs {
		p := strings.TrimRight(hrefPath(r.Href), "/")
		if p == self || p == "" {
			continue
		}
		e := toEntry(r)
		if e.Name == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *FS) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	rs, err := f.propfind(ctx, p, "0")
	if err != nil {
		return vfs.Entry{}, vfs.WrapErr("stat", p, err)
	}
	if len(rs) == 0 {
		return vfs.Entry{}, vfs.WrapErr("stat", p, vfs.ErrNotExist)
	}
	e := toEntry(rs[0])
	e.Name = path.Base(p)
	return e, nil
}


func (f *FS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", f.url(p), nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := f.do(req, nil)
	if err != nil {
		return nil, vfs.WrapErr("download", p, err)
	}
	if err := statusErr(resp); err != nil {
		resp.Body.Close()
		return nil, vfs.WrapErr("download", p, err)
	}
	if offset > 0 && resp.StatusCode == 200 {
		// Range ignored: skip manually.
		if _, err := io.CopyN(io.Discard, resp.Body, offset); err != nil {
			resp.Body.Close()
			return nil, err
		}
	}
	return resp.Body, nil
}

type putWriter struct {
	pw   *io.PipeWriter
	done chan error
}

func (w *putWriter) Write(p []byte) (int, error) { return w.pw.Write(p) }

func (w *putWriter) Close() error {
	w.pw.Close()
	return <-w.done
}

func (f *FS) Create(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	if opt.Offset > 0 {
		return nil, vfs.WrapErr("resume", p, vfs.ErrUnsupported)
	}
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, "PUT", f.url(p), pr)
	if err != nil {
		return nil, err
	}
	if opt.Size >= 0 {
		req.ContentLength = opt.Size
		if opt.Size == 0 {
			req.Body = http.NoBody
		}
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if !opt.ModTime.IsZero() {
		req.Header.Set("X-OC-Mtime", strconv.FormatInt(opt.ModTime.Unix(), 10))
	}
	w := &putWriter{pw: pw, done: make(chan error, 1)}
	go func() {
		resp, err := f.do(req, nil)
		if err != nil {
			pr.CloseWithError(err)
			w.done <- vfs.WrapErr("upload", p, err)
			return
		}
		io.Copy(io.Discard, pr) // unblock writer if server answered early
		err = statusErr(resp)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		pr.CloseWithError(err)
		w.done <- vfs.WrapErr("upload", p, err)
	}()
	return w, nil
}

func (f *FS) simple(ctx context.Context, method, p string, hdr map[string]string, body string) error {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.url(p), rd)
	if err != nil {
		return err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	var b []byte
	if body != "" {
		b = []byte(body)
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	resp, err := f.do(req, b)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return err
	}
	if resp.StatusCode == 207 {
		// multistatus on DELETE/MOVE means partial failure
		data, _ := io.ReadAll(resp.Body)
		if bytes.Contains(data, []byte(" 4")) || bytes.Contains(data, []byte(" 5")) {
			var ms multistatus
			if xml.Unmarshal(data, &ms) == nil {
				for _, r := range ms.Responses {
					for _, ps := range r.Propstats {
						if ps.Status != "" && !strings.Contains(ps.Status, " 2") {
							return fmt.Errorf("webdav: %s failed: %s", method, ps.Status)
						}
					}
				}
			}
		}
	}
	return nil
}

func (f *FS) Mkdir(ctx context.Context, p string) error {
	return vfs.WrapErr("mkdir", p, f.simple(ctx, "MKCOL", p, nil, ""))
}

func (f *FS) Remove(ctx context.Context, p string, isDir bool) error {
	if isDir && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return vfs.WrapErr("delete", p, f.simple(ctx, "DELETE", p, nil, ""))
}

// RemoveAll: DELETE on a collection is recursive.
func (f *FS) RemoveAll(ctx context.Context, p string) error { return f.Remove(ctx, p, true) }

func (f *FS) moveCopy(ctx context.Context, method, from, to string) error {
	return f.simple(ctx, method, from, map[string]string{
		"Destination": f.url(to),
		"Overwrite":   "T",
	}, "")
}

func (f *FS) Rename(ctx context.Context, from, to string) error {
	return vfs.WrapErr("move", from, f.moveCopy(ctx, "MOVE", from, to))
}

func (f *FS) Copy(ctx context.Context, from, to string) error {
	return vfs.WrapErr("copy", from, f.moveCopy(ctx, "COPY", from, to))
}

func (f *FS) Chmod(ctx context.Context, p string, mode uint32) error { return vfs.ErrUnsupported }

func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:z="urn:schemas-microsoft-com:"><d:set><d:prop><d:lastmodified>%d</d:lastmodified><z:Win32LastModifiedTime>%s</z:Win32LastModifiedTime></d:prop></d:set></d:propertyupdate>`,
		mtime.Unix(), mtime.UTC().Format(http.TimeFormat))
	err := f.simple(ctx, "PROPPATCH", p, nil, body)
	if err != nil {
		return vfs.WrapErr("touch", p, vfs.ErrUnsupported)
	}
	return nil
}

func (f *FS) Space(ctx context.Context, p string) (int64, int64, error) {
	rs, err := f.propfind(ctx, p, "0")
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rs {
		for _, ps := range r.Propstats {
			if ps.Prop.QuotaAvail != "" {
				avail, _ := strconv.ParseInt(ps.Prop.QuotaAvail, 10, 64)
				used, _ := strconv.ParseInt(ps.Prop.QuotaUsed, 10, 64)
				if avail < 0 {
					return 0, 0, vfs.ErrUnsupported
				}
				return avail + used, avail, nil
			}
		}
	}
	return 0, 0, vfs.ErrUnsupported
}

func (f *FS) Fork(ctx context.Context) (vfs.FS, error) { return sharedFork{f}, nil }

type sharedFork struct{ *FS }

func (sharedFork) IsSharedFork() bool { return true }
func (sharedFork) Close() error       { return nil }

func (f *FS) Info() []vfs.InfoItem {
	return []vfs.InfoItem{
		{Group: "WebDAV", Key: "URL", Value: f.base.String()},
		{Group: "WebDAV", Key: "Server", Value: f.server},
		{Group: "WebDAV", Key: "DAV compliance", Value: f.dav},
		{Group: "WebDAV", Key: "Authentication", Value: f.scheme},
	}
}

func (f *FS) Close() error {
	f.client.CloseIdleConnections()
	return nil
}
