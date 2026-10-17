package script

import "testing"

func FuzzTokenize(f *testing.F) {
	for _, s := range []string{`open "sftp://u@h/" -hostkey=*`, `get "a b"/*.txt "c:\x y\"`, `put -nopreservetime "%TIMESTAMP#yyyymmdd%"`, `"unterminated`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		Tokenize(line)
		expand(line)
	})
}
