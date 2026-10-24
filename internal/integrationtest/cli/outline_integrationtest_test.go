package cli_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

func TestScenarioOutlineOfATree(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	res := itest.RunVisual(t, root, 150)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	want := strings.Join([]string{
		"  6 files · 420 B · 84 lines",
		"  types  100% .md",
		"  entry       files   size  share largest inside, without a row of its own",
		"  people          2  170 B    40% ana.md 17 · ben.md 17",
		"  meetings        3  150 B    36% a.md 10 · b.md 10 · c.md 10",
		"  README.md       -  100 B      - 20 lines",
		"",
	}, "\n")
	if res.Stdout != want {
		t.Fatalf("view =\n%s\nwant\n%s", res.Stdout, want)
	}
}

func TestScenarioEmptyDirectory(t *testing.T) {
	t.Parallel()
	res := itest.RunVisual(t, t.TempDir(), 150)
	if res.Code != 0 {
		t.Fatalf("duls = %d, %q", res.Code, res.Stderr)
	}
	want := "  0 files · 0 B · 0 lines\n" +
		"  entry   files   size  share largest inside, without a row of its own\n"
	if res.Stdout != want {
		t.Fatalf("view = %q, want %q", res.Stdout, want)
	}
}

func TestScenarioHiddenEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "a.md", 1)
	write(t, root, ".hidden.md", 1)
	if res := itest.RunVisual(t, root, 150); !strings.HasPrefix(res.Stdout, "  1 file · 5 B · 1 line\n") {
		t.Fatalf("default view = %q, want the hidden file skipped", res.Stdout)
	}
	if res := itest.RunVisual(t, root, 150, "--all"); !strings.HasPrefix(res.Stdout, "  2 files · 10 B · 2 lines\n") {
		t.Fatalf("--all view = %q, want both files", res.Stdout)
	}
}

func TestScenarioFlagsAfterThePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, ".a.md", 1)
	if res := itest.RunVisual(t, root, 150, root, "-a"); !strings.HasPrefix(res.Stdout, "  1 file · 5 B · 1 line\n") {
		t.Fatalf("view = %q, want the hidden file counted", res.Stdout)
	}
}

func TestScenarioTerminatorMakesAFlagAPath(t *testing.T) {
	t.Parallel()
	res := itest.Run(t, t.TempDir(), "--", "-a")
	if res.Code != 1 || !strings.HasPrefix(res.Stderr, "error: duls: inspect ") {
		t.Fatalf("duls -- -a = %d, %q; want the missing path", res.Code, res.Stderr)
	}
}

func TestScenarioMissingDirectoryIsOneLine(t *testing.T) {
	t.Parallel()
	res := itest.Run(t, t.TempDir(), "gone")
	if res.Code != 1 || res.Stdout != "" || !strings.HasPrefix(res.Stderr, "error: ") {
		t.Fatalf("duls gone = %d, %q, %q", res.Code, res.Stdout, res.Stderr)
	}
	if strings.Count(res.Stderr, "\n") != 1 {
		t.Fatalf("stderr = %q, want exactly one line", res.Stderr)
	}
}

func TestScenarioTooManyPaths(t *testing.T) {
	t.Parallel()
	res := itest.Run(t, t.TempDir(), "one", "two")
	if res.Code != 1 || !strings.Contains(res.Stderr, "at most one path argument is accepted, got 2") {
		t.Fatalf("duls one two = %d, %q", res.Code, res.Stderr)
	}
}

func TestScenarioHelpAndVersion(t *testing.T) {
	t.Parallel()
	help := itest.Run(t, t.TempDir(), "-h")
	if help.Code != 0 || !strings.Contains(help.Stdout, "Usage:") || help.Stderr != "" {
		t.Fatalf("duls -h = %d, %q, %q", help.Code, help.Stdout, help.Stderr)
	}
	for _, flag := range []string{
		"-a, --all", "--all-types", "-bytes", "-json", "-m, --mode",
		"-r, --refresh", "-t, --timeout", "-h, --help", "-v, --version",
	} {
		if !strings.Contains(help.Stdout, flag) {
			t.Fatalf("help does not list %q:\n%s", flag, help.Stdout)
		}
	}
	version := itest.Run(t, t.TempDir(), "--version")
	if version.Code != 0 || !strings.HasPrefix(version.Stdout, "duls ") || version.Stderr != "" {
		t.Fatalf("duls --version = %d, %q, %q", version.Code, version.Stdout, version.Stderr)
	}
	if got := strings.TrimSuffix(version.Stdout, "\n"); len(strings.Fields(got)) != 2 {
		t.Fatalf("version line = %q, want 'duls <version>'", got)
	}
}

// TestScenarioCountsUseSIUnits proves a file and a line count of 1000 or more
// print with an SI prefix, in the totals line and in the type line.
func TestScenarioCountsUseSIUnits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 1234 {
		write(t, root, fmt.Sprintf("f%04d.txt", i), 1)
	}
	res := itest.RunVisual(t, root, 150)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	totals := lineWith(t, res.Stdout, "files ·")
	if !strings.HasPrefix(totals, "  1.2k files · ") || !strings.Contains(totals, "1.2k lines") {
		t.Fatalf("totals = %q, want both counts in SI units", totals)
	}
	if types := lineWith(t, res.Stdout, "types"); types != "  types  100% .txt" {
		t.Fatalf("type line = %q, want the one type at 100%%", types)
	}
}
