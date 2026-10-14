package server

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"skiff/internal/vfs"
)

// handleDownload streams a file to the browser (previews, "save as" in web
// mode). Supports single byte ranges for media seeking.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, p := q.Get("conn"), q.Get("path")
	f, err := s.core.FSFor(r.Context(), conn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	e, err := f.Stat(r.Context(), p)
	if err != nil {
		st, _ := errorCode(err)
		http.Error(w, err.Error(), st)
		return
	}
	if e.IsDir {
		http.Error(w, "is a directory", http.StatusBadRequest)
		return
	}
	name := vfs.Base(f.Caps().PathStyle, p)
	ctype := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	// Never let the browser render active content inline from the app origin.
	inline := q.Get("inline") == "1"
	if inline && (strings.Contains(ctype, "html") || strings.Contains(ctype, "svg") || strings.Contains(ctype, "xml") || strings.Contains(ctype, "javascript")) {
		ctype = "text/plain; charset=utf-8"
	}
	disp := "attachment"
	if inline {
		disp = "inline"
	}
	w.Header().Set("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	if !e.ModTime.IsZero() {
		w.Header().Set("Last-Modified", e.ModTime.UTC().Format(http.TimeFormat))
	}
	start, end := int64(0), e.Size-1
	status := http.StatusOK
	if rg := r.Header.Get("Range"); rg != "" && strings.HasPrefix(rg, "bytes=") && e.Size > 0 {
		spec := strings.SplitN(strings.TrimPrefix(rg, "bytes="), ",", 2)[0]
		parts := strings.SplitN(spec, "-", 2)
		if len(parts) == 2 {
			if parts[0] == "" {
				n, _ := strconv.ParseInt(parts[1], 10, 64)
				if n > 0 && n <= e.Size {
					start = e.Size - n
				}
			} else {
				start, _ = strconv.ParseInt(parts[0], 10, 64)
				if parts[1] != "" {
					end, _ = strconv.ParseInt(parts[1], 10, 64)
				}
			}
			if start < 0 || start >= e.Size || end < start {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", e.Size))
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if end >= e.Size {
				end = e.Size - 1
			}
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, e.Size))
		}
	}
	length := end - start + 1
	if e.Size == 0 {
		length = 0
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}
	rd, err := f.Open(r.Context(), p, start)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rd.Close()
	w.WriteHeader(status)
	io.Copy(w, io.LimitReader(rd, length))
}

// handleUpload receives a raw request body (drag & drop from the OS).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method", "PUT required")
		return
	}
	q := r.URL.Query()
	conn, p := q.Get("conn"), q.Get("path")
	f, err := s.core.FSFor(r.Context(), conn)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	style := f.Caps().PathStyle
	if q.Get("mkdirs") == "1" {
		if err := vfs.MkdirAll(r.Context(), f, vfs.Dir(style, p)); err != nil {
			writeErr(w, http.StatusInternalServerError, "error", err.Error())
			return
		}
	}
	if q.Get("overwrite") != "1" {
		if _, err := f.Stat(r.Context(), p); err == nil {
			writeErr(w, http.StatusConflict, "exists", "a file with this name already exists")
			return
		}
	}
	var mtime time.Time
	if ms, err := strconv.ParseInt(q.Get("mtime"), 10, 64); err == nil && ms > 0 {
		mtime = time.UnixMilli(ms)
	}
	wc, err := f.Create(r.Context(), p, vfs.CreateOpts{Size: r.ContentLength, ModTime: mtime})
	if err != nil {
		st, code := errorCode(err)
		writeErr(w, st, code, err.Error())
		return
	}
	n, err := io.Copy(wc, r.Body)
	cerr := wc.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && r.ContentLength >= 0 && n != r.ContentLength {
		err = fmt.Errorf("upload interrupted (%d of %d bytes)", n, r.ContentLength)
	}
	if err != nil {
		f.Remove(r.Context(), p, false)
		writeErr(w, http.StatusInternalServerError, "error", err.Error())
		return
	}
	if !mtime.IsZero() && f.Caps().SetMtime {
		f.Chtimes(r.Context(), p, mtime)
	}
	s.core.Changed(conn, vfs.Dir(style, p))
	writeJSON(w, map[string]any{"result": map[string]any{"size": n}})
}
