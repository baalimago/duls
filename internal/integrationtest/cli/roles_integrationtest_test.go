package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// roleShare is one element of the document's roles array. Lines and Share are
// nil when the count proved no file of the role.
type roleShare struct {
	Role  string `json:"role"`
	Files int    `json:"files"`
	Lines *int64 `json:"lines"`
	Share *int   `json:"share"`
}

// roleTree holds 200 lines in six roles, and one image the deny-list skips.
func roleTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "main.go", 50)
	write(t, root, "cmd/run.go", 30)
	write(t, root, "main_test.go", 40)
	write(t, root, "tests/e2e.py", 20)
	write(t, root, "README.md", 20)
	write(t, root, "docs/setup.go", 10)
	write(t, root, "config.yaml", 10)
	write(t, root, "Makefile", 6)
	write(t, root, "go.sum", 8)
	write(t, root, "vendor/lib/lib.go", 6)
	write(t, root, "pic.png", 4)
	return root
}

// TestScenarioRolesLineSharesTheLines proves the visual view states each
// role's share of the proven lines, largest first, and leaves out a role whose
// lines the count did not prove.
func TestScenarioRolesLineSharesTheLines(t *testing.T) {
	t.Parallel()
	res := itest.RunVisual(t, roleTree(t), 150, "-r", "0")
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	want := "  lines  40% source · 30% test · 15% docs · 8% config · 4% generated · 3% vendored"
	if got := lineWith(t, res.Stdout, "  lines  "); got != want {
		t.Fatalf("role line = %q, want %q", got, want)
	}
	lines := strings.Split(res.Stdout, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "  types  ") && (i+1 >= len(lines) || lines[i+1] != want) {
			t.Fatalf("view =\n%s\nwant the role line right after the type line", res.Stdout)
		}
	}
}

// TestScenarioAgentDocumentStatesTheRoles proves the document carries every
// role the walk saw: its exact file count, its proven lines and their share,
// and null where the count proved no line. The roles add up to the totals.
func TestScenarioAgentDocumentStatesTheRoles(t *testing.T) {
	t.Parallel()
	doc := decodeAgent(t, itest.Run(t, roleTree(t), "-r", "0"))
	want := []struct {
		role  string
		files int
		lines int64
		share int
	}{
		{"source", 2, 80, 40},
		{"test", 2, 60, 30},
		{"docs", 2, 30, 15},
		{"config", 2, 16, 8},
		{"generated", 1, 8, 4},
		{"vendored", 1, 6, 3},
		{"other", 1, -1, -1},
	}
	if len(doc.Roles) != len(want) {
		t.Fatalf("roles = %+v, want %d roles", doc.Roles, len(want))
	}
	var files int
	var lines int64
	for i, w := range want {
		got := doc.Roles[i]
		files += got.Files
		if got.Role != w.role || got.Files != w.files {
			t.Fatalf("roles[%d] = %+v, want %s with %d files", i, got, w.role, w.files)
		}
		if w.lines < 0 {
			if got.Lines != nil || got.Share != nil {
				t.Fatalf("roles[%d] = %+v, want null lines and share for a role the count did not prove", i, got)
			}
			continue
		}
		if got.Lines == nil || *got.Lines != w.lines || got.Share == nil || *got.Share != w.share {
			t.Fatalf("roles[%d] = %+v, want %d lines at %d%%", i, got, w.lines, w.share)
		}
		lines += *got.Lines
	}
	if files != doc.Totals.Files || lines != doc.Totals.Lines {
		t.Fatalf("roles hold %d files and %d lines, want the totals' %+v", files, lines, doc.Totals)
	}
}

// TestScenarioRolesFollowThePathConventions proves a file's role is read from
// its path below the root alone: test, docs and vendor directories, the
// naming conventions of test files, lockfiles and generated code, and the
// precedence between them.
func TestScenarioRolesFollowThePathConventions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		role string
	}{
		{"walk.go", "source"},
		{"walk_test.go", "test"},
		{"src/app.test.ts", "test"},
		{"test_parse.py", "test"},
		{"src/test/java/FooTest.java", "test"},
		{"__tests__/a.js", "test"},
		{"spec/user_spec.rb", "test"},
		{"notes/testdata/a.md", "test"},
		{"README", "docs"},
		{"LICENSE-MIT", "docs"},
		{"docs/conf.py", "docs"},
		{"install.sh", "source"},
		{"Dockerfile", "config"},
		{"deploy.yaml", "config"},
		{"api.pb.go", "generated"},
		{"package-lock.json", "generated"},
		{"tests/api.pb.go", "generated"},
		{"node_modules/x/index.js", "vendored"},
		{"vendor/x/x_test.go", "vendored"},
		{"site/app.min.js", "vendored"},
		{"photo.jpg", "other"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			write(t, root, tc.path, 1)
			doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
			if len(doc.Roles) != 1 || doc.Roles[0].Role != tc.role || doc.Roles[0].Files != 1 {
				t.Fatalf("roles = %+v, want one %s file", doc.Roles, tc.role)
			}
		})
	}
}

// TestScenarioRolesIgnoreTheRootsOwnPath proves the path a role is read from
// starts below the root, so a root named tests holds source like any other.
func TestScenarioRolesIgnoreTheRootsOwnPath(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "tests")
	write(t, root, "main.go", 3)
	doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
	if len(doc.Roles) != 1 || doc.Roles[0].Role != "source" {
		t.Fatalf("roles = %+v, want the file below the root read as source", doc.Roles)
	}
}

// TestScenarioRolesMarkALowerBound proves a run whose line total is a lower
// bound marks every role share as one, and names a role whose lines it could
// not prove as unknown instead of leaving it out.
func TestScenarioRolesMarkALowerBound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "ok.go", 3)
	write(t, root, "secret_test.go", 2)
	secret := filepath.Join(root, "secret_test.go")
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })
	res := itest.RunVisual(t, root, 150, "-r", "0")
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	if got := lineWith(t, res.Stdout, "  lines  "); got != "  lines  ~100% source · ? test" {
		t.Fatalf("role line = %q, want the share marked and the unproven role unknown", got)
	}
}

// TestScenarioRolesLeaveOutExcludedEntries proves an excluded entry adds to no
// role, so the roles still add up to the totals.
func TestScenarioRolesLeaveOutExcludedEntries(t *testing.T) {
	t.Parallel()
	doc := decodeAgent(t, itest.Run(t, roleTree(t), "-r", "0", "--exclude", "vendor"))
	files := 0
	for _, r := range doc.Roles {
		files += r.Files
		if r.Role == "vendored" {
			t.Fatalf("roles = %+v, want no vendored role once vendor is excluded", doc.Roles)
		}
	}
	if files != doc.Totals.Files || doc.Totals.Files != 10 {
		t.Fatalf("roles hold %d files, want the %d the totals report and 10", files, doc.Totals.Files)
	}
}

// TestScenarioBinaryDocumentsAreSkipped proves the count never reads a binary
// document format as lines: such a file is skipped, leaves the line total, and
// stays out of the role line, while --all-types still measures it.
func TestScenarioBinaryDocumentsAreSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "main.go", 10)
	for _, name := range []string{"guide.pdf", "notes.DOCX", "sheet.xlsx", "slides.pptx", "book.epub"} {
		write(t, root, name, 50)
	}
	doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
	if doc.Accounting.Skipped != 5 || doc.Totals.Lines != 10 || !doc.Totals.LinesExact {
		t.Fatalf("totals = %+v, accounting = %+v; want the five documents skipped and 10 exact lines", doc.Totals, doc.Accounting)
	}
	res := itest.RunVisual(t, root, 150, "-r", "0")
	if got := lineWith(t, res.Stdout, "  lines  "); got != "  lines  100% source" {
		t.Fatalf("role line = %q, want the skipped documents left out", got)
	}
	all := decodeAgent(t, itest.Run(t, root, "-r", "0", "--all-types"))
	if all.Accounting.Skipped != 0 || all.Totals.Lines != 260 {
		t.Fatalf("--all-types totals = %+v; want every document measured", all.Totals)
	}
}
