package config

import "testing"

// Session URLs come from links clicked anywhere: none may crash Skiff.
func FuzzParseURL(f *testing.F) {
	for _, s := range []string{"sftp://alex@serveur:22/var/www", "ftps://u:p%40ss@[::1]:990/", "s3://AK:SK@s3.example.com/bucket", "dav://h/a b", "docker://web/etc", "sftp://u;fingerprint=SHA256:abc@h/", "h", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		s, _, err := ParseURL(raw)
		if err == nil && s.Protocol == "" {
			t.Errorf("%q: no protocol", raw)
		}
	})
}
