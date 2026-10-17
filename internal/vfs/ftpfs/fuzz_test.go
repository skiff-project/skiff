package ftpfs

import (
	"strings"
	"testing"
)

// Listing lines come from the server.
func FuzzListingLines(f *testing.F) {
	for _, s := range []string{"type=file;size=42;modify=20240102030405;UNIX.mode=0640;UNIX.owner=bob; report final.pdf", "type=cdir; .", "01-02-24  03:04PM       <DIR>          My Folder", "12-31-99  11:59AM              1234 a.txt", "257 \"/home/x\" is current"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, ln string) {
		for _, parse := range []func(string) (string, bool){
			func(s string) (string, bool) { e, ok := parseMLSx(s); return e.Name, ok },
			func(s string) (string, bool) { e, ok := parseDOS(s); return e.Name, ok },
		} {
			if name, ok := parse(ln); ok && (name == "" || name == "." || name == "..") {
				t.Errorf("%q: entry named %q", ln, name)
			}
		}
		parsePWD(ln)
		parseFTPTime(strings.TrimSpace(ln))
	})
}
