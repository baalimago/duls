package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/app"
)

func run(t *testing.T, cwd string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut strings.Builder
	code := Run(context.Background(), append([]string{"duls"}, args...), app.Options{Stdout: &out, Stderr: &errOut, Cwd: cwd})
	return code, out.String(), errOut.String()
}

func TestHelpExitsZero(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		code, out, errOut := run(t, t.TempDir(), args...)
		if code != 0 || !strings.Contains(out, "Usage:") || errOut != "" {
			t.Fatalf("duls %v = %d, %q, %q; want 0 and the usage", args, code, out, errOut)
		}
	}
}

func TestHelpAfterThePathExitsZero(t *testing.T) {
	t.Parallel()
	code, out, errOut := run(t, t.TempDir(), "gone", "-h")
	if code != 0 || !strings.Contains(out, "Usage:") || errOut != "" {
		t.Fatalf("duls gone -h = %d, %q, %q; want 0 and the usage", code, out, errOut)
	}
}

func TestVersionExitsZero(t *testing.T) {
	t.Parallel()
	code, out, errOut := run(t, t.TempDir(), "--version")
	if code != 0 || out != app.VersionLine()+"\n" || errOut != "" {
		t.Fatalf("duls --version = %d, %q, %q", code, out, errOut)
	}
}

func TestUnknownFlagExitsOne(t *testing.T) {
	t.Parallel()
	code, _, errOut := run(t, t.TempDir(), "-x")
	if code != 1 || !strings.HasPrefix(errOut, "error: ") || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("duls -x = %d, %q; want one error line and exit 1", code, errOut)
	}
	if !strings.Contains(errOut, "flag provided but not defined") {
		t.Fatalf("error = %q, want the flag failure", errOut)
	}
}

func TestTooManyPathsExitOne(t *testing.T) {
	t.Parallel()
	code, _, errOut := run(t, t.TempDir(), "one", "two")
	if code != 1 || !strings.Contains(errOut, "at most one path argument is accepted, got 2") {
		t.Fatalf("duls one two = %d, %q", code, errOut)
	}
}

func TestMissingDirectoryExitsOne(t *testing.T) {
	t.Parallel()
	code, out, errOut := run(t, t.TempDir(), "gone")
	if code != 1 || out != "" || !strings.HasPrefix(errOut, "error: duls: inspect ") {
		t.Fatalf("duls gone = %d, %q, %q", code, out, errOut)
	}
}

func TestAnswerIsWrittenToStdout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, root)
	if code != 0 || errOut != "" {
		t.Fatalf("duls = %d, %q; want 0", code, errOut)
	}
	if !strings.Contains(out, `"totals":{"files":1,"bytes":2,"lines":1,`) {
		t.Fatalf("answer = %q, want the totals", out)
	}
}

func TestFlagsAfterThePathAreParsed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".a.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, root, ".", "-a")
	if code != 0 || !strings.Contains(out, `".a.md"`) || !strings.Contains(out, `"files":1`) {
		t.Fatalf("duls . -a = %d, %q; want the hidden file counted", code, out)
	}
}

func TestWithProcessEnvFillsTheEnvironment(t *testing.T) {
	t.Parallel()
	if got := withProcessEnv(app.Options{}); got.Env == nil {
		t.Fatal("nil Env was not filled from the process")
	}
	given := []string{"A=1"}
	if got := withProcessEnv(app.Options{Env: given}); &got.Env[0] != &given[0] {
		t.Fatal("a set Env must be kept")
	}
}

// TestCancelIsAnError proves a cancelled context is one error line and exit 1,
// with no view (CLI.8, I11). A process cannot observe a cancel: the command
// installs no signal handler and its surface has no cancel input.
func TestCancelIsAnError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut strings.Builder
	code := Run(ctx, []string{"duls", root}, app.Options{Stdout: &out, Stderr: &errOut, Cwd: root})
	if code != 1 || out.String() != "" {
		t.Fatalf("Run = %d, %q; want exit 1 and no view", code, out.String())
	}
	if !strings.HasPrefix(errOut.String(), "error: ") || strings.Count(errOut.String(), "\n") != 1 {
		t.Fatalf("stderr = %q, want exactly one error line", errOut.String())
	}
	if !strings.Contains(errOut.String(), "context canceled") {
		t.Fatalf("stderr = %q, want it to name the cancel", errOut.String())
	}
}
