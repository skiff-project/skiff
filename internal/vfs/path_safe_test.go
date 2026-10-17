package vfs

import "testing"

func TestSafeName(t *testing.T) {
	for _, c := range []struct {
		style, in, out string
		ok             bool
	}{
		{"posix", "rapport.pdf", "rapport.pdf", true},
		{"posix", "../../.bashrc", ".._.._.bashrc", true},
		{"posix", "a/../../b", "a_.._.._b", true},
		{"posix", `..\x`, `..\x`, true}, // a plain character on Linux
		{"posix", "..", "..", false},
		{"posix", ".", ".", false},
		{"posix", "", "", false},
		{"posix", "a\x00b", "a_b", true},
		{"windows", `..\..\evil.bat`, ".._.._evil.bat", true},
		{"windows", "file:stream", "file_stream", true},
		{"windows", "C:", "C_", true},
		{"windows", "...", "...", false},
		{"windows", ". .", ". .", false},
		{"windows", "notes.txt", "notes.txt", true},
	} {
		out, ok := SafeName(c.style, c.in)
		if out != c.out || ok != c.ok {
			t.Errorf("SafeName(%s, %q) = %q, %v; want %q, %v", c.style, c.in, out, ok, c.out, c.ok)
		}
	}
}
