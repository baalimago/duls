// Package agent encodes one planned view as the JSON document of agent mode:
// the totals, the type census, the rows the algorithm chose, and what the run
// could not prove. The document is the visual view's facts as data, not a
// second layout (architecture/agent.md).
package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
)

// Request is one agent-mode answer: the planned view, the resolved root it
// describes, and the deadline the run obeyed.
type Request struct {
	Root  string
	Limit time.Duration
	View  plan.View
}

// Document is the answer of agent mode. Every number is a fact the run proved
// or a count of the files it could not measure; a quantity that does not
// belong to a row's kind has no key at all, never a zero.
type Document struct {
	Root    string `json:"root"`
	Totals  Totals `json:"totals"`
	Types   []Type `json:"types"`
	Entries []any  `json:"entries"`
	// Filesystem is the usage of the device that holds the root, absent when
	// the run's totals are exact or the platform cannot report it.
	Filesystem *Filesystem `json:"filesystem,omitempty"`
	Accounting Accounting  `json:"accounting"`
}

// Filesystem is the device that holds the root: the bytes it allocates, which
// include the subtrees the run could not reach.
type Filesystem struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Avail int64 `json:"avail"`
}

// Totals are the view's headline numbers. Lines is a lower bound when
// LinesExact is false.
type Totals struct {
	Files      int   `json:"files"`
	Bytes      int64 `json:"bytes"`
	Lines      int64 `json:"lines"`
	BytesExact bool  `json:"bytesExact"`
	LinesExact bool  `json:"linesExact"`
	// TypesMore counts the file types the census left out.
	TypesMore int `json:"typesMore"`
}

// Type is one file type's share of the file count. Ext is the final extension
// with its dot, or "" for a name without one.
type Type struct {
	Ext   string `json:"ext"`
	Files int    `json:"files"`
	Share int    `json:"share"`
}

// DirEntry is a directory row: its subtree's files and bytes, its share of the
// view's bytes, and the children that have no row of their own.
type DirEntry struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Kind     string  `json:"kind"`
	Depth    int     `json:"depth"`
	Files    int     `json:"files"`
	Bytes    int64   `json:"bytes"`
	Share    int     `json:"share"`
	Exact    bool    `json:"exact"`
	Children []Child `json:"children"`
	More     int     `json:"more"`
}

// FileEntry is a file row: its own bytes and the line count the count proved,
// which is null when the run did not prove one.
type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Depth int    `json:"depth"`
	Bytes int64  `json:"bytes"`
	Exact bool   `json:"exact"`
	Lines *int64 `json:"lines"`
}

// FoldedEntry is the row of the top-level entries that got no row of their own.
type FoldedEntry struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Files int      `json:"files"`
	Bytes int64    `json:"bytes"`
	Share int      `json:"share"`
	Exact bool     `json:"exact"`
	Names []string `json:"names"`
}

// Child is one child of a directory row. Files is set for a directory and null
// for a file; Lines is set for a file and null when the run did not prove one.
type Child struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Files *int   `json:"files"`
	Lines *int64 `json:"lines"`
}

// Accounting states the completeness of the run: the phase that stopped, the
// deadline it obeyed, and the files the count never reached, skipped, could
// not prove, could not read, or took from the store.
type Accounting struct {
	// Stopped is "none" when both phases finished, else the phase that stopped.
	Stopped string `json:"stopped"`
	// Limit is the deadline the stopped phase obeyed, absent when none was set.
	Limit      string `json:"limit,omitempty"`
	Measured   int    `json:"measured"`
	NotScanned int    `json:"notScanned"`
	Skipped    int    `json:"skipped"`
	Unknown    int    `json:"unknown"`
	Unreadable int    `json:"unreadable"`
	// UnreadableDirs counts the directories whose listing the walk was
	// refused. Their subtrees hold no fact, so Files, Bytes and Lines are
	// lower bounds while this count is above zero.
	UnreadableDirs int `json:"unreadableDirs"`
	// OtherFilesystems counts the directories the walk left because another
	// filesystem is mounted there. Their subtrees hold no fact either, so the
	// same totals are lower bounds while this count is above zero.
	OtherFilesystems int `json:"otherFilesystems"`
	Reused           int `json:"reused"`
}

const (
	kindDir    = "dir"
	kindFile   = "file"
	kindFolded = "folded"
)

// Encode returns the agent document of req as JSON without a trailing newline.
func Encode(req Request) ([]byte, error) {
	v := req.View
	entries := make([]any, 0, len(v.Entries))
	for _, entry := range v.Entries {
		row, err := row(entry)
		if err != nil {
			return nil, err
		}
		entries = append(entries, row)
	}
	doc := Document{
		Root:       req.Root,
		Totals:     totals(v.Totals),
		Types:      types(v.Totals.Types),
		Entries:    entries,
		Filesystem: filesystem(v.Totals.Filesystem),
		Accounting: accounting(v.Totals, req.Limit),
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("agent: encode: %w", err)
	}
	return out, nil
}

func totals(t plan.Totals) Totals {
	return Totals{
		Files:      t.Files,
		Bytes:      t.Bytes,
		Lines:      t.Lines,
		BytesExact: t.BytesExact,
		LinesExact: t.LinesExact,
		TypesMore:  t.TypesMore,
	}
}

func filesystem(fs *scan.Filesystem) *Filesystem {
	if fs == nil {
		return nil
	}
	return &Filesystem{Total: fs.Total, Used: fs.Used, Avail: fs.Avail}
}

func types(shares []plan.TypeShare) []Type {
	out := make([]Type, 0, len(shares))
	for _, share := range shares {
		out = append(out, Type{Ext: share.Ext, Files: share.Files, Share: share.Share})
	}
	return out
}

func accounting(t plan.Totals, limit time.Duration) Accounting {
	a := Accounting{
		Stopped:          "none",
		Measured:         t.Measured,
		NotScanned:       max(0, t.Files-t.Measured-t.Skipped-t.Unknown),
		Skipped:          t.Skipped,
		Unknown:          t.Unknown,
		Unreadable:       t.Unreadable,
		UnreadableDirs:   t.UnreadableDirs,
		OtherFilesystems: t.OtherFilesystems,
		Reused:           t.Reused,
	}
	if t.Stopped != scan.StopNone {
		a.Stopped = "walk"
		if t.StoppedPhase == scan.PhaseCount {
			a.Stopped = "count"
		}
		if limit > 0 {
			a.Limit = limit.String()
		}
	}
	return a
}

func row(e plan.Entry) (any, error) {
	switch e.Kind {
	case plan.Directory:
		children := make([]Child, 0, len(e.Kids))
		for _, kid := range e.Kids {
			children = append(children, child(kid))
		}
		return DirEntry{
			Name: e.Name, Path: e.Path, Kind: kindDir, Depth: e.Depth,
			Files: e.Files, Bytes: e.Bytes, Share: e.Share, Exact: e.Exact,
			Children: children, More: e.More,
		}, nil
	case plan.File:
		return FileEntry{
			Name: e.Name, Path: e.Path, Kind: kindFile, Depth: e.Depth,
			Bytes: e.Bytes, Exact: e.Exact, Lines: lines(e.Lines),
		}, nil
	case plan.Folded:
		names := e.Folded
		if names == nil {
			names = []string{}
		}
		return FoldedEntry{
			Name: foldedName(len(e.Folded)), Kind: kindFolded, Files: e.Files, Bytes: e.Bytes,
			Share: e.Share, Exact: e.Exact, Names: names,
		}, nil
	default:
		return nil, fmt.Errorf("agent: encode: entry %q has no kind: %d", e.Name, e.Kind)
	}
}

func child(kid plan.Item) Child {
	c := Child{Name: kid.Name, Kind: kindFile}
	if kid.Dir {
		files := kid.Files
		c.Kind, c.Files = kindDir, &files
		return c
	}
	c.Lines = lines(kid.Lines)
	return c
}

// foldedName is the row's own name: the planner keeps the folded entries in
// Names and leaves the row unnamed, so the count names it.
func foldedName(n int) string { return "+" + strconv.Itoa(n) + " smaller" }

// lines is nil for the count's -1 sentinel: a line count the run did not prove
// has no value, and 0 would be a lie (I3).
func lines(n int64) *int64 {
	if n < 0 {
		return nil
	}
	return &n
}
