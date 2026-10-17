package scpfs

import "testing"

// The output of find and ls comes from the server.
func FuzzListings(f *testing.F) {
	f.Add("f|644|12|1700000000|alice|staff|a.txt\nd|755|0|1700000000|alice|staff|sub\n", "-rw-r--r-- 1 alice staff 12 Jan  2 03:04 a.txt\ndrwxr-xr-x 2 alice staff 0 Jan  2  2020 sub\n")
	f.Fuzz(func(t *testing.T, find, ls string) {
		for _, e := range append(parseFind(find), parseLs(ls, "posix")...) {
			if e.Name == "." || e.Name == ".." {
				t.Errorf("entry %q listed", e.Name)
			}
		}
		ParseLsLine(ls, "windows")
	})
}
