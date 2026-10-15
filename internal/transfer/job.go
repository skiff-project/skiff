package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/time/rate"

	"skiff/internal/mask"
	"skiff/internal/prompt"
	"skiff/internal/vfs"
)

type task struct {
	src   string
	dst   string
	rel   string
	isDir bool
	size  int64
	mtime time.Time
	mode  uint32
	hasMode bool
	text  bool
}

type activeFile struct {
	name string
	size int64
	done atomic.Int64
}

type job struct {
	m   *Manager
	id  string
	req Request

	ctx    context.Context
	cancelFn context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	snap     Snapshot
	held     bool
	dirty    bool
	failed   []Pair
	active   map[*activeFile]bool
	lastBytes int64
	lastTick  time.Time

	doneBytes atomic.Int64

	pauseMu sync.Mutex
	paused  bool
	pauseCh chan struct{}

	promptMu      sync.Mutex
	overwriteAll  string // decision applied to all remaining conflicts
	errorAll      string // "skip" when the user chose skip all

	limiter *rate.Limiter

	dirsMu  sync.Mutex
	dirsMade map[string]bool

	srcCaps, dstCaps vfs.Caps
	fmask    *mask.Mask
	textMask *mask.Mask
}

func newJob(m *Manager, req Request) *job {
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{m: m, id: newID(), req: req, ctx: ctx, cancelFn: cancel, done: make(chan struct{}),
		active: map[*activeFile]bool{}, dirsMade: map[string]bool{}}
	label := req.Label
	if label == "" {
		if len(req.Paths) == 1 {
			label = path.Base(strings.ReplaceAll(req.Paths[0], `\`, "/"))
		} else if len(req.Paths) > 1 {
			label = fmt.Sprintf("%d items", len(req.Paths))
		} else {
			label = fmt.Sprintf("%d files", len(req.Explicit))
		}
	}
	j.snap = Snapshot{
		ID: j.id, Label: label, Src: req.Src, Dst: req.Dst, DstDir: req.DstDir, Move: req.Options.Move,
		Status: Queued, Created: time.Now(), Paths: req.Paths,
		SrcTitle: m.res.Title(req.Src), DstTitle: m.res.Title(req.Dst),
	}
	if req.Options.SpeedLimit > 0 {
		bps := float64(req.Options.SpeedLimit * 1024)
		j.limiter = rate.NewLimiter(rate.Limit(bps), int(max64(int64(bps), 64*1024)))
	}
	return j
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (j *job) status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap.Status
}

func (j *job) setStatus(s Status) {
	j.mu.Lock()
	j.snap.Status = s
	j.dirty = true
	switch s {
	case Running:
		if j.snap.Started.IsZero() {
			j.snap.Started = time.Now()
		}
	case Done, Failed, Cancelled, Partial:
		j.snap.Finished = time.Now()
	}
	j.mu.Unlock()
	j.m.publish(j)
}

func (j *job) snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.snap
	s.DoneBytes = j.doneBytes.Load()
	if j.paused && (s.Status == Running || s.Status == Scanning) {
		s.Status = Paused
	}
	if j.held && s.Status == Queued {
		s.Status = Paused
	}
	s.Active = make([]Active, 0, len(j.active))
	for a := range j.active {
		s.Active = append(s.Active, Active{Name: a.name, Size: a.size, Done: a.done.Load()})
	}
	sort.Slice(s.Active, func(a, b int) bool { return s.Active[a].Name < s.Active[b].Name })
	s.Errors = append([]FileError(nil), s.Errors...)
	return s
}

// tick updates speed/ETA; reports whether something changed.
func (j *job) tick() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.snap.Status
	if st != Running && st != Scanning {
		d := j.dirty
		j.dirty = false
		return d
	}
	now := time.Now()
	done := j.doneBytes.Load()
	if !j.lastTick.IsZero() {
		dt := now.Sub(j.lastTick).Seconds()
		if dt > 0 {
			inst := float64(done-j.lastBytes) / dt
			if j.snap.Speed == 0 {
				j.snap.Speed = inst
			} else {
				j.snap.Speed = 0.7*j.snap.Speed + 0.3*inst
			}
		}
	}
	j.lastTick, j.lastBytes = now, done
	if j.snap.Speed > 1 && j.snap.TotalBytes > done {
		j.snap.ETA = float64(j.snap.TotalBytes-done) / j.snap.Speed
	} else {
		j.snap.ETA = 0
	}
	j.dirty = false
	return true
}

func (j *job) cancel() {
	j.setPaused(false)
	j.cancelFn()
	j.mu.Lock()
	queued := j.snap.Status == Queued
	j.mu.Unlock()
	if queued {
		j.setStatus(Cancelled)
		select {
		case <-j.done:
		default:
			close(j.done)
		}
	}
}

func (j *job) setPaused(p bool) {
	j.pauseMu.Lock()
	defer j.pauseMu.Unlock()
	if p == j.paused {
		return
	}
	j.paused = p
	if p {
		j.pauseCh = make(chan struct{})
	} else if j.pauseCh != nil {
		close(j.pauseCh)
		j.pauseCh = nil
	}
	j.mu.Lock()
	j.dirty = true
	j.mu.Unlock()
}

// waitPaused blocks while the job is paused.
func (j *job) waitPaused() error {
	j.pauseMu.Lock()
	ch := j.pauseCh
	j.pauseMu.Unlock()
	if ch == nil {
		return j.ctx.Err()
	}
	select {
	case <-ch:
		return nil
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
}

func (j *job) addError(p string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.snap.FailedFiles++
	if len(j.snap.Errors) < 200 {
		j.snap.Errors = append(j.snap.Errors, FileError{Path: p, Error: err.Error()})
	}
	j.dirty = true
}

func (j *job) failedPairs() []Pair {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]Pair(nil), j.failed...)
}

func (j *job) logf(format string, a ...any) {
	if j.m.Logf != nil {
		j.m.Logf(format, a...)
	}
}

// ---------------------------------------------------------------------------

func (j *job) run() {
	defer func() {
		select {
		case <-j.done:
		default:
			close(j.done)
		}
	}()
	ctx := j.ctx
	o := &j.req.Options
	var err error
	j.fmask, err = mask.Parse(o.FileMask)
	if err != nil {
		j.fail(fmt.Errorf("invalid file mask: %w", err))
		return
	}
	j.textMask = mask.MustParse(o.TextMask)

	src, relSrc, err := j.m.res.Acquire(ctx, j.req.Src)
	if err != nil {
		j.fail(err)
		return
	}
	dst, relDst, err := j.m.res.Acquire(ctx, j.req.Dst)
	if err != nil {
		relSrc()
		j.fail(err)
		return
	}
	j.srcCaps, j.dstCaps = src.Caps(), dst.Caps()

	tasks, err := j.scan(ctx, src)
	relSrc()
	relDst()
	_ = dst
	if err != nil {
		if ctx.Err() != nil {
			j.setStatus(Cancelled)
			return
		}
		j.fail(err)
		return
	}
	j.setStatus(Running)

	var files []*task
	var dirs []*task
	for _, t := range tasks {
		if t.isDir {
			dirs = append(dirs, t)
		} else {
			files = append(files, t)
		}
	}
	// Create directories first (in order) so that empty ones exist too.
	if len(dirs) > 0 {
		d, rel, err := j.m.res.Acquire(ctx, j.req.Dst)
		if err == nil {
			for _, t := range dirs {
				if ctx.Err() != nil {
					break
				}
				if err := j.ensureDir(ctx, d, t.dst); err != nil {
					j.addError(t.dst, err)
				}
			}
			rel()
		} else {
			j.fail(err)
			return
		}
	}

	par := o.Parallel
	if par <= 0 {
		par = 2
	}
	if par > len(files) {
		par = len(files)
	}
	ch := make(chan *task)
	var wg sync.WaitGroup
	var abort atomic.Bool
	for w := 0; w < par; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wk := &worker{j: j}
			defer wk.release()
			for t := range ch {
				if abort.Load() {
					continue
				}
				if err := j.processFile(ctx, wk, t); err != nil {
					if errors.Is(err, errAbort) {
						abort.Store(true)
						j.cancelFn()
					}
				}
			}
		}()
	}
	for _, t := range files {
		if ctx.Err() != nil || abort.Load() {
			break
		}
		select {
		case ch <- t:
		case <-ctx.Done():
		}
	}
	close(ch)
	wg.Wait()

	// Directory attributes and move cleanup, deepest first.
	if ctx.Err() == nil && len(dirs) > 0 {
		sort.Slice(dirs, func(a, b int) bool { return len(dirs[a].dst) > len(dirs[b].dst) })
		d, rel, err := j.m.res.Acquire(ctx, j.req.Dst)
		if err == nil {
			for _, t := range dirs {
				j.applyAttrs(ctx, d, t.dst, t, true)
			}
			rel()
		}
		if o.Move {
			s, rel, err := j.m.res.Acquire(ctx, j.req.Src)
			if err == nil {
				sort.Slice(dirs, func(a, b int) bool { return len(dirs[a].src) > len(dirs[b].src) })
				for _, t := range dirs {
					if es, err := s.List(ctx, t.src); err == nil && len(es) == 0 {
						s.Remove(ctx, t.src, true)
					}
				}
				rel()
				j.m.res.Changed(j.req.Src, "")
			}
		}
	}
	j.m.res.Changed(j.req.Dst, j.req.DstDir)

	switch {
	case ctx.Err() != nil:
		j.setStatus(Cancelled)
	default:
		j.mu.Lock()
		failed := j.snap.FailedFiles
		skipped := j.snap.SkippedFiles
		j.mu.Unlock()
		if failed > 0 {
			j.setStatus(Partial)
		} else {
			_ = skipped
			j.setStatus(Done)
		}
	}
}

func (j *job) fail(err error) {
	j.mu.Lock()
	j.snap.Error = err.Error()
	j.mu.Unlock()
	j.setStatus(Failed)
}

// scan expands the request into tasks.
func (j *job) scan(ctx context.Context, src vfs.FS) ([]*task, error) {
	o := j.req.Options
	dstStyle := j.dstCaps.PathStyle
	srcStyle := j.srcCaps.PathStyle
	var tasks []*task
	var total int64
	nfiles := 0
	update := func() {
		j.mu.Lock()
		j.snap.TotalBytes, j.snap.TotalFiles = total, nfiles
		j.dirty = true
		j.mu.Unlock()
	}
	add := func(t *task) {
		if !t.isDir {
			t.text = j.isText(path.Base(strings.ReplaceAll(t.src, `\`, "/")))
			total += t.size
			nfiles++
			if nfiles%200 == 0 {
				update()
			}
		}
		tasks = append(tasks, t)
	}

	if len(j.req.Explicit) > 0 {
		for _, p := range j.req.Explicit {
			t := &task{src: p.Src, dst: p.Dst, isDir: p.IsDir, size: p.Size, mtime: p.MTime, mode: p.Mode, hasMode: p.Mode != 0, rel: path.Base(p.Src)}
			if !p.IsDir && p.MTime.IsZero() {
				if e, err := src.Stat(ctx, p.Src); err == nil {
					t.size, t.mtime, t.mode, t.hasMode = e.Size, e.ModTime, e.Mode, e.Mode != 0
				}
			}
			add(t)
		}
		update()
		return tasks, nil
	}

	var walk func(dir, dstDir, rel string, depth int) error
	walk = func(dir, dstDir, rel string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("%s: directory nesting too deep", dir)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := src.List(ctx, dir)
		if err != nil {
			j.addError(dir, err)
			return nil
		}
		sort.Slice(entries, func(a, b int) bool { return entries[a].Name < entries[b].Name })
		for _, e := range entries {
			if o.ExcludeHidden && e.Hidden {
				continue
			}
			r := rel + "/" + e.Name
			if !j.fmask.Match(e.Name, r, e.IsDir, e.Size, e.ModTime) {
				continue
			}
			name := j.targetName(e.Name)
			sp := vfs.Join(srcStyle, dir, e.Name)
			dp := vfs.Join(dstStyle, dstDir, name)
			if e.IsDir {
				add(&task{src: sp, dst: dp, rel: r, isDir: true, mtime: e.ModTime, mode: e.Mode, hasMode: j.srcCaps.Permissions})
				if err := walk(sp, dp, r, depth+1); err != nil {
					return err
				}
				continue
			}
			add(&task{src: sp, dst: dp, rel: r, size: e.Size, mtime: e.ModTime, mode: e.Mode, hasMode: j.srcCaps.Permissions})
		}
		return nil
	}

	for _, p := range j.req.Paths {
		e, err := src.Stat(ctx, p)
		if err != nil {
			j.addError(p, err)
			continue
		}
		name := j.req.Names[p]
		if name == "" {
			name = j.targetName(vfs.Base(srcStyle, p))
		}
		dp := vfs.Join(dstStyle, j.req.DstDir, name)
		if e.IsDir {
			if !j.fmask.Match(e.Name, e.Name, true, 0, e.ModTime) {
				continue
			}
			add(&task{src: p, dst: dp, rel: name, isDir: true, mtime: e.ModTime, mode: e.Mode, hasMode: j.srcCaps.Permissions})
			if err := walk(p, dp, name, 0); err != nil {
				return nil, err
			}
		} else {
			if len(j.req.Paths) > 1 && !j.fmask.Match(e.Name, e.Name, false, e.Size, e.ModTime) {
				continue
			}
			add(&task{src: p, dst: dp, rel: name, size: e.Size, mtime: e.ModTime, mode: e.Mode, hasMode: j.srcCaps.Permissions})
		}
	}
	update()
	return tasks, nil
}

func (j *job) isText(name string) bool {
	switch j.req.Options.Mode {
	case "text", "ascii":
		return true
	case "auto", "automatic":
		return !j.textMask.Empty() && j.textMask.Match(name, name, false, 0, time.Time{})
	}
	return false
}

var invalidWin = strings.NewReplacer(`\`, "%5C", ":", "%3A", "*", "%2A", "?", "%3F", `"`, "%22", "<", "%3C", ">", "%3E", "|", "%7C")

func (j *job) targetName(name string) string {
	o := j.req.Options
	switch o.CaseMode {
	case "lower":
		name = strings.ToLower(name)
	case "upper":
		name = strings.ToUpper(name)
	case "firstUpper":
		if name != "" {
			r := []rune(strings.ToLower(name))
			r[0] = unicode.ToUpper(r[0])
			name = string(r)
		}
	}
	if o.ReplaceInvalid && j.dstCaps.PathStyle == "windows" {
		name = invalidWin.Replace(name)
		name = strings.TrimRight(name, ". ")
	}
	return name
}

func (j *job) ensureDir(ctx context.Context, d vfs.FS, p string) error {
	j.dirsMu.Lock()
	if j.dirsMade[p] {
		j.dirsMu.Unlock()
		return nil
	}
	j.dirsMu.Unlock()
	err := vfs.MkdirAll(ctx, d, p)
	if err == nil {
		j.dirsMu.Lock()
		j.dirsMade[p] = true
		j.dirsMu.Unlock()
	}
	return err
}

func (j *job) applyAttrs(ctx context.Context, d vfs.FS, p string, t *task, isDir bool) {
	o := j.req.Options
	caps := j.dstCaps
	if caps.Chmod {
		var mode uint32
		switch {
		case o.SetPerms && o.Perms != 0:
			mode = o.Perms & 0o7777
			if isDir && o.AddXToDirs {
				mode |= (mode & 0o444) >> 2
			}
		case o.PreservePerms && t.hasMode && t.mode != 0 && j.srcCaps.Permissions:
			mode = t.mode & 0o7777
		}
		if mode != 0 {
			if err := d.Chmod(ctx, p, mode); err != nil && !o.IgnorePermErrors {
				j.logf("chmod %s: %v", p, err)
			}
		}
	}
	if o.PreserveTime && caps.SetMtime && !t.mtime.IsZero() {
		if err := d.Chtimes(ctx, p, t.mtime); err != nil && !errors.Is(err, vfs.ErrUnsupported) {
			j.logf("touch %s: %v", p, err)
		}
	}
}

// ---------------------------------------------------------------------------

var errAbort = errors.New("transfer aborted")
var errSkip = errors.New("skipped")

type worker struct {
	j          *job
	src, dst   vfs.FS
	relS, relD func()
}

func (w *worker) acquire(ctx context.Context) error {
	if w.src == nil {
		s, r, err := w.j.m.res.Acquire(ctx, w.j.req.Src)
		if err != nil {
			return err
		}
		w.src, w.relS = s, r
	}
	if w.dst == nil {
		d, r, err := w.j.m.res.Acquire(ctx, w.j.req.Dst)
		if err != nil {
			return err
		}
		w.dst, w.relD = d, r
	}
	return nil
}

func (w *worker) release() {
	if w.relS != nil {
		w.relS()
	}
	if w.relD != nil {
		w.relD()
	}
	w.src, w.dst, w.relS, w.relD = nil, nil, nil, nil
}

func isNetErr(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	s := strings.ToLower(err.Error())
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		strings.Contains(s, "connection lost") || strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") || strings.Contains(s, "use of closed") || strings.Contains(s, "eof")
}

func (j *job) processFile(ctx context.Context, w *worker, t *task) error {
	o := j.req.Options
	retries := o.RetryCount
	for attempt := 0; ; attempt++ {
		if err := j.waitPaused(); err != nil {
			return err
		}
		if err := w.acquire(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < retries {
				j.sleep(ctx, o.RetryDelay)
				continue
			}
			return j.handleError(ctx, t, err)
		}
		err := j.transferFile(ctx, w, t)
		if err == nil || errors.Is(err, errSkip) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errAbort) || errors.Is(err, prompt.ErrCancelled) {
			return errAbort
		}
		if isNetErr(err) && attempt < retries {
			j.logf("%s: %v — retrying (%d/%d)", t.src, err, attempt+1, retries)
			w.release()
			j.sleep(ctx, o.RetryDelay)
			continue
		}
		return j.handleError(ctx, t, err)
	}
}

func (j *job) sleep(ctx context.Context, sec int) {
	if sec <= 0 {
		sec = 1
	}
	select {
	case <-time.After(time.Duration(sec) * time.Second):
	case <-ctx.Done():
	}
}

func (j *job) handleError(ctx context.Context, t *task, err error) error {
	j.mu.Lock()
	j.failed = append(j.failed, Pair{Src: t.src, Dst: t.dst, Size: t.size, MTime: t.mtime, Mode: t.mode})
	j.mu.Unlock()
	j.addError(t.src, err)
	if j.req.Options.ContinueOnError || j.m.pr == nil {
		return nil
	}
	j.promptMu.Lock()
	defer j.promptMu.Unlock()
	if j.errorAll == "skip" {
		return nil
	}
	resp, perr := j.m.pr.Prompt(ctx, prompt.Request{
		Kind:    prompt.Error,
		Title:   "Transfer error",
		Message: err.Error(),
		Details: map[string]string{"file": t.src, "conn": j.req.Src},
		Choices: []prompt.Choice{
			{ID: "skip", Label: "Skip", Primary: true},
			{ID: "skipall", Label: "Skip all"},
			{ID: "abort", Label: "Abort", Danger: true},
		},
	})
	if perr != nil {
		return nil
	}
	switch resp.Choice {
	case "skipall":
		j.errorAll = "skip"
	case "abort":
		return errAbort
	}
	return nil
}

func (j *job) markSkipped(t *task) {
	j.doneBytes.Add(t.size)
	j.mu.Lock()
	j.snap.SkippedFiles++
	j.snap.DoneFiles++
	j.dirty = true
	j.mu.Unlock()
}

// decide resolves a conflict with an existing target.
func (j *job) decide(ctx context.Context, t *task, existing vfs.Entry, canResume bool) (string, error) {
	o := j.req.Options
	prec := j.srcCaps.MtimePrecision
	if j.dstCaps.MtimePrecision > prec {
		prec = j.dstCaps.MtimePrecision
	}
	if prec < time.Second {
		prec = time.Second
	}
	srcNewer := t.mtime.Truncate(prec).After(existing.ModTime.Truncate(prec))
	if o.NewerOnly && !srcNewer {
		return "skip", nil
	}
	apply := func(policy string) (string, bool) {
		switch policy {
		case "overwrite", "skip", "rename":
			return policy, true
		case "newer":
			if srcNewer {
				return "overwrite", true
			}
			return "skip", true
		case "differentSize":
			if existing.Size != t.size {
				return "overwrite", true
			}
			return "skip", true
		case "resume":
			if canResume && existing.Size < t.size {
				return "resume", true
			}
			return "overwrite", true
		}
		return "", false
	}
	j.promptMu.Lock()
	all := j.overwriteAll
	j.promptMu.Unlock()
	policy := o.Overwrite
	if all != "" {
		policy = all
	}
	if d, ok := apply(policy); ok {
		return d, nil
	}
	if j.m.pr == nil {
		return "overwrite", nil
	}
	j.promptMu.Lock()
	defer j.promptMu.Unlock()
	if j.overwriteAll != "" {
		if d, ok := apply(j.overwriteAll); ok {
			return d, nil
		}
	}
	choices := []prompt.Choice{
		{ID: "overwrite", Label: "Overwrite", Primary: true},
		{ID: "newer", Label: "Overwrite if newer"},
		{ID: "skip", Label: "Skip"},
	}
	if canResume && existing.Size < t.size {
		choices = append(choices, prompt.Choice{ID: "resume", Label: "Resume"})
	}
	choices = append(choices, prompt.Choice{ID: "rename", Label: "Keep both"}, prompt.Choice{ID: "cancel", Label: "Cancel", Danger: true})
	resp, err := j.m.pr.Prompt(ctx, prompt.Request{
		Kind:    prompt.Overwrite,
		Title:   "File already exists",
		Message: existing.Name,
		Details: map[string]string{
			"target":   t.dst,
			"srcSize":  fmt.Sprint(t.size),
			"srcTime":  t.mtime.Format(time.RFC3339),
			"dstSize":  fmt.Sprint(existing.Size),
			"dstTime":  existing.ModTime.Format(time.RFC3339),
			"srcNewer": fmt.Sprint(srcNewer),
		},
		Choices:  choices,
		ApplyAll: true,
	})
	if err != nil {
		return "", err
	}
	choice := resp.Choice
	if choice == "cancel" || choice == "" {
		return "", errAbort
	}
	if resp.ApplyAll {
		j.overwriteAll = choice
	}
	if d, ok := apply(choice); ok {
		return d, nil
	}
	return choice, nil
}

func uniqueName(ctx context.Context, d vfs.FS, style, p string) string {
	dir, base := vfs.Dir(style, p), vfs.Base(style, p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		cand := vfs.Join(style, dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := d.Stat(ctx, cand); vfs.IsNotExist(err) {
			return cand
		}
	}
	return p
}

func (j *job) transferFile(ctx context.Context, w *worker, t *task) error {
	o := j.req.Options
	src, dst := w.src, w.dst
	dstStyle := j.dstCaps.PathStyle
	target := t.dst

	if err := j.ensureDir(ctx, dst, vfs.Dir(dstStyle, target)); err != nil {
		return err
	}

	canResume := j.srcCaps.ResumeDownload && j.dstCaps.ResumeUpload && !t.text
	var offset int64
	existing, statErr := dst.Stat(ctx, target)
	if statErr == nil {
		if existing.IsDir {
			return fmt.Errorf("%s: target is a directory", target)
		}
		decision, err := j.decide(ctx, t, existing, canResume)
		if err != nil {
			return err
		}
		switch decision {
		case "skip":
			j.markSkipped(t)
			return errSkip
		case "rename":
			target = uniqueName(ctx, dst, dstStyle, target)
		case "resume":
			offset = existing.Size
		}
	}

	useTemp := offset == 0 && o.ResumeThreshold >= 0 && t.size >= o.ResumeThreshold &&
		j.dstCaps.Protocol != "s3" && !t.text
	writeTo := target
	if useTemp {
		writeTo = target + ".filepart"
		if canResume {
			if st, err := dst.Stat(ctx, writeTo); err == nil && !st.IsDir && st.Size > 0 && st.Size < t.size {
				offset = st.Size
				j.logf("Resuming %s at %d bytes", t.src, offset)
			}
		}
	}

	af := &activeFile{name: path.Base(strings.ReplaceAll(t.src, `\`, "/")), size: t.size}
	af.done.Store(offset)
	j.doneBytes.Add(offset)
	j.mu.Lock()
	j.active[af] = true
	j.dirty = true
	j.mu.Unlock()
	counted := offset
	defer func() {
		j.mu.Lock()
		delete(j.active, af)
		j.dirty = true
		j.mu.Unlock()
	}()
	rollback := func() {
		j.doneBytes.Add(-counted)
		counted = 0
	}

	r, err := src.Open(ctx, t.src, offset)
	if err != nil {
		rollback()
		return err
	}
	size := t.size
	if t.text {
		size = -1
	}
	mode := uint32(0)
	if o.SetPerms && o.Perms != 0 {
		mode = o.Perms
	} else if o.PreservePerms && t.hasMode {
		mode = t.mode & 0o777
	}
	wc, err := dst.Create(ctx, writeTo, vfs.CreateOpts{Offset: offset, Size: size, Mode: mode, ModTime: t.mtime})
	if err != nil {
		r.Close()
		rollback()
		return err
	}

	progress := func(n int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.waitPaused(); err != nil {
			return err
		}
		if j.limiter != nil && n > 0 {
			for n > 0 {
				c := n
				if c > j.limiter.Burst() {
					c = j.limiter.Burst()
				}
				if err := j.limiter.WaitN(ctx, c); err != nil {
					return err
				}
				n -= c
			}
		}
		return nil
	}
	var reader io.Reader = &countReader{r: r, af: af, j: j, counted: &counted, check: progress}
	if t.text {
		reader = newEOLReader(reader, o.SrcEOL, o.DstEOL)
	}
	_, cerr := io.Copy(wc, reader)
	werr := wc.Close()
	r.Close()
	if cerr == nil {
		cerr = werr
	}
	if cerr != nil {
		rollback()
		if ctx.Err() != nil && useTemp {
			// keep .filepart for a later resume
		}
		return cerr
	}
	// account for size differences (text mode, file changed while copying)
	if counted != t.size {
		j.doneBytes.Add(t.size - counted)
	}

	if writeTo != target {
		if err := dst.Rename(ctx, writeTo, target); err != nil {
			if rerr := dst.Remove(ctx, target, false); rerr == nil {
				err = dst.Rename(ctx, writeTo, target)
			}
			if err != nil {
				return fmt.Errorf("rename %s: %w", writeTo, err)
			}
		}
	}
	j.applyAttrs(ctx, dst, target, t, false)

	if o.Verify && !t.text {
		if err := j.verify(ctx, src, dst, t.src, target); err != nil {
			return err
		}
	}
	if o.Move {
		if err := src.Remove(ctx, t.src, false); err != nil {
			return fmt.Errorf("delete source: %w", err)
		}
	}
	j.mu.Lock()
	j.snap.DoneFiles++
	j.dirty = true
	j.mu.Unlock()
	return nil
}

func (j *job) verify(ctx context.Context, src, dst vfs.FS, sp, dp string) error {
	sc, ok1 := src.(vfs.Checksummer)
	dc, ok2 := dst.(vfs.Checksummer)
	if !ok1 || !ok2 || !j.srcCaps.Checksum || !j.dstCaps.Checksum {
		return nil
	}
	algo := "sha256"
	if j.srcCaps.Protocol == "s3" || j.dstCaps.Protocol == "s3" {
		algo = "md5"
	}
	a, err := sc.Checksum(ctx, sp, algo)
	if err != nil {
		j.logf("verify %s: %v", sp, err)
		return nil
	}
	b, err := dc.Checksum(ctx, dp, algo)
	if err != nil {
		j.logf("verify %s: %v", dp, err)
		return nil
	}
	if !strings.EqualFold(a, b) {
		return fmt.Errorf("checksum mismatch after transfer (%s %s ≠ %s)", algo, a, b)
	}
	return nil
}

type countReader struct {
	r       io.Reader
	af      *activeFile
	j       *job
	counted *int64
	check   func(n int) error
}

func (c *countReader) Read(p []byte) (int, error) {
	if len(p) > 256*1024 {
		p = p[:256*1024]
	}
	if err := c.check(0); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.af.done.Add(int64(n))
		c.j.doneBytes.Add(int64(n))
		*c.counted += int64(n)
		if cerr := c.check(n); cerr != nil && err == nil {
			err = cerr
		}
	}
	return n, err
}

// eolReader converts line endings on the fly.
type eolReader struct {
	r        io.Reader
	toCRLF   bool
	buf      []byte
	pending  []byte
	lastCR   bool
}

func newEOLReader(r io.Reader, from, to string) io.Reader {
	if strings.EqualFold(from, to) || to == "" {
		return r
	}
	return &eolReader{r: r, toCRLF: strings.EqualFold(to, "CRLF"), buf: make([]byte, 64*1024)}
}

func (e *eolReader) Read(p []byte) (int, error) {
	for len(e.pending) == 0 {
		n, err := e.r.Read(e.buf)
		if n > 0 {
			out := make([]byte, 0, n+n/16)
			for _, b := range e.buf[:n] {
				if e.toCRLF {
					if b == '\n' && !e.lastCR {
						out = append(out, '\r', '\n')
					} else {
						out = append(out, b)
					}
					e.lastCR = b == '\r'
				} else {
					if e.lastCR && b != '\n' {
						out = append(out, '\r')
					}
					if b == '\r' {
						e.lastCR = true
						continue
					}
					e.lastCR = false
					out = append(out, b)
				}
			}
			e.pending = out
		}
		if err != nil {
			if err == io.EOF && !e.toCRLF && e.lastCR {
				e.pending = append(e.pending, '\r')
				e.lastCR = false
			}
			if len(e.pending) == 0 {
				return 0, err
			}
			break
		}
	}
	n := copy(p, e.pending)
	e.pending = e.pending[n:]
	return n, nil
}

