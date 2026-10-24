package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// TestScenarioPathResolution proves a relative path, an empty path, an absolute
// path and a root named through a symlink all name the same directory (I8).
func TestScenarioPathResolution(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	absolute := itest.RunVisual(t, root, 150, "-r", "0", root)
	if absolute.Code != 0 || absolute.Stderr != "" {
		t.Fatalf("duls %s = %d, %q; want 0", root, absolute.Code, absolute.Stderr)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		dir  string
		args []string
	}{
		{"a relative path", filepath.Dir(root), []string{filepath.Base(root)}},
		{"an empty path", root, nil},
		{"a root named through a symlink", filepath.Dir(link), []string{filepath.Base(link)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := itest.RunVisual(t, tt.dir, 150, append([]string{"-r", "0"}, tt.args...)...)
			if res.Code != 0 || res.Stderr != "" || res.Stdout != absolute.Stdout {
				t.Fatalf("duls %v in %s = %d, %q, %q; want the resolved root's view",
					tt.args, tt.dir, res.Code, res.Stdout, res.Stderr)
			}
		})
	}
}

// TestScenarioAgentDocumentDeclaresRefusedDirectories proves a refused directory
// keeps the run a declared lower bound without naming a stopped phase (I24).
func TestScenarioAgentDocumentDeclaresRefusedDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "ok.md", 3)
	write(t, root, "locked/hidden.md", 2)
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
	if doc.Accounting.UnreadableDirs != 1 || doc.Accounting.Stopped != "none" {
		t.Fatalf("accounting = %+v, want one refused directory and no stopped phase", doc.Accounting)
	}
	if doc.Totals.Files != 1 || doc.Totals.BytesExact || doc.Totals.LinesExact {
		t.Fatalf("totals = %+v, want the proved file and lower bounds", doc.Totals)
	}
	if doc.Filesystem == nil || doc.Filesystem.Total <= 0 || doc.Filesystem.Used < 0 {
		t.Fatalf("filesystem = %+v, want the device that holds the refused directory", doc.Filesystem)
	}
}

// TestScenarioUnreadableInput proves a refused file and a refused directory are
// declared unknowns and a refused root is a failure (I24).
func TestScenarioUnreadableInput(t *testing.T) {
	t.Parallel()
	t.Run("a permission-refused file", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, root, "ok.md", 1)
		write(t, root, "secret.md", 2)
		secret := filepath.Join(root, "secret.md")
		if err := os.Chmod(secret, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })
		res := itest.RunVisual(t, root, 150, root)
		if res.Code != 0 || res.Stderr != "" {
			t.Fatalf("duls = %d, %q; want 0 and no diagnostic", res.Code, res.Stderr)
		}
		if !strings.HasPrefix(res.Stdout, "  2 files · ") {
			t.Fatalf("view = %q, want the file facts kept", res.Stdout)
		}
		if row := lineWith(t, res.Stdout, "secret.md"); !strings.HasSuffix(row, " ?") {
			t.Fatalf("row = %q, want an unknown line count", row)
		}
		if !strings.Contains(res.Stdout, "unknown 1") || !strings.Contains(res.Stdout, "unreadable 1") {
			t.Fatalf("view = %q, want the footer to count the refused file", res.Stdout)
		}
	})
	t.Run("a permission-refused directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, root, "ok.md", 1)
		write(t, root, "locked/hidden.md", 40)
		locked := filepath.Join(root, "locked")
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		res := itest.RunVisual(t, root, 150, root)
		if res.Code != 0 || res.Stderr != "" {
			t.Fatalf("duls = %d, %q; want 0 and no diagnostic", res.Code, res.Stderr)
		}
		if !strings.HasPrefix(res.Stdout, "  ~1 file · ~") {
			t.Fatalf("view = %q, want the proved file and lower-bound marks", res.Stdout)
		}
		if !strings.Contains(res.Stdout, "unreadable dirs 1") {
			t.Fatalf("view = %q, want the footer to count the refused directory", res.Stdout)
		}
		if !strings.Contains(res.Stdout, "filesystem ") || !strings.Contains(res.Stdout, " used of ") {
			t.Fatalf("view = %q, want the device usage beside the lower bounds", res.Stdout)
		}
		if strings.Contains(res.Stdout, "locked") {
			t.Fatalf("view = %q, want no row for a directory the walk never read", res.Stdout)
		}
	})
	t.Run("a permission-refused root", func(t *testing.T) {
		t.Parallel()
		parent := t.TempDir()
		root := filepath.Join(parent, "locked")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
		res := itest.Run(t, parent, root)
		if res.Code != 1 || res.Stdout != "" {
			t.Fatalf("duls = %d, %q; want exit 1 and no view", res.Code, res.Stdout)
		}
		if !strings.Contains(res.Stderr, "locked") || !strings.HasPrefix(res.Stderr, "error: ") {
			t.Fatalf("stderr = %q, want one error line naming the root", res.Stderr)
		}
	})
}
