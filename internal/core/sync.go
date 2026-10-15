package core

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"skiff/internal/mask"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
	"skiff/internal/vfs/localfs"
)

// SyncRequest mirrors WinSCP's "Synchronize" dialog.
type SyncRequest struct {
	LocalConn     string `json:"localConn"` // usually "local"
	LocalDir      string `json:"localDir"`
	RemoteConn    string `json:"remoteConn"`
	RemoteDir     string `json:"remoteDir"`
	Direction     string `json:"direction"` // remote (upload), local (download), both
	Mode          string `json:"mode"`      // standard, mirror, timestamps
	Criteria      string `json:"criteria"`  // time, size, either, checksum, none
	Delete        bool   `json:"delete"`
	ExistingOnly  bool   `json:"existingOnly"`
	FileMask      string `json:"fileMask,omitempty"`
	ExcludeHidden bool   `json:"excludeHidden,omitempty"`
	CaseInsensitive bool `json:"caseInsensitive,omitempty"`
}

type SyncAction struct {
	ID     string     `json:"id"`
	Action string     `json:"action"` // upload, download, deleteLocal, deleteRemote, touchLocal, touchRemote, mkdirLocal, mkdirRemote
	Rel    string     `json:"rel"`
	IsDir  bool       `json:"isDir"`
	Local  *vfs.Entry `json:"local,omitempty"`
	Remote *vfs.Entry `json:"remote,omitempty"`
	Reason string     `json:"reason"` // new, newer, different, orphan, time
}

type SyncPlan struct {
	ID      string       `json:"id"`
	Req     SyncRequest  `json:"req"`
	Actions []SyncAction `json:"actions"`
	Scanned int          `json:"scanned"`
	Errors  []string     `json:"errors,omitempty"`
	Bytes   int64        `json:"bytes"`
}

var plans sync.Map // id -> *SyncPlan

func (c *Core) listAdjusted(ctx context.Context, f vfs.FS, conn, dir string) ([]vfs.Entry, error) {
	es, err := f.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	if conn != LocalID {
		if cn, err := c.Conn(conn); err == nil && cn.site.TimeOffset != 0 {
			off := time.Duration(cn.site.TimeOffset) * time.Minute
			for i := range es {
				es[i].ModTime = es[i].ModTime.Add(off)
			}
		}
	}
	return es, nil
}

// SyncCompare builds a synchronisation plan (preview).
func (c *Core) SyncCompare(ctx context.Context, r SyncRequest) (*SyncPlan, error) {
	if r.LocalConn == "" {
		r.LocalConn = LocalID
	}
	if r.LocalConn == LocalID && c.Local.Root == "" {
		r.LocalDir = localfs.ExpandHome(r.LocalDir)
	}
	if r.Direction == "" {
		r.Direction = "remote"
	}
	if r.Mode == "" {
		r.Mode = "standard"
	}
	if r.Criteria == "" {
		r.Criteria = "time"
	}
	if r.Direction == "both" && r.Delete {
		return nil, errors.New("deleting files is not possible when synchronizing in both directions")
	}
	lf, err := c.FSFor(ctx, r.LocalConn)
	if err != nil {
		return nil, err
	}
	rf, err := c.FSFor(ctx, r.RemoteConn)
	if err != nil {
		return nil, err
	}
	if r.LocalConn == LocalID && runtime.GOOS == "windows" {
		r.CaseInsensitive = true
	}
	m, err := mask.Parse(r.FileMask)
	if err != nil {
		return nil, err
	}
	lc, rc := lf.Caps(), rf.Caps()
	prec := lc.MtimePrecision
	if rc.MtimePrecision > prec {
		prec = rc.MtimePrecision
	}
	if prec < time.Second {
		prec = time.Second
	}
	plan := &SyncPlan{ID: randID(6), Req: r}
	n := 0
	next := func() string { n++; return fmt.Sprintf("a%d", n) }
	add := func(a SyncAction) {
		a.ID = next()
		if (a.Action == "upload") && a.Local != nil {
			plan.Bytes += a.Local.Size
		}
		if (a.Action == "download") && a.Remote != nil {
			plan.Bytes += a.Remote.Size
		}
		plan.Actions = append(plan.Actions, a)
	}
	key := func(name string) string {
		if r.CaseInsensitive {
			return strings.ToLower(name)
		}
		return name
	}
	differ := func(l, rm vfs.Entry, lp, rp string) (bool, int) {
		// returns (different, newerSide: 1 local, -1 remote, 0 unknown)
		lt, rt := l.ModTime.Truncate(prec), rm.ModTime.Truncate(prec)
		side := 0
		if lt.After(rt) {
			side = 1
		} else if rt.After(lt) {
			side = -1
		}
		switch r.Criteria {
		case "none":
			return false, side
		case "size":
			return l.Size != rm.Size, side
		case "either":
			return side != 0 || l.Size != rm.Size, side
		case "checksum":
			if l.Size != rm.Size {
				return true, side
			}
			lcs, ok1 := lf.(vfs.Checksummer)
			rcs, ok2 := rf.(vfs.Checksummer)
			if ok1 && ok2 && lc.Checksum && rc.Checksum {
				algo := "sha256"
				if rc.Protocol == "s3" {
					algo = "md5"
				}
				a, e1 := lcs.Checksum(ctx, lp, algo)
				b, e2 := rcs.Checksum(ctx, rp, algo)
				if e1 == nil && e2 == nil {
					return !strings.EqualFold(a, b), side
				}
			}
			return side != 0, side
		}
		return side != 0, side
	}

	// transferNew walks a one-sided directory adding mkdir + file actions.
	var transferNew func(f vfs.FS, conn, dir, rel string, upload bool, depth int)
	transferNew = func(f vfs.FS, conn, dir, rel string, upload bool, depth int) {
		if depth > 64 {
			return
		}
		es, err := c.listAdjusted(ctx, f, conn, dir)
		if err != nil {
			plan.Errors = append(plan.Errors, err.Error())
			return
		}
		sort.Slice(es, func(i, j int) bool { return es[i].Name < es[j].Name })
		style := f.Caps().PathStyle
		for _, e := range es {
			e := e
			plan.Scanned++
			if r.ExcludeHidden && e.Hidden {
				continue
			}
			erel := rel + "/" + e.Name
			if !m.Match(e.Name, erel, e.IsDir, e.Size, e.ModTime) {
				continue
			}
			a := SyncAction{Rel: strings.TrimPrefix(erel, "/"), IsDir: e.IsDir, Reason: "new"}
			if upload {
				a.Local = &e
			} else {
				a.Remote = &e
			}
			if e.IsDir {
				a.Action = map[bool]string{true: "mkdirRemote", false: "mkdirLocal"}[upload]
				add(a)
				transferNew(f, conn, vfs.Join(style, dir, e.Name), erel, upload, depth+1)
				continue
			}
			a.Action = map[bool]string{true: "upload", false: "download"}[upload]
			add(a)
		}
	}

	var walk func(ldir, rdir, rel string, depth int) error
	walk = func(ldir, rdir, rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 64 {
			return errors.New("directory structure too deep")
		}
		les, err := c.listAdjusted(ctx, lf, r.LocalConn, ldir)
		if err != nil {
			return err
		}
		res, err := c.listAdjusted(ctx, rf, r.RemoteConn, rdir)
		if err != nil {
			return err
		}
		lmap := map[string]vfs.Entry{}
		rmap := map[string]vfs.Entry{}
		var names []string
		for _, e := range les {
			if r.ExcludeHidden && e.Hidden {
				continue
			}
			lmap[key(e.Name)] = e
			names = append(names, key(e.Name))
		}
		for _, e := range res {
			if r.ExcludeHidden && e.Hidden {
				continue
			}
			if _, ok := lmap[key(e.Name)]; !ok {
				names = append(names, key(e.Name))
			}
			rmap[key(e.Name)] = e
		}
		sort.Strings(names)
		for _, k := range names {
			plan.Scanned++
			l, hasL := lmap[k]
			rm, hasR := rmap[k]
			name := l.Name
			if !hasL {
				name = rm.Name
			}
			erel := rel + "/" + name
			isDir := (hasL && l.IsDir) || (hasR && rm.IsDir)
			ref := l
			if !hasL {
				ref = rm
			}
			if !m.Match(name, erel, isDir, ref.Size, ref.ModTime) {
				continue
			}
			lp := vfs.Join(lc.PathStyle, ldir, name)
			rp := vfs.Join(rc.PathStyle, rdir, name)
			relOut := strings.TrimPrefix(erel, "/")
			switch {
			case hasL && hasR:
				if l.IsDir != rm.IsDir {
					plan.Errors = append(plan.Errors, fmt.Sprintf("%s: file on one side, directory on the other", relOut))
					continue
				}
				if l.IsDir {
					if err := walk(lp, rp, erel, depth+1); err != nil {
						plan.Errors = append(plan.Errors, err.Error())
					}
					continue
				}
				lcopy, rcopy := l, rm
				diff, side := differ(l, rm, lp, rp)
				if r.Mode == "timestamps" {
					if l.Size == rm.Size && side != 0 {
						act := "touchRemote"
						if r.Direction == "local" || (r.Direction == "both" && side < 0) {
							act = "touchLocal"
						}
						add(SyncAction{Action: act, Rel: relOut, Local: &lcopy, Remote: &rcopy, Reason: "time"})
					}
					continue
				}
				if !diff {
					continue
				}
				reason := "different"
				switch r.Direction {
				case "remote":
					if r.Mode == "mirror" || side > 0 || (side == 0 && r.Criteria != "time") {
						if side > 0 {
							reason = "newer"
						}
						add(SyncAction{Action: "upload", Rel: relOut, Local: &lcopy, Remote: &rcopy, Reason: reason})
					}
				case "local":
					if r.Mode == "mirror" || side < 0 || (side == 0 && r.Criteria != "time") {
						if side < 0 {
							reason = "newer"
						}
						add(SyncAction{Action: "download", Rel: relOut, Local: &lcopy, Remote: &rcopy, Reason: reason})
					}
				case "both":
					if side > 0 {
						add(SyncAction{Action: "upload", Rel: relOut, Local: &lcopy, Remote: &rcopy, Reason: "newer"})
					} else if side < 0 {
						add(SyncAction{Action: "download", Rel: relOut, Local: &lcopy, Remote: &rcopy, Reason: "newer"})
					} else {
						plan.Errors = append(plan.Errors, fmt.Sprintf("%s: differs but has the same timestamp", relOut))
					}
				}
			case hasL:
				lcopy := l
				switch {
				case r.Direction == "remote" || r.Direction == "both":
					if r.ExistingOnly || r.Mode == "timestamps" {
						continue
					}
					if l.IsDir {
						add(SyncAction{Action: "mkdirRemote", Rel: relOut, IsDir: true, Local: &lcopy, Reason: "new"})
						transferNew(lf, r.LocalConn, lp, erel, true, depth+1)
					} else {
						add(SyncAction{Action: "upload", Rel: relOut, Local: &lcopy, Reason: "new"})
					}
				case r.Direction == "local" && r.Delete:
					add(SyncAction{Action: "deleteLocal", Rel: relOut, IsDir: l.IsDir, Local: &lcopy, Reason: "orphan"})
				}
			case hasR:
				rcopy := rm
				switch {
				case r.Direction == "local" || r.Direction == "both":
					if r.ExistingOnly || r.Mode == "timestamps" {
						continue
					}
					if rm.IsDir {
						add(SyncAction{Action: "mkdirLocal", Rel: relOut, IsDir: true, Remote: &rcopy, Reason: "new"})
						transferNew(rf, r.RemoteConn, rp, erel, false, depth+1)
					} else {
						add(SyncAction{Action: "download", Rel: relOut, Remote: &rcopy, Reason: "new"})
					}
				case r.Direction == "remote" && r.Delete:
					add(SyncAction{Action: "deleteRemote", Rel: relOut, IsDir: rm.IsDir, Remote: &rcopy, Reason: "orphan"})
				}
			}
		}
		return nil
	}
	if err := walk(r.LocalDir, r.RemoteDir, "", 0); err != nil {
		return nil, err
	}
	plans.Store(plan.ID, plan)
	go func(id string) {
		time.Sleep(2 * time.Hour)
		plans.Delete(id)
	}(plan.ID)
	return plan, nil
}

type SyncResult struct {
	Jobs    []string `json:"jobs"`
	Deleted int      `json:"deleted"`
	Touched int      `json:"touched"`
	Errors  []string `json:"errors,omitempty"`
}

// SyncExecute performs the selected actions of a plan (all when ids is nil).
func (c *Core) SyncExecute(ctx context.Context, planID string, ids []string, opts *transfer.Options) (SyncResult, error) {
	v, ok := plans.Load(planID)
	if !ok {
		return SyncResult{}, errors.New("synchronization preview expired, compare again")
	}
	plan := v.(*SyncPlan)
	r := plan.Req
	sel := map[string]bool{}
	for _, id := range ids {
		sel[id] = true
	}
	lf, err := c.FSFor(ctx, r.LocalConn)
	if err != nil {
		return SyncResult{}, err
	}
	rf, err := c.FSFor(ctx, r.RemoteConn)
	if err != nil {
		return SyncResult{}, err
	}
	ls, rs := lf.Caps().PathStyle, rf.Caps().PathStyle
	var res SyncResult
	var up, down []transfer.Pair
	for _, a := range plan.Actions {
		if ids != nil && !sel[a.ID] {
			continue
		}
		lp := vfs.JoinRel(ls, r.LocalDir, a.Rel)
		rp := vfs.JoinRel(rs, r.RemoteDir, a.Rel)
		switch a.Action {
		case "upload":
			up = append(up, transfer.Pair{Src: lp, Dst: rp, Size: a.Local.Size, MTime: a.Local.ModTime, Mode: a.Local.Mode})
		case "download":
			down = append(down, transfer.Pair{Src: rp, Dst: lp, Size: a.Remote.Size, MTime: a.Remote.ModTime, Mode: a.Remote.Mode})
		case "mkdirRemote":
			up = append(up, transfer.Pair{Src: lp, Dst: rp, IsDir: true, MTime: a.Local.ModTime, Mode: a.Local.Mode})
		case "mkdirLocal":
			down = append(down, transfer.Pair{Src: rp, Dst: lp, IsDir: true, MTime: a.Remote.ModTime, Mode: a.Remote.Mode})
		case "deleteLocal":
			if err := vfs.RemoveAll(ctx, lf, lp, a.IsDir); err != nil {
				res.Errors = append(res.Errors, err.Error())
			} else {
				res.Deleted++
			}
		case "deleteRemote":
			if err := vfs.RemoveAll(ctx, rf, rp, a.IsDir); err != nil {
				res.Errors = append(res.Errors, err.Error())
			} else {
				res.Deleted++
			}
		case "touchRemote":
			if err := rf.Chtimes(ctx, rp, a.Local.ModTime); err != nil {
				res.Errors = append(res.Errors, err.Error())
			} else {
				res.Touched++
			}
		case "touchLocal":
			if err := lf.Chtimes(ctx, lp, a.Remote.ModTime); err != nil {
				res.Errors = append(res.Errors, err.Error())
			} else {
				res.Touched++
			}
		}
	}
	var o transfer.Options
	if opts != nil {
		o = *opts
	} else {
		o = c.DefaultTransferOptions(r.LocalConn, r.RemoteConn)
	}
	o.Overwrite = "overwrite"
	o.PreserveTime = true
	o.Move = false
	o.FileMask = ""
	if len(up) > 0 {
		res.Jobs = append(res.Jobs, c.Enqueue(transfer.Request{Src: r.LocalConn, Dst: r.RemoteConn, DstDir: r.RemoteDir, Explicit: up, Options: o, Label: "Synchronize ↑ " + vfs.Base(rs, r.RemoteDir)}))
	}
	if len(down) > 0 {
		res.Jobs = append(res.Jobs, c.Enqueue(transfer.Request{Src: r.RemoteConn, Dst: r.LocalConn, DstDir: r.LocalDir, Explicit: down, Options: o, Label: "Synchronize ↓ " + vfs.Base(ls, r.LocalDir)}))
	}
	if res.Deleted > 0 || res.Touched > 0 {
		c.Changed(r.LocalConn, r.LocalDir)
		c.Changed(r.RemoteConn, r.RemoteDir)
	}
	return res, nil
}
