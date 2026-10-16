package core

import (
	"skiff/internal/transfer"
)

// DefaultTransferOptions builds options from the preferences.
func (c *Core) DefaultTransferOptions(src, dst string) transfer.Options {
	p := c.Store.Prefs()
	t := p.Transfer
	o := transfer.Options{
		Mode: t.Mode, TextMask: t.TextMask, PreserveTime: t.PreserveTime, PreservePerms: t.PreservePerms,
		SetPerms: t.SetPerms, Perms: t.Perms, AddXToDirs: t.AddXToDirs, Overwrite: t.Overwrite,
		ResumeThreshold: t.ResumeThreshold, FileMask: t.FileMask, SpeedLimit: t.SpeedLimit, CaseMode: t.CaseMode,
		Verify: t.Verify, ExcludeHidden: t.ExcludeHidden, ReplaceInvalid: t.ReplaceInvalid,
		IgnorePermErrors: t.IgnorePermErrors, RetryCount: p.Queue.RetryCount, RetryDelay: p.Queue.RetryDelay,
		ContinueOnError: p.Queue.ContinueOnError, SegmentSize: p.Queue.SegmentSize, Segments: p.Queue.Segments,
	}
	if !p.ConfirmOverwrite && o.Overwrite == "ask" {
		o.Overwrite = "overwrite"
	}
	o.Parallel = 2
	for _, id := range []string{src, dst} {
		if cn, err := c.Conn(id); err == nil && cn.site.Parallel > 0 {
			o.Parallel = cn.site.Parallel
		}
	}
	return o
}

func transferRequest(src, dst string, paths []string, dstDir string, names map[string]string, o transfer.Options) transfer.Request {
	return transfer.Request{Src: src, Dst: dst, Paths: paths, DstDir: dstDir, Names: names, Options: o}
}

// Enqueue normalises a request (EOLs) and queues it.
func (c *Core) Enqueue(req transfer.Request) string {
	req.Options.SrcEOL = c.EOL(req.Src)
	req.Options.DstEOL = c.EOL(req.Dst)
	if req.Options.Parallel <= 0 {
		req.Options.Parallel = 2
	}
	return c.Transfers.Enqueue(req)
}
