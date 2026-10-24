package cli_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// write writes a fixture file of lines LF-terminated lines.
func write(t *testing.T, root, name string, lines int) {
	t.Helper()
	writeRaw(t, root, name, strings.Repeat("line\n", lines))
}

// writeRaw writes a fixture file with the exact content.
func writeRaw(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture is the six-file tree the outline scenarios expect.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "README.md", 20)
	write(t, root, "meetings/a.md", 10)
	write(t, root, "meetings/b.md", 10)
	write(t, root, "meetings/c.md", 10)
	write(t, root, "people/ana.md", 17)
	write(t, root, "people/ben.md", 17)
	return root
}

// wide writes dirs directories of files two-line files: enough work that a
// styled run outlives the frame trigger (README I12).
func wide(t *testing.T, dirs, files int) string {
	t.Helper()
	root := t.TempDir()
	for d := range dirs {
		dir := filepath.Join(root, "d"+strconv.Itoa(d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := range files {
			path := filepath.Join(dir, "f"+strconv.Itoa(f)+".md")
			if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// cacheRoot is an isolated store root that never sits under a fixture
// (README I10).
func cacheRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "cache")
}

// settle is the fact settle window: a witness younger than this is never
// stored (README D27).
const settle = 2100 * time.Millisecond

// settled waits until every file of a fresh fixture can be stored, and returns
// the fixture.
func settled(t *testing.T, root string) string {
	t.Helper()
	time.Sleep(settle)
	return root
}

// storeFiles lists the regular files under dir, and is empty when dir does not
// exist.
func storeFiles(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	return found
}

// lineWith returns the first line of s that contains substr.
func lineWith(t *testing.T, s, substr string) string {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSuffix(s, "\n"), "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", substr, s)
	return ""
}
