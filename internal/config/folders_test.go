package config

import "testing"

func TestDeleteFolderKeepsSites(t *testing.T) {
	st, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSite()
	s.Host = "h"
	saved, err := st.SaveSite(s, SecretInput{})
	if err != nil {
		t.Fatal(err)
	}
	st.AddFolder("Vide")
	st.RenameFolder("Vide", "Archives")
	if err := st.MoveSites([]string{saved.ID}, "Archives"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteFolder("Archives", false); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Site(saved.ID)
	if got.Folder != "" {
		t.Fatalf("folder = %q", got.Folder)
	}
	for _, f := range st.Folders() {
		if f == "Archives" {
			t.Fatal("folder still listed")
		}
	}
}
