package core

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"skiff/internal/vfs"
)

// A project is a folder opened with its tree, its files in tabs and a
// search in all its files. Nothing is installed on the server: the file
// list and the search use "find" and "grep" when the connection has a
// shell, and else go through the files one by one.

// DefaultProjectExcludes are the folders a project skips by default.
var DefaultProjectExcludes = []string{".git", ".hg", ".svn", "node_modules", "vendor", "__pycache__", ".venv", "venv", ".cache", ".next", ".nuxt", ".terraform"}

const (
	maxProjectFiles   = 20000
	maxSearchFileSize = 2 << 20
	maxScanFiles      = 20000 // files read one by one, without a shell
	maxMatchText      = 300
)

func (c *Core) projectExcludes() []string {
	s := strings.TrimSpace(c.Store.Prefs().Editor.ProjectExcludes)
	if s == "" {
		return DefaultProjectExcludes
	}
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
}

// ProjectFiles is the list of the files of a project, relative to its root.
type ProjectFiles struct {
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated,omitempty"`
}

// hasShell tells whether commands can run on a connection (SSH with a
// shell, or a container).
func (c *Core) hasShell(conn string) (*Conn, bool) {
	if conn == LocalID || conn == "" {
		return nil, false
	}
	cn, err := c.Conn(conn)
	if err != nil {
		return nil, false
	}
	cn.mu.Lock()
	ok := cn.caps.Shell
	cn.mu.Unlock()
	return cn, ok
}

// findPrune is the part of a find command that skips the excluded folders.
func findPrune(ex []string) string {
	if len(ex) == 0 {
		return ""
	}
	q := make([]string, len(ex))
	for i, e := range ex {
		q[i] = "-name " + vfs.ShellQuote(e)
	}
	return `\( -type d \( ` + strings.Join(q, " -o ") + ` \) \) -prune -o `
}

// ProjectFiles lists the files of a project (for "open a file by its name").
func (c *Core) ProjectFiles(ctx context.Context, conn, root string) (ProjectFiles, error) {
	ex := c.projectExcludes()
	if cn, ok := c.hasShell(conn); ok {
		cmd := "cd " + vfs.ShellQuote(root) + " && find . " + findPrune(ex) + "-type f -print 2>/dev/null | head -n " + strconv.Itoa(maxProjectFiles+1) + "; true"
		if out, err := cn.sshOutput(ctx, cmd); err == nil {
			res := ProjectFiles{Files: []string{}}
			for _, ln := range strings.Split(out, "\n") {
				if p := strings.TrimPrefix(strings.TrimRight(ln, "\r"), "./"); p != "" && p != "." {
					res.Files = append(res.Files, p)
				}
			}
			if len(res.Files) > maxProjectFiles {
				res.Files, res.Truncated = res.Files[:maxProjectFiles], true
			}
			sort.Strings(res.Files)
			return res, nil
		}
	}
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return ProjectFiles{}, err
	}
	res := ProjectFiles{Files: []string{}}
	err = walkProject(ctx, f, root, ex, func(rel string, e vfs.Entry) bool {
		if len(res.Files) >= maxProjectFiles {
			res.Truncated = true
			return false
		}
		res.Files = append(res.Files, rel)
		return true
	})
	sort.Strings(res.Files)
	return res, err
}

// walkProject visits the files under root, skipping the excluded folders;
// fn returns false to stop.
func walkProject(ctx context.Context, f vfs.FS, root string, ex []string, fn func(rel string, e vfs.Entry) bool) error {
	style := f.Caps().PathStyle
	skip := map[string]bool{}
	for _, e := range ex {
		skip[e] = true
	}
	type dir struct{ path, rel string }
	queue := []dir{{root, ""}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := queue[0]
		queue = queue[1:]
		list, err := f.List(ctx, d.path)
		if err != nil {
			if d.rel == "" {
				return err
			}
			continue // an unreadable sub-folder
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		for _, e := range list {
			rel := e.Name
			if d.rel != "" {
				rel = d.rel + "/" + e.Name
			}
			if e.IsDir {
				if !skip[e.Name] && !e.IsLink {
					queue = append(queue, dir{vfs.Join(style, d.path, e.Name), rel})
				}
				continue
			}
			if !fn(rel, e) {
				return nil
			}
		}
	}
	return nil
}

// SearchOptions is a search in the files of a project.
type SearchOptions struct {
	Query         string `json:"query"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
	MaxResults    int    `json:"maxResults,omitempty"`
}

// SearchMatch is a line that contains the searched text.
type SearchMatch struct {
	Path string `json:"path"` // relative to the root of the project
	Line int    `json:"line"`
	Col  int    `json:"col"` // 1-based, in characters
	Len  int    `json:"len"` // length of the match, in characters
	Text string `json:"text"`
}

// SearchResult is the result of ProjectSearch.
type SearchResult struct {
	Matches   []SearchMatch `json:"matches"`
	Files     int           `json:"files"` // files with a match
	Truncated bool          `json:"truncated,omitempty"`
	// Method is "grep" (run on the server) or "scan" (files read here).
	Method string `json:"method"`
}

// matcher finds the query in a line: its start and length in bytes.
func (o SearchOptions) matcher() (func(string) (int, int), error) {
	if o.Regex {
		expr := o.Query
		if !o.CaseSensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
		return func(s string) (int, int) {
			if m := re.FindStringIndex(s); m != nil {
				return m[0], m[1] - m[0]
			}
			return -1, 0
		}, nil
	}
	if o.CaseSensitive {
		return func(s string) (int, int) { return strings.Index(s, o.Query), len(o.Query) }, nil
	}
	q := strings.ToLower(o.Query)
	return func(s string) (int, int) {
		ls := strings.ToLower(s)
		if len(ls) != len(s) { // a case change resized a character
			re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(o.Query))
			if m := re.FindStringIndex(s); m != nil {
				return m[0], m[1] - m[0]
			}
			return -1, 0
		}
		return strings.Index(ls, q), len(q)
	}, nil
}

// match makes a SearchMatch of a line, or reports none.
func makeMatch(find func(string) (int, int), rel string, line int, text string) (SearchMatch, bool) {
	at, n := find(text)
	if at < 0 {
		return SearchMatch{}, false
	}
	col := utf8.RuneCountInString(text[:at])
	ln := utf8.RuneCountInString(text[at : at+n])
	// a long line is cut around the match
	if utf8.RuneCountInString(text) > maxMatchText {
		r := []rune(text)
		start := col - 40
		if start < 0 {
			start = 0
		}
		end := start + maxMatchText
		if end > len(r) {
			end = len(r)
		}
		prefix := ""
		if start > 0 {
			prefix = "…"
		}
		text = prefix + string(r[start:end])
		col = col - start + utf8.RuneCountInString(prefix)
	}
	return SearchMatch{Path: rel, Line: line, Col: col + 1, Len: ln, Text: text}, true
}

// ProjectSearch looks for a text in the files of a project.
func (c *Core) ProjectSearch(ctx context.Context, conn, root string, o SearchOptions) (SearchResult, error) {
	if o.Query == "" {
		return SearchResult{}, errors.New("nothing to search for")
	}
	if o.MaxResults <= 0 || o.MaxResults > 5000 {
		o.MaxResults = 1000
	}
	find, err := o.matcher()
	if err != nil {
		return SearchResult{}, err
	}
	ex := c.projectExcludes()
	if cn, ok := c.hasShell(conn); ok {
		if res, err := c.grepSearch(ctx, cn, root, ex, o, find); err == nil {
			sortMatches(res.Matches)
			return res, nil
		}
	}
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return SearchResult{}, err
	}
	res := SearchResult{Matches: []SearchMatch{}, Method: "scan"}
	scanned := 0
	style := f.Caps().PathStyle
	err = walkProject(ctx, f, root, ex, func(rel string, e vfs.Entry) bool {
		if e.Size > maxSearchFileSize {
			return true
		}
		if scanned++; scanned > maxScanFiles {
			res.Truncated = true
			return false
		}
		r, err := f.Open(ctx, vfs.Join(style, root, strings.ReplaceAll(rel, "/", sepOf(style))), 0)
		if err != nil {
			return true
		}
		data, _ := io.ReadAll(io.LimitReader(r, maxSearchFileSize+1))
		r.Close()
		head := data
		if len(head) > 8192 {
			head = head[:8192]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			return true // binary
		}
		text := decodeText(data, "").Content
		found := false
		for i, ln := range strings.Split(text, "\n") {
			if m, ok := makeMatch(find, rel, i+1, ln); ok {
				if len(res.Matches) >= o.MaxResults {
					res.Truncated = true
					return false
				}
				res.Matches = append(res.Matches, m)
				found = true
			}
		}
		if found {
			res.Files++
		}
		return true
	})
	sortMatches(res.Matches)
	return res, err
}

// sortMatches orders the matches by file, then line.
func sortMatches(ms []SearchMatch) {
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Path != ms[j].Path {
			return ms[i].Path < ms[j].Path
		}
		return ms[i].Line < ms[j].Line
	})
}

func sepOf(style string) string {
	if style == "windows" {
		return `\`
	}
	return "/"
}

var grepLine = regexp.MustCompile(`^\./(.+?):(\d+):(.*)$`)

// grepSearch runs find and grep on the server.
func (c *Core) grepSearch(ctx context.Context, cn *Conn, root string, ex []string, o SearchOptions, find func(string) (int, int)) (SearchResult, error) {
	flags := "-nH"
	if !o.CaseSensitive {
		flags += "i"
	}
	if o.Regex {
		flags += "E"
	} else {
		flags += "F"
	}
	// -I (skip binary files) is missing from some grep (busybox)
	cmd := "cd " + vfs.ShellQuote(root) + " || exit 1\n" +
		`I=; echo | grep -I x >/dev/null 2>&1; [ $? -le 1 ] && I=-I` + "\n" +
		"find . " + findPrune(ex) + "-type f -size -" + strconv.Itoa(maxSearchFileSize>>10) + "k -print0 2>/dev/null | " +
		"xargs -0 grep " + flags + " $I -e " + vfs.ShellQuote(o.Query) + " -- 2>/dev/null | head -n " + strconv.Itoa(o.MaxResults*2+1) + "; true"
	out, err := cn.sshOutput(ctx, cmd)
	if err != nil {
		return SearchResult{}, err
	}
	res := SearchResult{Matches: []SearchMatch{}, Method: "grep"}
	files := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		g := grepLine.FindStringSubmatch(strings.TrimRight(sc.Text(), "\r"))
		if g == nil {
			continue // "Binary file … matches"
		}
		n, _ := strconv.Atoi(g[2])
		text := g[3]
		if !utf8.ValidString(text) {
			text = decodeText([]byte(text), "").Content
		}
		m, ok := makeMatch(find, g[1], n, text)
		if !ok {
			// the server's regular expressions differ a little: keep the line
			m = SearchMatch{Path: g[1], Line: n, Col: 1, Text: text}
			if utf8.RuneCountInString(text) > maxMatchText {
				m.Text = string([]rune(text)[:maxMatchText])
			}
		}
		if len(res.Matches) >= o.MaxResults {
			res.Truncated = true
			break
		}
		res.Matches = append(res.Matches, m)
		files[m.Path] = true
	}
	res.Files = len(files)
	return res, nil
}

// ---- history: the content of a file before each save ----

// HistoryVersion is a copy of a file kept before a save.
type HistoryVersion struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Size int64     `json:"size"`
}

const (
	historyKeep    = 20
	historyMaxAge  = 30 * 24 * time.Hour
	historyMaxFile = 10 << 20
)

// historyDir is the folder of the copies of one file of one server.
func (c *Core) historyDir(conn, p string) string {
	where := "local"
	if conn != LocalID && conn != "" {
		if cn, err := c.Conn(conn); err == nil {
			where = cn.Info().URL
		}
	}
	h := sha256.Sum256([]byte(where + "\x00" + p))
	return filepath.Join(c.Store.Dir, "history", hex.EncodeToString(h[:12]))
}

// keepVersion copies the current content of a file into its history,
// unless the save does not change it.
func (c *Core) keepVersion(ctx context.Context, f vfs.FS, conn, p string, size int64, next []byte) error {
	if size > historyMaxFile {
		return nil
	}
	r, err := f.Open(ctx, p, 0)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(r, historyMaxFile+1))
	r.Close()
	if err != nil {
		return err
	}
	if bytes.Equal(data, next) {
		return nil
	}
	dir := c.historyDir(conn, p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// the same content as the latest copy is not kept twice
	if vs, _ := c.History(conn, p); len(vs) > 0 {
		if last, err := os.ReadFile(filepath.Join(dir, vs[0].ID+".bak")); err == nil && bytes.Equal(last, data) {
			return nil
		}
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(filepath.Join(dir, id+".bak"), data, 0o600); err != nil {
		return err
	}
	where := "local"
	if cn, err := c.Conn(conn); err == nil {
		where = cn.Info().URL
	}
	meta, _ := json.Marshal(map[string]string{"server": where, "path": p})
	os.WriteFile(filepath.Join(dir, "file.json"), meta, 0o600)
	// the last ones, of the last days
	vs, _ := c.History(conn, p)
	for i, v := range vs {
		if i >= historyKeep || time.Since(v.Time) > historyMaxAge {
			os.Remove(filepath.Join(dir, v.ID+".bak"))
		}
	}
	return nil
}

// History lists the copies of a file, the newest first.
func (c *Core) History(conn, p string) ([]HistoryVersion, error) {
	ents, err := os.ReadDir(c.historyDir(conn, p))
	if err != nil {
		if os.IsNotExist(err) {
			return []HistoryVersion{}, nil
		}
		return nil, err
	}
	out := []HistoryVersion{}
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".bak")
		n, err := strconv.ParseInt(id, 10, 64)
		if !ok || err != nil {
			continue
		}
		v := HistoryVersion{ID: id, Time: time.Unix(0, n)}
		if st, err := e.Info(); err == nil {
			v.Size = st.Size()
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// HistoryRead decodes a copy of a file like the editor reads files.
func (c *Core) HistoryRead(conn, p, id, encoding string) (FileContent, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return FileContent{}, errors.New("no such version")
	}
	data, err := os.ReadFile(filepath.Join(c.historyDir(conn, p), id+".bak"))
	if err != nil {
		return FileContent{}, errors.New("no such version")
	}
	fc := decodeText(data, encoding)
	fc.Path = p
	return fc, nil
}

// warn adds a warning to the log of a connection.
func (c *Core) warn(conn, format string, args ...any) {
	if cn, err := c.Conn(conn); err == nil {
		cn.log.Warnf(format, args...)
	}
}
