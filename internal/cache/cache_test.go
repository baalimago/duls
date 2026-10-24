package cache_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/duls/internal/cache"
	"github.com/baalimago/duls/internal/measure"
	"github.com/baalimago/duls/internal/scan"
)

var _ measure.Measured = cache.Store(nil)

// epoch is the tests' clock: an hour ahead of the real one, so a file whose
// change time the kernel stamps now is already settled for it.
var epoch = time.Now().UTC().Add(time.Hour).Truncate(time.Second)

type clock struct{ at time.Time }

func newClock() *clock { return &clock{at: epoch} }

func (c *clock) now() time.Time { return c.at }

func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func settledWitness() scan.Witness {
	old := epoch.Add(-time.Hour)
	return scan.Witness{Dev: 1, Ino: 7, Size: 40, Mod: old, Change: old}
}

func openStore(t *testing.T, cfg cache.Config) cache.Store {
	t.Helper()
	if cfg.Now == nil {
		cfg.Now = newClock().now
	}
	if cfg.Refresh == 0 {
		cfg.Refresh = time.Hour
	}
	store, err := cache.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func storeDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "store")
}

func fact(path string, lines int64) measure.Fact {
	return measure.Fact{Path: path, Lines: lines, W: settledWitness()}
}

func TestStoreSatisfiesTheMeasuredSeam(t *testing.T) {
	store := openStore(t, cache.Config{Dir: storeDir(t)})
	var value any = store
	if _, ok := value.(measure.Measured); !ok {
		t.Fatal("Open.1: the store is not a measure.Measured")
	}
	if _, ok := value.(cache.Store); !ok {
		t.Fatal("Open.1: the store is not a cache.Store")
	}
}

func TestWitnessMismatchAndTrustWindow(t *testing.T) {
	clk := newClock()
	store := openStore(t, cache.Config{Dir: storeDir(t), Now: clk.now, Refresh: time.Minute})
	path := filepath.Join(t.TempDir(), "a.txt")
	w := settledWitness()
	store.StoreFact(measure.Fact{Path: path, Lines: 3, W: w})
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("FileLines.1: Flush: %v", err)
	}
	if lines, ok := store.FileLines(path, w); !ok || lines != 3 {
		t.Fatalf("FileLines.1: got %d, %v; want 3, true", lines, ok)
	}
	for name, mutated := range map[string]scan.Witness{
		"dev":    {Dev: 2, Ino: w.Ino, Size: w.Size, Mod: w.Mod, Change: w.Change},
		"ino":    {Dev: w.Dev, Ino: 8, Size: w.Size, Mod: w.Mod, Change: w.Change},
		"size":   {Dev: w.Dev, Ino: w.Ino, Size: 41, Mod: w.Mod, Change: w.Change},
		"mtime":  {Dev: w.Dev, Ino: w.Ino, Size: w.Size, Mod: w.Mod.Add(time.Second), Change: w.Change},
		"ctime":  {Dev: w.Dev, Ino: w.Ino, Size: w.Size, Mod: w.Mod, Change: w.Change.Add(time.Second)},
		"absent": {},
	} {
		if _, ok := store.FileLines(path, mutated); ok {
			t.Fatalf("FileLines.1: a %s mismatch read as a hit", name)
		}
	}
	clk.advance(time.Minute)
	if _, ok := store.FileLines(path, w); ok {
		t.Fatal("FileLines.2: a fact outside the trust window read as a hit")
	}
}

func TestTornShardIsAMiss(t *testing.T) {
	dir := storeDir(t)
	store := openStore(t, cache.Config{Dir: dir})
	path := filepath.Join(t.TempDir(), "a")
	store.StoreFact(fact(path, 1))
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("Open.4: Flush: %v", err)
	}
	shards := shardFiles(t, dir)
	if len(shards) != 1 {
		t.Fatalf("Open.4: wrote %d shard files, want 1", len(shards))
	}
	data, err := os.ReadFile(shards[0])
	if err != nil {
		t.Fatalf("Open.4: read shard: %v", err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(shards[0], data, 0o600); err != nil {
		t.Fatalf("Open.4: write shard: %v", err)
	}
	if _, ok := store.FileLines(path, settledWitness()); ok {
		t.Fatal("Open.4: a torn shard read as a hit")
	}
	if _, err := os.Stat(shards[0]); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open.4: the torn shard was not removed: %v", err)
	}
}

func TestRootOverlap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a")
	t.Run("store inside root keeps the store", func(t *testing.T) {
		store := openStore(t, cache.Config{Dir: filepath.Join(root, "store"), Root: root})
		store.StoreFact(fact(path, 1))
		if err := store.Flush(time.Second); err != nil {
			t.Fatalf("Open.1: Flush: %v", err)
		}
		if _, ok := store.FileLines(path, settledWitness()); !ok {
			t.Fatal("Open.1: a store inside the root served nothing")
		}
	})
	t.Run("root inside store disables the store", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "store")
		store := openStore(t, cache.Config{Dir: dir, Root: filepath.Join(dir, "sub")})
		store.StoreFact(fact(path, 1))
		if err := store.Flush(time.Second); err != nil {
			t.Fatalf("Open.1: Flush: %v", err)
		}
		store.Close()
		if _, ok := store.FileLines(path, settledWitness()); ok {
			t.Fatal("Open.3: a disabled store served a fact")
		}
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("I10: the disabled store wrote %s", dir)
		}
	})
}

func TestCapEvictsShards(t *testing.T) {
	dir := storeDir(t)
	const limit = 2048
	store := openStore(t, cache.Config{Dir: dir, Cap: limit})
	base := t.TempDir()
	var paths []string
	for i := range 60 {
		path := filepath.Join(base, "directory-with-a-long-name", strings.Repeat("x", 40), string(rune('a'+i%26)), string(rune('0'+i/26)))
		paths = append(paths, path)
		store.StoreFact(fact(path, int64(i)))
	}
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("Open.5: Flush: %v", err)
	}
	var total int64
	for _, name := range shardFiles(t, dir) {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("Open.5: stat shard: %v", err)
		}
		total += info.Size()
	}
	if total > limit {
		t.Fatalf("Open.5: the store holds %d bytes, cap %d", total, limit)
	}
	hits := 0
	for i, path := range paths {
		if got, ok := store.FileLines(path, settledWitness()); ok && got == int64(i) {
			hits++
		}
	}
	if hits == 0 {
		t.Fatal("Open.5: eviction left no readable fact")
	}
}

func TestFullMailboxDrops(t *testing.T) {
	store := openStore(t, cache.Config{Dir: storeDir(t)})
	base := t.TempDir()
	var paths []string
	start := time.Now()
	for i := range 300 {
		path := filepath.Join(base, "d", strings.Repeat("y", 30), string(rune('a'+i%26)), string(rune('a'+i/26)))
		paths = append(paths, path)
		store.StoreFact(fact(path, int64(i)))
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("StoreFact.1: 300 offers took %v", elapsed)
	}
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("StoreFact.1: Flush: %v", err)
	}
	hits := 0
	for _, path := range paths {
		if _, ok := store.FileLines(path, settledWitness()); ok {
			hits++
		}
	}
	if hits != 256 {
		t.Fatalf("StoreFact.1: the store kept %d of 300 offers, want the 256-wide mailbox", hits)
	}
}

func TestFlushBudget(t *testing.T) {
	store := openStore(t, cache.Config{Dir: storeDir(t)})
	path := filepath.Join(t.TempDir(), "a")
	store.StoreFact(fact(path, 1))
	start := time.Now()
	err := store.Flush(0)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Flush.1: a spent budget returned no error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Flush.1: error is %v, want a deadline", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Flush.1: Flush(0) took %v", elapsed)
	}
	store.StoreFact(fact(path, 2))
	if err := store.Flush(time.Minute); err != nil {
		t.Fatalf("Flush.2: Flush: %v", err)
	}
	if got, ok := store.FileLines(path, settledWitness()); !ok || got != 2 {
		t.Fatalf("Flush.2: got %d, %v; want lines 2", got, ok)
	}
}

func TestTwoStoresMerge(t *testing.T) {
	dir := storeDir(t)
	first := openStore(t, cache.Config{Dir: dir})
	second := openStore(t, cache.Config{Dir: dir, Now: newClock().now})
	base := t.TempDir()
	var paths []string
	for i := range 32 {
		pathA := filepath.Join(base, "a", string(rune('a'+i%26)), string(rune('a'+i/26)))
		pathB := filepath.Join(base, "b", string(rune('a'+i%26)), string(rune('a'+i/26)))
		paths = append(paths, pathA, pathB)
		first.StoreFact(fact(pathA, int64(i)))
		second.StoreFact(fact(pathB, int64(i)))
	}
	if err := first.Flush(time.Second); err != nil {
		t.Fatalf("Flush.3: first Flush: %v", err)
	}
	if err := second.Flush(time.Second); err != nil {
		t.Fatalf("Flush.3: second Flush: %v", err)
	}
	for _, path := range paths {
		if _, ok := first.FileLines(path, settledWitness()); !ok {
			t.Fatalf("Flush.3: %s was lost by the second store", path)
		}
	}
	if got := len(shardFiles(t, dir)); got >= len(paths) {
		t.Fatalf("Flush.3: %d keys occupy %d shards, so some shard merged", len(paths), got)
	}
}

func TestSettleWindow(t *testing.T) {
	store := openStore(t, cache.Config{Dir: storeDir(t)})
	base := t.TempDir()
	young := scan.Witness{Dev: 1, Ino: 1, Size: 1, Mod: epoch.Add(-time.Second), Change: epoch.Add(-time.Second)}
	old := settledWitness()
	youngFile := filepath.Join(base, "young.txt")
	oldFile := filepath.Join(base, "old.txt")
	store.StoreFact(measure.Fact{Path: youngFile, Lines: 1, W: young})
	store.StoreFact(measure.Fact{Path: oldFile, Lines: 2, W: old})
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("StoreFact.2: Flush: %v", err)
	}
	if _, ok := store.FileLines(youngFile, young); ok {
		t.Fatal("StoreFact.2: a young fact was persisted")
	}
	if _, ok := store.FileLines(oldFile, old); !ok {
		t.Fatal("StoreFact.2: an old fact was not persisted")
	}
}

func TestDisabledStoreIsSilent(t *testing.T) {
	root := t.TempDir()
	clk := newClock()
	store, err := cache.Open(cache.Config{Dir: "", Root: root, Refresh: time.Hour, Now: clk.now})
	if err != nil {
		t.Fatalf("Open.1: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	store.StoreFact(fact(path, 1))
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("Open.1: Flush: %v", err)
	}
	store.Close()
	store.Close()
	if _, ok := store.FileLines(path, settledWitness()); ok {
		t.Fatal("Open.1: a disabled store served a fact")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("Open.1: read root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Open.1: a disabled store created %d entries under the root", len(entries))
	}
}

func TestStoreRetainsLineCounts(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "a.txt")
	content := "one\ntwo\nthree"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("FileLines.3: write file: %v", err)
	}
	old := epoch.Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("FileLines.3: chtimes: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("FileLines.3: stat: %v", err)
	}
	want := int64(strings.Count(content, "\n") + 1)
	store := openStore(t, cache.Config{Dir: storeDir(t)})
	store.StoreFact(measure.Fact{Path: path, Lines: want, W: scan.WitnessOf(info)})
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("FileLines.3: Flush: %v", err)
	}
	got, ok := store.FileLines(path, scan.WitnessOf(info))
	if !ok || got != want {
		t.Fatalf("FileLines.3: got %d, %v; want %d, true", got, ok, want)
	}
}

func TestStoreUsesResolvedPathsAndTrustWindow(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("Open.6: mkdir: %v", err)
	}
	file := filepath.Join(real, "a.txt")
	if err := os.WriteFile(file, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatalf("Open.6: write: %v", err)
	}
	old := epoch.Add(-time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatalf("Open.6: chtimes: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("Open.6: symlink: %v", err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("Open.6: stat: %v", err)
	}
	w := scan.WitnessOf(info)
	clk := newClock()
	store := openStore(t, cache.Config{Dir: storeDir(t), Now: clk.now, Refresh: time.Minute})
	store.StoreFact(measure.Fact{Path: filepath.Join(link, "a.txt"), Lines: 2, W: w})
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("Open.6: Flush: %v", err)
	}
	if lines, ok := store.FileLines(file, w); !ok || lines != 2 {
		t.Fatalf("Open.6: an aliased path read as %d, %v; want 2, true", lines, ok)
	}
	clk.advance(2 * time.Minute)
	if _, ok := store.FileLines(file, w); ok {
		t.Fatal("Open.6: a fact beyond the trust window read as a hit")
	}
}

func TestCloseAfterFailedFlush(t *testing.T) {
	store := openStore(t, cache.Config{Dir: storeDir(t)})
	path := filepath.Join(t.TempDir(), "a")
	store.StoreFact(fact(path, 1))
	if err := store.Flush(0); err == nil {
		t.Fatal("Close.1: a spent budget returned no error")
	}
	store.Close()
	store.Close()
	if _, ok := store.FileLines(path, settledWitness()); ok {
		t.Fatal("Close.1: a closed store served a fact")
	}
}

func TestOpenFailureDisablesCache(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("Open.2: write: %v", err)
	}
	dir := filepath.Join(file, "store")
	_, err := cache.Open(cache.Config{Dir: dir, Refresh: time.Hour, Now: newClock().now})
	if err == nil {
		t.Fatal("Open.2: Open with an unusable directory returned no error")
	}
	if want := "cache: open " + dir + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("Open.2: error is %q, want the prefix %q", err.Error(), want)
	}
}

func shardFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	var out []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "shard-") {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	return out
}
