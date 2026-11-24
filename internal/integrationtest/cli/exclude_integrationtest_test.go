package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// resolved is the root as duls reports it: absolute patterns name this path.
func resolved(t *testing.T, root string) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// TestScenarioExcludeLeavesOutMatchingEntries proves --exclude removes every
// matching directory and regular file from the walk, the count and the census,
// counts what it left out, and keeps the totals exact (architecture/scan.md).
func TestScenarioExcludeLeavesOutMatchingEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "keep/a.md", 5)
	write(t, root, "node_modules/x.js", 10)
	write(t, root, "src/node_modules/y.js", 10)
	write(t, root, "src/main.go", 4)
	write(t, root, "app.log", 3)
	write(t, root, "src/debug.log", 2)

	all := decodeAgent(t, itest.Run(t, root, "-r", "0"))
	if all.Totals.Files != 6 || all.Accounting.Excluded != 0 || all.Accounting.Exclude != nil {
		t.Fatalf("a run without patterns = %+v, %+v; want 6 files, excluded 0 and no exclude key", all.Totals, all.Accounting)
	}

	doc := decodeAgent(t, itest.Run(t, root, "-r", "0", "--exclude", "node_modules", "--exclude", "*.log"))
	if doc.Totals.Files != 2 || doc.Totals.Bytes != 45 || doc.Totals.Lines != 9 {
		t.Fatalf("totals = %+v, want keep/a.md and src/main.go only: 2 files, 45 B, 9 lines", doc.Totals)
	}
	if !doc.Totals.BytesExact || !doc.Totals.LinesExact || doc.Filesystem != nil {
		t.Fatalf("totals = %+v, filesystem = %+v; want exact totals and no device", doc.Totals, doc.Filesystem)
	}
	if doc.Accounting.Excluded != 4 {
		t.Fatalf("excluded = %d, want two directories and two files", doc.Accounting.Excluded)
	}
	if want := []string{"node_modules", "*.log"}; !reflect.DeepEqual(doc.Accounting.Exclude, want) {
		t.Fatalf("exclude = %q, want %q", doc.Accounting.Exclude, want)
	}
	a := doc.Accounting
	if a.Measured+a.Skipped+a.Unknown != doc.Totals.Files || a.NotScanned != 0 {
		t.Fatalf("accounting = %+v, want every counted file dispatched once", a)
	}
	for _, ty := range doc.Types {
		if ty.Ext == ".log" || ty.Ext == ".js" {
			t.Fatalf("types = %+v, want no excluded file in the census", doc.Types)
		}
	}
	for _, row := range doc.Entries {
		if row.Name == "node_modules" || row.Name == "app.log" {
			t.Fatalf("entries = %+v, want no excluded entry as a row", doc.Entries)
		}
	}
}

// TestScenarioExcludeMatchesLikeFnmatch proves the ncdu 1.x rule: a pattern is
// tested against the absolute path and every suffix after a '/', '*' crosses
// '/', and a bracket negates with '!' (architecture/cli.md).
func TestScenarioExcludeMatchesLikeFnmatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := resolved(t, root)
	write(t, root, "src/gen/a.md", 1)
	write(t, root, "other/gen/b.md", 1)
	write(t, root, "deep/a/b/target/c.md", 1)
	write(t, root, "drop/d.md", 1)
	write(t, root, ".hidden/e.md", 1)
	write(t, root, ".git/f.md", 1)
	write(t, root, "keep.md", 1)
	for _, tt := range []struct {
		name    string
		pattern string
		gone    string
	}{
		{"a suffix with a slash", "src/gen", "src/gen/a.md"},
		{"an absolute path", base + "/drop", "drop/d.md"},
		{"a star that crosses directories", base + "/*/target", "deep/a/b/target/c.md"},
		{"a negated bracket", ".[!g]*", ".hidden/e.md"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := decodeAgent(t, itest.Run(t, root, "-r", "0", "-a", "--exclude", tt.pattern))
			if doc.Totals.Files != 6 || doc.Accounting.Excluded != 1 {
				t.Fatalf("--exclude %q: files %d, excluded %d; want 6 and 1 (only %s left out)",
					tt.pattern, doc.Totals.Files, doc.Accounting.Excluded, tt.gone)
			}
		})
	}
}

// TestScenarioExcludeMatchesInvalidUTF8PerByte proves a name or pattern that is
// not valid UTF-8 is matched per byte, as glibc falls back, so two different
// Latin-1 bytes never compare equal (architecture/cli.md).
func TestScenarioExcludeMatchesInvalidUTF8PerByte(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "caf\xe9s", 1)
	write(t, root, "caf\xe8s", 1)
	doc := decodeAgent(t, itest.Run(t, root, "-r", "0", "--exclude", "*caf\xe9*"))
	if doc.Totals.Files != 1 || doc.Accounting.Excluded != 1 {
		t.Fatalf("files %d, excluded %d; want only the \\xe9 name left out", doc.Totals.Files, doc.Accounting.Excluded)
	}
}

// TestScenarioExcludeCountsOnlyWhatTheWalkWouldCount proves a pattern never
// counts a hidden entry the walk skips, a symlink it never follows, or the root
// itself, and that a pattern matching the root's own name with '/*' empties
// the view, as ncdu 1.x does (architecture/scan.md).
func TestScenarioExcludeCountsOnlyWhatTheWalkWouldCount(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "proj")
	write(t, root, "ok.md", 2)
	write(t, root, ".cache/c.md", 2)
	if err := os.Symlink("ok.md", filepath.Join(root, "lnk.md")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name     string
		args     []string
		files    int
		excluded int
	}{
		{"a hidden match without --all", []string{"--exclude", ".cache"}, 1, 0},
		{"a symlink match", []string{"--exclude", "lnk*"}, 1, 0},
		{"the root's own name", []string{"--exclude", "proj"}, 1, 0},
		{"everything below the root's name", []string{"--exclude", "proj/*"}, 0, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := decodeAgent(t, itest.Run(t, root, append([]string{"-r", "0"}, tt.args...)...))
			if doc.Totals.Files != tt.files || doc.Accounting.Excluded != tt.excluded {
				t.Fatalf("%v: files %d, excluded %d; want %d and %d",
					tt.args, doc.Totals.Files, doc.Accounting.Excluded, tt.files, tt.excluded)
			}
		})
	}
}

// TestScenarioExcludeOutranksTheDenyListAndARefusal proves an excluded file is
// never counted as skipped, and an excluded directory is never listed, so its
// refusal leaves the totals exact (architecture/measure.md).
func TestScenarioExcludeOutranksTheDenyListAndARefusal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "a.md", 3)
	writeRaw(t, root, "pic.png", "p\n")
	write(t, root, "locked/secret.md", 2)
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	doc := decodeAgent(t, itest.Run(t, root, "-r", "0", "--exclude", "*.png", "--exclude", "locked"))
	a := doc.Accounting
	if doc.Totals.Files != 1 || a.Skipped != 0 || a.Measured+a.Skipped+a.Unknown != 1 {
		t.Fatalf("totals = %+v, accounting = %+v; want a.md alone, measured, nothing skipped", doc.Totals, a)
	}
	if a.UnreadableDirs != 0 || !doc.Totals.BytesExact || doc.Filesystem != nil || a.Excluded != 2 {
		t.Fatalf("accounting = %+v, totals = %+v; want no refusal, exact totals and two exclusions", a, doc.Totals)
	}
}

// TestScenarioExcludeFromReadsPatternsInOrder proves -X reads one pattern per
// line, relative to the working directory, ignoring blank lines and CRs, and
// that the document lists the patterns in command-line order.
func TestScenarioExcludeFromReadsPatternsInOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"a/f.md", "b/f.md", "c/f.md", "d/f.md", "e/f.md"} {
		write(t, root, name, 1)
	}
	cwd := t.TempDir()
	writeRaw(t, cwd, "pats.txt", "c\r\n\r\n\nd\n")
	doc := decodeAgent(t, itest.Run(t, cwd, "-r", "0", "--exclude", "a", "-X", "pats.txt", root, "--exclude-from=pats.txt", "--exclude", "b"))
	if want := []string{"a", "c", "d", "c", "d", "b"}; !reflect.DeepEqual(doc.Accounting.Exclude, want) {
		t.Fatalf("exclude = %q, want %q", doc.Accounting.Exclude, want)
	}
	if doc.Totals.Files != 1 || doc.Accounting.Excluded != 4 {
		t.Fatalf("files %d, excluded %d; want e/f.md alone and four directories left out", doc.Totals.Files, doc.Accounting.Excluded)
	}
}

// TestScenarioExcludeFileNamesTheBadLine proves a pattern from -X that can never
// match is one error line that names the file and the line.
func TestScenarioExcludeFileNamesTheBadLine(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeRaw(t, cwd, "pats.txt", "ok\nsrc/\n")
	res := itest.Run(t, cwd, "-X", "pats.txt")
	if res.Code != 1 || res.Stdout != "" || strings.Count(res.Stderr, "\n") != 1 {
		t.Fatalf("duls -X = %d, %q, %q; want exit 1 and one error line", res.Code, res.Stdout, res.Stderr)
	}
	for _, want := range []string{"error: flag: pats.txt:2: ", `"src/"`, "drop the trailing '/'", "run 'duls -h' for its flags"} {
		if !strings.Contains(res.Stderr, want) {
			t.Fatalf("stderr = %q, want %q", res.Stderr, want)
		}
	}
}

// TestScenarioExcludeFooter proves the visual footer states the exclusion
// count whenever a pattern was given, zero included.
func TestScenarioExcludeFooter(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	for _, tt := range []struct {
		pattern string
		want    string
	}{
		{"meetings", "excluded 1"},
		{"nothing-matches", "excluded 0"},
	} {
		res := itest.RunVisual(t, root, 150, "-r", "0", "--exclude", tt.pattern, root)
		if res.Code != 0 || res.Stderr != "" {
			t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
		}
		lines := strings.Split(strings.TrimRight(itest.StripANSI(res.Stdout), "\n"), "\n")
		if footer := lines[len(lines)-1]; !strings.Contains(footer, tt.want) {
			t.Fatalf("--exclude %s footer = %q, want %q", tt.pattern, footer, tt.want)
		}
	}
	plain := itest.RunVisual(t, root, 150, "-r", "0", root)
	if strings.Contains(itest.StripANSI(plain.Stdout), "excluded") {
		t.Fatalf("view without patterns = %q, want no exclusion count", plain.Stdout)
	}
}
