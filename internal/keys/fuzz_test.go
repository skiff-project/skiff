package keys

import (
	"strings"
	"testing"
)

// authorized_keys comes from the server.
func FuzzParseAuthorized(f *testing.F) {
	f.Add("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAZVXRMv9N7JittOu7saEaVX6WnFHU3ePYhQ04lSAd5S alex@portable\n# c\n\nfrom=\"10.0.0.0/8,1.2.3.4\",command=\"x y\" ssh-rsa AAAA")
	f.Add("cert-authority ssh-ed25519 AAAA\r\nnot a key")
	f.Fuzz(func(t *testing.T, content string) {
		for _, k := range ParseAuthorized(content) {
			if k.Line < 1 || k.Text == "" {
				t.Errorf("line %+v", k)
			}
			if !k.Invalid && !strings.HasPrefix(k.Fingerprint, "SHA256:") {
				t.Errorf("valid key without fingerprint: %+v", k)
			}
		}
		Fingerprint(content)
	})
}
