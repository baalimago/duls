package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/duls/internal/measure"
	"github.com/baalimago/duls/internal/scan"
)

func TestShardReplaceIsAtomic(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "store")
	opened, err := Open(Config{Dir: dir, Refresh: time.Hour, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatalf("Flush.3: Open: %v", err)
	}
	store := opened.(*store)
	base := t.TempDir()
	witness := scan.Witness{Dev: 1, Ino: 2, Size: 3, Mod: at.Add(-time.Hour), Change: at.Add(-time.Hour)}
	path := filepath.Join(base, "a.txt")
	store.StoreFact(measure.Fact{Path: path, Lines: 1, W: witness})
	if err := store.Flush(time.Second); err != nil {
		t.Fatalf("Flush.3: Flush: %v", err)
	}
	store.rename = func(string, string) error { return errors.New("rename failed") }
	store.StoreFact(measure.Fact{Path: path, Lines: 2, W: witness})
	if err := store.Flush(time.Second); err == nil {
		t.Fatal("Flush.3: Flush succeeded while the rename failed")
	}
	store.rename = os.Rename
	got, ok := store.FileLines(path, witness)
	if !ok {
		t.Fatal("Flush.3: the previous shard no longer loads")
	}
	if got != 1 {
		t.Fatalf("Flush.3: the previous shard holds %d lines, want 1", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Flush.3: read store: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "tmp-") {
			t.Fatalf("Flush.3: a failed write left %s", entry.Name())
		}
	}
}
