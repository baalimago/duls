package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/baalimago/duls/internal/tui"
)

var escape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// visualForm forces the visual answer, so a test reads the layout a terminal
// would get without owning a terminal.
func visualForm(io.Writer) tui.Form { return tui.Visual }

// body is the visual answer as its layout text: the strip and the blank line
// after it dropped, every escape sequence removed, and every row box turned
// back into its two-column margin.
func body(out *bytes.Buffer) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSuffix(escape.ReplaceAllString(answer(out), ""), "\n"), "\n") {
		if strings.HasPrefix(line, "\u2588") {
			continue
		}
		if line == "" && len(lines) == 0 {
			continue
		}
		lines = append(lines, strings.Replace(line, "  \u25a0 ", "  ", 1))
	}
	return lines
}

// answer is the answer a visual run wrote last: everything after the frame's
// final erase, or the whole stream when no frame was drawn.
func answer(out *bytes.Buffer) string {
	raw := out.String()
	if cut := strings.LastIndex(raw, "\x1b[2K"); cut >= 0 {
		return raw[cut+len("\x1b[2K"):]
	}
	return raw
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "README.md", strings.Repeat("line\n", 20))
	writeFile(t, root, "meetings/a.md", strings.Repeat("line\n", 10))
	writeFile(t, root, "meetings/b.md", strings.Repeat("line\n", 10))
	writeFile(t, root, "meetings/c.md", strings.Repeat("line\n", 10))
	writeFile(t, root, "people/ana.md", strings.Repeat("line\n", 17))
	writeFile(t, root, "people/ben.md", strings.Repeat("line\n", 17))
	return root
}

func TestRunRendersTheDirectory(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Stdout: &out, Cwd: root, Form: visualForm}, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	view := strings.Join(body(&out), "\n")
	if !strings.HasPrefix(view, "  6 files · 420 B · 84 lines\n") {
		t.Fatalf("view = %q, want the bytes-first totals", view)
	}
	people := strings.Index(view, "people")
	meetings := strings.Index(view, "meetings")
	readme := strings.Index(view, "README.md")
	if !(people < meetings && meetings < readme) {
		t.Fatalf("rows are not ordered by bytes: %q", view)
	}
	if !strings.Contains(view, "84 lines") || strings.Contains(view, "partial") {
		t.Fatalf("view = %q, want a complete view", view)
	}
}

func TestRunEmptyDirectory(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Stdout: &out, Cwd: t.TempDir(), Form: visualForm}, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := body(&out)[0]; !strings.HasPrefix(got, "  0 files · 0 B · 0 lines") {
		t.Fatalf("view = %q, want the empty totals", got)
	}
}

func TestRunStyledEmptyViewStartsAtTheTotalsLine(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	opts := Options{
		Stdout: &out,
		Cwd:    t.TempDir(),
		Style:  func() tui.Style { return tui.New(tui.TrueColor) },
		Form:   visualForm,
		Width:  func(io.Writer) int { return 100 },
	}
	if err := Run(context.Background(), opts, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(escape.ReplaceAllString(answer(&out), ""), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("an empty styled view is %d lines, want the totals and the header: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "  0 files · 0 B · 0 lines") {
		t.Fatalf("line 1 = %q, want the totals line", lines[0])
	}
}

func TestRunAllIncludesHiddenEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a.md", "a\n")
	writeFile(t, root, ".hidden.md", "b\n")
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Stdout: &out, Cwd: root, Form: visualForm}, Request{All: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := body(&out)[0]; !strings.HasPrefix(got, "  2 files · 4 B · 2 lines") {
		t.Fatalf("view = %q, want both files", got)
	}
}

func TestRunOnAStreamWithoutATerminalWritesTheAgentDocument(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Stdout: &out, Cwd: root}, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	doc := out.String()
	if !json.Valid([]byte(doc)) {
		t.Fatalf("the agent answer is not JSON: %q", doc)
	}
	for _, want := range []string{`"root":"`, `"totals":{"files":6,"bytes":420,"lines":84`, `"entries":[`, `"accounting":{"stopped":"none"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document = %s, want it to hold %s", doc, want)
		}
	}
	if strings.Contains(doc, "\x1b") {
		t.Fatalf("the agent answer carries an escape sequence: %q", doc)
	}
}

func TestRunJSONWinsOverAVisualStream(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	opts := Options{Stdout: &out, Cwd: fixture(t), Form: visualForm}
	if err := Run(context.Background(), opts, Request{JSON: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("duls -json on a terminal = %q, want the agent document", out.String())
	}
}

func TestRunStyledAdaptsToTheWidthSeam(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 20 {
		writeFile(t, root, "team-"+string(rune('a'+i))+"/note.md", "line\n")
	}
	var out bytes.Buffer
	opts := Options{
		Stdout: &out,
		Cwd:    root,
		Style:  func() tui.Style { return tui.New(tui.TrueColor) },
		Form:   visualForm,
		Width:  func(io.Writer) int { return 60 },
	}
	if err := Run(context.Background(), opts, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[38;2;") || !strings.Contains(out.String(), "█") {
		t.Fatalf("styled view carries no colour or strip: %q", out.String())
	}
	lines := strings.Split(strings.TrimSuffix(escape.ReplaceAllString(answer(&out), ""), "\n"), "\n")
	if got := utf8.RuneCountInString(lines[0]); got != 60 {
		t.Fatalf("strip is %d columns, want the 60 of a 60-column view", got)
	}
	for _, line := range lines {
		if got := utf8.RuneCountInString(line); got > 60 {
			t.Fatalf("line is %d columns, want at most 60: %q", got, line)
		}
	}
}

func TestRunFrameStartsAtOnce(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 20 {
		writeFile(t, root, "team-"+string(rune('a'+i))+"/note.md", "line\n")
	}
	var out bytes.Buffer
	opts := Options{
		Stdout: &out,
		Cwd:    root,
		Style:  func() tui.Style { return tui.New(tui.TrueColor) },
		Form:   visualForm,
		Width:  func(io.Writer) int { return 100 },
	}
	if err := Run(context.Background(), opts, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw := out.String()
	cut := strings.LastIndex(raw, "\x1b[2K")
	if cut < 0 {
		t.Fatalf("the run drew no frame before its answer: %q", raw)
	}
	if frame := escape.ReplaceAllString(raw[:cut], ""); !strings.Contains(frame, "~") {
		t.Fatalf("the frame stated no lower bound: %q", frame)
	}
	last := answer(&out)
	if strings.Contains(last, "~") || strings.Contains(last, "partial") {
		t.Fatalf("the complete answer carries a mark or a footer: %q", last)
	}
}

func TestRunRejectsAMissingDirectory(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := Run(context.Background(), Options{Stdout: &out, Cwd: t.TempDir()}, Request{Path: "gone"})
	if err == nil || !strings.Contains(err.Error(), "inspect") {
		t.Fatalf("error = %v, want an inspect failure", err)
	}
}

func TestRunResolvesASymlinkedRoot(t *testing.T) {
	t.Parallel()
	real := t.TempDir()
	writeFile(t, real, "a.md", "a\n")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Stdout: &out, Cwd: link, Form: visualForm}, Request{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := body(&out)[0]; !strings.HasPrefix(got, "  1 file · 2 B · 1 line") {
		t.Fatalf("view = %q, want the linked directory's file", got)
	}
}

func TestRunRejectsAFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a.md", "a\n")
	err := Run(context.Background(), Options{Stdout: &bytes.Buffer{}, Cwd: root}, Request{Path: "a.md"})
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("error = %v, want a not-a-directory failure", err)
	}
}

func TestRunStopsOnACanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, Options{Stdout: &bytes.Buffer{}, Cwd: t.TempDir()}, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// failingWriter refuses every write, so the run reports one write error.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestRunReportsAWriteFailure(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), Options{Stdout: failingWriter{}, Cwd: fixture(t)}, Request{})
	if err == nil || !strings.Contains(err.Error(), "write: boom") {
		t.Fatalf("error = %v, want the write failure", err)
	}
}

func TestResolveRoot(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	notes := filepath.Join(work, "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		cwd  string
		path string
		want string
	}{
		{"the empty path is the working directory", work, "", work},
		{"a relative path joins the working directory", work, "notes", notes},
		{"an absolute path is cleaned", work, filepath.Join(work, "notes", "..", "notes"), notes},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveRoot(tt.cwd, tt.path)
			if err != nil {
				t.Fatalf("resolveRoot: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveRoot(%q, %q) = %q, want %q", tt.cwd, tt.path, got, tt.want)
			}
		})
	}
}
