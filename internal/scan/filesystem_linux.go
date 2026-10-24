//go:build linux

package scan

import (
	"fmt"
	"syscall"
)

// FilesystemOf reports the usage of the filesystem that holds path. It needs no
// read permission on path itself, so a run can state what the device holds even
// where it may not enter (architecture/scan.md).
func FilesystemOf(path string) (Filesystem, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Filesystem{}, fmt.Errorf("scan: filesystem %s: %w", path, err)
	}
	block := int64(st.Bsize)
	return Filesystem{
		Total: int64(st.Blocks) * block,
		Used:  (int64(st.Blocks) - int64(st.Bfree)) * block,
		Avail: int64(st.Bavail) * block,
	}, nil
}
