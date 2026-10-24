//go:build linux

package scan

import (
	"testing"
)

// A run states the device that holds the root even where it may not enter, so
// the file and byte totals read as a part of what the device allocates
// (architecture/scan.md).
func TestFilesystemOfTheRootsDevice(t *testing.T) {
	fs, err := FilesystemOf(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem: %v", err)
	}
	if fs.Total <= 0 {
		t.Fatalf("total = %d, want the device's size", fs.Total)
	}
	if fs.Used < 0 || fs.Avail < 0 {
		t.Fatalf("usage = %+v, want no negative quantity", fs)
	}
	if fs.Used+fs.Avail > fs.Total {
		t.Fatalf("used %d + avail %d exceeds total %d", fs.Used, fs.Avail, fs.Total)
	}
}
