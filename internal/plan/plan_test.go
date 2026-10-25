package plan_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
)

func TestFoldKeepsEightRows(t *testing.T) {
	dirs := []scan.Dir{{Path: "", Files: 20, Bytes: 200}}
	for i := range 20 {
		dirs = append(dirs, scan.Dir{Path: fmt.Sprintf("d%02d", i), Files: 1, Bytes: 10})
	}
	view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 20, Bytes: 200}, plan.Measured{})
	if len(view.Entries) != 8 {
		t.Fatalf("entries = %d, want 8", len(view.Entries))
	}
	last := view.Entries[7]
	if last.Kind != plan.Folded || len(last.Folded) != 13 {
		t.Fatalf("last row = %+v, want the 13 folded entries", last)
	}
}

func TestOrderAndShare(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 5, Bytes: 100},
		{Path: "a", Files: 1, Bytes: 50},
		{Path: "b", Files: 3, Bytes: 30},
		{Path: "b/sub", Files: 2, Bytes: 20},
		{Path: "c", Files: 1, Bytes: 20},
	}
	view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 5, Bytes: 100}, plan.Measured{})
	var names []string
	var shares []int
	for _, entry := range view.Entries {
		names = append(names, entry.Name)
		shares = append(shares, entry.Share)
	}
	if want := []string{"a", "b", "sub", "c"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("row order = %v, want %v", names, want)
	}
	if want := []int{50, 30, 20, 20}; !reflect.DeepEqual(shares, want) {
		t.Fatalf("shares = %v, want %v", shares, want)
	}
}

func TestRowKinds(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 3, Bytes: 30, Kids: []scan.Child{
			{Name: "one.txt", Bytes: 10, Lines: -1},
			{Name: "d", Dir: true, Files: 2, Bytes: 20, Lines: -1},
		}},
		{Path: "d", Files: 2, Bytes: 20},
	}
	measured := plan.Measured{Lines: map[string]int64{"one.txt": 10, "d/x": 5, "d/y": 5}, Total: 20, Measured: 3}
	view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 3, Bytes: 30}, measured)
	if len(view.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(view.Entries))
	}
	dirRow, fileRow := view.Entries[0], view.Entries[1]
	if fileRow.Kind != plan.File || fileRow.Files != 0 || fileRow.Lines != 10 {
		t.Fatalf("file row = %+v, want a file row with 10 lines and no files", fileRow)
	}
	if dirRow.Kind != plan.Directory || dirRow.Files != 2 || dirRow.Bytes != 20 {
		t.Fatalf("dir row = %+v, want the subtree totals", dirRow)
	}
}

func TestKidsAndMore(t *testing.T) {
	kids := make([]scan.Child, 6)
	for i := range kids {
		kids[i] = scan.Child{Name: fmt.Sprintf("f%d", i), Bytes: int64(10 - i), Lines: -1}
	}
	dirs := []scan.Dir{
		{Path: "", Files: 10, Bytes: 55},
		{Path: "d", Files: 10, Bytes: 55, Kids: kids},
	}
	view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 10, Bytes: 55}, plan.Measured{})
	row := view.Entries[0]
	if len(row.Kids) != 6 {
		t.Fatalf("kids = %d, want 6", len(row.Kids))
	}
	if row.More != 4 {
		t.Fatalf("More = %d, want 4", row.More)
	}
	if row.Kids[0].Name != "f0" || row.Kids[5].Name != "f5" {
		t.Fatalf("kids order = %v, want largest first", row.Kids)
	}
}

func TestPromotionFillsSlots(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 8, Bytes: 125},
		{Path: "alpha", Files: 5, Bytes: 100},
		{Path: "beta", Files: 3, Bytes: 25},
		{Path: "beta/deep", Files: 3, Bytes: 25},
		{Path: "beta/deep/small", Files: 2, Bytes: 20},
		{Path: "beta/deep/single", Files: 1, Bytes: 5},
	}
	view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 8, Bytes: 125}, plan.Measured{})
	var names []string
	for _, entry := range view.Entries {
		names = append(names, entry.Name)
	}
	if want := []string{"alpha", "beta", "deep", "small"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rows = %v, want %v", names, want)
	}
	promoted := view.Entries[3]
	if promoted.Depth != 2 {
		t.Fatalf("promoted depth = %d, want 2", promoted.Depth)
	}
}

func TestCutWalkIsMarked(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 4, Bytes: 170, Kids: []scan.Child{
			{Name: "a", Dir: true, Files: 1, Bytes: 100},
			{Name: "b", Dir: true, Files: 3, Bytes: 70},
		}},
		{Path: "a", Files: 1, Bytes: 100},
		{Path: "b", Files: 3, Bytes: 70, Kids: []scan.Child{
			{Name: "x", Bytes: 40, Lines: -1},
			{Name: "c", Dir: true, Files: 2, Bytes: 30},
		}},
		{Path: "b/c", Files: 2, Bytes: 30},
	}
	snap := scan.Snapshot{Dirs: dirs, Files: 4, Bytes: 170, Stopped: scan.StopDeadline, Frontier: []string{"b/c/d"}}
	measured := plan.Measured{Lines: map[string]int64{"b/x": 5}, Total: 5, Measured: 1, Unknown: 1}
	view := plan.New().Plan(snap, measured)
	if view.Totals.BytesExact {
		t.Fatal("a cut walk reports exact bytes")
	}
	if !view.Entries[0].Exact {
		t.Fatal("the complete row is marked inexact")
	}
	if view.Entries[1].Exact {
		t.Fatal("the cut row is marked exact")
	}
	kids := view.Entries[1].Kids
	if len(kids) != 2 {
		t.Fatalf("cut row shows %d kids, want the 2 children it proved", len(kids))
	}
	if kids[0].Name != "x" || kids[0].Lines != 5 || !kids[0].Exact {
		t.Fatalf("first kid = %+v, want the file it measured exactly", kids[0])
	}
	if kids[1].Name != "c" || kids[1].Exact {
		t.Fatalf("second kid = %+v, want the unfinished subtree marked a lower bound", kids[1])
	}
	if view.Totals.Lines != 5 || view.Totals.LinesExact {
		t.Fatalf("lines = %d exact=%v, want 5 and inexact", view.Totals.Lines, view.Totals.LinesExact)
	}
	if view.Totals.Bytes == 0 {
		t.Fatal("a cut walk reports zero bytes")
	}
}

func TestCutCountIsMarked(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 2, Bytes: 100, Kids: []scan.Child{
			{Name: "a", Bytes: 60, Lines: -1},
			{Name: "b", Bytes: 40, Lines: -1},
		}},
	}
	snap := scan.Snapshot{Dirs: dirs, Files: 2, Bytes: 100}
	measured := plan.Measured{Lines: map[string]int64{"a": 5}, Total: 5, Measured: 1, Stopped: scan.StopDeadline}
	view := plan.New().Plan(snap, measured)
	totals := view.Totals
	if totals.Lines != 5 || totals.LinesExact {
		t.Fatalf("lines = %d exact=%v, want 5 and a lower bound", totals.Lines, totals.LinesExact)
	}
	if totals.Stopped != scan.StopDeadline || totals.StoppedPhase != scan.PhaseCount {
		t.Fatalf("stopped = %v at %v, want the count's deadline", totals.Stopped, totals.StoppedPhase)
	}
	if !totals.BytesExact {
		t.Fatal("a cut count calls the walk's bytes a lower bound")
	}
}

// TestFileRowIsExactWhileTheRootIsListing proves a file row states its own bytes,
// so an unfinished root does not mark it a lower bound: a subtree can be open, a
// file cannot (Plan.8, architecture/plan.md).
func TestFileRowIsExactWhileTheRootIsListing(t *testing.T) {
	snap := scan.Snapshot{
		Dirs: []scan.Dir{{
			Path: "", Files: 1, Bytes: 60,
			Kids: []scan.Child{{Name: "big.bin", Bytes: 60, Lines: -1}},
		}},
		Files: 1, Bytes: 60, Stopped: scan.StopDeadline, Frontier: []string{""},
	}
	view := plan.New().Plan(snap, plan.Measured{})
	if len(view.Entries) != 1 || view.Entries[0].Kind != plan.File {
		t.Fatalf("rows = %+v, want the file row of the root", view.Entries)
	}
	if !view.Entries[0].Exact {
		t.Fatal("the file row is marked a lower bound, want its own exact bytes")
	}
}

// TestRefusedSizeMarksTheBytes proves a file whose size the walk could not read
// leaves the byte total a lower bound: the run counted a file it cannot weigh
// (Plan.8, architecture/plan.md).
func TestRefusedSizeMarksTheBytes(t *testing.T) {
	snap := scan.Snapshot{
		Dirs:       []scan.Dir{{Path: "", Files: 1}},
		Files:      1,
		Unreadable: 1,
		Filesystem: &scan.Filesystem{Total: 100, Used: 40, Avail: 60},
	}
	view := plan.New().Plan(snap, plan.Measured{})
	if view.Totals.BytesExact {
		t.Fatal("bytes are exact, want a lower bound while a file's size is refused")
	}
	if view.Totals.Filesystem == nil {
		t.Fatal("no device, want the device beside a lower-bound total")
	}
	if view.Totals.Unreadable != 1 {
		t.Fatalf("unreadable = %d, want the refused file", view.Totals.Unreadable)
	}
}

func TestOpenSubtreeIsMarked(t *testing.T) {
	for _, tt := range []struct {
		name              string
		frontier          string
		unreadable, other int
	}{
		{name: "refused", frontier: "a/locked", unreadable: 1},
		{name: "other filesystem", frontier: "a/mounted", other: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snap := scan.Snapshot{
				Dirs: []scan.Dir{
					{Path: "", Files: 1, Bytes: 60, Kids: []scan.Child{{Name: "a", Dir: true, Files: 1, Bytes: 60}}},
					{Path: "a", Files: 1, Bytes: 60, Kids: []scan.Child{{Name: "x", Bytes: 60, Lines: -1}}},
				},
				Files: 1, Bytes: 60, Frontier: []string{tt.frontier},
				UnreadableDirs: tt.unreadable, OtherFilesystems: tt.other,
			}
			view := plan.New().Plan(snap, plan.Measured{})
			if view.Totals.Stopped != scan.StopNone {
				t.Fatalf("stopped = %v, want a walk that finished", view.Totals.Stopped)
			}
			if open := view.Totals.UnreadableDirs + view.Totals.OtherFilesystems; open != 1 {
				t.Fatalf("open subtrees = %d, want the directory named", open)
			}
			if view.Totals.BytesExact || view.Totals.LinesExact {
				t.Fatalf("totals = %+v, want lower bounds beside an open subtree", view.Totals)
			}
			if len(view.Entries) != 1 || view.Entries[0].Exact {
				t.Fatalf("rows = %+v, want the row above the open subtree marked a lower bound", view.Entries)
			}
		})
	}
}

func TestFilesystemIsStatedWithTheLowerBounds(t *testing.T) {
	device := &scan.Filesystem{Total: 1000, Used: 400, Avail: 500}
	for _, tt := range []struct {
		name   string
		snap   scan.Snapshot
		stated bool
	}{
		{
			"a finished walk",
			scan.Snapshot{Files: 1, Bytes: 1, Filesystem: device},
			false,
		},
		{
			"a refused directory",
			scan.Snapshot{Files: 1, Bytes: 1, UnreadableDirs: 1, Frontier: []string{"locked"}, Filesystem: device},
			true,
		},
		{
			"a mounted filesystem",
			scan.Snapshot{Files: 1, Bytes: 1, OtherFilesystems: 1, Frontier: []string{"mnt"}, Filesystem: device},
			true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			totals := plan.New().Plan(tt.snap, plan.Measured{}).Totals
			if stated := totals.Filesystem != nil; stated != tt.stated {
				t.Fatalf("filesystem stated = %v, want %v", stated, tt.stated)
			}
		})
	}
}

func TestFoldedRowKeepsTheFrontier(t *testing.T) {
	root := scan.Dir{Path: "", Files: 10, Bytes: 155}
	dirs := []scan.Dir{root}
	for i := range 10 {
		name := fmt.Sprintf("d%02d", i)
		dir := scan.Dir{Path: name, Files: 1, Bytes: int64(10 + i)}
		root.Kids = append(root.Kids, scan.Child{Name: name, Dir: true, Files: 1, Bytes: dir.Bytes})
		dirs = append(dirs, dir)
	}
	dirs[0] = root
	for _, tt := range []struct {
		name    string
		frontie []string
		exact   bool
	}{
		{"a finished walk", nil, true},
		{"a refused entry in the folded remainder", []string{"d00"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snap := scan.Snapshot{Dirs: dirs, Files: 10, Bytes: 155, Frontier: tt.frontie}
			rows := plan.New().Plan(snap, plan.Measured{}).Entries
			folded := rows[len(rows)-1]
			if folded.Kind != plan.Folded || folded.Exact != tt.exact {
				t.Fatalf("folded row = %+v, want exact %v", folded, tt.exact)
			}
		})
	}
}

func TestReusedFactsAreCounted(t *testing.T) {
	dirs := []scan.Dir{{Path: "", Files: 1, Bytes: 10, Kids: []scan.Child{{Name: "f", Bytes: 10, Lines: -1}}}}
	snap := scan.Snapshot{Dirs: dirs, Files: 1, Bytes: 10}
	warm := plan.New().Plan(snap, plan.Measured{Lines: map[string]int64{"f": 2}, Total: 2, Measured: 1, Reused: 3})
	if warm.Totals.Reused != 3 {
		t.Fatalf("Reused = %d, want the count the measure reported", warm.Totals.Reused)
	}
	cold := plan.New().Plan(snap, plan.Measured{Lines: map[string]int64{"f": 2}, Total: 2, Measured: 1})
	if cold.Totals.Reused != 0 {
		t.Fatalf("Reused = %d for a cold count, want 0", cold.Totals.Reused)
	}
}

func TestUnknownLines(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 1, Bytes: 10, Kids: []scan.Child{{Name: "f", Bytes: 10, Lines: -1}}},
	}
	measured := plan.Measured{Unknown: 1, Total: 5}
	view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 1, Bytes: 10}, measured)
	if view.Totals.Lines != 5 || view.Totals.LinesExact {
		t.Fatalf("totals = %d exact=%v, want 5 and inexact", view.Totals.Lines, view.Totals.LinesExact)
	}
	if view.Entries[0].Lines != -1 {
		t.Fatalf("unknown file lines = %d, want -1", view.Entries[0].Lines)
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	dirs := []scan.Dir{
		{Path: "", Files: 5, Bytes: 300, Kids: []scan.Child{
			{Name: "f", Bytes: 30, Lines: -1},
			{Name: "a", Dir: true, Files: 3, Bytes: 200},
		}},
		{Path: "a", Files: 3, Bytes: 200},
		{Path: "a/b", Files: 2, Bytes: 90},
	}
	snap := scan.Snapshot{Dirs: dirs, Files: 5, Bytes: 300}
	measured := plan.Measured{Lines: map[string]int64{"f": 7}, Total: 7, Measured: 1}
	want := plan.New().Plan(snap, measured)
	for range 100 {
		got := plan.New().Plan(snap, measured)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("view is not deterministic:\n got %+v\nwant %+v", got, want)
		}
	}
}

func TestColorFollowsTheEntry(t *testing.T) {
	planOf := func(a, b int64) map[string]int {
		dirs := []scan.Dir{
			{Path: "", Files: 2, Bytes: a + b},
			{Path: "a", Files: 1, Bytes: a},
			{Path: "b", Files: 1, Bytes: b},
		}
		view := plan.New().Plan(scan.Snapshot{Dirs: dirs, Files: 2, Bytes: a + b}, plan.Measured{})
		colors := map[string]int{}
		for _, entry := range view.Entries {
			colors[entry.Name] = entry.Color
		}
		return colors
	}
	first := planOf(100, 50)
	second := planOf(100, 200)
	if first["a"] != second["a"] || first["b"] != second["b"] {
		t.Fatalf("colors changed when a row grew: %v then %v", first, second)
	}
}

func TestEmptyRoot(t *testing.T) {
	for _, snap := range []scan.Snapshot{{}, {Dirs: []scan.Dir{{Path: ""}}}} {
		view := plan.New().Plan(snap, plan.Measured{})
		if len(view.Entries) != 0 {
			t.Fatalf("empty root has %d rows", len(view.Entries))
		}
		if view.Totals.Files != 0 || view.Totals.Bytes != 0 {
			t.Fatalf("empty root totals = %+v", view.Totals)
		}
	}
}

func TestTypeShares(t *testing.T) {
	census := map[string]int{".go": 60, ".md": 20, ".json": 10, ".txt": 5, ".s": 3, ".h": 1, ".yml": 1}
	snap := scan.Snapshot{Files: 100, Types: census}
	totals := plan.New().Plan(snap, plan.Measured{}).Totals
	want := []plan.TypeShare{
		{Ext: ".go", Files: 60, Share: 60},
		{Ext: ".md", Files: 20, Share: 20},
		{Ext: ".json", Files: 10, Share: 10},
		{Ext: ".txt", Files: 5, Share: 5},
		{Ext: ".s", Files: 3, Share: 3},
		{Ext: ".h", Files: 1, Share: 1},
	}
	if !reflect.DeepEqual(totals.Types, want) {
		t.Fatalf("types = %v, want %v", totals.Types, want)
	}
	if totals.TypesMore != 1 {
		t.Fatalf("more = %d, want the one type that did not fit", totals.TypesMore)
	}
	if empty := plan.New().Plan(scan.Snapshot{}, plan.Measured{}).Totals; empty.Types != nil || empty.TypesMore != 0 {
		t.Fatalf("an empty census gave %v and %d more", empty.Types, empty.TypesMore)
	}
}
