//go:build windows

package localfs

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"skiff/internal/vfs"
)

func fillOwner(e *vfs.Entry, fi os.FileInfo) {}

func isHidden(name string, fi os.FileInfo) bool {
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
	}
	return false
}

func chown(p, owner, group string) error { return vfs.ErrUnsupported }

func listDrives() []vfs.Entry {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []vfs.Entry
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		name := string(rune('A'+i)) + `:\`
		out = append(out, vfs.Entry{Name: name, IsDir: true, ModTime: time.Time{}})
	}
	return out
}

func diskSpace(p string) (int64, int64, error) {
	var free, total, totalFree uint64
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(ptr, &free, &total, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(total), int64(free), nil
}

func isCrossDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
