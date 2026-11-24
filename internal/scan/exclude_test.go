package scan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/baalimago/duls/internal/scan"
)

func TestNewExcludeNamesTheRefusedPattern(t *testing.T) {
	t.Parallel()
	_, err := scan.NewExclude([]string{"ok", "*.log", "src/"})
	var refused *scan.PatternError
	if !errors.As(err, &refused) || refused.Index != 2 || refused.Pattern != "src/" {
		t.Fatalf("NewExclude error = %#v, want the third pattern refused", err)
	}
	want := `scan: exclude pattern "src/" can never match a path; drop the trailing '/'`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestExcludeMatchesThePathAndEverySuffix(t *testing.T) {
	t.Parallel()
	exclude, err := scan.NewExclude([]string{"node_modules", "*.log", "src/gen", "/r/abs", "/r/*/deep"})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]scan.Verdict{
		"/r/node_modules":   scan.Excluded,
		"/r/a/node_modules": scan.Excluded,
		"/r/node_modules_x": scan.Kept,
		"/r/a/b.log":        scan.Excluded,
		"/r/x/src/gen":      scan.Excluded,
		"/r/srcgen":         scan.Kept,
		"/r/abs":            scan.Excluded,
		"/x/r/abs":          scan.Kept,
		"/r/a/b/deep":       scan.Excluded,
		"/r/keep":           scan.Kept,
	} {
		if got := exclude.Match(path); got != want {
			t.Errorf("Match(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestZeroExcludeKeepsEverything(t *testing.T) {
	t.Parallel()
	none, err := scan.NewExclude(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(none, scan.Exclude{}) || none.Match("/a") != scan.Kept || none.Patterns() != nil {
		t.Fatalf("NewExclude(nil) = %+v, want the zero value that keeps everything", none)
	}
	given := []string{"a"}
	exclude, err := scan.NewExclude(given)
	if err != nil {
		t.Fatal(err)
	}
	given[0] = "b"
	got := exclude.Patterns()
	got[0] = "c"
	if exclude.Patterns()[0] != "a" {
		t.Fatalf("patterns = %q, want a copy the caller cannot change", exclude.Patterns())
	}
}

// TestWalkExcludesAndCounts proves the walk leaves out and counts each matching
// directory and regular file once, and never tests a hidden entry, a Skip
// path, a symlink or a special file (architecture/scan.md).
func TestWalkExcludesAndCounts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file(t, root, "keep/a", 3)
	file(t, root, "src/main.go", 4)
	file(t, root, "node_modules/x", 5)
	file(t, root, "src/node_modules/y", 5)
	file(t, root, "src/b.log", 2)
	file(t, root, "logs/only.log", 2)
	file(t, root, ".hidden.log", 2)
	file(t, root, "cache/c.log", 2)
	if err := os.Symlink("src/b.log", filepath.Join(root, "link.log")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "pipe.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	patterns := []string{"node_modules", "*.log", "cache"}
	exclude, err := scan.NewExclude(patterns)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := scan.New().Walk(context.Background(), root, scan.WalkOptions{
		Workers: 4,
		Exclude: exclude,
		Skip:    []string{filepath.Join(root, "cache")},
	}, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if snap.Files != 2 || snap.Bytes != 7 || snap.Excluded != 4 {
		t.Fatalf("facts = %d files, %d bytes, %d excluded; want keep/a and src/main.go, and four exclusions",
			snap.Files, snap.Bytes, snap.Excluded)
	}
	if !reflect.DeepEqual(snap.Exclude, patterns) || len(snap.Frontier) != 0 {
		t.Fatalf("exclude = %q, frontier = %q; want the patterns and no open subtree", snap.Exclude, snap.Frontier)
	}
	if !reflect.DeepEqual(snap.Types, map[string]int{"": 1, ".go": 1}) {
		t.Fatalf("types = %v, want the excluded files left out of the census", snap.Types)
	}
	if slices.ContainsFunc(snap.Dirs, func(d scan.Dir) bool {
		return d.Path == "node_modules" || d.Path == "src/node_modules"
	}) {
		t.Fatalf("dirs = %+v, want no excluded directory listed", snap.Dirs)
	}
}

// BenchmarkExcludeMatch measures one entry against a .gitignore-sized list and
// against two patterns.
func BenchmarkExcludeMatch(b *testing.B) {
	path := "/home/me/src/project/packages/app/src/components/Button/index.tsx"
	for name, patterns := range map[string][]string{
		"two": {"node_modules", "*.log"},
		"gitignore": {
			"logs", "*.log", "npm-debug.log*", "yarn-debug.log*", "yarn-error.log*", "lerna-debug.log*",
			".pnpm-debug.log*", "report.[0-9]*.[0-9]*.[0-9]*.[0-9]*.json", "pids", "*.pid", "*.seed",
			"*.pid.lock", "lib-cov", "coverage", "*.lcov", ".nyc_output", ".grunt", "bower_components",
			".lock-wscript", "build/Release", "node_modules", "jspm_packages", "web_modules",
			"*.tsbuildinfo", ".npm", ".eslintcache", ".stylelintcache", ".rpt2_cache", ".rts2_cache_cjs",
			".rts2_cache_es", ".rts2_cache_umd", ".node_repl_history", "*.tgz", ".yarn-integrity", ".env",
			".env.development.local", ".env.test.local", ".env.production.local", ".env.local", ".cache",
			".parcel-cache", ".next", "out", ".nuxt", "dist", ".vuepress/dist", ".temp", ".docusaurus",
			".serverless", ".fusebox", ".dynamodb", ".tern-port", ".vscode-test", ".yarn/cache",
			".yarn/unplugged", ".yarn/build-state.yml", ".yarn/install-state.gz", ".pnp.*", "*.swp",
			"*~", ".DS_Store",
		},
	} {
		exclude, err := scan.NewExclude(patterns)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				exclude.Match(path)
			}
		})
	}
}
