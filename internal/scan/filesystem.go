package scan

// Filesystem is the usage of the filesystem that holds the root. It counts
// every file on the device, including the subtrees the walk could not reach,
// so it is the quantity df reports and the run's own totals are a part of it
// (architecture/scan.md).
type Filesystem struct {
	Total int64
	Used  int64
	Avail int64
}
