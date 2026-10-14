//go:build !windows

package localfs

import (
	"errors"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"skiff/internal/vfs"
)

var (
	idMu   sync.Mutex
	users  = map[uint32]string{}
	groups = map[uint32]string{}
)

func lookupUser(uid uint32) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := users[uid]; ok {
		return n
	}
	n := strconv.Itoa(int(uid))
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	users[uid] = n
	return n
}

func lookupGroup(gid uint32) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := groups[gid]; ok {
		return n
	}
	n := strconv.Itoa(int(gid))
	if g, err := user.LookupGroupId(n); err == nil {
		n = g.Name
	}
	groups[gid] = n
	return n
}

func fillOwner(e *vfs.Entry, fi os.FileInfo) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.UID, e.GID = int(st.Uid), int(st.Gid)
		e.Owner = lookupUser(st.Uid)
		e.Group = lookupGroup(st.Gid)
	}
}

func isHidden(name string, fi os.FileInfo) bool { return strings.HasPrefix(name, ".") }

func chown(p, owner, group string) error {
	uid, gid := -1, -1
	if owner != "" {
		if n, err := strconv.Atoi(owner); err == nil {
			uid = n
		} else if u, err := user.Lookup(owner); err == nil {
			uid, _ = strconv.Atoi(u.Uid)
		} else {
			return err
		}
	}
	if group != "" {
		if n, err := strconv.Atoi(group); err == nil {
			gid = n
		} else if g, err := user.LookupGroup(group); err == nil {
			gid, _ = strconv.Atoi(g.Gid)
		} else {
			return err
		}
	}
	return os.Lchown(p, uid, gid)
}

func listDrives() []vfs.Entry { return nil }

func diskSpace(p string) (int64, int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err != nil {
		return 0, 0, err
	}
	bs := int64(st.Bsize)
	return int64(st.Blocks) * bs, int64(st.Bavail) * bs, nil
}

func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
