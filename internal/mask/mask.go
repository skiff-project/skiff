// Package mask implements WinSCP-compatible file masks:
//
//	*.txt; *.log | *.bak; temp/       include masks | exclude masks
//	*.jpg>100K                        size condition
//	*.log>1D, *>=2024-01-01           modification time conditions (>1D = newer than a day)
//	/home/*/public_html/*.php         path masks
//	cache/                            directory masks (end with /)
//
// Masks are separated by ";" or ","; matching is case-insensitive.
package mask

import (
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type cond struct {
	op   string // ">", "<", ">=", "<=", "="
	size int64
	t    time.Time
	isT  bool
}

type item struct {
	pattern string // lower-case glob
	dir     bool   // matches directories only
	path    bool   // matches full path
	conds   []cond
}

// Mask is a compiled file mask.
type Mask struct {
	src      string
	includes []item
	excludes []item
}

// Parse compiles a mask. An empty mask matches everything.
func Parse(s string) (*Mask, error) {
	m := &Mask{src: s}
	s = strings.TrimSpace(s)
	if s == "" {
		return m, nil
	}
	inc, exc := s, ""
	if i := indexUnquoted(s, '|'); i >= 0 {
		inc, exc = s[:i], s[i+1:]
	}
	var err error
	if m.includes, err = parseList(inc); err != nil {
		return nil, err
	}
	if m.excludes, err = parseList(exc); err != nil {
		return nil, err
	}
	return m, nil
}

// MustParse is Parse that ignores errors (returns match-all).
func MustParse(s string) *Mask {
	m, err := Parse(s)
	if err != nil {
		return &Mask{}
	}
	return m
}

func indexUnquoted(s string, c byte) int {
	inq := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			inq = !inq
		case s[i] == c && !inq:
			return i
		}
	}
	return -1
}

func splitList(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for _, r := range s {
		switch {
		case r == '"':
			inq = !inq
		case (r == ';' || r == ',') && !inq:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	out = append(out, cur.String())
	return out
}

func parseList(s string) ([]item, error) {
	var out []item
	for _, part := range splitList(s) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		it := item{}
		// conditions
		for {
			i := strings.LastIndexAny(part, "<>=")
			if i <= 0 {
				break
			}
			j := i
			for j > 0 && strings.ContainsRune("<>=", rune(part[j-1])) {
				j--
			}
			op := part[j : i+1]
			val := strings.TrimSpace(part[i+1:])
			c, ok := parseCond(op, val)
			if !ok {
				break
			}
			it.conds = append(it.conds, c)
			part = strings.TrimSpace(part[:j])
		}
		if strings.HasSuffix(part, "/") || strings.HasSuffix(part, `\`) {
			it.dir = true
			part = strings.TrimRight(part, `/\`)
		}
		part = strings.ReplaceAll(part, `\`, "/")
		if strings.Contains(part, "/") {
			it.path = true
		}
		if part == "" {
			part = "*"
		}
		it.pattern = strings.ToLower(part)
		out = append(out, it)
	}
	return out, nil
}

func parseCond(op, val string) (cond, bool) {
	switch op {
	case ">", "<", ">=", "<=", "=":
	default:
		return cond{}, false
	}
	if val == "" {
		return cond{}, false
	}
	// absolute date
	for _, l := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(l, val, time.Local); err == nil {
			return cond{op: op, t: t, isT: true}, true
		}
	}
	// relative time, as in WinSCP: <number><unit>[S] with Y (years),
	// D (days), H (hours), N (minutes) or S (seconds); "today" and
	// "yesterday" are accepted too. ">1D" means "newer than one day",
	// "<1D" older. A trailing S rounds down to the start of the unit
	// (">1DS" = since midnight yesterday). M is a size unit, not months.
	if t, ok := relativeTime(val, time.Now()); ok {
		return cond{op: op, t: t, isT: true}, true
	}
	last := unicode.ToUpper(rune(val[len(val)-1]))
	// size with K/M/G suffix
	mult := int64(1)
	switch unicode.ToUpper(last) {
	case 'K':
		mult = 1 << 10
	case 'M':
		mult = 1 << 20
	case 'G':
		mult = 1 << 30
	case 'T':
		mult = 1 << 40
	}
	num := val
	if mult > 1 {
		num = val[:len(val)-1]
	}
	if n, err := strconv.ParseFloat(num, 64); err == nil {
		return cond{op: op, size: int64(n * float64(mult))}, true
	}
	return cond{}, false
}

func (c cond) match(size int64, mtime time.Time) bool {
	if c.isT {
		switch c.op {
		case ">":
			return mtime.After(c.t)
		case "<":
			return mtime.Before(c.t)
		case ">=":
			return !mtime.Before(c.t)
		case "<=":
			return !mtime.After(c.t)
		default:
			y1, m1, d1 := mtime.Date()
			y2, m2, d2 := c.t.Date()
			return y1 == y2 && m1 == m2 && d1 == d2
		}
	}
	switch c.op {
	case ">":
		return size > c.size
	case "<":
		return size < c.size
	case ">=":
		return size >= c.size
	case "<=":
		return size <= c.size
	default:
		return size == c.size
	}
}

func (it item) match(name, relPath string, isDir bool, size int64, mtime time.Time) bool {
	if it.dir && !isDir {
		return false
	}
	target := strings.ToLower(name)
	if it.path {
		target = strings.ToLower("/" + strings.TrimLeft(relPath, "/"))
	}
	pat := it.pattern
	if it.path && !strings.HasPrefix(pat, "/") && !strings.HasPrefix(pat, "*") {
		pat = "*/" + pat
	}
	ok, _ := path.Match(pat, target)
	if !ok && pat == "*.*" {
		ok = true // WinSCP: *.* matches names without dot too
	}
	if !ok && it.path {
		ok = globDeep(pat, target)
	}
	if !ok {
		return false
	}
	for _, c := range it.conds {
		if !isDir && !c.match(size, mtime) {
			return false
		}
		if isDir && c.isT {
			continue
		}
	}
	return true
}

// globDeep lets "*" cross "/" boundaries for path masks.
func globDeep(pat, s string) bool {
	if pat == "" {
		return s == ""
	}
	switch pat[0] {
	case '*':
		for i := 0; i <= len(s); i++ {
			if globDeep(pat[1:], s[i:]) {
				return true
			}
		}
		return false
	case '?':
		return s != "" && globDeep(pat[1:], s[1:])
	default:
		return s != "" && s[0] == pat[0] && globDeep(pat[1:], s[1:])
	}
}

// Match reports whether a file passes the mask. relPath is the path relative
// to the transfer root, using forward slashes.
// MatchName is Match for listings (searches): a directory matches when its
// name matches a mask, instead of being traversed by default.
func (m *Mask) MatchName(name, relPath string, isDir bool, size int64, mtime time.Time) bool {
	if m == nil || !isDir {
		return m.Match(name, relPath, isDir, size, mtime)
	}
	for _, e := range m.excludes {
		if e.match(name, relPath, true, size, mtime) {
			return false
		}
	}
	if len(m.includes) == 0 {
		return true
	}
	for _, in := range m.includes {
		if in.match(name, relPath, true, size, mtime) {
			return true
		}
	}
	return false
}

func (m *Mask) Match(name, relPath string, isDir bool, size int64, mtime time.Time) bool {
	if m == nil {
		return true
	}
	for _, e := range m.excludes {
		if e.match(name, relPath, isDir, size, mtime) {
			return false
		}
	}
	if len(m.includes) == 0 {
		return true
	}
	if isDir {
		// Directories are traversed unless a directory-only include list
		// exists and none matches.
		hasDirMask := false
		for _, in := range m.includes {
			if in.dir {
				hasDirMask = true
				if in.match(name, relPath, true, 0, mtime) {
					return true
				}
			}
		}
		return !hasDirMask
	}
	for _, in := range m.includes {
		if in.dir {
			continue
		}
		if in.match(name, relPath, false, size, mtime) {
			return true
		}
	}
	// only directory masks: files inside are included
	for _, in := range m.includes {
		if !in.dir {
			return false
		}
	}
	return true
}

// Empty reports whether the mask matches everything.
func (m *Mask) Empty() bool { return m == nil || (len(m.includes) == 0 && len(m.excludes) == 0) }

func (m *Mask) String() string {
	if m == nil {
		return ""
	}
	return m.src
}

// relativeTime parses WinSCP relative times ("1D", "2HS", "today",
// "yesterday") into the corresponding point in the past.
func relativeTime(val string, now time.Time) (time.Time, bool) {
	startOfDay := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	}
	switch strings.ToLower(val) {
	case "today":
		return startOfDay(now), true
	case "yesterday":
		return startOfDay(now).AddDate(0, 0, -1), true
	}
	v := strings.ToUpper(val)
	round := false
	if len(v) > 2 && strings.HasSuffix(v, "S") && strings.ContainsRune("YDHNS", rune(v[len(v)-2])) {
		if _, err := strconv.Atoi(v[:len(v)-2]); err == nil {
			round, v = true, v[:len(v)-1]
		}
	}
	if len(v) < 2 {
		return time.Time{}, false
	}
	unit := rune(v[len(v)-1])
	n, err := strconv.Atoi(v[:len(v)-1])
	if err != nil || !strings.ContainsRune("YDHNS", unit) {
		return time.Time{}, false
	}
	var t time.Time
	switch unit {
	case 'Y':
		t = now.AddDate(-n, 0, 0)
		if round {
			t = time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
		}
	case 'D':
		t = now.AddDate(0, 0, -n)
		if round {
			t = startOfDay(t)
		}
	case 'H':
		t = now.Add(-time.Duration(n) * time.Hour)
		if round {
			t = t.Truncate(time.Hour)
		}
	case 'N':
		t = now.Add(-time.Duration(n) * time.Minute)
		if round {
			t = t.Truncate(time.Minute)
		}
	case 'S':
		t = now.Add(-time.Duration(n) * time.Second)
	}
	return t, true
}
