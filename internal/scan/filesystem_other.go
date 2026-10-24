//go:build !linux

package scan

import "fmt"

// FilesystemOf reports the usage of the filesystem that holds path. Only Linux
// can read it, so elsewhere the run states no device usage
// (architecture/scan.md).
func FilesystemOf(path string) (Filesystem, error) {
	return Filesystem{}, fmt.Errorf("scan: filesystem %s: usage needs a platform that reports it", path)
}
