package transfer

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"skiff/internal/vfs"
)

// Segmented transfers split a big file into parts copied at the same time,
// each on its own connection handle. Over SFTP every part gets its own
// channel, hence its own flow-control window: on a distant server the
// throughput is no longer capped by one window per round trip.

const (
	maxSegments    = 8
	minSegmentSize = 1 << 20
)

// segments returns into how many parts t is copied (1: not split).
func (j *job) segments(t *task, offset int64, dst vfs.FS) int {
	o := j.req.Options
	if o.SegmentSize <= 0 || o.Segments < 2 || t.text || offset > 0 || t.size < o.SegmentSize {
		return 1
	}
	if j.srcCaps.Protocol != "sftp" && j.dstCaps.Protocol != "sftp" {
		return 1
	}
	if !j.srcCaps.ResumeDownload { // the source must open at an offset
		return 1
	}
	if _, ok := dst.(vfs.RandomWriter); !ok {
		return 1
	}
	n := min(o.Segments, maxSegments, int(t.size/minSegmentSize))
	if n < 2 {
		return 1
	}
	return n
}

// copySegments copies t into writeTo in parts. counted receives the bytes
// copied (also on failure, for the rollback of the progress).
func (j *job) copySegments(ctx context.Context, w *worker, t *task, writeTo string, parts int, af *activeFile, counted *int64) error {
	wc, err := w.dst.Create(ctx, writeTo, vfs.CreateOpts{Size: t.size, Mode: j.fileMode(t), ModTime: t.mtime})
	if err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	j.logf("%s: %d parts copied in parallel", t.src, parts)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	size := (t.size + int64(parts) - 1) / int64(parts)
	var wg sync.WaitGroup
	var total atomic.Int64
	errs := make(chan error, parts)
	for i := 0; i < parts; i++ {
		start := int64(i) * size
		end := min(start+size, t.size)
		if start >= end {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := j.copyPart(ctx, w, i, t, writeTo, start, end, af, &total); err != nil {
				errs <- err
				cancel()
			}
		}()
	}
	wg.Wait()
	*counted += total.Load()
	close(errs)
	// the first error is the cause; the others are the cancellation it caused
	return <-errs
}

func (j *job) copyPart(ctx context.Context, w *worker, i int, t *task, writeTo string, start, end int64, af *activeFile, total *atomic.Int64) error {
	src, dst := w.src, w.dst
	if i > 0 {
		s, relS, err := j.m.res.Acquire(ctx, j.req.Src)
		if err != nil {
			return err
		}
		defer relS()
		d, relD, err := j.m.res.Acquire(ctx, j.req.Dst)
		if err != nil {
			return err
		}
		defer relD()
		src, dst = s, d
	}
	rw, ok := dst.(vfs.RandomWriter)
	if !ok {
		return fmt.Errorf("part %d: the destination cannot write at an offset", i+1)
	}
	r, err := src.Open(ctx, t.src, start)
	if err != nil {
		return err
	}
	defer r.Close()
	wa, err := rw.WriteAt(ctx, writeTo, start)
	if err != nil {
		return err
	}
	var n int64
	cr := &countReader{r: io.LimitReader(r, end-start), af: af, j: j, counted: &n, check: j.progress(ctx)}
	copied, cerr := io.Copy(wa, cr)
	werr := wa.Close()
	total.Add(n)
	if cerr == nil {
		cerr = werr
	}
	if cerr == nil && copied != end-start {
		cerr = fmt.Errorf("part %d: %d of %d bytes copied (the file changed?)", i+1, copied, end-start)
	}
	return cerr
}
