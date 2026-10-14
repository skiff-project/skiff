package scpfs

import "testing"

func TestParseLs(t *testing.T) {
	cases := []struct {
		line, style, name, target string
		size                      int64
		dir, link                 bool
		mode                      uint32
	}{
		{"-rw-r--r--  1 bob users  1234 2024-01-02 03:04:05.000000000 +0100 my file.txt", "full", "my file.txt", "", 1234, false, false, 0o644},
		{"drwxr-sr-x  2 bob users  4096 Jan  2 03:04 dir", "", "dir", "", 0, true, false, 0o2755},
		{"lrwxrwxrwx  1 bob users     4 Jan  2  2020 link -> target", "", "link", "target", 4, false, true, 0o777},
		{"-rwsr-xr-t  1 root root 10 Mar 10 12:00 suid", "", "suid", "", 10, false, false, 0o5755},
	}
	for _, c := range cases {
		e, ok := ParseLsLine(c.line, c.style)
		if !ok {
			t.Fatalf("cannot parse %q", c.line)
		}
		if e.Name != c.name || e.Size != c.size || e.IsDir != c.dir || e.IsLink != c.link || e.LinkTarget != c.target || e.Mode != c.mode {
			t.Errorf("%q => %+v (mode %o)", c.line, e, e.Mode)
		}
	}
}
