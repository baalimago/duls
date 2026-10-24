//go:build linux

package scan

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A directory whose device is not the root's holds another filesystem: the walk
// leaves it open, counts it, and keeps walking (architecture/scan.md). The
// device of a temporary directory cannot be chosen, so the walk is told that its
// root lives elsewhere than the tree it reads.
func TestWalkLeavesAnotherFilesystemUnentered(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"mounted/deep"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
	}
	for _, file := range []string{"top.md", "mounted/a.md", "mounted/deep/b.md"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}
	snap, err := newWalk(root, WitnessOf(info).Dev+1, WalkOptions{Workers: 2}, nil).run(context.Background())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if snap.OtherFilesystems != 1 || snap.UnreadableDirs != 0 {
		t.Fatalf("open subtrees = %d other filesystems, %d unreadable dirs; want the mounted directory alone",
			snap.OtherFilesystems, snap.UnreadableDirs)
	}
	if !slices.Equal(snap.Frontier, []string{"mounted"}) {
		t.Fatalf("frontier = %v, want the mounted directory open", snap.Frontier)
	}
	if snap.Stopped != StopNone {
		t.Fatalf("stopped = %v, want a walk that finished", snap.Stopped)
	}
	if len(snap.Dirs) != 1 || snap.Dirs[0].Path != "" {
		t.Fatalf("dirs = %+v, want the root alone", snap.Dirs)
	}
	if snap.Files != 1 || snap.Bytes != 2 {
		t.Fatalf("facts = %d files, %d bytes; want the root's own file alone", snap.Files, snap.Bytes)
	}
	if kids := snap.Dirs[0].Kids; len(kids) != 1 || kids[0].Name != "top.md" {
		t.Fatalf("kids = %+v, want the root's own file alone", kids)
	}
}
