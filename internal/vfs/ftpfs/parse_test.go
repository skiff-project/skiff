package ftpfs

import "testing"

func TestParseMLSx(t *testing.T) {
	e, ok := parseMLSx("type=file;size=42;modify=20240102030405;UNIX.mode=0640;UNIX.owner=bob; report final.pdf")
	if !ok || e.Name != "report final.pdf" || e.Size != 42 || e.Mode != 0o640 || e.Owner != "bob" || e.ModTime.Year() != 2024 {
		t.Errorf("unexpected %+v", e)
	}
	if _, ok := parseMLSx("type=cdir;modify=20240102030405; ."); ok {
		t.Error("cdir should be skipped")
	}
	d, ok := parseMLSx("type=dir;modify=20240102030405; sub")
	if !ok || !d.IsDir {
		t.Errorf("dir not parsed: %+v", d)
	}
}

func TestParseDOS(t *testing.T) {
	e, ok := parseDOS("01-02-24  03:04PM       <DIR>          My Folder")
	if !ok || !e.IsDir || e.Name != "My Folder" {
		t.Errorf("unexpected %+v", e)
	}
	f, ok := parseDOS("01-02-24  03:04PM              1,234 file.txt")
	if !ok || f.Size != 1234 || f.Name != "file.txt" {
		t.Errorf("unexpected %+v", f)
	}
}
