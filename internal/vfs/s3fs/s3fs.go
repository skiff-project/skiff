// Package s3fs implements vfs.FS over Amazon S3 and S3-compatible storage
// (MinIO, Wasabi, Backblaze B2, Cloudflare R2, Scaleway, OVH, ...).
package s3fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"skiff/internal/vfs"
)

type Config struct {
	Endpoint     string // host[:port], default s3.amazonaws.com
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Secure       bool
	PathStyle    bool
	// Bucket restricts the session to one bucket (optional).
	Bucket    string
	Transport http.RoundTripper
	Logf      func(string, ...any)
}

type FS struct {
	cfg Config
	c   *minio.Client
}

func New(ctx context.Context, cfg Config) (*FS, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "s3.amazonaws.com"
	}
	var creds *credentials.Credentials
	if cfg.AccessKey == "" && cfg.SecretKey == "" {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{}, &credentials.FileAWSCredentials{}, &credentials.IAM{},
		})
	} else {
		creds = credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken)
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:        creds,
		Secure:       cfg.Secure,
		Region:       cfg.Region,
		BucketLookup: lookup,
		Transport:    cfg.Transport,
	})
	if err != nil {
		return nil, err
	}
	c.SetAppInfo("Skiff", "1")
	f := &FS{cfg: cfg, c: c}
	// Validate credentials.
	if cfg.Bucket != "" {
		ok, err := c.BucketExists(ctx, cfg.Bucket)
		if err != nil {
			return nil, mapErr(err)
		}
		if !ok {
			return nil, fmt.Errorf("bucket %q does not exist", cfg.Bucket)
		}
	} else if _, err := c.ListBuckets(ctx); err != nil {
		return nil, mapErr(err)
	}
	return f, nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	er := minio.ToErrorResponse(err)
	switch er.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return fmt.Errorf("%s: %w", er.Message, vfs.ErrNotExist)
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return fmt.Errorf("%s: %w", er.Message, vfs.ErrPermission)
	case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
		return fmt.Errorf("%s: %w", er.Message, vfs.ErrExist)
	}
	if er.StatusCode == 404 {
		return fmt.Errorf("not found: %w", vfs.ErrNotExist)
	}
	return err
}

// split turns "/bucket/a/b" into ("bucket", "a/b").
func split(p string) (string, string) {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" || p == "." {
		return "", ""
	}
	i := strings.IndexByte(p, '/')
	if i < 0 {
		return p, ""
	}
	return p[:i], p[i+1:]
}

func (f *FS) Caps() vfs.Caps {
	return vfs.Caps{
		Protocol:       "s3",
		PathStyle:      "posix",
		ServerCopy:     true,
		Checksum:       true,
		ResumeDownload: true,
		SetMtime:       false,
		MtimePrecision: time.Second,
	}
}

func (f *FS) Home(ctx context.Context) (string, error) {
	if f.cfg.Bucket != "" {
		return "/" + f.cfg.Bucket, nil
	}
	return "/", nil
}

func (f *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	bucket, prefix := split(dir)
	if bucket == "" {
		bs, err := f.c.ListBuckets(ctx)
		if err != nil {
			return nil, vfs.WrapErr("list", dir, mapErr(err))
		}
		out := make([]vfs.Entry, 0, len(bs))
		for _, b := range bs {
			out = append(out, vfs.Entry{Name: b.Name, IsDir: true, ModTime: b.CreationDate, Mode: 0o755})
		}
		return out, nil
	}
	if prefix != "" {
		prefix += "/"
	}
	var out []vfs.Entry
	seen := map[string]bool{}
	for obj := range f.c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix}) {
		if obj.Err != nil {
			return nil, vfs.WrapErr("list", dir, mapErr(obj.Err))
		}
		name := strings.TrimPrefix(obj.Key, prefix)
		if name == "" {
			continue // directory marker
		}
		if strings.HasSuffix(name, "/") {
			n := strings.TrimSuffix(name, "/")
			if seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, vfs.Entry{Name: n, IsDir: true, Mode: 0o755, ModTime: obj.LastModified})
			continue
		}
		out = append(out, vfs.Entry{
			Name:     name,
			Size:     obj.Size,
			ModTime:  obj.LastModified,
			Mode:     0o644,
			ETag:     strings.Trim(obj.ETag, `"`),
			MimeType: obj.ContentType,
			Owner:    obj.Owner.DisplayName,
			Hidden:   strings.HasPrefix(name, "."),
		})
	}
	return out, nil
}

func mtimeFrom(meta map[string]string, def time.Time) time.Time {
	for k, v := range meta {
		if strings.EqualFold(k, "mtime") || strings.EqualFold(k, "X-Amz-Meta-Mtime") {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				sec := int64(f)
				return time.Unix(sec, int64((f-float64(sec))*1e9))
			}
		}
	}
	return def
}

func (f *FS) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	bucket, key := split(p)
	if bucket == "" {
		return vfs.Entry{Name: "/", IsDir: true}, nil
	}
	if key == "" {
		ok, err := f.c.BucketExists(ctx, bucket)
		if err != nil {
			return vfs.Entry{}, vfs.WrapErr("stat", p, mapErr(err))
		}
		if !ok {
			return vfs.Entry{}, vfs.WrapErr("stat", p, vfs.ErrNotExist)
		}
		return vfs.Entry{Name: bucket, IsDir: true, Mode: 0o755}, nil
	}
	info, err := f.c.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return vfs.Entry{
			Name:     path.Base(key),
			Size:     info.Size,
			ModTime:  mtimeFrom(info.UserMetadata, info.LastModified),
			Mode:     0o644,
			ETag:     strings.Trim(info.ETag, `"`),
			MimeType: info.ContentType,
		}, nil
	}
	// maybe a "directory"
	for obj := range f.c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: key + "/", MaxKeys: 1}) {
		if obj.Err != nil {
			return vfs.Entry{}, vfs.WrapErr("stat", p, mapErr(obj.Err))
		}
		return vfs.Entry{Name: path.Base(key), IsDir: true, Mode: 0o755}, nil
	}
	return vfs.Entry{}, vfs.WrapErr("stat", p, vfs.ErrNotExist)
}

func (f *FS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	bucket, key := split(p)
	if key == "" {
		return nil, vfs.WrapErr("open", p, vfs.ErrIsDir)
	}
	opts := minio.GetObjectOptions{}
	if offset > 0 {
		if err := opts.SetRange(offset, 0); err != nil {
			return nil, err
		}
	}
	obj, err := f.c.GetObject(ctx, bucket, key, opts)
	if err != nil {
		return nil, vfs.WrapErr("download", p, mapErr(err))
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, vfs.WrapErr("download", p, mapErr(err))
	}
	return obj, nil
}

type putWriter struct {
	pw   *io.PipeWriter
	done chan error
}

func (w *putWriter) Write(p []byte) (int, error) { return w.pw.Write(p) }
func (w *putWriter) Close() error {
	w.pw.Close()
	return <-w.done
}

func (f *FS) Create(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	if opt.Offset > 0 {
		return nil, vfs.WrapErr("resume", p, vfs.ErrUnsupported)
	}
	bucket, key := split(p)
	if key == "" {
		return nil, vfs.WrapErr("create", p, vfs.ErrIsDir)
	}
	pr, pw := io.Pipe()
	w := &putWriter{pw: pw, done: make(chan error, 1)}
	po := minio.PutObjectOptions{PartSize: 16 << 20, DisableContentSha256: opt.Size == 0}
	if !opt.ModTime.IsZero() {
		po.UserMetadata = map[string]string{"mtime": strconv.FormatFloat(float64(opt.ModTime.UnixNano())/1e9, 'f', 3, 64)}
	}
	go func() {
		_, err := f.c.PutObject(ctx, bucket, key, pr, opt.Size, po)
		pr.CloseWithError(err)
		w.done <- vfs.WrapErr("upload", p, mapErr(err))
	}()
	return w, nil
}

func (f *FS) Mkdir(ctx context.Context, p string) error {
	bucket, key := split(p)
	if key == "" {
		return vfs.WrapErr("mkdir", p, mapErr(f.c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: f.cfg.Region})))
	}
	// DisableContentSha256 avoids minio-go's broken empty aws-chunked upload.
	_, err := f.c.PutObject(ctx, bucket, key+"/", strings.NewReader(""), 0, minio.PutObjectOptions{DisableContentSha256: true})
	return vfs.WrapErr("mkdir", p, mapErr(err))
}

func (f *FS) Remove(ctx context.Context, p string, isDir bool) error {
	bucket, key := split(p)
	if key == "" {
		return vfs.WrapErr("rmdir", p, mapErr(f.c.RemoveBucket(ctx, bucket)))
	}
	if isDir {
		key += "/"
	}
	return vfs.WrapErr("delete", p, mapErr(f.c.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})))
}

func (f *FS) RemoveAll(ctx context.Context, p string) error {
	bucket, key := split(p)
	prefix := ""
	if key != "" {
		prefix = key + "/"
	}
	ch := make(chan minio.ObjectInfo)
	var listErr error
	go func() {
		defer close(ch)
		for obj := range f.c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if obj.Err != nil {
				listErr = obj.Err
				return
			}
			ch <- obj
		}
	}()
	for e := range f.c.RemoveObjects(ctx, bucket, ch, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return vfs.WrapErr("delete", e.ObjectName, mapErr(e.Err))
		}
	}
	if listErr != nil {
		return vfs.WrapErr("delete", p, mapErr(listErr))
	}
	// Some S3 implementations keep "folder" entries: remove the markers of
	// every remaining sub-prefix, deepest first.
	var prefixes []string
	var collect func(pre string, depth int)
	collect = func(pre string, depth int) {
		if depth > 64 {
			return
		}
		for obj := range f.c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: pre}) {
			if obj.Err != nil {
				return
			}
			if strings.HasSuffix(obj.Key, "/") && obj.Key != pre {
				collect(obj.Key, depth+1)
				prefixes = append(prefixes, obj.Key)
			}
		}
	}
	collect(prefix, 0)
	for _, pre := range prefixes {
		f.c.RemoveObject(ctx, bucket, pre, minio.RemoveObjectOptions{})
	}
	if key == "" {
		return vfs.WrapErr("rmdir", p, mapErr(f.c.RemoveBucket(ctx, bucket)))
	}
	f.c.RemoveObject(ctx, bucket, prefix, minio.RemoveObjectOptions{})
	return nil
}

func (f *FS) copyObject(ctx context.Context, sb, sk, db, dk string) error {
	src := minio.CopySrcOptions{Bucket: sb, Object: sk}
	dst := minio.CopyDestOptions{Bucket: db, Object: dk}
	info, err := f.c.StatObject(ctx, sb, sk, minio.StatObjectOptions{})
	if err == nil && info.Size > 5<<30 {
		_, err = f.c.ComposeObject(ctx, dst, src)
		return mapErr(err)
	}
	_, err = f.c.CopyObject(ctx, dst, src)
	return mapErr(err)
}

func (f *FS) copyTree(ctx context.Context, from, to string, move bool) error {
	sb, sk := split(from)
	db, dk := split(to)
	if sk == "" || dk == "" {
		return vfs.ErrUnsupported
	}
	if _, serr := f.c.StatObject(ctx, sb, sk, minio.StatObjectOptions{}); serr == nil {
		if err := f.copyObject(ctx, sb, sk, db, dk); err != nil {
			return err
		}
		if move {
			return mapErr(f.c.RemoveObject(ctx, sb, sk, minio.RemoveObjectOptions{}))
		}
		return nil
	}
	// directory: copy every object under the prefix
	found := false
	for obj := range f.c.ListObjects(ctx, sb, minio.ListObjectsOptions{Prefix: sk + "/", Recursive: true}) {
		if obj.Err != nil {
			return mapErr(obj.Err)
		}
		found = true
		rel := strings.TrimPrefix(obj.Key, sk+"/")
		if err := f.copyObject(ctx, sb, obj.Key, db, dk+"/"+rel); err != nil {
			return err
		}
		if move {
			if err := f.c.RemoveObject(ctx, sb, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				return mapErr(err)
			}
		}
	}
	if !found {
		return vfs.ErrNotExist
	}
	return nil
}

func (f *FS) Rename(ctx context.Context, from, to string) error {
	return vfs.WrapErr("rename", from, f.copyTree(ctx, from, to, true))
}

func (f *FS) Copy(ctx context.Context, from, to string) error {
	return vfs.WrapErr("copy", from, f.copyTree(ctx, from, to, false))
}

func (f *FS) Chmod(ctx context.Context, p string, mode uint32) error         { return vfs.ErrUnsupported }
func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error { return vfs.ErrUnsupported }

func (f *FS) Checksum(ctx context.Context, p, algo string) (string, error) {
	if !strings.EqualFold(algo, "md5") {
		return "", fmt.Errorf("S3 only exposes MD5 checksums (ETag): %w", vfs.ErrUnsupported)
	}
	bucket, key := split(p)
	info, err := f.c.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return "", mapErr(err)
	}
	etag := strings.Trim(info.ETag, `"`)
	if strings.Contains(etag, "-") {
		return "", errors.New("object was uploaded in parts: ETag is not an MD5")
	}
	return etag, nil
}

func (f *FS) Fork(ctx context.Context) (vfs.FS, error) { return sharedFork{f}, nil }

type sharedFork struct{ *FS }

func (sharedFork) IsSharedFork() bool { return true }
func (sharedFork) Close() error       { return nil }

func (f *FS) Info() []vfs.InfoItem {
	return []vfs.InfoItem{
		{Group: "S3", Key: "Endpoint", Value: f.cfg.Endpoint},
		{Group: "S3", Key: "Region", Value: f.cfg.Region},
		{Group: "S3", Key: "TLS", Value: strconv.FormatBool(f.cfg.Secure)},
		{Group: "S3", Key: "Addressing", Value: map[bool]string{true: "path-style", false: "auto"}[f.cfg.PathStyle]},
	}
}

func (f *FS) Close() error { return nil }
