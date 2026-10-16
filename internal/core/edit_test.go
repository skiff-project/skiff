package core

import (
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	for in, want := range map[string][]string{
		`code --wait !.!`: {"code", "--wait", "!.!"},
		`"C:\Program Files\Notepad++\notepad++.exe" !.!`: {`C:\Program Files\Notepad++\notepad++.exe`, "!.!"},
		`sh -c 'echo ok >> "$0"' !.!`:                    {"sh", "-c", `echo ok >> "$0"`, "!.!"},
		`vim ""`:                                         {"vim", ""},
		`ed "a \"b\""`:                                   {"ed", `a "b"`},
	} {
		if got := splitCommand(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s → %q, want %q", in, got, want)
		}
	}
}
