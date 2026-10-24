package measure_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/duls/internal/measure"
	"github.com/baalimago/duls/internal/scan"
)

func TestDispatchOrder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b", 2)
	write(t, root, "a", 5)
	write(t, root, "c", 5)
	write(t, root, "z/f", 10)
	plan := planFor(t, root)
	log := &openLog{}
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: log.open})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(plan.Root, "a"), filepath.Join(plan.Root, "c"), filepath.Join(plan.Root, "b"), filepath.Join(plan.Root, "z", "f")}
	if !reflect.DeepEqual(log.seen(), want) {
		t.Fatalf("open order = %v, want %v", log.seen(), want)
	}
	if result.Measured != 4 {
		t.Fatalf("measured = %d, want 4", result.Measured)
	}
}

func TestDirSeqBeatsGlobalSize(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/large", 100)
	write(t, root, "b/small", 1)
	plan := planFor(t, root)
	log := &openLog{}
	if _, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: log.open}); err != nil {
		t.Fatal(err)
	}
	seen := log.seen()
	if len(seen) != 2 || !strings.HasSuffix(seen[0], "large") {
		t.Fatalf("open order = %v, want the larger directory's files first", seen)
	}
}

func TestDenyListNeverOpens(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.PNG", 4)
	write(t, root, "b.json", 4)
	write(t, root, "c", 4)
	write(t, root, "d.gz.txt", 4)
	plan := planFor(t, root)
	log := &openLog{}
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: log.open})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range log.seen() {
		if strings.HasSuffix(path, "a.PNG") {
			t.Fatal("the denied file was opened")
		}
	}
	if result.Skipped != 1 || result.Measured != 3 {
		t.Fatalf("skipped = %d measured = %d, want 1 and 3", result.Skipped, result.Measured)
	}
}

func TestLargeFileSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sparse", 0)
	if err := os.Truncate(filepath.Join(root, "sparse"), 65<<20); err != nil {
		t.Fatal(err)
	}
	plan := planFor(t, root)
	log := &openLog{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := measure.New().Count(ctx, plan, measure.Options{Workers: 1, Large: 64 << 20, Open: log.open})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || len(log.seen()) != 0 {
		t.Fatalf("skipped = %d opens = %v, want the capped file skipped and unopened", result.Skipped, log.seen())
	}
}

func TestLargeFileMeasuredWithoutDeadline(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sparse", 0)
	if err := os.Truncate(filepath.Join(root, "sparse"), 8<<20); err != nil {
		t.Fatal(err)
	}
	plan := planFor(t, root)
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Measured != 1 || result.Skipped != 0 || result.Lines != 1 {
		t.Fatalf("result = %+v, want the large file measured without a cap", result)
	}
}

func TestChangedDuringReadIsUnknown(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	store := newStore()
	open := func(p string) (*os.File, error) {
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), 20), 0o644); err != nil {
			return nil, err
		}
		return os.Open(p)
	}
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: open, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if result.Unknown != 1 || result.Measured != 0 || store.offers() != 0 {
		t.Fatalf("result = %+v offers = %d, want an unknown fact and no store", result, store.offers())
	}
}

func TestSymlinkSwapRejected(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	target := write(t, root, "target", 30)
	plan := planFor(t, root)
	store := newStore()
	open := func(p string) (*os.File, error) {
		if filepath.Base(p) != "f" {
			return os.Open(p)
		}
		if err := os.Remove(p); err != nil {
			return nil, err
		}
		if err := os.Symlink(target, p); err != nil {
			return nil, err
		}
		return os.Open(target)
	}
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: open, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if result.Unknown != 1 || result.Measured != 1 || store.offers() != 1 {
		t.Fatalf("result = %+v offers = %d, want the swapped path rejected and the target measured", result, store.offers())
	}
}

func TestPermissionIsUnknown(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	open := func(string) (*os.File, error) { return nil, fs.ErrPermission }
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: open})
	if err != nil {
		t.Fatalf("permission refusal is an error: %v", err)
	}
	if result.Unknown != 1 || result.Unreadable != 1 {
		t.Fatalf("result = %+v, want one unknown unreadable file", result)
	}
}

func TestReadErrorFails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	open := func(p string) (*os.File, error) { return os.OpenFile(p, os.O_WRONLY, 0) }
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: open})
	if err == nil {
		t.Fatal("a read failure is not an error")
	}
	if !reflect.DeepEqual(result, measure.Result{}) {
		t.Fatalf("result = %+v, want none", result)
	}
}

func TestDeadlineAndCancel(t *testing.T) {
	root := t.TempDir()
	write(t, root, "one", 3)
	write(t, root, "two", 3)
	write(t, root, "big/huge", 0)
	if err := os.Truncate(filepath.Join(root, "big", "huge"), 4<<30); err != nil {
		t.Fatal(err)
	}
	plan := planFor(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	result, err := measure.New().Count(ctx, plan, measure.Options{Workers: 1})
	if err != nil {
		t.Fatalf("deadline is an error: %v", err)
	}
	if result.Stopped != scan.StopDeadline {
		t.Fatalf("Stopped = %v, want StopDeadline", result.Stopped)
	}
	if result.Measured < 2 {
		t.Fatalf("measured = %d, want the whole root facts kept", result.Measured)
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	result, err = measure.New().Count(ctx, plan, measure.Options{Workers: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want a wrapped context.Canceled", err)
	}
	if !reflect.DeepEqual(result, measure.Result{}) {
		t.Fatalf("result = %+v, want none", result)
	}
}

func TestAccountingCloses(t *testing.T) {
	root := t.TempDir()
	write(t, root, "good.txt", 5)
	write(t, root, "pic.png", 5)
	write(t, root, "huge.dat", 100)
	locked := write(t, root, "locked.txt", 5)
	plan := planFor(t, root)
	open := func(p string) (*os.File, error) {
		if p == locked {
			return nil, fs.ErrPermission
		}
		return os.Open(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	result, err := measure.New().Count(ctx, plan, measure.Options{Workers: 2, Large: 50, Open: open})
	if err != nil {
		t.Fatal(err)
	}
	dispatched := result.Measured + result.Skipped + result.Unknown
	if dispatched != 4 {
		t.Fatalf("accounting = %d + %d + %d = %d, want 4", result.Measured, result.Skipped, result.Unknown, dispatched)
	}
	if result.Measured != 1 || result.Skipped != 2 || result.Unknown != 1 || result.Unreadable != 1 {
		t.Fatalf("result = %+v, want 1 measured, 2 skipped, 1 unknown unreadable", result)
	}
}

func TestNamedCells(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		files   int
		outside string
		inside  string
	}{
		{name: "root", files: 10, outside: "f00", inside: "f04"},
		{name: "nested", dir: "d/", files: 7, outside: "d/f00", inside: "d/f06"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for i := range tt.files {
				write(t, root, tt.dir+fmt.Sprintf("f%02d", i), i+1)
			}
			plan := planFor(t, root)
			result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 2})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Named) != 6 {
				t.Fatalf("Named holds %d cells, want the six peek cells", len(result.Named))
			}
			if _, ok := result.Named[tt.outside]; ok {
				t.Fatalf("Named holds %s, a file no row can show", tt.outside)
			}
			if _, ok := result.Named[tt.inside]; !ok {
				t.Fatalf("Named lacks %s, a cell a row prints", tt.inside)
			}
		})
	}
}

func TestCountJoinsWorkers(t *testing.T) {
	baseline := runtime.NumGoroutine()
	root := t.TempDir()
	write(t, root, "a", 3)
	write(t, root, "b", 3)
	plan := planFor(t, root)
	if _, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 4}); err != nil {
		t.Fatal(err)
	}
	waitForGoroutines(t, baseline)

	big := t.TempDir()
	write(t, big, "big/huge", 0)
	if err := os.Truncate(filepath.Join(big, "big", "huge"), 2<<30); err != nil {
		t.Fatal(err)
	}
	bigPlan := planFor(t, big)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Millisecond)
	defer cancel()
	if _, err := measure.New().Count(ctx, bigPlan, measure.Options{Workers: 4}); err != nil {
		t.Fatal(err)
	}
	waitForGoroutines(t, baseline)

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond)
		cancel()
	}()
	_, _ = measure.New().Count(ctx, bigPlan, measure.Options{Workers: 4})
	waitForGoroutines(t, baseline)
}

func TestLinesAtChunkEdges(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		chunk   int
		lines   int64
	}{
		{"newline at the end", "a\nb\n", 2, 2},
		{"no final newline", "a\nb", 2, 2},
		{"empty", "", 2, 0},
		{"crlf", "\r\n\r\n", 2, 2},
		{"crlf without final newline", "a\r\nb", 2, 2},
		{"lf on every chunk edge", "aaaaaaaa\nbbbbbbbb\n", 4, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "f"), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			plan := planFor(t, root)
			result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Chunk: tt.chunk})
			if err != nil {
				t.Fatal(err)
			}
			if result.Lines != tt.lines {
				t.Fatalf("lines = %d, want %d", result.Lines, tt.lines)
			}
		})
	}
}

func TestCancelOutranksDeadlineAndFirstErrorWins(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", 3)
	write(t, root, "b", 3)
	plan := planFor(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	cancel()
	result, err := measure.New().Count(ctx, plan, measure.Options{Workers: 2})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want a wrapped context.Canceled", err)
	}
	if !reflect.DeepEqual(result, measure.Result{}) {
		t.Fatalf("result = %+v, want none", result)
	}

	baseline := runtime.NumGoroutine()
	open := func(string) (*os.File, error) { return nil, fmt.Errorf("boom: %w", fs.ErrInvalid) }
	_, err = measure.New().Count(context.Background(), plan, measure.Options{Workers: 2, Open: open})
	if err == nil {
		t.Fatal("a worker failure is not an error")
	}
	waitForGoroutines(t, baseline)
}

func TestCachedFactSkipsOpen(t *testing.T) {
	root := t.TempDir()
	path := write(t, root, "f", 10)
	plan := planFor(t, root)
	store := newStore()
	store.facts[path] = 7
	log := &openLog{}
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: log.open, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if len(log.seen()) != 0 {
		t.Fatalf("cache hit opened %v", log.seen())
	}
	if result.Measured != 1 || result.Lines != 7 || result.Reused != 1 {
		t.Fatalf("result = %+v, want the cached count and one reuse", result)
	}
}

func TestCacheReturnsMissOutsideTrustWindow(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	store := newStore()
	log := &openLog{}
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: log.open, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if len(log.seen()) != 1 || result.Measured != 1 {
		t.Fatalf("opens = %v result = %+v, want a miss and a normal measurement", log.seen(), result)
	}
}

func TestFullMailboxDrops(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	store := newStore()
	store.drop = true
	result, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if result.Measured != 1 || len(result.Named) != 1 {
		t.Fatalf("result = %+v, want a complete result despite the dropped fact", result)
	}
}

func TestStoreRetainsLineCounts(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	store := newStore()
	first, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	log := &openLog{}
	second, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Open: log.open, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if len(log.seen()) != 0 {
		t.Fatalf("second run opened %v", log.seen())
	}
	if second.Lines != first.Lines {
		t.Fatalf("second lines = %d, want the stored %d", second.Lines, first.Lines)
	}
}

func TestSettleWindow(t *testing.T) {
	root := t.TempDir()
	write(t, root, "f", 10)
	plan := planFor(t, root)
	store := newStore()
	store.settle = 2 * time.Second
	if _, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Store: store}); err != nil {
		t.Fatal(err)
	}
	if store.offers() == 0 {
		t.Fatal("the count offered no fact")
	}
	if len(store.persisted()) != 0 {
		t.Fatalf("a witness younger than the settle window was persisted: %v", store.persisted())
	}
}

func TestModesHaveSameMeasuredSet(t *testing.T) {
	root := t.TempDir()
	for i := range 8 {
		write(t, root, fmt.Sprintf("d%02d/f", i), i+1)
	}
	plan := planFor(t, root)
	var sets []map[string]int64
	for _, workers := range []int{1, 4, 16} {
		store := newStore()
		if _, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: workers, Store: store}); err != nil {
			t.Fatal(err)
		}
		sets = append(sets, store.factSet())
	}
	for i := 1; i < len(sets); i++ {
		if !reflect.DeepEqual(sets[i], sets[0]) {
			t.Fatalf("workers %d stored %v, want %v", []int{1, 4, 16}[i], sets[i], sets[0])
		}
	}
}

func TestCountProgressContract(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/one", 3)
	write(t, root, "b/two", 3)
	write(t, root, "c/three", 3)
	plan := planFor(t, root)
	sink := &recordingSink{}
	now := time.Unix(0, 0)
	advance := func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	if _, err := measure.New().Count(context.Background(), plan, measure.Options{Workers: 1, Sink: sink, Now: advance}); err != nil {
		t.Fatal(err)
	}
	got := sink.updates()
	if len(got) < 2 {
		t.Fatalf("published %d updates, want per-file and per-directory progress", len(got))
	}
	walkBytes := plan.Dirs[0].Bytes
	for i, update := range got {
		if update.Phase != scan.PhaseCount {
			t.Fatalf("update %d phase = %v, want PhaseCount", i, update.Phase)
		}
		if update.Bytes != walkBytes {
			t.Fatalf("update %d bytes = %d, want the walk total %d", i, update.Bytes, walkBytes)
		}
		if i == 0 {
			continue
		}
		if update.Files < got[i-1].Files || update.Lines < got[i-1].Lines || update.Dirs < got[i-1].Dirs {
			t.Fatalf("update %d is not monotone: %+v after %+v", i, update, got[i-1])
		}
	}
}

func write(t *testing.T, root, rel string, size int) string {
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

func planFor(t *testing.T, root string) measure.Plan {
	t.Helper()
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{Workers: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return measure.Plan{Root: snap.Root, Dirs: snap.Dirs}
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

type openLog struct {
	mu    sync.Mutex
	paths []string
}

func (l *openLog) open(path string) (*os.File, error) {
	l.mu.Lock()
	l.paths = append(l.paths, path)
	l.mu.Unlock()
	return os.Open(path)
}

func (l *openLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.paths...)
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

type store struct {
	mu       sync.Mutex
	facts    map[string]int64
	offered  int
	drop     bool
	settle   time.Duration
	retained []measure.Fact
}

func newStore() *store {
	return &store{facts: map[string]int64{}}
}

func (s *store) FileLines(path string, w scan.Witness) (int64, bool) {
	info, err := os.Lstat(path)
	if err != nil || scan.WitnessOf(info) != w {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, ok := s.facts[path]
	return lines, ok
}

func (s *store) StoreFact(f measure.Fact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offered++
	if s.drop {
		return
	}
	if s.settle > 0 && time.Since(f.W.Change) < s.settle {
		return
	}
	s.facts[f.Path] = f.Lines
	s.retained = append(s.retained, f)
}

func (s *store) offers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offered
}

func (s *store) persisted() []measure.Fact {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]measure.Fact(nil), s.retained...)
}

func (s *store) factSet() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int64{}
	maps.Copy(out, s.facts)
	return out
}
