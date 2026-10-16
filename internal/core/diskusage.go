package core

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"skiff/internal/vfs"
)

// What takes room in a folder: the size of each of its items, counted by
// du on a server with a shell (fast), else by going through the files.

// UsageEntry is an item of a folder with the room it takes.
type UsageEntry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"` // bytes (on disk, with du)
	IsDir bool   `json:"isDir"`
}

// DiskUsage is the room taken in a folder, and on its disk.
type DiskUsage struct {
	Dir     string       `json:"dir"`
	Total   int64        `json:"total"`
	Entries []UsageEntry `json:"entries"`
	// the file system of the folder (df), when it is known
	DiskSize int64  `json:"diskSize,omitempty"`
	DiskUsed int64  `json:"diskUsed,omitempty"`
	DiskFree int64  `json:"diskFree,omitempty"`
	Mount    string `json:"mount,omitempty"`
	// Method is "du" (on the server) or "scan" (files gone through here)
	Method string `json:"method"`
	// Partial: the scan stopped before the end (too many files or too long)
	Partial bool `json:"partial,omitempty"`
	// Denied: folders that could not be read (sizes are then lower)
	Denied int `json:"denied,omitempty"`
}

const (
	usageMaxEntries = 300000
	usageMaxTime    = 2 * time.Minute
)

// DiskUsage measures the items of a folder.
func (c *Core) DiskUsage(ctx context.Context, conn, dir string) (DiskUsage, error) {
	if dir == "" {
		return DiskUsage{}, errors.New("no folder")
	}
	if cn, ok := c.hasShell(conn); ok {
		d := vfs.ShellQuote(dir)
		// -x: this file system only; -k: kilobytes; files and folders of the first level
		cmd := "cd " + d + " || exit 7; du -xak -d 1 . 2>&1; echo SKIFF:DIRS; find . -mindepth 1 -maxdepth 1 -type d 2>/dev/null; " +
			"echo SKIFF:DF; df -kP . 2>/dev/null | tail -n 1; true"
		out, err := cn.sshOutput(ctx, cmd)
		if err == nil && strings.Contains(out, "SKIFF:DF") {
			u := parseDu(out)
			u.Dir = dir
			return u, nil
		}
		if err != nil && strings.Contains(err.Error(), "status 7") {
			return DiskUsage{}, err // no such folder, or not readable
		}
	}
	return c.scanUsage(ctx, conn, dir)
}

func parseDu(out string) DiskUsage {
	u := DiskUsage{Method: "du", Entries: []UsageEntry{}}
	du, rest, _ := strings.Cut(out, "SKIFF:DIRS")
	dirs, df, _ := strings.Cut(rest, "SKIFF:DF")
	isDir := map[string]bool{}
	for _, ln := range strings.Split(dirs, "\n") {
		if n := strings.TrimPrefix(strings.TrimSpace(ln), "./"); n != "" {
			isDir[n] = true
		}
	}
	for _, ln := range strings.Split(du, "\n") {
		if strings.HasPrefix(ln, "du:") {
			u.Denied++ // "du: cannot read directory …: Permission denied"
			continue
		}
		size, name, ok := strings.Cut(ln, "\t")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
		if err != nil {
			continue
		}
		if name == "." {
			u.Total = kb * 1024
			continue
		}
		name = strings.TrimPrefix(name, "./")
		if strings.Contains(name, "/") {
			continue // deeper (some du ignore -d with -a)
		}
		u.Entries = append(u.Entries, UsageEntry{Name: name, Size: kb * 1024, IsDir: isDir[name]})
	}
	// df -kP: Filesystem 1024-blocks Used Available Capacity Mounted-on
	if f := strings.Fields(df); len(f) >= 6 {
		size, _ := strconv.ParseInt(f[1], 10, 64)
		used, _ := strconv.ParseInt(f[2], 10, 64)
		free, _ := strconv.ParseInt(f[3], 10, 64)
		u.DiskSize, u.DiskUsed, u.DiskFree, u.Mount = size*1024, used*1024, free*1024, strings.Join(f[5:], " ")
	}
	sortUsage(u.Entries)
	return u
}

func sortUsage(es []UsageEntry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].Size != es[j].Size {
			return es[i].Size > es[j].Size
		}
		return es[i].Name < es[j].Name
	})
}

// scanUsage adds up the sizes of the files, through the file system of the
// connection; whether an item is a folder comes from the listing.
func (c *Core) scanUsage(ctx context.Context, conn, dir string) (DiskUsage, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return DiskUsage{}, err
	}
	top, err := f.List(ctx, dir)
	if err != nil {
		return DiskUsage{}, err
	}
	style := f.Caps().PathStyle
	u := DiskUsage{Dir: dir, Method: "scan", Entries: []UsageEntry{}}
	ctx, cancel := context.WithTimeout(ctx, usageMaxTime)
	defer cancel()
	seen := 0
	var size func(p string) int64
	size = func(p string) int64 {
		if ctx.Err() != nil || seen > usageMaxEntries {
			u.Partial = true
			return 0
		}
		list, err := f.List(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				u.Partial = true
			} else {
				u.Denied++
			}
			return 0
		}
		var n int64
		for _, e := range list {
			seen++
			if e.IsDir && !e.IsLink {
				n += size(vfs.Join(style, p, e.Name))
			} else if !e.IsLink {
				n += e.Size
			}
		}
		return n
	}
	for _, e := range top {
		ue := UsageEntry{Name: e.Name, IsDir: e.IsDir && !e.IsLink}
		if ue.IsDir {
			ue.Size = size(vfs.Join(style, dir, e.Name))
		} else if !e.IsLink {
			ue.Size = e.Size
		}
		u.Total += ue.Size
		u.Entries = append(u.Entries, ue)
	}
	sortUsage(u.Entries)
	return u, nil
}
