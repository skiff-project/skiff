package mask

import (
	"testing"
	"time"
)

func TestMask(t *testing.T) {
	now := time.Now()
	old := now.AddDate(-2, 0, 0)
	cases := []struct {
		mask  string
		name  string
		rel   string
		dir   bool
		size  int64
		mtime time.Time
		want  bool
	}{
		{"", "a.txt", "a.txt", false, 1, now, true},
		{"*.txt", "a.txt", "a.txt", false, 1, now, true},
		{"*.txt", "A.TXT", "A.TXT", false, 1, now, true},
		{"*.txt", "a.log", "a.log", false, 1, now, false},
		{"*.txt; *.log", "a.log", "a.log", false, 1, now, true},
		{"| *.bak", "a.bak", "a.bak", false, 1, now, false},
		{"| *.bak", "a.txt", "a.txt", false, 1, now, true},
		{"* | node_modules/", "node_modules", "src/node_modules", true, 0, now, false},
		{"* | node_modules/", "node_modules", "node_modules", false, 0, now, true},
		{"*>1K", "big", "big", false, 4096, now, true},
		{"*>1K", "small", "small", false, 10, now, false},
		{"*>1D", "new", "new", false, 1, now, true},
		{"*>1D", "old", "old", false, 1, old, false},
		{"*<1D", "old", "old", false, 1, old, true},
		{"*<1D", "new", "new", false, 1, now, false},
		{"*>1Y", "old", "old", false, 1, old, false},
		{"*<1Y", "old", "old", false, 1, old, true},
		{"*>=today", "new", "new", false, 1, now, true},
		{"*>=yesterday", "old", "old", false, 1, old, false},
		{"*>1M", "big", "big", false, 2 << 20, old, true},
		{"*>1M", "small", "small", false, 1 << 10, now, false},
		{"*.jpg>100K", "a.jpg", "a.jpg", false, 200 << 10, now, true},
		{"*>2DS", "new", "new", false, 1, now, true},
		{"*.*", "README", "README", false, 1, now, true},
		{"src/*.go", "main.go", "src/main.go", false, 1, now, true},
		{"src/*.go", "main.go", "other/main.go", false, 1, now, false},
	}
	for _, c := range cases {
		m, err := Parse(c.mask)
		if err != nil {
			t.Fatalf("%q: %v", c.mask, err)
		}
		if got := m.Match(c.name, c.rel, c.dir, c.size, c.mtime); got != c.want {
			t.Errorf("mask %q on %q (dir=%v): got %v want %v", c.mask, c.rel, c.dir, got, c.want)
		}
	}
}

func TestMatchName(t *testing.T) {
	m, _ := Parse("c* | cache/")
	now := time.Now()
	for name, want := range map[string]bool{"css": true, "doc": false, "cache": false} {
		if got := m.MatchName(name, name, true, 0, now); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
	if !m.Match("doc", "doc", true, 0, now) {
		t.Error("Match must still traverse directories")
	}
}
