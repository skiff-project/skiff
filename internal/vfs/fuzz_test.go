package vfs

import (
	"strings"
	"testing"
)

// Whatever a server names a file, a safe name is one element of a path.
func FuzzSafeName(f *testing.F) {
	for _, s := range []string{"a.txt", "../x", `..\x`, "a:b", "...", ".", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		for _, style := range []string{"posix", "windows"} {
			n, ok := SafeName(style, name)
			if !ok {
				continue
			}
			bad := "/"
			if style == "windows" {
				bad = `/\:`
			}
			if strings.ContainsAny(n, bad) || n == "." || n == ".." || strings.Contains(n, "\x00") {
				t.Errorf("SafeName(%s, %q) = %q", style, name, n)
			}
			if Join(style, "dir", n) == "dir" || !strings.HasPrefix(Join(style, "/dir", n), map[string]string{"posix": "/dir/", "windows": `/dir\`}[style]) {
				t.Errorf("Join(%s, dir, %q) leaves the folder: %q", style, n, Join(style, "/dir", n))
			}
		}
	})
}
