package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// TestScenarioSecondRunUsesTheStore proves the store is content resume state:
// a second run proves the same counts and says it reused stored facts.
func TestScenarioSecondRunUsesTheStore(t *testing.T) {
	t.Parallel()
	root := settled(t, fixture(t))
	cache := cacheRoot(t)
	env := []string{"XDG_CACHE_HOME=" + cache}
	first := itest.RunVisualEnv(t, root, 150, env, root)
	second := itest.RunVisualEnv(t, root, 150, env, root)
	if first.Code != 0 || second.Code != 0 {
		t.Fatalf("runs = %d, %d; want 0", first.Code, second.Code)
	}
	if strings.Contains(first.Stdout, "cache reused") {
		t.Fatalf("a cold run claims stored facts:\n%s", first.Stdout)
	}
	if !strings.HasSuffix(second.Stdout, "  cache reused\n") {
		t.Fatalf("second run = %q, want the cache token", second.Stdout)
	}
	if got := strings.TrimSuffix(second.Stdout, "  cache reused\n"); got != first.Stdout {
		t.Fatalf("second run view =\n%s\nwant the first run's view\n%s", got, first.Stdout)
	}
	if files := storeFiles(t, cache); len(files) == 0 {
		t.Fatal("the store holds no facts after a successful run")
	}
}

// TestScenarioStoreFallsBackToHome proves an unset XDG_CACHE_HOME stores facts
// under $HOME/.cache, so a second run reuses them.
func TestScenarioStoreFallsBackToHome(t *testing.T) {
	t.Parallel()
	root := settled(t, fixture(t))
	home := t.TempDir()
	env := []string{"XDG_CACHE_HOME=", "HOME=" + home}
	first := itest.RunVisualEnv(t, root, 150, env, root)
	second := itest.RunVisualEnv(t, root, 150, env, root)
	if first.Code != 0 || second.Code != 0 || first.Stderr != "" || second.Stderr != "" {
		t.Fatalf("runs = %d, %d, %q, %q; want 0 and no diagnostic", first.Code, second.Code, first.Stderr, second.Stderr)
	}
	if strings.Contains(first.Stdout, "cache reused") || !strings.HasSuffix(second.Stdout, "  cache reused\n") {
		t.Fatalf("first =\n%s\nsecond =\n%s\nwant a cold run and a warm one", first.Stdout, second.Stdout)
	}
	if files := storeFiles(t, filepath.Join(home, ".cache", "duls", "v1")); len(files) == 0 {
		t.Fatal("the fallback store wrote nothing under $HOME/.cache")
	}
}

// TestScenarioRefreshZeroWritesNothing proves -r 0 turns the store off.
func TestScenarioRefreshZeroWritesNothing(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	cache := cacheRoot(t)
	res := itest.RunEnv(t, root, []string{"XDG_CACHE_HOME=" + cache}, "-r", "0", root)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls -r 0 = %d, %q; want 0", res.Code, res.Stderr)
	}
	if files := storeFiles(t, cache); len(files) != 0 {
		t.Fatalf("store = %v, want nothing written", files)
	}
}

// TestScenarioWritesViewAndStoreFacts proves a successful run writes the
// complete view and persists its facts, and that the store stays outside the
// scanned root.
func TestScenarioWritesViewAndStoreFacts(t *testing.T) {
	t.Parallel()
	root := settled(t, fixture(t))
	cache := cacheRoot(t)
	res := itest.RunVisualEnv(t, root, 150, []string{"XDG_CACHE_HOME=" + cache}, root)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	if !strings.HasPrefix(res.Stdout, "  6 files · 420 B · 84 lines\n") || strings.Contains(res.Stdout, "cache reused") {
		t.Fatalf("view = %q, want the complete cold view", res.Stdout)
	}
	if files := storeFiles(t, cache); len(files) == 0 {
		t.Fatal("the run wrote no stored facts")
	}
	for _, path := range storeFiles(t, root) {
		if strings.HasPrefix(filepath.Base(path), "shard-") {
			t.Fatalf("the run wrote store data under the scanned root: %s", path)
		}
	}
}

// TestScenarioEnvironmentIsolation proves a store that cannot exist is not a
// run failure: the view is printed and nothing is reported.
func TestScenarioEnvironmentIsolation(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	notADirectory := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADirectory, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		env  []string
	}{
		{"no cache home", []string{"XDG_CACHE_HOME=", "HOME="}},
		{"XDG_CACHE_HOME under a file", []string{"XDG_CACHE_HOME=" + filepath.Join(notADirectory, "child")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := itest.RunVisualEnv(t, root, 150, tt.env, root)
			if res.Code != 0 || res.Stderr != "" {
				t.Fatalf("duls = %d, %q; want 0 and no diagnostic", res.Code, res.Stderr)
			}
			if !strings.HasPrefix(res.Stdout, "  6 files · 420 B · 84 lines\n") {
				t.Fatalf("view = %q, want the complete view", res.Stdout)
			}
		})
	}
}

// TestScenarioStoreUnderTheRootIsSkipped proves the tool never finds itself: a
// store inside the scanned root stays in use, and the walk leaves its directory
// out of the view.
func TestScenarioStoreUnderTheRootIsSkipped(t *testing.T) {
	t.Parallel()
	root := settled(t, fixture(t))
	cache := filepath.Join(root, "cache")
	env := []string{"XDG_CACHE_HOME=" + cache}
	first := itest.RunVisualEnv(t, root, 150, env, root)
	second := itest.RunVisualEnv(t, root, 150, env, root)
	if first.Code != 0 || second.Code != 0 || first.Stderr != "" {
		t.Fatalf("runs = %d, %d, %q; want 0 and no diagnostic", first.Code, second.Code, first.Stderr)
	}
	if !strings.HasPrefix(first.Stdout, "  6 files · 420 B · 84 lines\n") {
		t.Fatalf("view = %q, want the scanned tree only", first.Stdout)
	}
	if files := storeFiles(t, cache); len(files) == 0 {
		t.Fatal("the store inside the root holds no facts")
	}
	if strings.Contains(first.Stdout, "cache reused") {
		t.Fatalf("a cold run claims stored facts:\n%s", first.Stdout)
	}
	if got := strings.TrimSuffix(second.Stdout, "  cache reused\n"); got != first.Stdout || got == second.Stdout {
		t.Fatalf("second run =\n%s\nwant the first run's view plus the cache token", second.Stdout)
	}
}
