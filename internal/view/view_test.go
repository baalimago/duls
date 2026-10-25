package view_test

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
	"github.com/baalimago/duls/internal/tui"
	"github.com/baalimago/duls/internal/view"
)

func TestVisualAnswerCarriesTheLayout(t *testing.T) {
	raw := view.New().Lines(fullView(), view.Options{Width: 150, Style: tui.New(tui.Basic)})
	if !strings.Contains(raw[0], "\x1b[") {
		t.Fatalf("line 1 carries no colour: %q", raw[0])
	}
	got := body(fullView(), view.Options{Width: 150, Style: tui.New(tui.Basic)})
	if len(got) < 3 || got[0] != "  42 files · 1.5 MB · 900 lines" {
		t.Fatalf("body = %q, want the totals line first", got)
	}
	for _, line := range got {
		if strings.ContainsRune(line, '\x1b') {
			t.Fatalf("body line carries an escape sequence: %q", line)
		}
		if strings.Contains(line, "■") {
			t.Fatalf("body line carries a row box: %q", line)
		}
	}
}

func TestStyledViewWithoutRowsStartsAtTheTotalsLine(t *testing.T) {
	v := plan.View{Totals: plan.Totals{Lines: -1}}
	for _, tt := range []struct {
		name        string
		provisional bool
	}{
		{"an answer", false},
		{"a frame's first paint", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lines := body(v, view.Options{Width: 150, Style: tui.New(tui.TrueColor), Provisional: tt.provisional})
			if len(lines) != 2 {
				t.Fatalf("a view without rows is %d lines, want the totals and the header: %q", len(lines), lines)
			}
			if !strings.Contains(stripANSI(lines[0]), "files") {
				t.Fatalf("line 1 = %q, want the totals line", stripANSI(lines[0]))
			}
			if strings.Contains(lines[0], "█") || lines[1] == "" {
				t.Fatalf("a view without rows opens with a strip or a blank line: %q", lines)
			}
		})
	}
}

func TestWidthBounds(t *testing.T) {
	for _, width := range []int{1, 40, 60, 150, 400} {
		want := min(max(width, 40), 150)
		lines := body(fullView(), view.Options{Width: width, Style: tui.New(tui.TrueColor)})
		for _, line := range lines {
			if got := utf8.RuneCountInString(stripANSI(line)); got > want {
				t.Fatalf("width %d line is %d runes: %q", width, got, stripANSI(line))
			}
		}
	}
}

func TestUnknownAndBoundMarks(t *testing.T) {
	unknown := plan.View{Totals: plan.Totals{Files: 1, Bytes: 10, BytesExact: false, Lines: -1}}
	line := body(unknown, view.Options{Width: 150})[0]
	if !strings.Contains(line, "?") || !strings.Contains(line, "~") {
		t.Fatalf("totals line = %q, want ? and ~ marks", line)
	}
	bound := plan.View{Totals: plan.Totals{Files: 1, Bytes: 10, BytesExact: true, Lines: 5, LinesExact: false}}
	line = body(bound, view.Options{Width: 150})[0]
	if !strings.Contains(line, "~5") {
		t.Fatalf("totals line = %q, want a ~5 lower bound", line)
	}
	if strings.Contains(line, "0 lines") {
		t.Fatalf("totals line = %q, renders an unknown as zero", line)
	}
}

func TestRowColumns(t *testing.T) {
	lines := body(fullView(), view.Options{Width: 150})
	header := lines[1]
	for _, label := range []string{"entry", "files", "size", "share"} {
		if !strings.Contains(header, label) {
			t.Fatalf("header = %q, lacks %q", header, label)
		}
	}
	dirRow := rowContaining(t, lines, "src")
	fileRow := rowContaining(t, lines, "README.md")
	if !strings.Contains(dirRow, "30") || !strings.Contains(dirRow, "1.2 MB") || !strings.Contains(dirRow, "80%") {
		t.Fatalf("directory row = %q, want files, size and share", dirRow)
	}
	if !strings.Contains(fileRow, "40 lines") {
		t.Fatalf("file row = %q, want its line count", fileRow)
	}
}

func TestPeekFitting(t *testing.T) {
	entry := plan.Entry{
		Name: "src", Kind: plan.Directory, Files: 6, Bytes: 600, Exact: true, Share: 100,
		Kids: []plan.Item{
			{Name: "alpha.go", Lines: 1},
			{Name: "bravo.go", Lines: 2},
			{Name: "charlie.go", Lines: 3},
			{Name: "delta.go", Lines: 4},
			{Name: "echo.go", Lines: 5},
			{Name: "foxtrot.go", Lines: 6},
		},
		More: 3,
	}
	v := plan.View{Totals: plan.Totals{Files: 6, Bytes: 600, BytesExact: true, Lines: 21, LinesExact: true}, Entries: []plan.Entry{entry}}
	wide := rowContaining(t, body(v, view.Options{Width: 150}), "src")
	for _, name := range []string{"alpha.go", "bravo.go", "charlie.go", "delta.go", "echo.go", "foxtrot.go", "+3 more"} {
		if !strings.Contains(wide, name) {
			t.Fatalf("wide peek = %q, lacks %q", wide, name)
		}
	}
	narrow := rowContaining(t, body(v, view.Options{Width: 40}), "src")
	if !strings.Contains(narrow, "…") {
		t.Fatalf("narrow peek = %q, want a … cut", narrow)
	}
}

func TestPeekMarksALowerBoundChild(t *testing.T) {
	entry := plan.Entry{
		Name: "src", Kind: plan.Directory, Files: 6, Bytes: 600, Share: 100,
		Kids: []plan.Item{
			{Name: "lib", Dir: true, Files: 4, Exact: true},
			{Name: "docs", Dir: true, Files: 2},
		},
	}
	v := plan.View{Totals: plan.Totals{Files: 6, Bytes: 600}, Entries: []plan.Entry{entry}}
	row := rowContaining(t, body(v, view.Options{Width: 150}), "src")
	for _, want := range []string{"lib 4", "docs ~2"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row = %q, lacks %q", row, want)
		}
	}
}

func TestStripSpansTheWidth(t *testing.T) {
	for _, width := range []int{40, 60, 100, 150} {
		lines := view.New().Lines(fullView(), view.Options{Width: width, Style: tui.New(tui.TrueColor)})
		if got := utf8.RuneCountInString(stripANSI(lines[0])); got != width {
			t.Fatalf("strip at width %d is %d columns, want the whole width", width, got)
		}
	}
}

func TestProvisionalMarksOnlyTheLineTotal(t *testing.T) {
	v := plan.View{Totals: plan.Totals{
		Files: 6, Bytes: 420, BytesExact: true, Lines: 12, LinesExact: true,
		Skipped: 2, Unreadable: 1,
	}}
	lines := body(v, view.Options{Width: 150, Limit: 2 * time.Second, Provisional: true})
	if len(lines) != 2 {
		t.Fatalf("a provisional view is %d lines, want the totals and the header: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "6 files") || !strings.Contains(lines[0], "420 B") {
		t.Fatalf("totals = %q, want the proved files and bytes unmarked", lines[0])
	}
	if !strings.Contains(lines[0], "~12 lines") {
		t.Fatalf("totals = %q, want the line total a lower bound", lines[0])
	}
	answer := body(v, view.Options{Width: 150, Limit: 2 * time.Second})
	if !strings.Contains(answer[0], "12 lines") || !strings.Contains(answer[len(answer)-1], "skipped 2") {
		t.Fatalf("the answer = %q, want an exact total and its footer", answer)
	}
}

func TestFooterOnCut(t *testing.T) {
	v := plan.View{Totals: plan.Totals{
		Files: 1, Bytes: 10, BytesExact: false, Lines: 0, LinesExact: false, Stopped: scan.StopDeadline,
	}}
	lines := body(v, view.Options{Width: 150, Limit: 2 * time.Second})
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "walk") || !strings.Contains(footer, "2s") {
		t.Fatalf("footer = %q, want the stopped phase and the limit", footer)
	}
	if strings.Contains(footer, "ago") || strings.Contains(footer, "elapsed") {
		t.Fatalf("footer = %q, reports elapsed time", footer)
	}
}

func TestFooterNamesTheStoppedPhase(t *testing.T) {
	for _, tt := range []struct {
		name  string
		phase scan.Phase
		want  string
	}{
		{"a cut walk", scan.PhaseWalk, "walk"},
		{"a cut count", scan.PhaseCount, "count"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := plan.View{Totals: plan.Totals{
				Files: 1, Bytes: 10, BytesExact: false, Lines: 0, LinesExact: false,
				Stopped: scan.StopDeadline, StoppedPhase: tt.phase,
			}}
			lines := body(v, view.Options{Width: 150, Limit: 2 * time.Second})
			if footer := lines[len(lines)-1]; !strings.Contains(footer, tt.want) || !strings.Contains(footer, "2s") {
				t.Fatalf("footer = %q, want the %s deadline and the limit", footer, tt.want)
			}
		})
	}
}

func TestTotalsMarkOnlyTheFactsTheRunCannotProve(t *testing.T) {
	for _, tt := range []struct {
		name      string
		totals    plan.Totals
		wantFiles bool
	}{
		{"a cut walk", plan.Totals{
			Files: 2, Bytes: 10, BytesExact: true, Lines: 0, LinesExact: false,
			Stopped: scan.StopDeadline, StoppedPhase: scan.PhaseWalk,
		}, true},
		{"a cut count", plan.Totals{
			Files: 2, Bytes: 10, BytesExact: true, Lines: 0, LinesExact: false,
			Stopped: scan.StopDeadline, StoppedPhase: scan.PhaseCount,
		}, false},
		{"a refused directory", plan.Totals{
			Files: 2, Bytes: 10, BytesExact: false, Lines: 0, LinesExact: false,
			UnreadableDirs: 1,
		}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := plan.View{Totals: tt.totals}
			totals := body(v, view.Options{Width: 150})[0]
			marked := strings.Contains(totals, "~2 files")
			if marked != tt.wantFiles {
				t.Fatalf("totals = %q, marked = %v; want %v", totals, marked, tt.wantFiles)
			}
			if !strings.Contains(totals, "~0 lines") {
				t.Fatalf("totals = %q, want the line total a lower bound", totals)
			}
		})
	}
}

func TestFooterCounts(t *testing.T) {
	v := fullView()
	v.Totals.Skipped = 3
	v.Totals.Unknown = 1
	v.Totals.Unreadable = 2
	v.Totals.UnreadableDirs = 1
	v.Totals.OtherFilesystems = 2
	v.Totals.LinesExact = false
	lines := body(v, view.Options{Width: 150})
	footer := lines[len(lines)-1]
	for _, want := range []string{"skipped 3", "unknown 1", "unreadable 2", "unreadable dirs 1", "other filesystems 2"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer = %q, lacks %q", footer, want)
		}
	}
	for _, row := range lines[2 : len(lines)-1] {
		for _, word := range []string{"skipped", "unknown", "unreadable"} {
			if strings.Contains(row, word) {
				t.Fatalf("row = %q carries the footer's %q", row, word)
			}
		}
	}
}

func TestFooterCacheToken(t *testing.T) {
	reused := fullView()
	reused.Totals.Reused = 2
	lines := body(reused, view.Options{Width: 150})
	if footer := lines[len(lines)-1]; !strings.Contains(footer, "cache reused") {
		t.Fatalf("footer = %q, want the cache token", footer)
	}
	cold := fullView()
	if lines := body(cold, view.Options{Width: 150}); strings.Contains(strings.Join(lines, "\n"), "cache") {
		t.Fatal("a cold view carries a cache token")
	}
}

func TestSIUnits(t *testing.T) {
	for _, tt := range []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1_000, "1.0 kB"},
		{999_499, "999.5 kB"},
		{999_500, "1.0 MB"},
		{1e12, "1.0 TB"},
	} {
		v := plan.View{Totals: plan.Totals{Files: 1, Bytes: tt.size, BytesExact: true, Lines: 0, LinesExact: true}}
		line := body(v, view.Options{Width: 150})[0]
		if !strings.Contains(line, tt.want) {
			t.Fatalf("size %d totals line = %q, want %q", tt.size, line, tt.want)
		}
	}
	v := plan.View{Totals: plan.Totals{Files: 1, Bytes: 999_500, BytesExact: true, Lines: 0, LinesExact: true}}
	line := body(v, view.Options{Width: 150, Bytes: true})[0]
	if !strings.Contains(line, "999500 B") {
		t.Fatalf("--bytes totals line = %q, want the exact integer", line)
	}
	empty := body(plan.View{}, view.Options{Width: 150})[0]
	if !strings.Contains(empty, "0 files") || !strings.Contains(empty, "0 B") || !strings.Contains(empty, "0 lines") {
		t.Fatalf("empty totals line = %q", empty)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	v := fullView()
	want := body(v, view.Options{Width: 100, Style: tui.New(tui.TrueColor)})
	for range 100 {
		got := body(v, view.Options{Width: 100, Style: tui.New(tui.TrueColor)})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("render is not deterministic:\n got %q\nwant %q", got, want)
		}
	}
}

func fullView() plan.View {
	return plan.View{
		Totals: plan.Totals{
			Files: 42, Bytes: 1_500_000, BytesExact: true, Lines: 900, LinesExact: true, Measured: 40,
		},
		Entries: []plan.Entry{
			{
				Name: "src", Kind: plan.Directory, Files: 30, Bytes: 1_200_000, Exact: true, Share: 80, Color: 1,
				Kids: []plan.Item{
					{Name: "main.go", Lines: 120},
					{Name: "walk.go", Lines: 80},
					{Name: "lib", Dir: true, Files: 4, Exact: true},
				},
				More: 2,
			},
			{
				Name: "docs", Kind: plan.Directory, Files: 10, Bytes: 200_000, Exact: true, Share: 13, Color: 2,
				Kids: []plan.Item{{Name: "readme.md", Lines: 40}},
			},
			{
				Name: "README.md", Kind: plan.File, Bytes: 100_000, Exact: true, Lines: 40, Share: 7, Color: 0,
			},
			{
				Kind: plan.Folded, Folded: []string{"a", "b", "c"}, Files: 2, Bytes: 300_000, Exact: true, Share: 20,
			},
		},
	}
}

func rowContaining(t *testing.T, lines []string, text string) string {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no line contains %q: %q", text, lines)
	return ""
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansi.ReplaceAllString(s, "")
}

// body is the visual answer as its layout text: the strip and the blank line
// after it dropped, every escape sequence removed, and every row box turned
// back into its two-column margin. The layout is the same in every test, so a
// golden reads as a person does, without the presentation.
func body(v plan.View, opts view.Options) []string {
	var out []string
	for _, line := range view.New().Lines(v, opts) {
		line = stripANSI(line)
		if strings.HasPrefix(line, "█") {
			continue
		}
		if line == "" && len(out) == 0 {
			continue
		}
		out = append(out, strings.Replace(line, "  ■ ", "  ", 1))
	}
	return out
}

func TestCountsPrintSI(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files int
		lines int64
		want  string
	}{
		{"below the knee", 999, 0, "999 files"},
		{"at the knee", 1000, 0, "1.0k files"},
		{"thousands", 1234, 0, "1.2k files"},
		{"below the mega knee", 999_499, 0, "999.5k files"},
		{"at the mega knee", 999_500, 0, "1.0M files"},
		{"a long line total", 1, 12_104_952, "12.1M lines"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := plan.View{Totals: plan.Totals{Files: tt.files, Lines: tt.lines, LinesExact: true}}
			line := body(v, view.Options{Width: 150})[0]
			if !strings.Contains(line, tt.want) {
				t.Fatalf("totals line = %q, want %q", line, tt.want)
			}
		})
	}
	exact := plan.View{Totals: plan.Totals{Files: 1234, Lines: 0, LinesExact: true}}
	if line := body(exact, view.Options{Width: 150, Bytes: true})[0]; !strings.Contains(line, "1.2k files") {
		t.Fatalf("--bytes totals line = %q, want the count still in SI units", line)
	}
}

func TestTypeLine(t *testing.T) {
	types := []plan.TypeShare{
		{Ext: ".go", Files: 62, Share: 62},
		{Ext: ".json", Files: 12, Share: 12},
		{Ext: "", Files: 9, Share: 9},
		{Ext: ".md", Files: 8, Share: 8},
	}
	v := plan.View{Totals: plan.Totals{Files: 100, Lines: 0, LinesExact: true, Types: types, TypesMore: 3}}
	lines := body(v, view.Options{Width: 150})
	if want := "  types  62% .go · 12% .json · 9% (none) · 8% .md · +3 more"; lines[1] != want {
		t.Fatalf("type line = %q, want %q", lines[1], want)
	}
	if !strings.HasPrefix(lines[2], "  entry") {
		t.Fatalf("line 3 = %q, want the header", lines[2])
	}
	narrow := body(v, view.Options{Width: 40})
	if got := utf8.RuneCountInString(narrow[1]); got > 40 || !strings.HasSuffix(narrow[1], "…") {
		t.Fatalf("narrow type line is %d runes: %q", got, narrow[1])
	}
	plain := plan.View{Totals: plan.Totals{Files: 1, Lines: 0, LinesExact: true}}
	if line := body(plain, view.Options{Width: 150})[1]; !strings.HasPrefix(line, "  entry") {
		t.Fatalf("a census-free view has a type line: %q", line)
	}
}

func TestFooterReportsTheFilesTheCountNeverReached(t *testing.T) {
	v := plan.View{Totals: plan.Totals{
		Files: 1000, Measured: 120, Skipped: 10, Unknown: 5, Lines: 0, LinesExact: false,
		Stopped: scan.StopDeadline, StoppedPhase: scan.PhaseCount,
	}}
	lines := body(v, view.Options{Width: 150, Limit: 2 * time.Second})
	if footer := lines[len(lines)-1]; !strings.Contains(footer, "not scanned 865") {
		t.Fatalf("footer = %q, want the 865 files the count never dispatched", footer)
	}
	complete := plan.View{Totals: plan.Totals{Files: 1000, Measured: 990, Skipped: 10, Lines: 0, LinesExact: true}}
	for _, line := range body(complete, view.Options{Width: 150}) {
		if strings.Contains(line, "not scanned") {
			t.Fatalf("a completed count reports unscanned files: %q", line)
		}
	}
}

func TestFooterStatesTheDeviceUsage(t *testing.T) {
	device := &scan.Filesystem{Total: 1_000_000_000, Used: 400_000_000, Avail: 500_000_000}
	v := plan.View{Totals: plan.Totals{
		Files: 10, Bytes: 5, UnreadableDirs: 1, Filesystem: device, BytesExact: false, Lines: -1,
	}}
	footer := body(v, view.Options{Width: 150})
	if last := footer[len(footer)-1]; !strings.Contains(last, "filesystem 400.0 MB used of 1.0 GB") {
		t.Fatalf("footer = %q, want the device usage", last)
	}
	exact := body(v, view.Options{Width: 150, Bytes: true})
	if last := exact[len(exact)-1]; !strings.Contains(last, "filesystem 400000000 B used of 1000000000 B") {
		t.Fatalf("footer = %q, want exact device bytes", last)
	}
}

func TestFooterWrapsAtNarrowWidths(t *testing.T) {
	v := plan.View{Totals: plan.Totals{
		Files: 1000, Measured: 120, Skipped: 10, Unknown: 5, Unreadable: 2, Reused: 1,
		Lines: 0, LinesExact: false, Stopped: scan.StopDeadline, StoppedPhase: scan.PhaseCount,
	}}
	narrow := body(v, view.Options{Width: 40, Limit: 2 * time.Second})
	if len(narrow) < 4 {
		t.Fatalf("view = %q, want a footer that continues on a second line", narrow)
	}
	for _, line := range narrow {
		if got := utf8.RuneCountInString(line); got > 40 {
			t.Fatalf("line is %d columns: %q", got, line)
		}
	}
	joined := strings.Join(narrow, "\n")
	for _, want := range []string{"partial: count deadline 2s", "not scanned 865", "skipped 10", "unknown 5", "unreadable 2", "cache reused"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("narrow view lacks %q:\n%s", want, joined)
		}
	}
	wide := body(v, view.Options{Width: 150, Limit: 2 * time.Second})
	footer := wide[len(wide)-1]
	if !strings.Contains(footer, "partial: count deadline 2s") || !strings.Contains(footer, "cache reused") {
		t.Fatalf("footer = %q, want the whole footer on one line", footer)
	}
}
