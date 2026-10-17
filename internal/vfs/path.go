package vfs

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
)

// Path helpers aware of the backend path style ("posix" or "windows").

func Join(style, dir, name string) string {
	if style == "windows" {
		if dir == "" || dir == "/" {
			return name
		}
		if strings.HasSuffix(dir, `\`) {
			return dir + name
		}
		return dir + `\` + name
	}
	if dir == "" {
		dir = "/"
	}
	return path.Join(dir, name)
}

func Dir(style, p string) string {
	if style == "windows" {
		p = strings.TrimRight(p, `\`)
		i := strings.LastIndex(p, `\`)
		if i < 0 {
			return "" // drive list
		}
		d := p[:i]
		if len(d) == 2 && d[1] == ':' {
			return d + `\`
		}
		return d
	}
	if p == "" || p == "/" {
		return "/"
	}
	return path.Dir(strings.TrimRight(p, "/"))
}

func Base(style, p string) string {
	if style == "windows" {
		p = strings.TrimRight(p, `\`)
		i := strings.LastIndex(p, `\`)
		return p[i+1:]
	}
	if p == "/" {
		return "/"
	}
	return path.Base(p)
}

// Rel returns p relative to root using forward slashes (for comparisons).
func Rel(style, root, p string) string {
	sep := "/"
	if style == "windows" {
		sep = `\`
	}
	root = strings.TrimRight(root, sep)
	r := strings.TrimPrefix(p, root)
	r = strings.TrimLeft(r, sep)
	if style == "windows" {
		r = strings.ReplaceAll(r, `\`, "/")
	}
	return r
}

// JoinRel joins a forward-slash relative path onto dir in the given style.
func JoinRel(style, dir, rel string) string {
	if rel == "" {
		return dir
	}
	out := dir
	for _, part := range strings.Split(rel, "/") {
		if part == "" {
			continue
		}
		out = Join(style, out, part)
	}
	return out
}

// SkipDir can be returned by a WalkFunc to skip a directory.
var SkipDir = errors.New("skip this directory")

// WalkFunc is invoked for every entry; p is the full path.
type WalkFunc func(p string, e Entry, err error) error

// Walk visits root (if a directory) recursively in lexical order. The root
// itself is not passed to fn.
func Walk(ctx context.Context, f FS, root string, fn WalkFunc) error {
	style := f.Caps().PathStyle
	return walk(ctx, f, style, root, fn, 0)
}

func walk(ctx context.Context, f FS, style, dir string, fn WalkFunc, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return errors.New("directory nesting too deep (symlink loop?)")
	}
	entries, err := f.List(ctx, dir)
	if err != nil {
		return fn(dir, Entry{IsDir: true, Name: Base(style, dir)}, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, e := range entries {
		p := Join(style, dir, e.Name)
		err := fn(p, e, nil)
		if err == SkipDir {
			continue
		}
		if err != nil {
			return err
		}
		if e.IsDir && !e.IsLink {
			if err := walk(ctx, f, style, p, fn, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveAll deletes p recursively using the backend's native implementation
// when available.
func RemoveAll(ctx context.Context, f FS, p string, isDir bool) error {
	if !isDir {
		return f.Remove(ctx, p, false)
	}
	if ra, ok := f.(RemoveAller); ok {
		return ra.RemoveAll(ctx, p)
	}
	style := f.Caps().PathStyle
	entries, err := f.List(ctx, p)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		child := Join(style, p, e.Name)
		if e.IsDir && !e.IsLink {
			if err := RemoveAll(ctx, f, child, true); err != nil {
				return err
			}
		} else if err := f.Remove(ctx, child, false); err != nil {
			return err
		}
	}
	return f.Remove(ctx, p, true)
}

// MkdirAll creates p and any missing parents.
func MkdirAll(ctx context.Context, f FS, p string) error {
	style := f.Caps().PathStyle
	if e, err := f.Stat(ctx, p); err == nil {
		if e.IsDir {
			return nil
		}
		return WrapErr("mkdir", p, ErrExist)
	}
	parent := Dir(style, p)
	if parent != p && parent != "" && parent != "/" {
		if err := MkdirAll(ctx, f, parent); err != nil {
			return err
		}
	}
	err := f.Mkdir(ctx, p)
	if err != nil {
		if e, serr := f.Stat(ctx, p); serr == nil && e.IsDir {
			return nil
		}
	}
	return err
}

// ShellQuote quotes s for a POSIX shell.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:@%+,", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SafeName makes a name read from a listing usable as one element of a
// path of this style. A server may send any name: a separator in it would
// write elsewhere than the destination ("../../.bashrc", "a/../../b"; on
// Windows a backslash, or a colon: "a:b" is a hidden stream of "a"). They
// become "_". It reports false for a name that names no file of its own
// ("", ".", "..", and on Windows names of dots and spaces only).
func SafeName(style, name string) (string, bool) {
	r := strings.NewReplacer("/", "_", "\x00", "_")
	if style == "windows" {
		r = strings.NewReplacer("/", "_", `\`, "_", ":", "_", "\x00", "_")
	}
	n := r.Replace(name)
	if n == "" || n == "." || n == ".." || (style == "windows" && strings.Trim(n, ". ") == "") {
		return n, false
	}
	return n, true
}
