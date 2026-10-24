package scan_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/baalimago/duls/internal/scan"
)

func TestWalkIsScheduleIndependent(t *testing.T) {
	root := tree(t, 20, 10)
	var snaps []scan.Snapshot
	for _, workers := range []int{1, 4, 16} {
		snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: workers}, nil)
		if err != nil {
			t.Fatalf("walk with %d workers: %v", workers, err)
		}
		snaps = append(snaps, snap)
	}
	for i := 1; i < len(snaps); i++ {
		if !reflect.DeepEqual(snaps[0], snaps[i]) {
			t.Fatalf("snapshot for %d workers differs from one worker", []int{1, 4, 16}[i])
		}
	}
	if snaps[0].Files != 200 || snaps[0].Bytes == 0 {
		t.Fatalf("facts = %d files, %d bytes; want 200 files and a byte total", snaps[0].Files, snaps[0].Bytes)
	}
}

func TestDirsAreInDirSeqOrder(t *testing.T) {
	root := t.TempDir()
	file(t, root, "a/one", 10)
	file(t, root, "b/one", 30)
	file(t, root, "c/one", 30)
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 2}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var paths []string
	for _, dir := range snap.Dirs {
		paths = append(paths, dir.Path)
	}
	want := []string{"", "b", "c", "a"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Dirs paths = %v, want %v", paths, want)
	}
}

func TestWalkNeverOpensAFile(t *testing.T) {
	root := t.TempDir()
	path := file(t, root, "big", 1<<20)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 1}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if snap.Files != 1 || snap.Bytes != 1<<20 {
		t.Fatalf("facts = %d files, %d bytes; want 1 file and 1 MiB", snap.Files, snap.Bytes)
	}
}

func TestDeadlineReturnsSnapshot(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		file(t, root, name+"/one", 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	sink := &sleepSink{delay: 100 * time.Millisecond}
	snap, err := scan.New().Walk(ctx, root, scan.WalkOptions{Workers: 1}, sink)
	if err != nil {
		t.Fatalf("walk after deadline: %v", err)
	}
	if snap.Stopped != scan.StopDeadline {
		t.Fatalf("Stopped = %v, want StopDeadline", snap.Stopped)
	}
	if len(snap.Frontier) == 0 {
		t.Fatal("no frontier identifies the unfinished subtrees")
	}
	for _, path := range snap.Frontier {
		if path != "a" && path != "b" && path != "c" {
			t.Fatalf("frontier holds %q, want one of the unlisted directories", path)
		}
	}
}

func TestCancelReturnsError(t *testing.T) {
	root := tree(t, 2, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snap, err := scan.New().Walk(ctx, root, scan.WalkOptions{Workers: 2}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want a wrapped context.Canceled", err)
	}
	if !reflect.DeepEqual(snap, scan.Snapshot{}) {
		t.Fatalf("snapshot = %+v, want the zero snapshot", snap)
	}
}

func TestPermissionPolicy(t *testing.T) {
	t.Run("unreadable directory", func(t *testing.T) {
		root := t.TempDir()
		file(t, root, "ok", 3)
		file(t, root, "locked/hidden", 1)
		locked := filepath.Join(root, "locked")
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 1}, nil)
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if snap.Files != 1 || snap.UnreadableDirs != 1 {
			t.Fatalf("facts = %d files, %d unreadable dirs; want 1 file and the refused directory", snap.Files, snap.UnreadableDirs)
		}
		if snap.Stopped != scan.StopNone {
			t.Fatalf("Stopped = %v, want a finished walk", snap.Stopped)
		}
		if !slices.Contains(snap.Frontier, "locked") {
			t.Fatalf("frontier = %v, want the refused subtree", snap.Frontier)
		}
		for _, dir := range snap.Dirs {
			if dir.Path == "locked" {
				t.Fatalf("Dirs holds the refused directory: %+v", dir)
			}
		}
	})
	t.Run("permission refused file", func(t *testing.T) {
		root := t.TempDir()
		file(t, root, "one", 3)
		if err := os.Chmod(root, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
		snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 1}, nil)
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if snap.Files != 1 || snap.Unreadable != 1 {
			t.Fatalf("facts = %d files, %d unreadable; want 1 file counted as unreadable", snap.Files, snap.Unreadable)
		}
	})
}

func TestRootErrors(t *testing.T) {
	walker := scan.New()
	if _, err := walker.Walk(context.Background(), filepath.Join(t.TempDir(), "missing"), scan.WalkOptions{}, nil); err == nil {
		t.Fatal("a missing root is not an error")
	}
	path := file(t, t.TempDir(), "file", 1)
	if _, err := walker.Walk(context.Background(), path, scan.WalkOptions{}, nil); err == nil {
		t.Fatal("a non-directory root is not an error")
	}
}

// TestChildStatFailure proves one child the walk cannot inspect ends the walk:
// the first failure cancels the remaining workers and no snapshot is returned
// (Walk.7, Walk.10).
func TestChildStatFailure(t *testing.T) {
	root := tree(t, 60, 1)
	if err := os.Chmod(root, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 4}, nil)
	if err == nil || !strings.Contains(err.Error(), root) || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want a permission error naming a child of %s", err, root)
	}
	if !reflect.DeepEqual(snap, scan.Snapshot{}) {
		t.Fatalf("snapshot = %+v, want none", snap)
	}
}

func TestWalkJoinsWorkers(t *testing.T) {
	baseline := runtime.NumGoroutine()
	success := tree(t, 3, 3)
	if _, err := scan.New().Walk(context.Background(), success, scan.WalkOptions{Workers: 4}, nil); err != nil {
		t.Fatal(err)
	}
	waitForGoroutines(t, baseline)

	big := tree(t, 40, 5)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := scan.New().Walk(ctx, big, scan.WalkOptions{Workers: 4}, nil); err != nil {
		t.Fatal(err)
	}
	waitForGoroutines(t, baseline)

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond)
		cancel()
	}()
	_, _ = scan.New().Walk(ctx, big, scan.WalkOptions{Workers: 4}, nil)
	waitForGoroutines(t, baseline)

	broken := t.TempDir()
	locked := filepath.Join(broken, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, _ = scan.New().Walk(context.Background(), broken, scan.WalkOptions{Workers: 4}, nil)
	waitForGoroutines(t, baseline)
}

func TestKidsAreBounded(t *testing.T) {
	root := t.TempDir()
	sizes := map[string]int{"f01": 10, "f02": 10, "f03": 9, "f04": 8, "f05": 7, "f06": 6, "f07": 5, "f08": 4, "f09": 3, "f10": 2}
	for name, size := range sizes {
		file(t, root, name, size)
	}
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	kids := snap.Dirs[0].Kids
	if len(kids) != 6 {
		t.Fatalf("Kids holds %d children, want 6", len(kids))
	}
	want := []string{"f01", "f02", "f03", "f04", "f05", "f06"}
	var got []string
	for _, kid := range kids {
		got = append(got, kid.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Kids = %v, want %v", got, want)
	}
}

func TestPresetBounds(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	for _, tt := range []struct {
		mode    scan.Mode
		workers int
		buffer  int
		nice    int
	}{
		{scan.ModeOwl, 1, 64 << 10, 10},
		{scan.ModeDefault, 4, 256 << 10, 5},
		{scan.ModeDrone, procs, 1 << 20, 0},
		{scan.ModeHelicopter, 4 * procs, 4 << 20, 0},
	} {
		opts, ok := scan.Preset(tt.mode)
		if !ok {
			t.Fatalf("mode %d rejected", tt.mode)
		}
		if opts.Workers != tt.workers || opts.Buffer != tt.buffer || opts.Nice != tt.nice {
			t.Fatalf("mode %d = %+v, want %d workers, %d buffer, nice %d", tt.mode, opts, tt.workers, tt.buffer, tt.nice)
		}
		if opts.All {
			t.Fatalf("mode %d carries the All field: %+v", tt.mode, opts)
		}
	}
	if _, ok := scan.Preset(scan.Mode(4)); ok {
		t.Fatal("mode 4 accepted")
	}
}

func TestProgressContract(t *testing.T) {
	root := t.TempDir()
	for i := range 5000 {
		file(t, root, fmt.Sprintf("f%05d", i), 1)
	}
	sink := &recordingSink{}
	if _, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 1}, sink); err != nil {
		t.Fatal(err)
	}
	got := sink.updates()
	if len(got) < 2 {
		t.Fatalf("walk published %d updates, want at least one stride and one final", len(got))
	}
	for i, update := range got {
		if update.Phase != scan.PhaseWalk {
			t.Fatalf("update %d phase = %v, want PhaseWalk", i, update.Phase)
		}
		if update.Lines != 0 {
			t.Fatalf("update %d carries %d walk lines, want 0", i, update.Lines)
		}
		if i == 0 {
			continue
		}
		if update.Files < got[i-1].Files || update.Bytes < got[i-1].Bytes || update.Dirs < got[i-1].Dirs {
			t.Fatalf("update %d is not monotone: %+v after %+v", i, update, got[i-1])
		}
	}

	drop := &droppingSink{ch: make(chan scan.Progress, 1)}
	if _, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 1}, drop); err != nil {
		t.Fatalf("walk with a dropping sink: %v", err)
	}
	if len(drop.ch) == 0 {
		t.Fatal("the dropping sink received no update")
	}
}

func TestWalkEntryPolicy(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := file(t, base, "target", 4096)
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	file(t, root, "visible", 3)
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	walker := scan.New()
	plain, err := walker.Walk(context.Background(), root, scan.WalkOptions{Workers: 1}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if plain.Files != 1 || plain.Bytes != 3 {
		t.Fatalf("plain facts = %d files, %d bytes; want only the visible file", plain.Files, plain.Bytes)
	}
	all, err := walker.Walk(context.Background(), root, scan.WalkOptions{Workers: 1, All: true}, nil)
	if err != nil {
		t.Fatalf("walk with All: %v", err)
	}
	if all.Files != 2 || all.Bytes != 9 {
		t.Fatalf("all facts = %d files, %d bytes; want the visible and hidden files", all.Files, all.Bytes)
	}
}

func TestRootSymlinkIsResolved(t *testing.T) {
	root := t.TempDir()
	file(t, root, "one", 5)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	snap, err := scan.New().Walk(context.Background(), link, scan.WalkOptions{Workers: 1}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Root != resolved {
		t.Fatalf("Snapshot.Root = %q, want the resolved root %q", snap.Root, resolved)
	}
	if snap.Files != 1 {
		t.Fatalf("files = %d, want 1", snap.Files)
	}
}

func tree(t *testing.T, dirs, files int) string {
	t.Helper()
	root := t.TempDir()
	for i := range dirs {
		for j := range files {
			file(t, root, fmt.Sprintf("d%02d/f%02d", i, j), 1+i+j)
		}
	}
	return root
}

func file(t *testing.T, root, rel string, size int) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForGoroutines(t *testing.T, baseline int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("goroutines = %d, want at most the baseline %d", runtime.NumGoroutine(), baseline)
}

type recordingSink struct {
	mu   sync.Mutex
	seen []scan.Progress
}

func (s *recordingSink) Publish(p scan.Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, p)
}

func (s *recordingSink) updates() []scan.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]scan.Progress(nil), s.seen...)
}

type droppingSink struct {
	ch chan scan.Progress
}

func (s *droppingSink) Publish(p scan.Progress) {
	select {
	case s.ch <- p:
	default:
	}
}

type sleepSink struct {
	delay time.Duration
	once  sync.Once
}

func (s *sleepSink) Publish(scan.Progress) {
	s.once.Do(func() { time.Sleep(s.delay) })
}

func TestTypeCensus(t *testing.T) {
	root := t.TempDir()
	file(t, root, "a.go", 1)
	file(t, root, "b.go", 1)
	file(t, root, "c.MD", 1)
	file(t, root, "Makefile", 1)
	file(t, root, "x.tar.gz", 1)
	file(t, root, ".hidden.go", 1)
	file(t, root, "sub/d.go", 1)
	if err := os.MkdirAll(filepath.Join(root, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 2}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	want := map[string]int{".go": 3, ".md": 1, ".gz": 1, "": 1}
	if !reflect.DeepEqual(snap.Types, want) {
		t.Fatalf("types = %v, want %v; hidden files and directories never count", snap.Types, want)
	}
	sum := 0
	for _, files := range snap.Types {
		sum += files
	}
	if sum != snap.Files {
		t.Fatalf("types sum to %d, want the %d files the walk counted", sum, snap.Files)
	}
}

func TestSkipLeavesEntriesOut(t *testing.T) {
	root := t.TempDir()
	file(t, root, "a.txt", 10)
	file(t, root, "cache/b.txt", 10)
	file(t, root, "cache/deep/c.txt", 10)
	file(t, root, "keep/d.txt", 10)
	skip := []string{filepath.Join(root, "cache"), filepath.Join(root, "keep", "d.txt")}
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 2, Skip: skip}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if snap.Files != 1 || snap.Bytes != 10 {
		t.Fatalf("facts = %d files, %d bytes; want the one file the walk may report", snap.Files, snap.Bytes)
	}
	if !reflect.DeepEqual(snap.Types, map[string]int{".txt": 1}) {
		t.Fatalf("types = %v, want the skipped files left out", snap.Types)
	}
	for _, dir := range snap.Dirs {
		if dir.Path == "cache" {
			t.Fatalf("the walk listed the skipped directory: %v", snap.Dirs)
		}
	}
}
