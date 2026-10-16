package script

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTokenize(t *testing.T) {
	got := Tokenize(`put "my file.txt" "/remote dir/" -filemask="*.txt|*.bak" "say ""hi"""`)
	want := []string{"put", "my file.txt", "/remote dir/", "-filemask=*.txt|*.bak", `say "hi"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestExpand(t *testing.T) {
	t.Setenv("SKIFF_TEST_VAR", "abc")
	if got := expand("x %SKIFF_TEST_VAR% %UNKNOWN_SKIFF%"); got != "x abc %UNKNOWN_SKIFF%" {
		t.Errorf("got %q", got)
	}
	t.Setenv("1", "premier")
	if got := expand("put %1% /depot/"); got != "put premier /depot/" {
		t.Errorf("/parameter: got %q", got)
	}
	got := expand("backup-%TIMESTAMP#yyyy-mm-dd%.tgz")
	if !strings.HasPrefix(got, "backup-"+time.Now().Format("2006-01-02")) {
		t.Errorf("got %q", got)
	}
	y := expand("%TIMESTAMP-1D#yyyymmdd%")
	if y != time.Now().AddDate(0, 0, -1).Format("20060102") {
		t.Errorf("got %q", y)
	}
}
