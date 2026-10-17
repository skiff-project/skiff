package core

import "testing"

func TestNameFitsBothSides(t *testing.T) {
	for name, want := range map[string]bool{"index.html": true, "../x": false, "a/b": false, `a\b`: false, "c:d": false, "..": false, "...": false, "notes v2.txt": true} {
		if got := nameFits(name, "posix", "windows"); got != want {
			t.Errorf("nameFits(%q, posix, windows) = %v", name, got)
		}
	}
	if !nameFits(`a\b`, "posix", "posix") {
		t.Error(`a backslash is a plain character between two Linux servers`)
	}
}
