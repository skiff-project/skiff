package core

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"skiff/internal/vfs"
)

// Archives are made and opened where the files are: on a server with a
// shell, by its tar, zip and unzip (or Python when zip is missing); on this
// computer, by Skiff itself. Nothing goes through the network.

// CompressRequest makes an archive of some files of a folder.
type CompressRequest struct {
	Conn    string   `json:"conn"`
	Dir     string   `json:"dir"`
	Names   []string `json:"names"`
	Archive string   `json:"archive"` // file name, in Dir
	Format  string   `json:"format"`  // zip, tar.gz, tar
}

// ExtractRequest opens an archive into a folder (made if missing).
type ExtractRequest struct {
	Conn    string `json:"conn"`
	Archive string `json:"archive"`
	Dest    string `json:"dest"`
}

var (
	ErrNoZip       = errors.New("neither zip nor python3 is on the server: choose tar.gz")
	ErrNoUnzip     = errors.New("neither unzip nor python3 is on the server")
	ErrArchiveKind = errors.New("unknown kind of archive (zip, tar, tar.gz, tgz, tar.bz2, tar.xz)")
)

// archiveKind tells the kind of an archive by its name.
func archiveKind(name string) string {
	n := strings.ToLower(name)
	for _, k := range []string{".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".tar", ".zip"} {
		if strings.HasSuffix(n, k) {
			switch k {
			case ".tgz":
				return ".tar.gz"
			case ".tbz2":
				return ".tar.bz2"
			case ".txz":
				return ".tar.xz"
			}
			return k
		}
	}
	return ""
}

func validNames(names []string) error {
	if len(names) == 0 {
		return errors.New("nothing to compress")
	}
	for _, n := range names {
		if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\\") {
			return fmt.Errorf("invalid name %q", n)
		}
	}
	return nil
}

// Compress makes the archive and returns its path.
func (c *Core) Compress(ctx context.Context, r CompressRequest) (string, error) {
	if err := validNames(r.Names); err != nil {
		return "", err
	}
	if r.Archive == "" || strings.ContainsAny(r.Archive, "/\\") {
		return "", errors.New("invalid archive name")
	}
	switch r.Format {
	case "zip", "tar.gz", "tar":
	default:
		return "", errors.New("unknown format " + r.Format)
	}
	f, err := c.FSFor(ctx, r.Conn)
	if err != nil {
		return "", err
	}
	style := f.Caps().PathStyle
	dst := vfs.Join(style, r.Dir, r.Archive)
	if _, err := f.Stat(ctx, dst); err == nil {
		return "", fmt.Errorf("%s already exists", r.Archive)
	}
	if cn, ok := c.hasShell(r.Conn); ok {
		q := make([]string, len(r.Names))
		for i, n := range r.Names {
			q[i] = vfs.ShellQuote(n)
		}
		names, a := strings.Join(q, " "), vfs.ShellQuote(r.Archive)
		cmd := "cd " + vfs.ShellQuote(r.Dir) + " || exit 1; "
		switch r.Format {
		case "tar.gz":
			cmd += "tar -czf " + a + " -- " + names
		case "tar":
			cmd += "tar -cf " + a + " -- " + names
		case "zip":
			cmd += "if command -v zip >/dev/null 2>&1; then zip -qry " + a + " " + names +
				"; elif command -v python3 >/dev/null 2>&1; then python3 -m zipfile -c " + a + " " + names +
				"; else echo SKIFF:NOZIP >&2; exit 3; fi"
		}
		if _, err := cn.sshOutput(ctx, cmd); err != nil {
			if strings.Contains(err.Error(), "SKIFF:NOZIP") {
				return "", ErrNoZip
			}
			return "", err
		}
	} else if r.Conn == LocalID || r.Conn == "" {
		if err := compressFS(ctx, f, r.Dir, r.Names, dst, r.Format); err != nil {
			f.Remove(ctx, dst, false)
			return "", err
		}
	} else {
		return "", errors.New("archives need a shell on the server (or files on this computer)")
	}
	c.Changed(r.Conn, r.Dir)
	return dst, nil
}

// Extract opens the archive into the destination folder.
func (c *Core) Extract(ctx context.Context, r ExtractRequest) error {
	kind := archiveKind(r.Archive)
	if kind == "" {
		return ErrArchiveKind
	}
	f, err := c.FSFor(ctx, r.Conn)
	if err != nil {
		return err
	}
	if r.Dest == "" {
		return errors.New("no destination folder")
	}
	if _, err := f.Stat(ctx, r.Dest); err != nil {
		if err := mkdirAll(ctx, f, r.Dest); err != nil {
			return err
		}
	}
	if cn, ok := c.hasShell(r.Conn); ok {
		a, d := vfs.ShellQuote(r.Archive), vfs.ShellQuote(r.Dest)
		if err := checkMembers(ctx, cn, a, kind); err != nil {
			return err
		}
		var cmd string
		switch kind {
		case ".zip":
			cmd = "if command -v unzip >/dev/null 2>&1; then unzip -oq " + a + " -d " + d +
				"; elif command -v python3 >/dev/null 2>&1; then python3 -m zipfile -e " + a + " " + d +
				"; else echo SKIFF:NOUNZIP >&2; exit 3; fi"
		case ".tar.gz":
			cmd = "tar -xzf " + a + " -C " + d
		case ".tar.bz2":
			cmd = "tar -xjf " + a + " -C " + d
		case ".tar.xz":
			cmd = "tar -xJf " + a + " -C " + d
		default:
			cmd = "tar -xf " + a + " -C " + d
		}
		if _, err := cn.sshOutput(ctx, cmd); err != nil {
			if strings.Contains(err.Error(), "SKIFF:NOUNZIP") {
				return ErrNoUnzip
			}
			return err
		}
	} else if r.Conn == LocalID || r.Conn == "" {
		if err := extractFS(ctx, f, r.Archive, r.Dest, kind); err != nil {
			return err
		}
	} else {
		return errors.New("archives need a shell on the server (or files on this computer)")
	}
	style := f.Caps().PathStyle
	c.Changed(r.Conn, r.Dest)
	c.Changed(r.Conn, vfs.Dir(style, r.Dest))
	return nil
}

// checkMembers lists the archive on the server first: a member that would
// land outside the destination refuses the whole archive (all tar and
// unzip do not protect against it).
func checkMembers(ctx context.Context, cn *Conn, a, kind string) error {
	var cmd string
	switch kind {
	case ".zip":
		cmd = "if command -v unzip >/dev/null 2>&1; then unzip -l " + a +
			"; elif command -v python3 >/dev/null 2>&1; then python3 -m zipfile -l " + a + "; fi"
	case ".tar.gz":
		cmd = "tar -tzf " + a
	case ".tar.bz2":
		cmd = "tar -tjf " + a
	case ".tar.xz":
		cmd = "tar -tJf " + a
	default:
		cmd = "tar -tf " + a
	}
	out, err := cn.sshOutput(ctx, cmd)
	if err != nil {
		return err
	}
	for _, ln := range strings.Split(out, "\n") {
		name := strings.TrimSpace(ln)
		if kind == ".zip" {
			// "  Length  Date  Time  Name" (unzip) or "Name  Date Time  Size" (python)
			f := strings.Fields(ln)
			if len(f) >= 4 && f[0] != "Length" && !strings.HasPrefix(f[0], "-") {
				if _, err := fmt.Sscan(f[0], new(int64)); err == nil {
					name = strings.Join(f[3:], " ")
				} else {
					name = f[0]
				}
			} else {
				continue
			}
		}
		if name == "" {
			continue
		}
		if _, err := inside("posix", "/dest", name); err != nil {
			return err
		}
	}
	return nil
}

func mkdirAll(ctx context.Context, f vfs.FS, p string) error {
	style := f.Caps().PathStyle
	if _, err := f.Stat(ctx, p); err == nil {
		return nil
	}
	if parent := vfs.Dir(style, p); parent != p && parent != "" {
		if err := mkdirAll(ctx, f, parent); err != nil {
			return err
		}
	}
	if err := f.Mkdir(ctx, p); err != nil {
		if _, e := f.Stat(ctx, p); e == nil {
			return nil
		}
		return err
	}
	return nil
}

// ---- archives made and opened by Skiff (this computer) ----

type archiveFile struct {
	rel   string // with "/"
	entry vfs.Entry
	full  string
}

// collect lists the files and folders under the names, depth first.
func collect(ctx context.Context, f vfs.FS, dir string, names []string) ([]archiveFile, error) {
	style := f.Caps().PathStyle
	var out []archiveFile
	var walk func(full, rel string) error
	walk = func(full, rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		e, err := f.Stat(ctx, full)
		if err != nil {
			return err
		}
		out = append(out, archiveFile{rel, e, full})
		if !e.IsDir || e.IsLink {
			return nil
		}
		list, err := f.List(ctx, full)
		if err != nil {
			return err
		}
		for _, x := range list {
			if err := walk(vfs.Join(style, full, x.Name), rel+"/"+x.Name); err != nil {
				return err
			}
		}
		return nil
	}
	for _, n := range names {
		if err := walk(vfs.Join(style, dir, n), n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func compressFS(ctx context.Context, f vfs.FS, dir string, names []string, dst, format string) error {
	files, err := collect(ctx, f, dir, names)
	if err != nil {
		return err
	}
	w, err := f.Create(ctx, dst, vfs.CreateOpts{Size: -1})
	if err != nil {
		return err
	}
	if format == "zip" {
		zw := zip.NewWriter(w)
		for _, af := range files {
			h := &zip.FileHeader{Name: af.rel, Modified: af.entry.ModTime, Method: zip.Deflate}
			h.SetMode(os.FileMode(af.entry.Mode & 0o777))
			if af.entry.IsDir {
				h.Name += "/"
				h.Method = zip.Store
				h.SetMode(os.ModeDir | os.FileMode(af.entry.Mode&0o777|0o700))
				if _, err := zw.CreateHeader(h); err != nil {
					w.Close()
					return err
				}
				continue
			}
			fw, err := zw.CreateHeader(h)
			if err == nil {
				err = copyFile(ctx, f, af.full, fw)
			}
			if err != nil {
				w.Close()
				return err
			}
		}
		if err := zw.Close(); err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}
	var tw *tar.Writer
	var gz *gzip.Writer
	if format == "tar.gz" {
		gz = gzip.NewWriter(w)
		tw = tar.NewWriter(gz)
	} else {
		tw = tar.NewWriter(w)
	}
	for _, af := range files {
		h := &tar.Header{Name: af.rel, ModTime: af.entry.ModTime, Mode: int64(af.entry.Mode & 0o7777), Format: tar.FormatPAX}
		if af.entry.IsDir {
			h.Typeflag, h.Name = tar.TypeDir, af.rel+"/"
			if h.Mode == 0 {
				h.Mode = 0o755
			}
		} else {
			h.Typeflag, h.Size = tar.TypeReg, af.entry.Size
			if h.Mode == 0 {
				h.Mode = 0o644
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			w.Close()
			return err
		}
		if !af.entry.IsDir {
			if err := copyFile(ctx, f, af.full, tw); err != nil {
				w.Close()
				return err
			}
		}
	}
	err = tw.Close()
	if gz != nil && err == nil {
		err = gz.Close()
	}
	if err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func copyFile(ctx context.Context, f vfs.FS, p string, w io.Writer) error {
	r, err := f.Open(ctx, p, 0)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(w, r)
	return err
}

// inside joins an archive member to the destination, refusing members
// that would land outside it ("zip slip").
func inside(style, dest, name string) (string, error) {
	clean := path.Clean("/" + strings.ReplaceAll(name, "\\", "/"))
	if clean == "/" {
		return "", nil
	}
	if strings.HasPrefix(name, "/") || strings.Contains("/"+strings.ReplaceAll(name, "\\", "/")+"/", "/../") {
		return "", fmt.Errorf("unsafe path in the archive: %s", name)
	}
	p := dest
	for _, part := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		p = vfs.Join(style, p, part)
	}
	return p, nil
}

func extractFS(ctx context.Context, f vfs.FS, archive, dest, kind string) error {
	style := f.Caps().PathStyle
	put := func(name string, isDir bool, mode os.FileMode, mtime time.Time, r io.Reader) error {
		p, err := inside(style, dest, name)
		if err != nil || p == "" {
			return err
		}
		if isDir {
			return mkdirAll(ctx, f, p)
		}
		if err := mkdirAll(ctx, f, vfs.Dir(style, p)); err != nil {
			return err
		}
		w, err := f.Create(ctx, p, vfs.CreateOpts{Size: -1, Mode: uint32(mode.Perm())})
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, r); err != nil {
			w.Close()
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		if mode.Perm() != 0 && f.Caps().Chmod {
			f.Chmod(ctx, p, uint32(mode.Perm()))
		}
		if !mtime.IsZero() {
			f.Chtimes(ctx, p, mtime)
		}
		return nil
	}
	if kind == ".zip" {
		// zip needs to read anywhere in the file
		tmp, err := os.CreateTemp("", "skiff-*.zip")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		if err := copyFile(ctx, f, archive, tmp); err != nil {
			return err
		}
		st, _ := tmp.Stat()
		zr, err := zip.NewReader(tmp, st.Size())
		if err != nil {
			return err
		}
		for _, zf := range zr.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			if zf.Mode()&os.ModeSymlink != 0 {
				continue // links are not made from an archive
			}
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			err = put(zf.Name, zf.FileInfo().IsDir(), zf.Mode(), zf.Modified, rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	r, err := f.Open(ctx, archive, 0)
	if err != nil {
		return err
	}
	defer r.Close()
	var src io.Reader = r
	switch kind {
	case ".tar.gz":
		gz, err := gzip.NewReader(r)
		if err != nil {
			return err
		}
		defer gz.Close()
		src = gz
	case ".tar.bz2":
		src = bzip2.NewReader(r)
	case ".tar.xz":
		return errors.New("tar.xz archives are opened on servers only (with their tar)")
	}
	tr := tar.NewReader(src)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = put(h.Name, true, os.FileMode(h.Mode), h.ModTime, nil)
		case tar.TypeReg, tar.TypeRegA:
			err = put(h.Name, false, os.FileMode(h.Mode), h.ModTime, tr)
		default:
			continue // links, devices: not made from an archive
		}
		if err != nil {
			return err
		}
	}
}
