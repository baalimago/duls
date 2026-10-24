package cli_test

import (
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// TestScenarioBadFlags proves every rejected command line is one error line and
// exit 1, with no view.
func TestScenarioBadFlags(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"an unknown flag", []string{"--nope"}, "flag provided but not defined"},
		{"an unparsable duration", []string{"-t", "wat"}, "invalid value"},
		{"a negative refresh", []string{"-r", "-1s"}, "refresh must not be negative"},
		{"a mode above three", []string{"-m", "9"}, "mode must be 0, 1, 2 or 3"},
		{"two paths", []string{"one", "two"}, "at most one path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := itest.Run(t, t.TempDir(), tt.args...)
			if res.Code != 1 || res.Stdout != "" {
				t.Fatalf("duls %v = %d, %q; want exit 1 and no view", tt.args, res.Code, res.Stdout)
			}
			if !strings.HasPrefix(res.Stderr, "error: ") || strings.Count(res.Stderr, "\n") != 1 {
				t.Fatalf("stderr = %q, want exactly one error line", res.Stderr)
			}
			if !strings.Contains(res.Stderr, tt.want) {
				t.Fatalf("stderr = %q, want it to name %q", res.Stderr, tt.want)
			}
		})
	}
}

// TestScenarioBytesAndAllTypes proves --bytes prints exact integers and
// --all-types measures the files the deny-list leaves out.
func TestScenarioBytesAndAllTypes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeRaw(t, root, "pic.png", "a\nb\nc\nd\n")
	write(t, root, "big.md", 500)
	si := itest.RunVisual(t, root, 150, root)
	exact := itest.RunVisual(t, root, 150, "--bytes", root)
	all := itest.RunVisual(t, root, 150, "--all-types", root)
	for _, res := range []itest.Result{si, exact, all} {
		if res.Code != 0 || res.Stderr != "" {
			t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
		}
	}
	if !strings.Contains(si.Stdout, "2.5 kB") {
		t.Fatalf("SI view = %q, want a rounded size", si.Stdout)
	}
	if !strings.Contains(exact.Stdout, "2500 B") || strings.Contains(exact.Stdout, "kB") {
		t.Fatalf("--bytes view = %q, want exact integers", exact.Stdout)
	}
	if row := lineWith(t, si.Stdout, "pic.png"); !strings.HasSuffix(row, " ?") {
		t.Fatalf("denied row = %q, want an unmeasured mark", row)
	}
	if !strings.Contains(si.Stdout, "skipped 1") {
		t.Fatalf("view = %q, want the denied file counted as skipped", si.Stdout)
	}
	if row := lineWith(t, all.Stdout, "pic.png"); !strings.HasSuffix(row, " 4 lines") {
		t.Fatalf("--all-types row = %q, want the denied file's lines", row)
	}
	if strings.Contains(all.Stdout, "skipped") {
		t.Fatalf("--all-types view = %q, want nothing skipped", all.Stdout)
	}
}

// TestScenarioModesAgree proves the worker count changes the speed, never the
// answer, and that the order is fixed before measurement (README I17, I22).
func TestScenarioModesAgree(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	base := itest.RunVisual(t, root, 150, "-r", "0", "-t", "0", "-m", "0", root)
	if base.Code != 0 || base.Stderr != "" {
		t.Fatalf("duls -m 0 = %d, %q; want 0", base.Code, base.Stderr)
	}
	for _, mode := range []string{"1", "2", "3"} {
		got := itest.RunVisual(t, root, 150, "-r", "0", "-t", "0", "-m", mode, root)
		if got != base {
			t.Fatalf("duls -m %s = %d, %q\nwant -m 0 = %d, %q", mode, got.Code, got.Stdout, base.Code, base.Stdout)
		}
	}
}
