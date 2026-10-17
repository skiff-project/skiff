package keys

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"

	"skiff/internal/core"
	"skiff/internal/prompt"
	"skiff/internal/vfs"
)

// The keys a server lets in: ~/.ssh/authorized_keys of the account, read
// and written with file operations only (SFTP-only accounts too). Skiff
// keeps every line it does not remove as it was (options, comments).

// AuthorizedKey is a line of authorized_keys.
type AuthorizedKey struct {
	Line        int      `json:"line"` // from 1
	Type        string   `json:"type,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"` // SHA256:…
	Comment     string   `json:"comment,omitempty"`
	Options     []string `json:"options,omitempty"` // from="…", command="…", restrict…
	// CA: a certificate authority whose certificates are let in
	CA      bool   `json:"ca,omitempty"`
	Invalid bool   `json:"invalid,omitempty"` // not a key sshd can read
	Text    string `json:"text"`              // the line as written
}

// Authorized is the authorized_keys file of an account.
type Authorized struct {
	Path   string          `json:"path"`
	Exists bool            `json:"exists"`
	Keys   []AuthorizedKey `json:"keys"`
	// Version of the content: a change made from an older one is refused
	Version string `json:"version"`
	// LooseModes: the home folder, ~/.ssh or the file can be written by
	// others; sshd (StrictModes) then ignores the file
	LooseModes []string `json:"looseModes,omitempty"`
	// the key of this connection ("" for a password)
	AuthKey  string `json:"authKey,omitempty"`
	AuthCert bool   `json:"authCert,omitempty"`
}

// ErrChanged: the file changed since it was read (another session, another
// administrator).
var ErrChanged = errors.New("authorized_keys changed since it was read: reload it")

// ErrOwnKey: the key to remove is the one this connection logged in with.
var ErrOwnKey = errors.New("this connection logged in with this key: without it, it may no longer get in")

func version(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

// ParseAuthorized reads the lines of an authorized_keys file (the comments
// and the blank lines are not keys).
func ParseAuthorized(content string) []AuthorizedKey {
	var out []AuthorizedKey
	for i, ln := range strings.Split(content, "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k := AuthorizedKey{Line: i + 1, Text: t}
		pk, comment, opts, _, err := ssh.ParseAuthorizedKey([]byte(t))
		if err != nil {
			k.Invalid = true
		} else {
			k.Type, k.Fingerprint, k.Comment, k.Options = pk.Type(), ssh.FingerprintSHA256(pk), comment, opts
			for _, o := range opts {
				if strings.EqualFold(o, "cert-authority") {
					k.CA = true
				}
			}
		}
		out = append(out, k)
	}
	return out
}

type authFile struct {
	fs      vfs.FS
	home    string
	dir, ak string
	content string
	exists  bool
	link    bool // authorized_keys is a link: written in place
	modes   []string
}

func openAuthorized(ctx context.Context, c *core.Core, conn string) (*authFile, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return nil, err
	}
	home, err := f.Home(ctx)
	if err != nil {
		return nil, err
	}
	a := &authFile{fs: f, home: home, dir: vfs.Join("posix", home, ".ssh")}
	a.ak = vfs.Join("posix", a.dir, "authorized_keys")
	if r, err := f.Open(ctx, a.ak, 0); err == nil {
		b, rerr := io.ReadAll(io.LimitReader(r, 4<<20))
		r.Close()
		if rerr != nil {
			return nil, rerr
		}
		a.content, a.exists = string(b), true
	}
	// what sshd checks before reading the file (StrictModes)
	if st, err := f.Stat(ctx, home); err == nil && st.Mode&0o022 != 0 {
		a.modes = append(a.modes, fmt.Sprintf("%s %04o", home, st.Mode&0o7777))
	}
	if list, err := f.List(ctx, a.dir); err == nil {
		if st, err := f.Stat(ctx, a.dir); err == nil && st.Mode&0o022 != 0 {
			a.modes = append(a.modes, fmt.Sprintf("%s %04o", a.dir, st.Mode&0o7777))
		}
		for _, e := range list {
			if e.Name == "authorized_keys" {
				a.link = e.IsLink
				if !e.IsLink && e.Mode&0o022 != 0 {
					a.modes = append(a.modes, fmt.Sprintf("%s %04o", a.ak, e.Mode&0o7777))
				}
			}
		}
	}
	return a, nil
}

func (a *authFile) view(c *core.Core, conn string) Authorized {
	v := Authorized{Path: a.ak, Exists: a.exists, Keys: ParseAuthorized(a.content), Version: version(a.content), LooseModes: a.modes}
	if v.Keys == nil {
		v.Keys = []AuthorizedKey{}
	}
	if cn, err := c.Conn(conn); err == nil && cn.SSH() != nil {
		v.AuthKey, v.AuthCert = cn.SSH().Info.AuthKey, cn.SSH().Info.AuthCert
	}
	return v
}

// write replaces the file through a new one renamed over it: a connection
// lost in the middle leaves the old file, never half a file.
func (a *authFile) write(ctx context.Context, content string) error {
	f := a.fs
	if _, err := f.Stat(ctx, a.dir); err != nil {
		if err := f.Mkdir(ctx, a.dir); err != nil {
			return err
		}
		f.Chmod(ctx, a.dir, 0o700)
	}
	put := func(p string) error {
		w, err := f.Create(ctx, p, vfs.CreateOpts{Size: int64(len(content)), Mode: 0o600})
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}
	if a.link {
		if err := put(a.ak); err != nil {
			return err
		}
	} else {
		tmp := a.ak + ".skiff-new"
		if err := put(tmp); err != nil {
			f.Remove(ctx, tmp, false)
			return err
		}
		f.Chmod(ctx, tmp, 0o600)
		if err := f.Rename(ctx, tmp, a.ak); err != nil {
			f.Remove(ctx, tmp, false)
			return err
		}
	}
	f.Chmod(ctx, a.ak, 0o600)
	a.content, a.exists = content, true
	return nil
}

// ListAuthorized reads authorized_keys on the server of conn.
func ListAuthorized(ctx context.Context, c *core.Core, conn string) (Authorized, error) {
	a, err := openAuthorized(ctx, c, conn)
	if err != nil {
		return Authorized{}, err
	}
	return a.view(c, conn), nil
}

// RemoveAuthorized removes the lines of these keys (fingerprints) from
// authorized_keys. version is the one the user saw ("" not to check);
// force allows removing the key of this very connection.
func RemoveAuthorized(ctx context.Context, c *core.Core, conn string, fingerprints []string, ver string, force bool) (Authorized, error) {
	a, err := openAuthorized(ctx, c, conn)
	if err != nil {
		return Authorized{}, err
	}
	if ver != "" && ver != version(a.content) {
		return a.view(c, conn), ErrChanged
	}
	drop := map[string]bool{}
	for _, fp := range fingerprints {
		drop[fp] = true
	}
	if own := a.view(c, conn).AuthKey; own != "" && drop[own] && !force {
		return a.view(c, conn), ErrOwnKey
	}
	var kept []string
	removed := 0
	for _, ln := range strings.Split(a.content, "\n") {
		if pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(ln))); err == nil && drop[ssh.FingerprintSHA256(pk)] {
			removed++
			continue
		}
		kept = append(kept, ln)
	}
	if removed == 0 {
		return a.view(c, conn), nil
	}
	if err := a.write(ctx, strings.Join(kept, "\n")); err != nil {
		return Authorized{}, err
	}
	return a.view(c, conn), nil
}

// add appends a key to the file unless it is there; it reports whether it
// was added.
func (a *authFile) add(ctx context.Context, publicKey string) (bool, error) {
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return false, fmt.Errorf("invalid public key: %w", err)
	}
	for _, k := range ParseAuthorized(a.content) {
		if k.Fingerprint == ssh.FingerprintSHA256(pk) && !k.CA {
			return false, nil // already installed
		}
	}
	content := a.content
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return true, a.write(ctx, content+publicKey+"\n")
}

// ReplaceResult tells what a replacement did.
type ReplaceResult struct {
	Path      string `json:"path"`
	Added     bool   `json:"added"`    // the new key was not there yet
	Verified  bool   `json:"verified"` // the server let the new key in
	Removed   int    `json:"removed"`  // lines of the old key removed
	OldAbsent bool   `json:"oldAbsent,omitempty"`
}

// Replace installs publicKey and, when oldFingerprint is given, removes
// the old key, but only once a second connection made with the new key
// alone got in: a key whose private part is not here, or that the server
// refuses, leaves the old key in place.
func Replace(ctx context.Context, c *core.Core, conn, publicKey, oldFingerprint string, pr prompt.Prompter) (ReplaceResult, error) {
	publicKey = strings.TrimSpace(publicKey)
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("invalid public key: %w", err)
	}
	newFP := ssh.FingerprintSHA256(pk)
	if oldFingerprint, err = Fingerprint(oldFingerprint); err != nil {
		return ReplaceResult{}, err
	}
	a, err := openAuthorized(ctx, c, conn)
	if err != nil {
		return ReplaceResult{}, err
	}
	res := ReplaceResult{Path: a.ak}
	if res.Added, err = a.add(ctx, publicKey); err != nil {
		return res, err
	}
	if oldFingerprint == "" || oldFingerprint == newFP {
		return res, nil
	}
	present := 0
	for _, k := range ParseAuthorized(a.content) {
		if k.Fingerprint == oldFingerprint {
			present++
		}
	}
	if present == 0 {
		res.OldAbsent = true
		return res, nil
	}
	keyFile := ""
	for _, kf := range List() {
		if kf.Fingerprint == newFP && !strings.HasSuffix(kf.Path, ".pub") {
			keyFile = kf.Path
			break
		}
	}
	if err := c.CheckKeyLogin(ctx, conn, newFP, keyFile, pr); err != nil {
		return res, fmt.Errorf("the new key is installed, but the old one stays: %w", friendlyCheck(err))
	}
	res.Verified = true
	after, err := RemoveAuthorized(ctx, c, conn, []string{oldFingerprint}, "", true)
	if err != nil {
		return res, err
	}
	for _, k := range after.Keys {
		if k.Fingerprint == oldFingerprint {
			return res, errors.New("the old key is still in authorized_keys")
		}
	}
	res.Removed = present
	return res, nil
}

// Fingerprint is the SHA256 fingerprint of a key given by its fingerprint
// or by its public key line ("" stays "").
func Fingerprint(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "SHA256:") {
		return key, nil
	}
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key))
	if err != nil {
		return "", errors.New("the old key is neither a SHA256 fingerprint nor a public key")
	}
	return ssh.FingerprintSHA256(pk), nil
}

func friendlyCheck(err error) error {
	if strings.Contains(err.Error(), "unable to authenticate") {
		return errors.New("the server does not accept it (another authorized_keys file in sshd_config, or a restriction of the account?)")
	}
	return err
}
