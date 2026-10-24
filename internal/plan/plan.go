// Package plan turns a walk's fact set and a count's proof into the rows of
// one view: the largest entries, the largest hidden subdirectories promoted
// into rows of their own, and the rest folded into one row.
package plan

import (
	"cmp"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/baalimago/duls/internal/scan"
)

// Kind classifies a view row.
type Kind uint8

const (
	Directory Kind = iota
	File
	Folded
)

// Item is one child of a directory that has no row of its own.
// Lines is -1 when the run has not measured it.
type Item struct {
	Name  string
	Dir   bool
	Bytes int64
	Files int
	Lines int64
}

// Entry is one row of the view.
//
//   - Kind decides which fields are meaningful. (Entry.1)
//   - Exact is false when Bytes is a lower bound the run could not complete.
//     (Entry.2)
type Entry struct {
	Name string
	// Path is the entry's slash-separated path below the root: "meetings"
	// for a top-level directory, "meetings/autumn" for a promoted row. It is
	// empty for a Folded row, whose Name is empty too and which keeps the
	// entries it stands for in Folded.
	Path   string
	Kind   Kind
	Depth  int
	Files  int
	Bytes  int64
	Exact  bool
	Lines  int64
	Share  int
	Kids   []Item
	More   int
	Folded []string
	Color  int
}

// Totals are the view's headline numbers.
type Totals struct {
	Files          int
	Bytes          int64
	BytesExact     bool
	Lines          int64
	LinesExact     bool
	Measured       int
	Skipped        int
	Unknown        int
	Unreadable     int
	UnreadableDirs int
	// OtherFilesystems counts the directories the walk left because another
	// filesystem is mounted there. Their subtrees hold no fact, so every total
	// above one is a lower bound.
	OtherFilesystems int
	// Filesystem is the usage of the device that holds the root, stated when
	// the totals are lower bounds: the device holds what the run could not
	// reach (architecture/plan.md).
	Filesystem *scan.Filesystem
	Stopped    scan.Stop
	// StoppedPhase names the phase that stopped: the walk when the snapshot
	// stopped, the count when only the count did. It is meaningful only when
	// Stopped is not StopNone.
	StoppedPhase scan.Phase
	Reused       int
	// Types holds at most 6 of the walk's file types, largest first, and
	// TypesMore counts the types that did not fit.
	Types     []TypeShare
	TypesMore int
}

// TypeShare is one file type's share of the view's file count.
type TypeShare struct {
	// Ext is the final extension with its dot, lowercased; "" is a name
	// without one.
	Ext   string
	Files int
	Share int
}

// View is what the renderer draws.
type View struct {
	Totals  Totals
	Entries []Entry
}

// Measured is what the count phase proved.
type Measured struct {
	Lines      map[string]int64
	Total      int64
	Measured   int
	Skipped    int
	Unknown    int
	Unreadable int
	Reused     int
	Stopped    scan.Stop
}

// Planner chooses the rows of one view.
type Planner interface {
	// Plan returns the view of one fact set.
	//
	//   - Entries has at most 8 rows; at most one is Kind Folded and it is last.
	//     (Plan.1)
	//   - Top-level groups are ordered by Bytes descending, ties by Name. A
	//     promoted row follows its parent's subtree; promoted siblings are ordered
	//     by Bytes descending, then Name. Share is the rounded percentage of
	//     Totals.Bytes, and is 0 when Totals.Bytes is 0. (Plan.2, I2)
	//   - A Kind Directory row reports subtree Files and Bytes; a Kind File row
	//     reports Files 0, its Bytes and its Lines; a Kind Folded row names the
	//     top-level entries that got no row and sums their Files and Bytes.
	//     (Plan.3)
	//   - Kids holds at most 6 Items, sorted by Bytes descending then Name, and
	//     More counts the children that did not fit. (Plan.4, I13, I21)
	//   - Promotion: a directory with 2 or more files and no row of its own
	//     becomes a row while a slot remains, inserted after its parent's
	//     subtree, largest first. (Plan.5, I13)
	//   - Depth is the number of path elements in the row's path; the root's
	//     rows are depth 0. (Plan.6)
	//   - Color is a stable index derived from the row's top-level ancestor path,
	//     independent of current row order. (Plan.7, D14)
	//   - Totals.Lines is m.Total; Totals.LinesExact is false when
	//     m.Unknown+Totals.Unreadable > 0, a directory was refused or holds
	//     another filesystem, or any phase stopped. Totals.Stopped and
	//     Totals.StoppedPhase name the phase that stopped, the walk first.
	//     Totals.BytesExact is false when the walk stopped, a refused directory
	//     left a subtree open, or another filesystem was left unentered, and
	//     Totals.UnreadableDirs and Totals.OtherFilesystems count those
	//     directories. Rows whose subtree contains a Frontier path are lower
	//     bounds, and a folded row is a lower bound when one of the entries it
	//     stands for is. Totals.Filesystem states the usage of the device that
	//     holds the root under the same condition, because the device holds the
	//     subtrees the run could not reach. (Plan.8, I1, I3, I4)
	//   - Totals.Types holds at most 6 file types ordered by Files descending,
	//     ties by Ext, with Share the rounded percentage of Totals.Files; an
	//     empty census gives no types. Totals.TypesMore counts the types that
	//     did not fit. (Plan.10, I13)
	//   - Same snapshot and same Measured give an equal View; the frame has no
	//     separate planning or ordering rule. (Plan.9, I15, I17)
	//   - The view depends only on the supplied facts and declared completeness.
	//     (I1)
	//   - Lines from an unknown file are unknown, never zero; policy-skipped files
	//     are outside the denominator. (I3, I23)
	Plan(s scan.Snapshot, m Measured) View
}

// New returns the planner.
func New() Planner {
	return planner{}
}

// The bounds of a view (I13).
const (
	Entries  = 8
	Children = 6
	Palette  = 8
	// Types bounds the file types the totals line reports.
	Types = 6
)

type planner struct{}

type tree struct {
	dirs map[string]scan.Dir
	kids map[string][]string
}

type row struct {
	path  string
	name  string
	kind  Kind
	dir   bool
	files int
	bytes int64
}

type group struct {
	path  string
	name  string
	dir   bool
	files int
	bytes int64
}

func (planner) Plan(s scan.Snapshot, m Measured) View {
	view := View{Totals: totals(s, m)}
	if len(s.Dirs) == 0 || s.Dirs[0].Path != "" {
		return view
	}
	t := newTree(s.Dirs)
	groups := t.groups(s.Dirs[0])
	slices.SortFunc(groups, func(a, b group) int {
		if a.bytes != b.bytes {
			return cmpInt64(b.bytes, a.bytes)
		}
		return strings.Compare(a.name, b.name)
	})
	var folded []group
	if len(groups) > Entries {
		groups, folded = groups[:Entries-1], groups[Entries-1:]
	}
	rows := make([]row, 0, Entries)
	listed := map[string]bool{}
	for _, g := range groups {
		rows = append(rows, newRow(g))
		if g.dir {
			listed[g.path] = true
		}
	}
	free := Entries - len(rows)
	if len(folded) > 0 {
		free--
	}
	for free > 0 {
		at, best, ok := t.promote(rows, listed)
		if !ok {
			break
		}
		end := at + 1
		for end < len(rows) && under(rows[end].path, rows[at].path) {
			end++
		}
		rows = slices.Insert(rows, end, newRow(group{path: best.Path, name: base(best.Path), dir: true, files: best.Files, bytes: best.Bytes}))
		listed[best.Path] = true
		free--
	}
	for _, r := range rows {
		view.Entries = append(view.Entries, t.entry(r, s, m))
	}
	if len(folded) > 0 {
		view.Entries = append(view.Entries, foldedEntry(folded, s))
	}
	return view
}

func newTree(dirs []scan.Dir) *tree {
	t := &tree{dirs: map[string]scan.Dir{}, kids: map[string][]string{}}
	for _, dir := range dirs {
		t.dirs[dir.Path] = dir
	}
	for _, dir := range dirs {
		if dir.Path == "" {
			continue
		}
		parent := parentPath(dir.Path)
		t.kids[parent] = append(t.kids[parent], dir.Path)
	}
	return t
}

func (t *tree) groups(root scan.Dir) []group {
	var out []group
	for _, path := range t.kids[""] {
		dir := t.dirs[path]
		out = append(out, group{path: path, name: base(path), dir: true, files: dir.Files, bytes: dir.Bytes})
	}
	for _, kid := range root.Kids {
		if kid.Dir {
			continue
		}
		out = append(out, group{path: kid.Name, name: kid.Name, bytes: kid.Bytes})
	}
	return out
}

func (t *tree) promote(rows []row, listed map[string]bool) (int, scan.Dir, bool) {
	at := -1
	best := scan.Dir{}
	for i, r := range rows {
		if !r.dir {
			continue
		}
		for _, path := range t.kids[r.path] {
			dir := t.dirs[path]
			if listed[path] || dir.Files < 2 {
				continue
			}
			if at < 0 || dir.Bytes > best.Bytes || (dir.Bytes == best.Bytes && dir.Path < best.Path) {
				at, best = i, dir
			}
		}
	}
	return at, best, at >= 0
}

func (t *tree) entry(r row, s scan.Snapshot, m Measured) Entry {
	exact := exactPath(s, r.path)
	e := Entry{
		Name:  r.name,
		Path:  r.path,
		Kind:  r.kind,
		Depth: strings.Count(r.path, "/"),
		Files: r.files,
		Bytes: r.bytes,
		Exact: exact,
		Lines: -1,
		Share: share(r.bytes, s.Bytes),
		Color: color(topLevel(r.path)),
	}
	if !r.dir {
		e.Files = 0
		if lines, ok := m.Lines[r.path]; ok {
			e.Lines = lines
		}
		return e
	}
	if !exact {
		return e
	}
	dir := t.dirs[r.path]
	e.Kids = make([]Item, 0, len(dir.Kids))
	for _, kid := range dir.Kids {
		item := Item{Name: kid.Name, Dir: kid.Dir, Bytes: kid.Bytes, Files: kid.Files, Lines: -1}
		if !kid.Dir {
			if lines, ok := m.Lines[join(r.path, kid.Name)]; ok {
				item.Lines = lines
			}
		}
		e.Kids = append(e.Kids, item)
	}
	e.More = max(0, t.childCount(dir)-len(dir.Kids))
	return e
}

func (t *tree) childCount(dir scan.Dir) int {
	direct := dir.Files
	for _, path := range t.kids[dir.Path] {
		direct -= t.dirs[path].Files
	}
	return direct + len(t.kids[dir.Path])
}

func newRow(g group) row {
	kind := File
	if g.dir {
		kind = Directory
	}
	path := g.path
	if path == "" {
		path = g.name
	}
	return row{
		path:  path,
		name:  g.name,
		kind:  kind,
		dir:   g.dir,
		files: g.files,
		bytes: g.bytes,
	}
}

func totals(s scan.Snapshot, m Measured) Totals {
	unreadable := s.Unreadable + m.Unreadable
	stopped, phase := s.Stopped, scan.PhaseWalk
	if stopped == scan.StopNone && m.Stopped != scan.StopNone {
		stopped, phase = m.Stopped, scan.PhaseCount
	}
	complete := s.Stopped == scan.StopNone && s.UnreadableDirs == 0 && s.OtherFilesystems == 0
	device := s.Filesystem
	if complete {
		device = nil
	}
	types, more := typeShares(s.Types, s.Files)
	return Totals{
		Files:          s.Files,
		Bytes:          s.Bytes,
		BytesExact:     complete,
		Lines:          m.Total,
		LinesExact:     complete && m.Unknown+unreadable == 0 && stopped == scan.StopNone,
		Measured:       m.Measured,
		Skipped:        m.Skipped,
		Unknown:        m.Unknown,
		Unreadable:     unreadable,
		UnreadableDirs: s.UnreadableDirs,
		// The walk left another filesystem unentered, so every total it holds
		// is a lower bound of the tree below the root.
		OtherFilesystems: s.OtherFilesystems,
		Filesystem:       device,
		Stopped:          stopped,
		StoppedPhase:     phase,
		Reused:           m.Reused,
		Types:            types,
		TypesMore:        more,
	}
}

func typeShares(census map[string]int, files int) ([]TypeShare, int) {
	if len(census) == 0 {
		return nil, 0
	}
	shares := make([]TypeShare, 0, len(census))
	for ext, count := range census {
		shares = append(shares, TypeShare{Ext: ext, Files: count, Share: share(int64(count), int64(files))})
	}
	slices.SortFunc(shares, func(a, b TypeShare) int {
		if a.Files != b.Files {
			return cmp.Compare(b.Files, a.Files)
		}
		return strings.Compare(a.Ext, b.Ext)
	})
	if len(shares) <= Types {
		return shares, 0
	}
	return shares[:Types], len(shares) - Types
}

func exactPath(s scan.Snapshot, path string) bool {
	for _, frontier := range s.Frontier {
		if frontier == "" || frontier == path || strings.HasPrefix(frontier, path+"/") {
			return false
		}
	}
	return true
}

func foldedEntry(groups []group, s scan.Snapshot) Entry {
	e := Entry{Kind: Folded, Exact: true, Lines: -1}
	for _, g := range groups {
		e.Folded = append(e.Folded, g.name)
		e.Files += g.files
		e.Bytes += g.bytes
		if !exactPath(s, g.path) {
			e.Exact = false
		}
	}
	e.Share = share(e.Bytes, s.Bytes)
	return e
}

func share(part, whole int64) int {
	if whole <= 0 {
		return 0
	}
	return int((200*part + whole) / (2 * whole))
}

func color(ancestor string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(ancestor))
	return int(h.Sum32() % Palette)
}

func topLevel(path string) string {
	if before, _, ok := strings.Cut(path, "/"); ok {
		return before
	}
	return path
}

func parentPath(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return ""
}

func base(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func under(path, parent string) bool {
	return parent == "" || path == parent || strings.HasPrefix(path, parent+"/")
}

func join(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
